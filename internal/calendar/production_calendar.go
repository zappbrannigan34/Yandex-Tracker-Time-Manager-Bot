package calendar

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultTimeout = 10 * time.Second
)

// ProductionCalendar implements Calendar interface using production-calendar.ru API
type ProductionCalendar struct {
	apiURL      string
	apiToken    string
	country     string
	cacheTTL    time.Duration
	httpClient  *http.Client
	logger      *zap.Logger
	cache       map[string]*cachedMonth
	cacheMu     sync.RWMutex
}

type cachedMonth struct {
	data      *MonthInfo
	fetchedAt time.Time
}

// productionCalendarResponse represents API response
type productionCalendarResponse struct {
	Status      string `json:"status"`
	CountryCode string `json:"country_code"`
	DTStart     string `json:"dt_start"`
	DTEnd       string `json:"dt_end"`
	Statistic   struct {
		CalendarDays              int `json:"calendar_days"`
		CalendarDaysWithoutHolidays int `json:"calendar_days_without_holidays"`
		WorkDays                  int `json:"work_days"`
		Weekends                  int `json:"weekends"`
		Holidays                  int `json:"holidays"`
		ShortenedWorkingDays      int `json:"shortened_working_days"`
		WorkingHours              int `json:"working_hours"`
	} `json:"statistic"`
	Days json.RawMessage `json:"days"` // Can be array OR error string (guest token limitation)
}

// calendarDay represents a single day in the calendar
type calendarDay struct {
	Date         string `json:"date"`
	TypeID       int    `json:"type_id"`
	TypeText     string `json:"type_text"`
	Note         string `json:"note,omitempty"`
	WeekDay      string `json:"week_day"`
	WorkingHours int    `json:"working_hours"`
}

// NewProductionCalendar creates a new ProductionCalendar instance
func NewProductionCalendar(apiURL, apiToken, country string, cacheTTL time.Duration, logger *zap.Logger) *ProductionCalendar {
	return &ProductionCalendar{
		apiURL:   apiURL,
		apiToken: apiToken,
		country:  country,
		cacheTTL: cacheTTL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
		cache:  make(map[string]*cachedMonth),
	}
}

// IsWorkday checks if the given date is a working day
func (pc *ProductionCalendar) IsWorkday(date time.Time) (bool, int, error) {
	dayInfo, err := pc.GetDayInfo(date)
	if err != nil {
		return false, 0, err
	}

	return dayInfo.IsWorkday, dayInfo.WorkingHours, nil
}

// GetMonthInfo returns calendar info for the entire month
func (pc *ProductionCalendar) GetMonthInfo(year int, month time.Month) (*MonthInfo, error) {
	// Check cache
	cacheKey := fmt.Sprintf("%d-%02d", year, month)

	pc.cacheMu.RLock()
	if cached, ok := pc.cache[cacheKey]; ok {
		if time.Since(cached.fetchedAt) < pc.cacheTTL {
			pc.cacheMu.RUnlock()
			pc.logger.Debug("Using cached month info",
				zap.Int("year", year),
				zap.Int("month", int(month)))
			return cached.data, nil
		}
	}
	pc.cacheMu.RUnlock()

	// Fetch from API
	monthInfo, err := pc.fetchMonthInfo(year, month)
	if err != nil {
		return nil, err
	}

	// Update cache
	pc.cacheMu.Lock()
	pc.cache[cacheKey] = &cachedMonth{
		data:      monthInfo,
		fetchedAt: time.Now(),
	}
	pc.cacheMu.Unlock()

	pc.logger.Info("Month info fetched and cached",
		zap.Int("year", year),
		zap.Int("month", int(month)),
		zap.Int("working_hours", monthInfo.WorkingHours))

	return monthInfo, nil
}

// GetDayInfo returns detailed info for a specific day
func (pc *ProductionCalendar) GetDayInfo(date time.Time) (*DayInfo, error) {
	monthInfo, err := pc.GetMonthInfo(date.Year(), date.Month())
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

	return nil, fmt.Errorf("day not found in calendar data: %s", date.Format("2006-01-02"))
}

// fetchMonthInfo fetches month info from API
func (pc *ProductionCalendar) fetchMonthInfo(year int, month time.Month) (*MonthInfo, error) {
	// Build URL: https://production-calendar.ru/get-period/{token}/{country}/{MM.YYYY}/json
	period := fmt.Sprintf("%02d.%d", month, year)
	url := fmt.Sprintf("%s/get-period/%s/%s/%s/json",
		pc.apiURL, pc.apiToken, pc.country, period)

	pc.logger.Debug("Fetching calendar data",
		zap.String("url", url),
		zap.Int("year", year),
		zap.Int("month", int(month)))

	resp, err := pc.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch calendar data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var apiResp productionCalendarResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	if apiResp.Status != "ok" {
		return nil, fmt.Errorf("API returned status: %s", apiResp.Status)
	}

	// Try to parse Days as array
	var days []calendarDay
	if err := json.Unmarshal(apiResp.Days, &days); err != nil {
		// If failed, Days might be an error message string (guest token limitation)
		var errorMsg string
		if err2 := json.Unmarshal(apiResp.Days, &errorMsg); err2 == nil {
			return nil, fmt.Errorf("API error: %s", errorMsg)
		}
		return nil, fmt.Errorf("failed to parse days: %w", err)
	}

	// Convert to MonthInfo
	monthInfo := &MonthInfo{
		Year:         year,
		Month:        month,
		WorkingHours: apiResp.Statistic.WorkingHours,
		WorkDays:     apiResp.Statistic.WorkDays,
		Weekends:     apiResp.Statistic.Weekends,
		Holidays:     apiResp.Statistic.Holidays,
		Days:         make([]DayInfo, 0, len(days)),
	}

	for _, apiDay := range days {
		// Parse date (format: DD.MM.YYYY)
		date, err := time.Parse("02.01.2006", apiDay.Date)
		if err != nil {
			pc.logger.Warn("Failed to parse date",
				zap.String("date", apiDay.Date),
				zap.Error(err))
			continue
		}

		dayType := DayType(apiDay.TypeID)
		isWorkday := apiDay.WorkingHours > 0

		monthInfo.Days = append(monthInfo.Days, DayInfo{
			Date:         date,
			Type:         dayType,
			WorkingHours: apiDay.WorkingHours,
			IsWorkday:    isWorkday,
			Note:         apiDay.Note,
		})
	}

	return monthInfo, nil
}

// ClearCache clears the cache
func (pc *ProductionCalendar) ClearCache() {
	pc.cacheMu.Lock()
	defer pc.cacheMu.Unlock()

	pc.cache = make(map[string]*cachedMonth)
	pc.logger.Info("Calendar cache cleared")
}
