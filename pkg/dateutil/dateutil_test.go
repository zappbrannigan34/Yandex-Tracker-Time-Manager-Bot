package dateutil

import (
	"testing"
	"time"
)

func TestStartOfDay(t *testing.T) {
	input := time.Date(2025, 1, 15, 14, 30, 45, 123456789, time.UTC)
	expected := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	result := StartOfDay(input)

	if !result.Equal(expected) {
		t.Errorf("StartOfDay(%v) = %v, want %v", input, result, expected)
	}
}

func TestEndOfDay(t *testing.T) {
	input := time.Date(2025, 1, 15, 14, 30, 45, 0, time.UTC)
	result := EndOfDay(input)

	if result.Year() != 2025 || result.Month() != 1 || result.Day() != 15 {
		t.Errorf("EndOfDay(%v) wrong date: %v", input, result)
	}

	if result.Hour() != 23 || result.Minute() != 59 || result.Second() != 59 {
		t.Errorf("EndOfDay(%v) wrong time: %v", input, result)
	}
}

func TestStartOfWeek(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "Wednesday returns Monday",
			input:    time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC), // Wednesday
			expected: time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC),  // Monday
		},
		{
			name:     "Monday returns same Monday",
			input:    time.Date(2025, 1, 13, 12, 0, 0, 0, time.UTC), // Monday
			expected: time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "Sunday returns previous Monday",
			input:    time.Date(2025, 1, 19, 12, 0, 0, 0, time.UTC), // Sunday
			expected: time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC),  // Previous Monday
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StartOfWeek(tt.input)

			if !result.Equal(tt.expected) {
				t.Errorf("StartOfWeek(%v) = %v, want %v",
					tt.input.Format("2006-01-02 Mon"),
					result.Format("2006-01-02 Mon"),
					tt.expected.Format("2006-01-02 Mon"))
			}
		})
	}
}

func TestGetWeekNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		wantYear int
		wantWeek int
	}{
		{
			name:     "Mid January 2025",
			input:    time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			wantYear: 2025,
			wantWeek: 3,
		},
		{
			name:     "Start of year",
			input:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			wantYear: 2025,
			wantWeek: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			year, week := GetWeekNumber(tt.input)

			if year != tt.wantYear || week != tt.wantWeek {
				t.Errorf("GetWeekNumber(%v) = (%v, %v), want (%v, %v)",
					tt.input, year, week, tt.wantYear, tt.wantWeek)
			}
		})
	}
}

func TestIsWeekday(t *testing.T) {
	tests := []struct {
		name  string
		input time.Time
		want  bool
	}{
		{"Monday is weekday", time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC), true},
		{"Tuesday is weekday", time.Date(2025, 1, 14, 0, 0, 0, 0, time.UTC), true},
		{"Wednesday is weekday", time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), true},
		{"Thursday is weekday", time.Date(2025, 1, 16, 0, 0, 0, 0, time.UTC), true},
		{"Friday is weekday", time.Date(2025, 1, 17, 0, 0, 0, 0, time.UTC), true},
		{"Saturday is not weekday", time.Date(2025, 1, 18, 0, 0, 0, 0, time.UTC), false},
		{"Sunday is not weekday", time.Date(2025, 1, 19, 0, 0, 0, 0, time.UTC), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWeekday(tt.input)

			if result != tt.want {
				t.Errorf("IsWeekday(%v) = %v, want %v",
					tt.input.Format("2006-01-02 Mon"), result, tt.want)
			}
		})
	}
}

func TestIsWeekend(t *testing.T) {
	tests := []struct {
		name  string
		input time.Time
		want  bool
	}{
		{"Saturday is weekend", time.Date(2025, 1, 18, 0, 0, 0, 0, time.UTC), true},
		{"Sunday is weekend", time.Date(2025, 1, 19, 0, 0, 0, 0, time.UTC), true},
		{"Monday is not weekend", time.Date(2025, 1, 13, 0, 0, 0, 0, time.UTC), false},
		{"Friday is not weekend", time.Date(2025, 1, 17, 0, 0, 0, 0, time.UTC), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsWeekend(tt.input)

			if result != tt.want {
				t.Errorf("IsWeekend(%v) = %v, want %v",
					tt.input.Format("2006-01-02 Mon"), result, tt.want)
			}
		})
	}
}

func TestIsSameDay(t *testing.T) {
	tests := []struct {
		name  string
		date1 time.Time
		date2 time.Time
		want  bool
	}{
		{
			"Same date different time",
			time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			time.Date(2025, 1, 15, 20, 0, 0, 0, time.UTC),
			true,
		},
		{
			"Different date",
			time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC),
			time.Date(2025, 1, 16, 10, 0, 0, 0, time.UTC),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSameDay(tt.date1, tt.date2)

			if result != tt.want {
				t.Errorf("IsSameDay(%v, %v) = %v, want %v",
					tt.date1, tt.date2, result, tt.want)
			}
		})
	}
}

func TestFormatISO8601(t *testing.T) {
	input := time.Date(2025, 1, 15, 10, 30, 45, 0, time.UTC)
	result := FormatISO8601(input)

	expected := "2025-01-15T10:30:45.000+0000"
	if result != expected {
		t.Errorf("FormatISO8601(%v) = %v, want %v", input, result, expected)
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Time
		wantErr bool
	}{
		{
			"ISO format YYYY-MM-DD",
			"2025-01-15",
			time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			false,
		},
		{
			"Russian format DD.MM.YYYY",
			"15.01.2025",
			time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
			false,
		},
		{
			"ISO with time",
			"2025-01-15T10:30:00",
			time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDate(tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDate(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}

			if !tt.wantErr && !result.Equal(tt.want) {
				t.Errorf("ParseDate(%v) = %v, want %v", tt.input, result, tt.want)
			}
		})
	}
}
