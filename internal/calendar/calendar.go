package calendar

import "time"

// DayType represents the type of day
type DayType int

const (
	DayTypeWorkday DayType = iota + 1
	DayTypeWeekend
	DayTypeHoliday
	DayTypeShortened
)

// DayInfo represents information about a specific day
type DayInfo struct {
	Date         time.Time
	Type         DayType
	WorkingHours int
	IsWorkday    bool
	Note         string
}

// MonthInfo represents calendar information for a month
type MonthInfo struct {
	Year         int
	Month        time.Month
	WorkingHours int // Total working hours in the month
	WorkDays     int
	Weekends     int
	Holidays     int
	Days         []DayInfo
}

// Calendar interface for checking working days
type Calendar interface {
	// IsWorkday checks if the given date is a working day
	IsWorkday(date time.Time) (bool, int, error)

	// GetMonthInfo returns calendar info for the entire month
	GetMonthInfo(year int, month time.Month) (*MonthInfo, error)

	// GetDayInfo returns detailed info for a specific day
	GetDayInfo(date time.Time) (*DayInfo, error)
}
