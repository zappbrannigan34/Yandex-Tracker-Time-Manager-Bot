package dateutil

import "time"

// StartOfDay returns the start of the day (00:00:00) for the given date
func StartOfDay(date time.Time) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
}

// EndOfDay returns the end of the day (23:59:59.999) for the given date
func EndOfDay(date time.Time) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 999999999, date.Location())
}

// StartOfWeek returns the Monday of the week for the given date
func StartOfWeek(date time.Time) time.Time {
	weekday := int(date.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7
	}
	daysFromMonday := weekday - 1
	return StartOfDay(date.AddDate(0, 0, -daysFromMonday))
}

// EndOfWeek returns the Sunday of the week for the given date
func EndOfWeek(date time.Time) time.Time {
	monday := StartOfWeek(date)
	sunday := monday.AddDate(0, 0, 6)
	return EndOfDay(sunday)
}

// GetWeekNumber returns the ISO week number for the given date
func GetWeekNumber(date time.Time) (year int, week int) {
	year, week = date.ISOWeek()
	return
}

// IsWeekday returns true if the date is Monday-Friday
func IsWeekday(date time.Time) bool {
	weekday := date.Weekday()
	return weekday >= time.Monday && weekday <= time.Friday
}

// IsWeekend returns true if the date is Saturday or Sunday
func IsWeekend(date time.Time) bool {
	weekday := date.Weekday()
	return weekday == time.Saturday || weekday == time.Sunday
}

// IsSameDay returns true if two dates are on the same day
func IsSameDay(date1, date2 time.Time) bool {
	return date1.Year() == date2.Year() &&
		date1.Month() == date2.Month() &&
		date1.Day() == date2.Day()
}

// IsSameWeek returns true if two dates are in the same week
func IsSameWeek(date1, date2 time.Time) bool {
	year1, week1 := GetWeekNumber(date1)
	year2, week2 := GetWeekNumber(date2)
	return year1 == year2 && week1 == week2
}

// FormatISO8601 formats date to ISO 8601 format with timezone
// Example: 2025-01-15T10:00:00.000+0000
func FormatISO8601(date time.Time) string {
	return date.Format("2006-01-02T15:04:05.000-0700")
}

// ParseDate parses date string in various formats
func ParseDate(dateStr string) (time.Time, error) {
	formats := []string{
		"2006-01-02",
		"02.01.2006",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-0700",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, nil
}

// Today returns today's date (start of day)
func Today() time.Time {
	return StartOfDay(time.Now())
}

// Yesterday returns yesterday's date (start of day)
func Yesterday() time.Time {
	return StartOfDay(time.Now().AddDate(0, 0, -1))
}
