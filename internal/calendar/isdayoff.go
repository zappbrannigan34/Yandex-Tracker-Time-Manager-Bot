package calendar

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	isdayoffBaseURL     = "https://isdayoff.ru"
	defaultHTTPTimeout  = 10 * time.Second
	defaultCacheTTL     = 24 * time.Hour
)

// IsDayOffCalendar implements Calendar interface using isdayoff.ru API
type IsDayOffCalendar struct {
	httpClient  *http.Client
	logger      *zap.Logger
	cache       map[string]*cachedDayInfo
	cacheMu     sync.RWMutex
	cacheTTL    time.Duration
	fallbackURL string
	fallbackData map[int]*xmlCalendarYear // year → calendar data
}

type cachedDayInfo struct {
	data      *DayInfo
	fetchedAt time.Time
}

// xmlCalendarYear represents xmlcalendar.ru JSON structure
type xmlCalendarYear struct {
	Year    int               `json:"year"`
	Months  []xmlCalendarMonth `json:"months"`
	Statistic struct {
		Workdays int     `json:"workdays"`
		Holidays int     `json:"holidays"`
		Hours40  float64 `json:"hours40"`
	} `json:"statistic"`
	Transitions []xmlTransition `json:"transitions"`
}

type xmlCalendarMonth struct {
	Month int    `json:"month"`
	Days  string `json:"days"` // "1*,2,3+,4,8,9,..." where * = shortened, + = transferred
}

type xmlTransition struct {
	From string `json:"from"` // "MM.DD"
	To   string `json:"to"`   // "MM.DD"
}

// NewIsDayOffCalendar creates a new IsDayOffCalendar instance
func NewIsDayOffCalendar(fallbackURL string, cacheTTL time.Duration, logger *zap.Logger) *IsDayOffCalendar {
	if cacheTTL == 0 {
		cacheTTL = defaultCacheTTL
	}

	return &IsDayOffCalendar{
		httpClient: &http.Client{
			Timeout: defaultHTTPTimeout,
		},
		logger:       logger,
		cache:        make(map[string]*cachedDayInfo),
		cacheTTL:     cacheTTL,
		fallbackURL:  fallbackURL,
		fallbackData: make(map[int]*xmlCalendarYear),
	}
}

// IsWorkday checks if the given date is a working day
func (c *IsDayOffCalendar) IsWorkday(date time.Time) (bool, int, error) {
	dayInfo, err := c.GetDayInfo(date)
	if err != nil {
		return false, 0, err
	}

	return dayInfo.IsWorkday, dayInfo.WorkingHours, nil
}

// GetDayInfo returns detailed info for a specific day
func (c *IsDayOffCalendar) GetDayInfo(date time.Time) (*DayInfo, error) {
	// Check cache
	cacheKey := date.Format("2006-01-02")

	c.cacheMu.RLock()
	if cached, ok := c.cache[cacheKey]; ok {
		if time.Since(cached.fetchedAt) < c.cacheTTL {
			c.cacheMu.RUnlock()
			c.logger.Debug("Using cached day info",
				zap.String("date", cacheKey))
			return cached.data, nil
		}
	}
	c.cacheMu.RUnlock()

	// Try fetching from API
	dayInfo, err := c.fetchDayFromAPI(date)
	if err != nil {
		c.logger.Warn("Failed to fetch from API, trying fallback",
			zap.String("date", cacheKey),
			zap.Error(err))

		// Try fallback
		var fallbackErr error
		dayInfo, fallbackErr = c.fetchDayFromFallback(date)
		if fallbackErr != nil {
			return nil, fmt.Errorf("API and fallback both failed: API=%w, Fallback=%v", err, fallbackErr)
		}

		c.logger.Info("Using fallback data", zap.String("date", cacheKey))
		err = nil // Clear error since fallback succeeded
	}

	// Update cache
	c.cacheMu.Lock()
	c.cache[cacheKey] = &cachedDayInfo{
		data:      dayInfo,
		fetchedAt: time.Now(),
	}
	c.cacheMu.Unlock()

	return dayInfo, err
}

