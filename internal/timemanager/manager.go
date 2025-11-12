package timemanager

import (
	"fmt"
	"time"

	"github.com/username/time-tracker-bot/internal/calendar"
	"github.com/username/time-tracker-bot/internal/config"
	"github.com/username/time-tracker-bot/internal/tracker"
	"github.com/username/time-tracker-bot/pkg/random"
	"go.uber.org/zap"
)

// Manager manages time distribution logic
type Manager struct {
	config        *config.Config
	trackerClient *tracker.Client
	calendar      calendar.Calendar
	weeklyState   *WeeklyStateManager
	logger        *zap.Logger
}

// GetTrackerClient returns the tracker client (for cleanup command)
func (m *Manager) GetTrackerClient() *tracker.Client {
	return m.trackerClient
}

// GetCalendar returns the calendar (for cleanup command)
func (m *Manager) GetCalendar() calendar.Calendar {
	return m.calendar
}

// NewManager creates a new time manager
func NewManager(
	cfg *config.Config,
	trackerClient *tracker.Client,
	cal calendar.Calendar,
	weeklyState *WeeklyStateManager,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		config:       cfg,
		trackerClient: trackerClient,
		calendar:     cal,
		weeklyState:  weeklyState,
		logger:       logger,
	}
}

// DistributeTimeForDate distributes time for the given date
func (m *Manager) DistributeTimeForDate(date time.Time, dryRun bool) ([]tracker.TimeEntry, error) {
	m.logger.Info("Starting time distribution",
		zap.Time("date", date),
		zap.Bool("dry_run", dryRun))

	// 1. Check if it's a working day
	isWorkday, targetHours, err := m.calendar.IsWorkday(date)
	if err != nil {
		return nil, fmt.Errorf("failed to check if workday: %w", err)
	}

	if !isWorkday {
		m.logger.Info("Not a workday, skipping",
			zap.Time("date", date))
		return nil, nil
	}

	targetMinutes := float64(targetHours * 60)
	m.logger.Info("Target working time",
		zap.Int("hours", targetHours),
		zap.Float64("minutes", targetMinutes))

	// 2. Get already worked time
	workedMinutes, err := m.trackerClient.GetWorkedMinutesToday(date)
	if err != nil {
		return nil, fmt.Errorf("failed to get worked time: %w", err)
	}

	remainingMinutes := targetMinutes - workedMinutes
	m.logger.Info("Time calculation",
		zap.Float64("worked_minutes", workedMinutes),
		zap.Float64("remaining_minutes", remainingMinutes))

	if remainingMinutes <= 0 {
		m.logger.Info("Already worked enough today",
			zap.Float64("worked_minutes", workedMinutes),
			zap.Float64("target_minutes", targetMinutes))
		return nil, nil
	}

	entries := []tracker.TimeEntry{}

	// 3. Daily tasks
	dailyMinutes := 0.0
	for _, task := range m.config.TimeRules.DailyTasks {
		minutes := random.Randomize(float64(task.Minutes), m.config.TimeRules.RandomizationPercent)
		entries = append(entries, tracker.TimeEntry{
			IssueKey: task.Issue,
			Minutes:  minutes,
			Comment:  task.Description,
		})
		dailyMinutes += minutes
	}

	remainingMinutes -= dailyMinutes
	m.logger.Info("Daily tasks distributed",
		zap.Float64("total_minutes", dailyMinutes),
		zap.Int("count", len(m.config.TimeRules.DailyTasks)),
		zap.Float64("remaining_minutes", remainingMinutes))

	// 4. Weekly tasks
	weeklyEntries, weeklyMinutes, err := m.distributeWeeklyTasks(date)
	if err != nil {
		return nil, fmt.Errorf("failed to distribute weekly tasks: %w", err)
	}

	entries = append(entries, weeklyEntries...)
	remainingMinutes -= weeklyMinutes

	m.logger.Info("Weekly tasks distributed",
		zap.Float64("total_minutes", weeklyMinutes),
		zap.Int("count", len(weeklyEntries)),
		zap.Float64("remaining_minutes", remainingMinutes))

	// 5. Get open issues from board
	if remainingMinutes > 0 {
		issues, err := m.trackerClient.SearchIssues(m.config.Tracker.IssuesQuery)
		if err != nil {
			return nil, fmt.Errorf("failed to search issues: %w", err)
		}

		// Log all found issues before filtering
		issueKeys := []string{}
		issueTypes := make(map[string]string)   // key -> type
		issueStatuses := make(map[string]string) // key -> status
		for _, issue := range issues {
			issueKeys = append(issueKeys, issue.Key)
			if issue.Type != nil {
				issueTypes[issue.Key] = issue.Type.Key
			} else {
				issueTypes[issue.Key] = "unknown"
			}
			issueStatuses[issue.Key] = issue.Status.Key
		}
		m.logger.Info("Issues from API (before excluding fixed tasks)",
			zap.Strings("issues", issueKeys),
			zap.Any("types", issueTypes),
			zap.Any("statuses", issueStatuses))

		// Exclude fixed tasks (daily + weekly)
		issues = m.excludeFixedTasks(issues)

		// Log after filtering
		filteredKeys := []string{}
		for _, issue := range issues {
			filteredKeys = append(filteredKeys, issue.Key)
		}
		m.logger.Info("Open issues found (after excluding fixed tasks)",
			zap.Strings("issues", filteredKeys),
			zap.Int("count", len(issues)))

		// 6. Distribute remaining time
		if len(issues) > 0 {
			minutesPerIssue := remainingMinutes / float64(len(issues))

			for _, issue := range issues {
				minutes := random.Randomize(minutesPerIssue, m.config.TimeRules.RandomizationPercent)
				entries = append(entries, tracker.TimeEntry{
					IssueKey: issue.Key,
					Minutes:  minutes,
					Comment:  "Development work",
				})
			}

			m.logger.Info("Remaining time distributed to open issues",
				zap.Float64("minutes_per_issue", minutesPerIssue),
				zap.Int("issue_count", len(issues)))
		}
	}

	// 7. Normalize to exact target (CRITICAL: ensure total = targetMinutes)
	totalMinutes := 0.0
	for _, entry := range entries {
		totalMinutes += entry.Minutes
	}

	if totalMinutes > 0 && totalMinutes != targetMinutes {
		// Normalize all entries proportionally to hit exact target
		normalizationFactor := targetMinutes / totalMinutes
		m.logger.Info("Normalizing time entries to exact target",
			zap.Float64("total_before", totalMinutes),
			zap.Float64("target", targetMinutes),
			zap.Float64("factor", normalizationFactor))

		for i := range entries {
			entries[i].Minutes = entries[i].Minutes * normalizationFactor
		}

		// Verify total (for logging)
		verifyTotal := 0.0
		for _, entry := range entries {
			verifyTotal += entry.Minutes
		}
		m.logger.Info("Normalization completed",
			zap.Float64("total_after", verifyTotal),
			zap.Float64("target", targetMinutes))
	}

	// 8. Create worklogs (if not dry run)
	if !dryRun {
		if err := m.createWorklogs(date, entries); err != nil {
			return nil, fmt.Errorf("failed to create worklogs: %w", err)
		}
	}

	m.logger.Info("Time distribution completed",
		zap.Int("total_entries", len(entries)),
		zap.Bool("dry_run", dryRun))

	return entries, nil
}

