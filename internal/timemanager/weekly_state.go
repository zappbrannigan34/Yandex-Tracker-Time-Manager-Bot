package timemanager

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/username/time-tracker-bot/pkg/dateutil"
	"github.com/username/time-tracker-bot/pkg/random"
	"go.uber.org/zap"
)

// WeeklyState represents the weekly schedule state
type WeeklyState struct {
	Year         int                `json:"year"`
	Week         int                `json:"week"`
	StartDate    string             `json:"start_date"`
	EndDate      string             `json:"end_date"`
	SelectedDays map[string][]string `json:"selected_days"` // task -> [dates]
	CreatedAt    string             `json:"created_at"`
}

// WeeklyStateManager manages weekly task scheduling
type WeeklyStateManager struct {
	stateFile string
	state     *WeeklyState
	logger    *zap.Logger
}

// NewWeeklyStateManager creates a new weekly state manager
func NewWeeklyStateManager(stateFile string, logger *zap.Logger) *WeeklyStateManager {
	return &WeeklyStateManager{
		stateFile: stateFile,
		logger:    logger,
	}
}

// Load loads the weekly state from file
func (wsm *WeeklyStateManager) Load() error {
	data, err := os.ReadFile(wsm.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist yet - will be created on first save
			wsm.state = &WeeklyState{
				SelectedDays: make(map[string][]string),
			}
			return nil
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	var state WeeklyState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to parse state file: %w", err)
	}

	wsm.state = &state
	wsm.logger.Info("Weekly state loaded",
		zap.Int("year", state.Year),
		zap.Int("week", state.Week))

	return nil
}

// Save saves the weekly state to file
func (wsm *WeeklyStateManager) Save() error {
	data, err := json.MarshalIndent(wsm.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(wsm.stateFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	wsm.logger.Info("Weekly state saved",
		zap.Int("year", wsm.state.Year),
		zap.Int("week", wsm.state.Week))

	return nil
}

// IsNewWeek checks if the given date is in a new week
func (wsm *WeeklyStateManager) IsNewWeek(date time.Time) bool {
	year, week := dateutil.GetWeekNumber(date)

	if wsm.state == nil || wsm.state.Year == 0 {
		return true
	}

	return year != wsm.state.Year || week != wsm.state.Week
}

// SelectDaysForWeek selects random days for weekly tasks
func (wsm *WeeklyStateManager) SelectDaysForWeek(date time.Time, weeklyTasks map[string]int) error {
	year, week := dateutil.GetWeekNumber(date)

	// Get start and end of week
	monday := dateutil.StartOfWeek(date)
	sunday := dateutil.EndOfWeek(date)

	wsm.state = &WeeklyState{
		Year:         year,
		Week:         week,
		StartDate:    monday.Format("2006-01-02"),
		EndDate:      sunday.Format("2006-01-02"),
		SelectedDays: make(map[string][]string),
		CreatedAt:    time.Now().Format(time.RFC3339),
	}

	// Select random days for each task
	for taskKey, daysPerWeek := range weeklyTasks {
		dates := random.SelectRandomWeekdayDates(date, daysPerWeek)

		dateStrings := make([]string, len(dates))
		for i, d := range dates {
			dateStrings[i] = d.Format("2006-01-02")
		}

		wsm.state.SelectedDays[taskKey] = dateStrings

		wsm.logger.Info("Selected random days for weekly task",
			zap.String("task", taskKey),
			zap.Int("days_per_week", daysPerWeek),
			zap.Strings("selected_dates", dateStrings))
	}

	return wsm.Save()
}

// IsSelectedDay checks if the given date is selected for the task
func (wsm *WeeklyStateManager) IsSelectedDay(date time.Time, taskKey string) bool {
	if wsm.state == nil {
		return false
	}

	dateStr := date.Format("2006-01-02")
	selectedDates, ok := wsm.state.SelectedDays[taskKey]
	if !ok {
		return false
	}

	for _, d := range selectedDates {
		if d == dateStr {
			return true
		}
	}

	return false
}

// GetSelectedDays returns all selected days for the task
func (wsm *WeeklyStateManager) GetSelectedDays(taskKey string) []string {
	if wsm.state == nil {
		return []string{}
	}

	return wsm.state.SelectedDays[taskKey]
}

// GetCurrentState returns current state
func (wsm *WeeklyStateManager) GetCurrentState() *WeeklyState {
	return wsm.state
}
