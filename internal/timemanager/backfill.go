package timemanager

import (
	"fmt"
	"sort"
	"time"

	"github.com/username/time-tracker-bot/internal/tracker"
	"go.uber.org/zap"
)

// StatusTimeline represents status changes over time for an issue
type StatusTimeline struct {
	IssueKey string
	Changes  []StatusChange
}

// StatusChange represents a single status change
type StatusChange struct {
	Timestamp time.Time
	Status    string // "open", "inProgress", "resolved", "closed"
}

// buildStatusTimeline builds a timeline of status changes from changelog
func buildStatusTimeline(issueKey string, changelog []tracker.ChangelogEntry) *StatusTimeline {
	timeline := &StatusTimeline{
		IssueKey: issueKey,
		Changes:  []StatusChange{},
	}

	for _, entry := range changelog {
		for _, field := range entry.Fields {
			if field.Field.ID == "status" {
				// Parse "to" status
				statusKey := "unknown"
				if toMap, ok := field.To.(map[string]interface{}); ok {
					if key, ok := toMap["key"].(string); ok {
						statusKey = key
					}
				}

				timeline.Changes = append(timeline.Changes, StatusChange{
					Timestamp: entry.UpdatedAt.Time,
					Status:    statusKey,
				})
			}
		}
	}

	// Sort by timestamp (oldest first)
	sort.Slice(timeline.Changes, func(i, j int) bool {
		return timeline.Changes[i].Timestamp.Before(timeline.Changes[j].Timestamp)
	})

	return timeline
}

// StatusOnDate returns the status of the issue on a specific date
func (t *StatusTimeline) StatusOnDate(date time.Time) string {
	if len(t.Changes) == 0 {
		return "unknown"
	}

	// Find the latest status change before or on the date
	var currentStatus string
	for _, change := range t.Changes {
		// Compare dates (ignoring time)
		changeDate := time.Date(change.Timestamp.Year(), change.Timestamp.Month(), change.Timestamp.Day(), 0, 0, 0, 0, time.Local)
		targetDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)

		if changeDate.After(targetDate) {
			// This change happened after the target date
			break
		}
		currentStatus = change.Status
	}

	if currentStatus == "" {
		// No changes before this date, return first status
		return t.Changes[0].Status
	}

	return currentStatus
}

// extractUniqueIssueKeys extracts unique issue keys from worklogs
func extractUniqueIssueKeys(worklogs []tracker.Worklog) []string {
	keysMap := make(map[string]bool)
	for _, wl := range worklogs {
		keysMap[wl.Issue.Key] = true
	}

	keys := make([]string, 0, len(keysMap))
	for key := range keysMap {
		keys = append(keys, key)
	}

	return keys
}

// wasOnBoard checks if the issue was ever on the specified board
func wasOnBoard(changelog []tracker.ChangelogEntry, boardID int) bool {
	for _, entry := range changelog {
		for _, field := range entry.Fields {
			if field.Field.ID == "boards" {
				// Check both "from" and "to" for board presence
				if checkBoardInValue(field.From, boardID) || checkBoardInValue(field.To, boardID) {
					return true
				}
			}
		}
	}
	return false
}

// checkBoardInValue checks if a board ID is present in the field value
func checkBoardInValue(value interface{}, boardID int) bool {
	// Value can be:
	// - array of objects: [{"id": 19}, {"id": 20}]
	// - single object: {"id": 20}
	// - nil

	if value == nil {
		return false
	}

	// Try as array
	if arr, ok := value.([]interface{}); ok {
		for _, item := range arr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if id, ok := itemMap["id"].(float64); ok && int(id) == boardID {
					return true
				}
			}
		}
	}

	// Try as single object
	if objMap, ok := value.(map[string]interface{}); ok {
		if id, ok := objMap["id"].(float64); ok && int(id) == boardID {
			return true
		}
	}

	return false
}

// mergeUnique merges multiple string slices and returns unique values
func mergeUnique(lists ...[]string) []string {
	uniqueMap := make(map[string]bool)

	for _, list := range lists {
		for _, item := range list {
			uniqueMap[item] = true
		}
	}

	result := make([]string, 0, len(uniqueMap))
	for key := range uniqueMap {
		result = append(result, key)
	}

	// Sort for consistent output
	sort.Strings(result)

	return result
}

