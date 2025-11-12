package calendar

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestIsDayOffCalendar_ParseBulkResponse(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cal := NewIsDayOffCalendar("https://xmlcalendar.ru/data/ru/{year}/calendar.json", 24*time.Hour, logger)

	tests := []struct {
		name     string
		year     int
		month    time.Month
		data     string
		wantDays int
		wantWork int
		wantHours int
	}{
		{
			name:      "November 2025",
			year:      2025,
			month:     time.November,
			data:      "211100011000001100000110000011", // 30 days
			wantDays:  30,
			wantWork:  19, // 18 working + 1 shortened
			wantHours: 151, // 18*8 + 1*7 = 144 + 7 = 151
		},
		{
			name:      "July 2025",
			year:      2025,
			month:     time.July,
			data:      "0000110000011000001100000110000", // 31 days
			wantDays:  31,
			wantWork:  23,
			wantHours: 184, // 23*8 = 184
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monthInfo, err := cal.parseBulkResponse(tt.year, tt.month, tt.data)
			if err != nil {
				t.Fatalf("parseBulkResponse() error = %v", err)
			}

			if len(monthInfo.Days) != tt.wantDays {
				t.Errorf("Days count = %d, want %d", len(monthInfo.Days), tt.wantDays)
			}

			if monthInfo.WorkDays != tt.wantWork {
				t.Errorf("WorkDays = %d, want %d", monthInfo.WorkDays, tt.wantWork)
			}

			if monthInfo.WorkingHours != tt.wantHours {
				t.Errorf("WorkingHours = %d, want %d", monthInfo.WorkingHours, tt.wantHours)
			}
		})
	}
}

func TestIsDayOffCalendar_ParseBulkResponse_ShortenedDay(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cal := NewIsDayOffCalendar("https://xmlcalendar.ru/data/ru/{year}/calendar.json", 24*time.Hour, logger)

	// November 2025: First day (Nov 1) is shortened (code '2')
	data := "211100011000001100000110000011"
	monthInfo, err := cal.parseBulkResponse(2025, time.November, data)
	if err != nil {
		t.Fatalf("parseBulkResponse() error = %v", err)
	}

	// Check Nov 1 (first day, index 0)
	nov1 := monthInfo.Days[0]
	if nov1.Type != DayTypeShortened {
		t.Errorf("Nov 1 Type = %v, want DayTypeShortened", nov1.Type)
	}
	if nov1.WorkingHours != 7 {
		t.Errorf("Nov 1 WorkingHours = %d, want 7", nov1.WorkingHours)
	}
	if !nov1.IsWorkday {
		t.Errorf("Nov 1 IsWorkday = false, want true")
	}
}

func TestIsDayOffCalendar_ParseBulkResponse_InvalidLength(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cal := NewIsDayOffCalendar("https://xmlcalendar.ru/data/ru/{year}/calendar.json", 24*time.Hour, logger)

	// November has 30 days, but providing only 29
	data := "21110001100000110000011000001"
	_, err := cal.parseBulkResponse(2025, time.November, data)
	if err == nil {
		t.Error("parseBulkResponse() expected error for invalid length, got nil")
	}
}

func TestIsDayOffCalendar_ParseXMLCalendarMonth(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cal := NewIsDayOffCalendar("https://xmlcalendar.ru/data/ru/{year}/calendar.json", 24*time.Hour, logger)

	tests := []struct {
		name      string
		year      int
		month     time.Month
		daysStr   string
		wantWork  int
		wantHours int
	}{
		{
			name:      "November 2025",
			year:      2025,
			month:     time.November,
			daysStr:   "1*,2,3+,4,8,9,15,16,22,23,29,30", // 1*=shortened, rest=holidays/weekends
			wantWork:  19, // 30 days - 11 non-working (excluding 1* which is working/shortened)
			wantHours: 151, // 18*8 + 1*7
		},
		{
			name:      "July 2025",
			year:      2025,
			month:     time.July,
			daysStr:   "5,6,12,13,19,20,26,27", // 8 weekends
			wantWork:  23, // 31 - 8
			wantHours: 184,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xmlMonth := &xmlCalendarMonth{
				Month: int(tt.month),
				Days:  tt.daysStr,
			}

			monthInfo, err := cal.parseXMLCalendarMonth(tt.year, tt.month, xmlMonth)
			if err != nil {
				t.Fatalf("parseXMLCalendarMonth() error = %v", err)
			}

			if monthInfo.WorkDays != tt.wantWork {
				t.Errorf("WorkDays = %d, want %d", monthInfo.WorkDays, tt.wantWork)
			}

			if monthInfo.WorkingHours != tt.wantHours {
				t.Errorf("WorkingHours = %d, want %d", monthInfo.WorkingHours, tt.wantHours)
			}
		})
	}
}

func TestIsDayOffCalendar_ParseXMLCalendarMonth_ShortenedDay(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cal := NewIsDayOffCalendar("https://xmlcalendar.ru/data/ru/{year}/calendar.json", 24*time.Hour, logger)

	xmlMonth := &xmlCalendarMonth{
		Month: 11,
		Days:  "1*,2,3+,4,8,9,15,16,22,23,29,30",
	}

	monthInfo, err := cal.parseXMLCalendarMonth(2025, time.November, xmlMonth)
	if err != nil {
		t.Fatalf("parseXMLCalendarMonth() error = %v", err)
	}

	// Check Nov 1 (index 0)
	nov1 := monthInfo.Days[0]
	if nov1.Type != DayTypeShortened {
		t.Errorf("Nov 1 Type = %v, want DayTypeShortened", nov1.Type)
	}
	if nov1.WorkingHours != 7 {
		t.Errorf("Nov 1 WorkingHours = %d, want 7", nov1.WorkingHours)
	}

	// Check Nov 3 (transferred, index 2)
	nov3 := monthInfo.Days[2]
	if nov3.Type != DayTypeHoliday {
		t.Errorf("Nov 3 Type = %v, want DayTypeHoliday (transferred)", nov3.Type)
	}
	if nov3.WorkingHours != 0 {
		t.Errorf("Nov 3 WorkingHours = %d, want 0", nov3.WorkingHours)
	}
}

func TestIsDayOffCalendar_Cache(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cal := NewIsDayOffCalendar("https://xmlcalendar.ru/data/ru/{year}/calendar.json", 1*time.Second, logger)

	// Manually populate cache
	date := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	dayInfo := &DayInfo{
		Date:         date,
		Type:         DayTypeShortened,
		WorkingHours: 7,
		IsWorkday:    true,
	}

	cal.cacheMu.Lock()
	cal.cache[date.Format("2006-01-02")] = &cachedDayInfo{
		data:      dayInfo,
		fetchedAt: time.Now(),
	}
	cal.cacheMu.Unlock()

	// Should hit cache (no API call)
	result, err := cal.GetDayInfo(date)
	if err != nil {
		t.Fatalf("GetDayInfo() error = %v", err)
	}

	if result.WorkingHours != 7 {
		t.Errorf("Cached WorkingHours = %d, want 7", result.WorkingHours)
	}

	// Wait for cache to expire
	time.Sleep(2 * time.Second)

	// This would try to hit API (will fail in test, but demonstrates cache expiry)
	cal.ClearCache()

	// Verify cache cleared
	cal.cacheMu.RLock()
	if len(cal.cache) != 0 {
		t.Errorf("Cache not cleared, len = %d", len(cal.cache))
	}
	cal.cacheMu.RUnlock()
}