// GetMonthInfo returns calendar info for the entire month
func (c *IsDayOffCalendar) GetMonthInfo(year int, month time.Month) (*MonthInfo, error) {
	// Try API first
	monthInfo, err := c.fetchMonthFromAPI(year, month)
	if err != nil {
		c.logger.Warn("Failed to fetch month from API, trying fallback",
			zap.Int("year", year),
			zap.Int("month", int(month)),
			zap.Error(err))

		// Try fallback
		var fallbackErr error
		monthInfo, fallbackErr = c.fetchMonthFromFallback(year, month)
		if fallbackErr != nil {
			return nil, fmt.Errorf("API and fallback both failed: API=%w, Fallback=%v", err, fallbackErr)
		}
	}

	return monthInfo, nil
}

// fetchDayFromAPI fetches single day from isdayoff.ru API
func (c *IsDayOffCalendar) fetchDayFromAPI(date time.Time) (*DayInfo, error) {
	// Use bulk API for the month and extract the day
	monthInfo, err := c.fetchMonthFromAPI(date.Year(), date.Month())
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

	return nil, fmt.Errorf("day not found in month data: %s", date.Format("2006-01-02"))
}

// fetchMonthFromAPI fetches entire month from isdayoff.ru bulk API
func (c *IsDayOffCalendar) fetchMonthFromAPI(year int, month time.Month) (*MonthInfo, error) {
	// Build URL: https://isdayoff.ru/api/getdata?year=2025&month=11&pre=1
	url := fmt.Sprintf("%s/api/getdata?year=%d&month=%d&pre=1",
		isdayoffBaseURL, year, int(month))

	c.logger.Debug("Fetching month from isdayoff.ru",
		zap.String("url", url),
		zap.Int("year", year),
		zap.Int("month", int(month)))

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch calendar data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	bulkData := string(body)
	c.logger.Debug("Received bulk data",
		zap.String("data", bulkData),
		zap.Int("length", len(bulkData)))

	// Parse bulk response
	monthInfo, err := c.parseBulkResponse(year, month, bulkData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse bulk response: %w", err)
	}

	c.logger.Info("Month info fetched from API",
		zap.Int("year", year),
		zap.Int("month", int(month)),
		zap.Int("working_hours", monthInfo.WorkingHours))

	return monthInfo, nil
}

// parseBulkResponse parses isdayoff.ru bulk response string
// Format: "211100011000001100000110000011" where:
// 0 = working day (8 hours)
// 1 = non-working day (holiday/weekend)
// 2 = shortened day (7 hours)
func (c *IsDayOffCalendar) parseBulkResponse(year int, month time.Month, data string) (*MonthInfo, error) {
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()

	if len(data) != daysInMonth {
		return nil, fmt.Errorf("bulk data length mismatch: expected %d, got %d", daysInMonth, len(data))
	}

	monthInfo := &MonthInfo{
		Year:  year,
		Month: month,
		Days:  make([]DayInfo, 0, daysInMonth),
	}

	for i, code := range data {
		day := i + 1
		date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)

		var dayType DayType
		var workingHours int
		var isWorkday bool

		switch code {
		case '0': // Working day
			dayType = DayTypeWorkday
			workingHours = 8
			isWorkday = true
			monthInfo.WorkDays++
		case '1': // Non-working (holiday or weekend)
			if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
				dayType = DayTypeWeekend
				monthInfo.Weekends++
			} else {
				dayType = DayTypeHoliday
				monthInfo.Holidays++
			}
			workingHours = 0
			isWorkday = false
		case '2': // Shortened day
			dayType = DayTypeShortened
			workingHours = 7
			isWorkday = true
			monthInfo.WorkDays++
		default:
			return nil, fmt.Errorf("unknown code '%c' at position %d", code, i)
		}

		monthInfo.WorkingHours += workingHours

		monthInfo.Days = append(monthInfo.Days, DayInfo{
			Date:         date,
			Type:         dayType,
			WorkingHours: workingHours,
			IsWorkday:    isWorkday,
		})
	}

	return monthInfo, nil
}

// fetchDayFromFallback fetches single day from xmlcalendar.ru fallback
func (c *IsDayOffCalendar) fetchDayFromFallback(date time.Time) (*DayInfo, error) {
	monthInfo, err := c.fetchMonthFromFallback(date.Year(), date.Month())
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

	return nil, fmt.Errorf("day not found in fallback data: %s", date.Format("2006-01-02"))
}