// distributeWeeklyTasks distributes weekly tasks for the given date
func (m *Manager) distributeWeeklyTasks(date time.Time) ([]tracker.TimeEntry, float64, error) {
	// Check if we need to select new days for the week
	if m.weeklyState.IsNewWeek(date) {
		m.logger.Info("New week detected, selecting random days")

		// Build map of task -> days per week
		weeklyTasks := make(map[string]int)
		for _, task := range m.config.TimeRules.WeeklyTasks {
			weeklyTasks[task.Issue] = task.DaysPerWeek
		}

		if err := m.weeklyState.SelectDaysForWeek(date, weeklyTasks); err != nil {
			return nil, 0, fmt.Errorf("failed to select days for week: %w", err)
		}
	}

	entries := []tracker.TimeEntry{}
	totalMinutes := 0.0

	// Check if today is a selected day for each task
	for _, task := range m.config.TimeRules.WeeklyTasks {
		if m.weeklyState.IsSelectedDay(date, task.Issue) {
			// Calculate hours per day
			hoursPerDay := task.HoursPerWeek / float64(task.DaysPerWeek)
			minutesPerDay := hoursPerDay * 60

			minutes := random.Randomize(minutesPerDay, m.config.TimeRules.RandomizationPercent)

			entries = append(entries, tracker.TimeEntry{
				IssueKey: task.Issue,
				Minutes:  minutes,
				Comment:  task.Description,
			})

			totalMinutes += minutes

			m.logger.Info("Weekly task scheduled for today",
				zap.String("task", task.Issue),
				zap.Float64("minutes", minutes))
		}
	}

	return entries, totalMinutes, nil
}

