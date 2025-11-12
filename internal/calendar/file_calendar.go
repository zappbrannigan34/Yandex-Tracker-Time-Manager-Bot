package calendar

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// FileCalendar implements Calendar interface using a local text file
type FileCalendar struct {
	filePath string
	logger   *zap.Logger
	data     map[string]*MonthInfo // key: "YYYY-MM"
}

// NewFileCalendar creates a new FileCalendar instance
func NewFileCalendar(filePath string, logger *zap.Logger) *FileCalendar {
	return &FileCalendar{
		filePath: filePath,
		logger:   logger,
		data:     make(map[string]*MonthInfo),
	}
}

// Load loads calendar data from file
func (fc *FileCalendar) Load() error {
	file, err := os.Open(fc.filePath)
	if err != nil {
		return fmt.Errorf("failed to open calendar file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var currentMonth *MonthInfo

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse line
		// Format: YYYY-MM-DD type working_hours [note]
		// Example: 2025-01-01 holiday 0 Новогодние каникулы
		parts := strings.SplitN(line, " ", 4)
		if len(parts) < 3 {
			fc.logger.Warn("Invalid line format", zap.String("line", line))
			continue
		}

		dateStr := parts[0]
		typeStr := parts[1]
		hoursStr := parts[2]
		note := ""
		if len(parts) == 4 {
			note = parts[3]
		}

		// Parse date
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			fc.logger.Warn("Failed to parse date", zap.String("date", dateStr), zap.Error(err))
			continue
		}

		// Parse working hours
		hours, err := strconv.Atoi(hoursStr)
		if err != nil {
			fc.logger.Warn("Failed to parse hours", zap.String("hours", hoursStr), zap.Error(err))
			continue
		}

		// Determine day type
		var dayType DayType
		isWorkday := false
		switch typeStr {
		case "workday":
			dayType = DayTypeWorkday
			isWorkday = true
		case "weekend":
			dayType = DayTypeWeekend
		case "holiday":
			dayType = DayTypeHoliday
		case "shortened":
			dayType = DayTypeShortened
			isWorkday = true
		default:
			fc.logger.Warn("Unknown day type", zap.String("type", typeStr))
			continue
		}

		// Create month key
		monthKey := fmt.Sprintf("%d-%02d", date.Year(), date.Month())

		// Get or create month info
		if currentMonth == nil || fc.getMonthKey(currentMonth) != monthKey {
			if currentMonth != nil {
				// Save previous month
				fc.data[fc.getMonthKey(currentMonth)] = currentMonth
			}

			currentMonth = &MonthInfo{
				Year:  date.Year(),
				Month: date.Month(),
				Days:  []DayInfo{},
			}
		}

		// Add day
		dayInfo := DayInfo{
			Date:         date,
			Type:         dayType,
			WorkingHours: hours,
			IsWorkday:    isWorkday,
			Note:         note,
		}
		currentMonth.Days = append(currentMonth.Days, dayInfo)

		// Update statistics
		if isWorkday {
			currentMonth.WorkDays++
			currentMonth.WorkingHours += hours
		} else if dayType == DayTypeWeekend {
			currentMonth.Weekends++
		} else if dayType == DayTypeHoliday {
			currentMonth.Holidays++
		}
	}

	// Save last month
	if currentMonth != nil {
		fc.data[fc.getMonthKey(currentMonth)] = currentMonth
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading calendar file: %w", err)
	}

	fc.logger.Info("Calendar file loaded",
		zap.String("file", fc.filePath),
		zap.Int("months", len(fc.data)))

	return nil
}

// IsWorkday checks if the given date is a working day
func (fc *FileCalendar) IsWorkday(date time.Time) (bool, int, error) {
	dayInfo, err := fc.GetDayInfo(date)
	if err != nil {
		return false, 0, err
	}

	return dayInfo.IsWorkday, dayInfo.WorkingHours, nil
}

// GetMonthInfo returns calendar info for the entire month
func (fc *FileCalendar) GetMonthInfo(year int, month time.Month) (*MonthInfo, error) {
	monthKey := fmt.Sprintf("%d-%02d", year, month)

	monthInfo, ok := fc.data[monthKey]
	if !ok {
		return nil, fmt.Errorf("month not found in calendar: %s", monthKey)
	}

	return monthInfo, nil
}

// GetDayInfo returns detailed info for a specific day
func (fc *FileCalendar) GetDayInfo(date time.Time) (*DayInfo, error) {
	monthInfo, err := fc.GetMonthInfo(date.Year(), date.Month())
	if err != nil {
		return nil, err
	}

	// Find the specific day
	for _, day := range monthInfo.Days {
		if day.Date.Year() == date.Year() &&
			day.Date.Month() == date.Month() &&
			day.Date.Day() == date.Day() {
			return &day, nil
		}
	}

	return nil, fmt.Errorf("day not found in calendar: %s", date.Format("2006-01-02"))
}

func (fc *FileCalendar) getMonthKey(month *MonthInfo) string {
	return fmt.Sprintf("%d-%02d", month.Year, month.Month)
}