// fetchMonthFromFallback fetches month from xmlcalendar.ru
func (c *IsDayOffCalendar) fetchMonthFromFallback(year int, month time.Month) (*MonthInfo, error) {
	// Check if year data already loaded
	c.cacheMu.RLock()
	yearData, exists := c.fallbackData[year]
	c.cacheMu.RUnlock()

	if !exists {
		// Download year data
		var err error
		yearData, err = c.downloadFallbackYear(year)
		if err != nil {
			return nil, fmt.Errorf("failed to download fallback data: %w", err)
		}

		// Cache year data
		c.cacheMu.Lock()
		c.fallbackData[year] = yearData
		c.cacheMu.Unlock()
	}

	// Find month in year data
	var xmlMonth *xmlCalendarMonth
	for i := range yearData.Months {
		if yearData.Months[i].Month == int(month) {
			xmlMonth = &yearData.Months[i]
			break
		}
	}

	if xmlMonth == nil {
		return nil, fmt.Errorf("month %d not found in fallback data for year %d", month, year)
	}

	// Parse xmlcalendar month format
	return c.parseXMLCalendarMonth(year, month, xmlMonth)
}

// downloadFallbackYear downloads entire year from xmlcalendar.ru
func (c *IsDayOffCalendar) downloadFallbackYear(year int) (*xmlCalendarYear, error) {
	url := strings.ReplaceAll(c.fallbackURL, "{year}", strconv.Itoa(year))

	c.logger.Info("Downloading fallback calendar data",
		zap.String("url", url),
		zap.Int("year", year))

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch fallback data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fallback API returned status %d", resp.StatusCode)
	}

	var yearData xmlCalendarYear
	if err := json.NewDecoder(resp.Body).Decode(&yearData); err != nil {
		return nil, fmt.Errorf("failed to parse fallback JSON: %w", err)
	}

	c.logger.Info("Fallback data downloaded",
		zap.Int("year", year),
		zap.Int("months", len(yearData.Months)))

	return &yearData, nil
}

// parseXMLCalendarMonth parses xmlcalendar.ru compact format
// Format: "1*,2,3+,4,8,9,15,16,22,23,29,30"
// * = shortened day, + = transferred day, others = weekends/holidays
func (c *IsDayOffCalendar) parseXMLCalendarMonth(year int, month time.Month, xmlMonth *xmlCalendarMonth) (*MonthInfo, error) {
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()

	// Parse non-working days string
	nonWorkingMap := make(map[int]rune) // day → marker (* or + or 0)
	if xmlMonth.Days != "" {
		parts := strings.Split(xmlMonth.Days, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			marker := rune(0)
			dayStr := part

			// Check for markers
			if strings.HasSuffix(part, "*") {
				marker = '*' // shortened
				dayStr = strings.TrimSuffix(part, "*")
			} else if strings.HasSuffix(part, "+") {
				marker = '+' // transferred
				dayStr = strings.TrimSuffix(part, "+")
			}

			day, err := strconv.Atoi(dayStr)
			if err != nil {
				c.logger.Warn("Failed to parse day number",
					zap.String("part", part),
					zap.Error(err))
				continue
			}

			nonWorkingMap[day] = marker
		}
	}

	monthInfo := &MonthInfo{
		Year:  year,
		Month: month,
		Days:  make([]DayInfo, 0, daysInMonth),
	}

	for day := 1; day <= daysInMonth; day++ {
		date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)

		marker, isNonWorking := nonWorkingMap[day]

		var dayType DayType
		var workingHours int
		var isWorkday bool

		if marker == '*' {
			// Shortened day (working but 7 hours)
			dayType = DayTypeShortened
			workingHours = 7
			isWorkday = true
			monthInfo.WorkDays++
		} else if isNonWorking {
			// Holiday or transferred weekend
			if date.Weekday() == time.Saturday || date.Weekday() == time.Sunday {
				dayType = DayTypeWeekend
				monthInfo.Weekends++
			} else {
				dayType = DayTypeHoliday
				monthInfo.Holidays++
			}
			workingHours = 0
			isWorkday = false
		} else {
			// Regular working day
			dayType = DayTypeWorkday
			workingHours = 8
			isWorkday = true
			monthInfo.WorkDays++
		}

		monthInfo.WorkingHours += workingHours

		monthInfo.Days = append(monthInfo.Days, DayInfo{
			Date:         date,
			Type:         dayType,
			WorkingHours: workingHours,
			IsWorkday:    isWorkday,
		})
	}

	return monthInfo, nil
}

// ClearCache clears the cache
func (c *IsDayOffCalendar) ClearCache() {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.cache = make(map[string]*cachedDayInfo)
	c.fallbackData = make(map[int]*xmlCalendarYear)
	c.logger.Info("Calendar cache cleared")
}