// excludeFixedTasks excludes daily and weekly tasks from the issue list
func (m *Manager) excludeFixedTasks(issues []tracker.Issue) []tracker.Issue {
	fixedTasks := make(map[string]bool)

	// Add daily tasks
	for _, task := range m.config.TimeRules.DailyTasks {
		fixedTasks[task.Issue] = true
	}

	// Add weekly tasks
	for _, task := range m.config.TimeRules.WeeklyTasks {
		fixedTasks[task.Issue] = true
	}

	// Filter out fixed tasks
	filtered := []tracker.Issue{}
	for _, issue := range issues {
		if !fixedTasks[issue.Key] {
			filtered = append(filtered, issue)
		}
	}

	return filtered
}

// createWorklogs creates worklog entries in Tracker
func (m *Manager) createWorklogs(date time.Time, entries []tracker.TimeEntry) error {
	startTime := time.Date(date.Year(), date.Month(), date.Day(), 10, 0, 0, 0, date.Location())

	for i, entry := range entries {
		// Calculate start time (stagger entries)
		entryStart := startTime.Add(time.Duration(i*5) * time.Minute)

		// Format duration
		durationISO := tracker.FormatDuration(entry.Minutes)

		// Create worklog
		_, err := m.trackerClient.CreateWorklog(entry.IssueKey, entryStart, durationISO, entry.Comment)
		if err != nil {
			m.logger.Error("Failed to create worklog",
				zap.String("issue", entry.IssueKey),
				zap.Error(err))
			return fmt.Errorf("failed to create worklog for %s: %w", entry.IssueKey, err)
		}

		m.logger.Info("Worklog created",
			zap.String("issue", entry.IssueKey),
			zap.Float64("minutes", entry.Minutes),
			zap.String("duration", durationISO))
	}

	return nil
}

// GetStatus returns current status for the date
func (m *Manager) GetStatus(date time.Time) (float64, float64, error) {
	// Check if workday
	isWorkday, targetHours, err := m.calendar.IsWorkday(date)
	if err != nil {
		return 0, 0, err
	}

	if !isWorkday {
		return 0, 0, nil
	}

	// Get worked time
	workedMinutes, err := m.trackerClient.GetWorkedMinutesToday(date)
	if err != nil {
		return 0, 0, err
	}

	targetMinutes := float64(targetHours * 60)

	return workedMinutes, targetMinutes, nil
}

// BackfillResult represents the result of a backfill operation
type BackfillResult struct {
	ProcessedDays int
	TotalEntries  int
	TotalMinutes  float64
	DayResults    []DayBackfillResult
}

// DayBackfillResult represents the result for a single day
type DayBackfillResult struct {
	Date         time.Time
	Success      bool
	EntriesCount int
	TotalMinutes float64
	Entries      []tracker.TimeEntry
}