// findMissingWorkdays finds working days in the period that have less than target hours logged
func (m *Manager) findMissingWorkdays(from, to time.Time) ([]time.Time, error) {
	var missingDays []time.Time

	// Iterate through each day in the period
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		// Check if it's a working day
		isWorkday, targetHours, err := m.calendar.IsWorkday(d)
		if err != nil {
			return nil, fmt.Errorf("failed to check if %s is workday: %w", d.Format("2006-01-02"), err)
		}

		if !isWorkday {
			continue
		}

		// Check if it's today (skip current day)
		today := time.Now().Truncate(24 * time.Hour)
		if d.Truncate(24 * time.Hour).Equal(today) {
			continue
		}

		// Get worked time for this day
		workedMinutes, err := m.trackerClient.GetWorkedMinutesToday(d)
		if err != nil {
			return nil, fmt.Errorf("failed to get worked time for %s: %w", d.Format("2006-01-02"), err)
		}

		targetMinutes := float64(targetHours * 60)

		// If worked less than target, it's a missing day
		if workedMinutes < targetMinutes {
			missingDays = append(missingDays, d)
		}
	}

	return missingDays, nil
}

// NormalizeWorkdaysRange ensures historic working days do not exceed target minutes
func (m *Manager) NormalizeWorkdaysRange(from, to time.Time, dryRun bool) (*NormalizationSummary, error) {
	start := time.Now()
	summary := &NormalizationSummary{}

	if to.Before(from) {
		summary.Duration = time.Since(start)
		return summary, nil
	}

	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		isWorkday, targetHours, err := m.calendar.IsWorkday(d)
		if err != nil {
			return nil, fmt.Errorf("failed to check if %s is workday: %w", d.Format("2006-01-02"), err)
		}
		if !isWorkday || targetHours == 0 {
			continue
		}

		summary.ProcessedDays++

		targetMinutes := float64(targetHours * 60)
		workedMinutes, err := m.trackerClient.GetWorkedMinutesToday(d)
		if err != nil {
			return nil, fmt.Errorf("failed to get worked time for %s: %w", d.Format("2006-01-02"), err)
		}

		diff := workedMinutes - targetMinutes
		if diff > cleanupEpsilonMinutes {
			summary.NormalizedDays++
			summary.TotalMinutesTrimmed += diff

			m.logger.Info("Historic overage detected",
				zap.Time("date", d),
				zap.Float64("worked_minutes", workedMinutes),
				zap.Float64("target_minutes", targetMinutes),
				zap.Bool("dry_run", dryRun))

			if dryRun {
				continue
			}

			if err := m.cleanupAndNormalize(d); err != nil {
				return nil, fmt.Errorf("failed to cleanup %s: %w", d.Format("2006-01-02"), err)
			}
		}
	}

	summary.Duration = time.Since(start)
	return summary, nil
}

// buildStatusTimelines загружает историю статусов для всех релевантных задач.
func (m *Manager) buildStatusTimelines(from, to time.Time) (map[string]*StatusTimeline, error) {
	issueKeys, err := m.collectAllRelevantIssues(from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to collect relevant issues: %w", err)
	}

	timelines := make(map[string]*StatusTimeline, len(issueKeys))

	for _, issueKey := range issueKeys {
		changelog, err := m.trackerClient.GetChangelog(issueKey)
		if err != nil {
			m.logger.Warn(fmt.Sprintf("failed to load changelog for %s: %v", issueKey, err))
			continue
		}
		timelines[issueKey] = buildStatusTimeline(issueKey, changelog)
	}

	return timelines, nil
}

// issuesInProgressOnDate возвращает список задач, которые были в работе в указанную дату.
func issuesInProgressOnDate(date time.Time, timelines map[string]*StatusTimeline) []string {
	if len(timelines) == 0 {
		return nil
	}

	var result []string

	for issueKey, timeline := range timelines {
		if timeline == nil {
			continue
		}

		status := timeline.StatusOnDate(date)
		if status == "inProgress" || status == "open" || status == "" {
			result = append(result, issueKey)
		}
	}

	sort.Strings(result)

	return result
}
