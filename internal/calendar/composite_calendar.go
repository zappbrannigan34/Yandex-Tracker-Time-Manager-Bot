package calendar

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// CompositeCalendar implements Calendar with fallback strategy
// Primary: ProductionCalendar (API)
// Fallback: FileCalendar (local file)
type CompositeCalendar struct {
	primary  Calendar
	fallback Calendar
	logger   *zap.Logger
}

// NewCompositeCalendar creates a new CompositeCalendar
func NewCompositeCalendar(primary, fallback Calendar, logger *zap.Logger) *CompositeCalendar {
	return &CompositeCalendar{
		primary:  primary,
		fallback: fallback,
		logger:   logger,
	}
}

// IsWorkday checks if the given date is a working day
func (cc *CompositeCalendar) IsWorkday(date time.Time) (bool, int, error) {
	// Try primary first
	isWorkday, hours, err := cc.primary.IsWorkday(date)
	if err == nil {
		return isWorkday, hours, nil
	}

	cc.logger.Warn("Primary calendar failed, falling back to file",
		zap.Error(err))

	// Fallback to file
	return cc.fallback.IsWorkday(date)
}

// GetMonthInfo returns calendar info for the entire month
func (cc *CompositeCalendar) GetMonthInfo(year int, month time.Month) (*MonthInfo, error) {
	// Try primary first
	monthInfo, err := cc.primary.GetMonthInfo(year, month)
	if err == nil {
		return monthInfo, nil
	}

	cc.logger.Warn("Primary calendar failed, falling back to file",
		zap.Int("year", year),
		zap.Int("month", int(month)),
		zap.Error(err))

	// Fallback to file
	return cc.fallback.GetMonthInfo(year, month)
}

// GetDayInfo returns detailed info for a specific day
func (cc *CompositeCalendar) GetDayInfo(date time.Time) (*DayInfo, error) {
	// Try primary first
	dayInfo, err := cc.primary.GetDayInfo(date)
	if err == nil {
		return dayInfo, nil
	}

	cc.logger.Warn("Primary calendar failed, falling back to file",
		zap.Time("date", date),
		zap.Error(err))

	// Fallback to file
	return cc.fallback.GetDayInfo(date)
}

// LoadFallback loads the fallback calendar (if FileCalendar)
func (cc *CompositeCalendar) LoadFallback() error {
	if fc, ok := cc.fallback.(*FileCalendar); ok {
		if err := fc.Load(); err != nil {
			return fmt.Errorf("failed to load fallback calendar: %w", err)
		}
		cc.logger.Info("Fallback calendar loaded successfully")
	}
	return nil
}