// BackfillPeriod fills missing time entries for a period using 120% coverage algorithm
func (m *Manager) BackfillPeriod(from, to time.Time, dryRun bool) (*BackfillResult, error) {
	m.logger.Info("Starting backfill with 120% coverage algorithm",
		zap.Time("from", from),
		zap.Time("to", to),
		zap.Bool("dry_run", dryRun))

	// Step 1: Find missing workdays
	missingDays, err := m.findMissingWorkdays(from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to find missing workdays: %w", err)
	}

	if len(missingDays) == 0 {
		m.logger.Info("No missing workdays found")
		return &BackfillResult{
			ProcessedDays: 0,
			TotalEntries:  0,
			TotalMinutes:  0,
			DayResults:    []DayBackfillResult{},
		}, nil
	}

	m.logger.Info("Found missing workdays",
		zap.Int("count", len(missingDays)))

	// Step 2: Collect all tasks from 3 sources (120% coverage)
	allIssueKeys, err := m.collectAllRelevantIssues(from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to collect relevant issues: %w", err)
	}

	m.logger.Info("Collected unique issue keys from all sources",
		zap.Int("count", len(allIssueKeys)),
		zap.Strings("keys", allIssueKeys))

	// Step 3: Build timeline for each issue
	timelines := make(map[string]*StatusTimeline)
	boardID := m.config.Tracker.BoardID

	for _, issueKey := range allIssueKeys {
		// Get changelog
		changelog, err := m.trackerClient.GetChangelog(issueKey)
		if err != nil {
			m.logger.Warn("Failed to get changelog, skipping issue",
				zap.String("issue", issueKey),
				zap.Error(err))
			continue
		}

		// Build timeline
		timeline := buildStatusTimeline(issueKey, changelog)

		// Check if issue was on board
		if !wasOnBoard(changelog, boardID) {
			m.logger.Debug("Issue was never on board, skipping",
				zap.String("issue", issueKey),
				zap.Int("board_id", boardID))
			continue
		}

		timelines[issueKey] = timeline
		m.logger.Debug("Timeline built for issue",
			zap.String("issue", issueKey),
			zap.Int("changes", len(timeline.Changes)))
	}

	m.logger.Info("Timelines built",
		zap.Int("issue_count", len(timelines)))

	// Step 4: Process each missing day
	result := &BackfillResult{
		ProcessedDays: 0,
		TotalEntries:  0,
		TotalMinutes:  0,
		DayResults:    []DayBackfillResult{},
	}

	for _, day := range missingDays {
		dayResult, err := m.backfillDay(day, timelines, dryRun)
		if err != nil {
			m.logger.Error("Failed to backfill day",
				zap.Time("date", day),
				zap.Error(err))
			// Continue with other days
			dayResult = &DayBackfillResult{
				Date:    day,
				Success: false,
			}
		}

		result.DayResults = append(result.DayResults, *dayResult)
		result.ProcessedDays++
		result.TotalEntries += dayResult.EntriesCount
		result.TotalMinutes += dayResult.TotalMinutes
	}

	m.logger.Info("Backfill completed",
		zap.Int("processed_days", result.ProcessedDays),
		zap.Int("total_entries", result.TotalEntries),
		zap.Float64("total_minutes", result.TotalMinutes))

	return result, nil
}

// collectAllRelevantIssues collects issues from 3 sources (120% coverage)
func (m *Manager) collectAllRelevantIssues(from, to time.Time) ([]string, error) {
	// Source 1: Worklogs (already logged time)
	worklogs, err := m.trackerClient.GetWorklogsForRange(from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to get worklogs: %w", err)
	}
	worklogKeys := extractUniqueIssueKeys(worklogs)
	m.logger.Info("Source 1: Worklogs",
		zap.Int("count", len(worklogKeys)),
		zap.Strings("keys", worklogKeys))

	// Source 2: Current board (tasks on board now)
	boardQuery := fmt.Sprintf("Boards: %d AND Assignee: me()", m.config.Tracker.BoardID)
	boardIssues, err := m.trackerClient.SearchIssues(boardQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to search board issues: %w", err)
	}
	boardKeys := []string{}
	for _, issue := range boardIssues {
		boardKeys = append(boardKeys, issue.Key)
	}
	m.logger.Info("Source 2: Current board",
		zap.Int("count", len(boardKeys)),
		zap.Strings("keys", boardKeys))

	// Source 3: Updated since start of period
	updatedQuery := fmt.Sprintf("Assignee: me() AND Updated: >= %s", from.Format("2006-01-02"))
	updatedIssues, err := m.trackerClient.SearchIssues(updatedQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to search updated issues: %w", err)
	}
	updatedKeys := []string{}
	for _, issue := range updatedIssues {
		updatedKeys = append(updatedKeys, issue.Key)
	}
	m.logger.Info("Source 3: Updated filter",
		zap.Int("count", len(updatedKeys)),
		zap.Strings("keys", updatedKeys))

	// Merge all sources
	allKeys := mergeUnique(worklogKeys, boardKeys, updatedKeys)
	m.logger.Info("Merged all sources (120% coverage)",
		zap.Int("total_unique", len(allKeys)))

	return allKeys, nil
}

// backfillDay performs backfill for a single day
func (m *Manager) backfillDay(date time.Time, timelines map[string]*StatusTimeline, dryRun bool) (*DayBackfillResult, error) {
	m.logger.Info("Backfilling day",
		zap.Time("date", date))

	// IDEMPOTENCY CHECK: Verify day still needs backfill
	workedMinutes, err := m.trackerClient.GetWorkedMinutesToday(date)
	if err != nil {
		return nil, fmt.Errorf("failed to check worked time: %w", err)
	}

	_, targetHours, err := m.calendar.IsWorkday(date)
	if err != nil {
		return nil, fmt.Errorf("failed to check workday: %w", err)
	}

	targetMinutes := float64(targetHours * 60)

	if workedMinutes >= targetMinutes {
		m.logger.Info("Day already has sufficient time logged, skipping",
			zap.Time("date", date),
			zap.Float64("worked", workedMinutes),
			zap.Float64("target", targetMinutes))
		return &DayBackfillResult{
			Date:    date,
			Success: true, // Not an error - day is already complete
		}, nil
	}

	// Find tasks that were "inProgress" on this day
	inProgressIssues := []string{}
	for issueKey, timeline := range timelines {
		status := timeline.StatusOnDate(date)
		if status == "inProgress" {
			inProgressIssues = append(inProgressIssues, issueKey)
		}
	}

	m.logger.Info("Tasks in progress on date",
		zap.Time("date", date),
		zap.Int("count", len(inProgressIssues)),
		zap.Strings("issues", inProgressIssues))

	if len(inProgressIssues) == 0 {
		m.logger.Warn("No tasks in progress on date, skipping",
			zap.Time("date", date))
		return &DayBackfillResult{
			Date:    date,
			Success: false,
		}, nil
	}

	// targetMinutes already calculated above during idempotency check
	entries := []tracker.TimeEntry{}

	// Distribute time: daily tasks + weekly tasks + inProgress tasks
	// 1. Daily tasks
	dailyMinutes := 0.0
	for _, task := range m.config.TimeRules.DailyTasks {
		minutes := random.Randomize(float64(task.Minutes), m.config.TimeRules.RandomizationPercent)
		entries = append(entries, tracker.TimeEntry{
			IssueKey: task.Issue,
			Minutes:  minutes,
			Comment:  task.Description,
		})
		dailyMinutes += minutes
	}

	remainingMinutes := targetMinutes - dailyMinutes

	// 2. Weekly tasks (check if selected for this day)
	weeklyEntries, weeklyMinutes, err := m.distributeWeeklyTasks(date)
	if err != nil {
		return nil, fmt.Errorf("failed to distribute weekly tasks: %w", err)
	}
	entries = append(entries, weeklyEntries...)
	remainingMinutes -= weeklyMinutes

	// 3. Distribute remaining to inProgress tasks
	if remainingMinutes > 0 && len(inProgressIssues) > 0 {
		// Exclude fixed tasks from inProgress list
		fixedTasks := make(map[string]bool)
		for _, task := range m.config.TimeRules.DailyTasks {
			fixedTasks[task.Issue] = true
		}
		for _, task := range m.config.TimeRules.WeeklyTasks {
			fixedTasks[task.Issue] = true
		}

		filteredInProgress := []string{}
		for _, key := range inProgressIssues {
			if !fixedTasks[key] {
				filteredInProgress = append(filteredInProgress, key)
			}
		}

		if len(filteredInProgress) > 0 {
			minutesPerIssue := remainingMinutes / float64(len(filteredInProgress))

			for _, issueKey := range filteredInProgress {
				minutes := random.Randomize(minutesPerIssue, m.config.TimeRules.RandomizationPercent)
				entries = append(entries, tracker.TimeEntry{
					IssueKey: issueKey,
					Minutes:  minutes,
					Comment:  "Development work",
				})
			}
		}
	}

	// Normalize to exact target
	totalMinutes := 0.0
	for _, entry := range entries {
		totalMinutes += entry.Minutes
	}

	if totalMinutes > 0 && totalMinutes != targetMinutes {
		normalizationFactor := targetMinutes / totalMinutes
		for i := range entries {
			entries[i].Minutes = entries[i].Minutes * normalizationFactor
		}
		totalMinutes = targetMinutes
	}

	// Create worklogs (if not dry run)
	if !dryRun {
		if err := m.createWorklogs(date, entries); err != nil {
			return nil, fmt.Errorf("failed to create worklogs: %w", err)
		}
	}

	return &DayBackfillResult{
		Date:         date,
		Success:      true,
		EntriesCount: len(entries),
		TotalMinutes: totalMinutes,
		Entries:      entries,
	}, nil
}
