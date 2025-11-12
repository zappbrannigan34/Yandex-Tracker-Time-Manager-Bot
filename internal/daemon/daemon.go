package daemon

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/username/time-tracker-bot/internal/timemanager"
	"github.com/username/time-tracker-bot/pkg/dateutil"
	"go.uber.org/zap"
)

// Daemon represents the daemon process
type Daemon struct {
	manager       *timemanager.Manager
	checkInterval time.Duration // Deprecated: kept for backward compatibility
	dailyHour     int           // Hour to run daily sync (0-23)
	dailyMinute   int           // Minute to run daily sync (0-59)
	systemTray    bool          // Show system tray icon
	logger        *zap.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	trayApp       *TrayApp
	lastRunDate   string    // Track last successful run date to avoid duplicates
	lastRunTime   time.Time // Track last successful run time
	mu            sync.Mutex // Protect against concurrent runs
	syncRunning   bool      // Flag to prevent concurrent sync operations
}

// NewDaemon creates a new daemon instance with interval-based checks (deprecated)
func NewDaemon(manager *timemanager.Manager, checkInterval time.Duration, logger *zap.Logger) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())

	return &Daemon{
		manager:       manager,
		checkInterval: checkInterval,
		dailyHour:     20, // Default to 20:00
		dailyMinute:   0,
		systemTray:    false,
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// NewScheduledDaemon creates a new daemon instance with daily schedule
func NewScheduledDaemon(manager *timemanager.Manager, dailyHour, dailyMinute int, systemTray bool, logger *zap.Logger) *Daemon {
	ctx, cancel := context.WithCancel(context.Background())

	return &Daemon{
		manager:     manager,
		dailyHour:   dailyHour,
		dailyMinute: dailyMinute,
		systemTray:  systemTray,
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start starts the daemon
func (d *Daemon) Start() error {
	// Check if we're using scheduled mode or interval mode
	if d.checkInterval > 0 {
		return d.startIntervalMode()
	}
	return d.startScheduledMode()
}

// startIntervalMode runs daemon in interval-based mode (deprecated)
func (d *Daemon) startIntervalMode() error {
	d.logger.Info("Daemon started in interval mode",
		zap.Duration("check_interval", d.checkInterval))

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Run initial check immediately
	go d.runCheck()

	// Setup ticker for periodic checks
	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			d.logger.Info("Daemon stopped")
			return nil

		case sig := <-sigChan:
			d.logger.Info("Received signal, shutting down",
				zap.String("signal", sig.String()))
			d.Stop()
			return nil

		case <-ticker.C:
			go d.runCheck()
		}
	}
}

// startScheduledMode runs daemon in scheduled mode (daily at specific time)
func (d *Daemon) startScheduledMode() error {
	// Initialize system tray if enabled (Windows only)
	if d.systemTray {
		d.logger.Info("Initializing system tray")
		trayApp, err := NewTrayApp(d, d.logger)
		if err != nil {
			d.logger.Warn("Failed to initialize system tray", zap.Error(err))
			// Fall back to non-tray mode
			return d.startScheduledModeWithoutTray()
		}
		d.trayApp = trayApp
		// Run tray (blocks until Quit)
		d.trayApp.Run()
		return nil
	}

	// Fallback to console mode
	d.logger.Info("Running without system tray")
	return d.startScheduledModeWithoutTray()
}

// startScheduledModeWithoutTray runs daemon in console mode
func (d *Daemon) startScheduledModeWithoutTray() error {
	d.logger.Info("Starting console mode")
	d.runScheduledLogic()
	return nil
}

// runScheduledLogic runs the scheduled sync logic (called from tray or standalone)
func (d *Daemon) runScheduledLogic() {
	d.logger.Info("Daemon scheduled logic started",
		zap.Int("daily_hour", d.dailyHour),
		zap.Int("daily_minute", d.dailyMinute),
		zap.String("timezone", "MSK (UTC+3)"))

	// Check if we should run immediately (if scheduled time already passed today)
	mskLocation := time.FixedZone("MSK", 3*60*60)
	now := time.Now().In(mskLocation)
	today := now.Format("2006-01-02")

	scheduledToday := time.Date(now.Year(), now.Month(), now.Day(),
		d.dailyHour, d.dailyMinute, 0, 0, mskLocation)

	if now.After(scheduledToday) && d.lastRunDate != today {
		d.logger.Info("Scheduled time already passed today, running sync now",
			zap.Time("scheduled_time", scheduledToday),
			zap.Time("current_time", now))

		if err := d.runSync(); err != nil {
			d.logger.Error("Initial sync failed", zap.Error(err))
			if d.trayApp != nil {
				d.trayApp.ShowNotification("Sync Failed", fmt.Sprintf("Error: %v", err))
			}
		} else {
			d.lastRunDate = today
			d.logger.Info("Initial sync completed successfully")
			if d.trayApp != nil {
				d.trayApp.ShowNotification("Sync Completed", "Time logged for today")
			}
		}
	}

	// Calculate and log next run time
	nextRun := d.calculateNextRun()
	d.logger.Info("Next sync scheduled",
		zap.Time("next_run", nextRun),
		zap.Duration("wait_duration", time.Until(nextRun)))

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Check every minute if it's time to run
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			d.logger.Info("Daemon stopped")
			if d.trayApp != nil {
				d.trayApp.Stop()
			}
			return

		case sig := <-sigChan:
			d.logger.Info("Received signal, shutting down",
				zap.String("signal", sig.String()))
			if d.trayApp != nil {
				d.trayApp.Stop()
			}
			d.Stop()
			return

		case now := <-ticker.C:
			// Check if it's time to run
			if d.shouldRunAt(now) {
				// Check if we already ran today
				today := now.Format("2006-01-02")
				if d.lastRunDate == today {
					d.logger.Debug("Already ran today, skipping")
					continue
				}

				d.logger.Info("Starting scheduled sync", zap.Time("time", now))

				if err := d.runSync(); err != nil {
					d.logger.Error("Sync failed", zap.Error(err))
					if d.trayApp != nil {
						d.trayApp.ShowNotification("Sync Failed", fmt.Sprintf("Error: %v", err))
					}
				} else {
					d.lastRunDate = today
					d.logger.Info("Sync completed successfully")
					if d.trayApp != nil {
						d.trayApp.ShowNotification("Sync Completed", "Time logged successfully for today")
					}

					// Calculate next run
					nextRun = d.calculateNextRun()
					d.logger.Info("Next sync scheduled",
						zap.Time("next_run", nextRun),
						zap.Duration("wait_duration", time.Until(nextRun)))
				}
			}
		}
	}
}

// Stop stops the daemon
func (d *Daemon) Stop() {
	d.cancel()
}

// runCheck performs a single time distribution check
func (d *Daemon) runCheck() {
	d.logger.Info("Running periodic time distribution check")

	date := dateutil.Today()

	// Distribute time for today
	entries, err := d.manager.DistributeTimeForDate(date, false)
	if err != nil {
		d.logger.Error("Failed to distribute time",
			zap.Time("date", date),
			zap.Error(err))
		return
	}

	if len(entries) == 0 {
		d.logger.Info("No time entries created (either non-workday or already worked enough)",
			zap.Time("date", date))
		return
	}

	totalMinutes := 0.0
	for _, entry := range entries {
		totalMinutes += entry.Minutes
	}

	d.logger.Info("Time distribution completed",
		zap.Time("date", date),
		zap.Int("entries_count", len(entries)),
		zap.Float64("total_minutes", totalMinutes),
		zap.Float64("total_hours", totalMinutes/60))
}

// RunWithTimeout runs the daemon with a timeout (for testing)
func (d *Daemon) RunWithTimeout(timeout time.Duration) error {
	d.logger.Info("Daemon started with timeout",
		zap.Duration("timeout", timeout),
		zap.Duration("check_interval", d.checkInterval))

	// Setup timeout
	timeoutCtx, timeoutCancel := context.WithTimeout(d.ctx, timeout)
	defer timeoutCancel()

	// Run initial check
	go d.runCheck()

	// Setup ticker
	ticker := time.NewTicker(d.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			d.logger.Info("Daemon stopped (timeout reached)")
			return nil

		case <-ticker.C:
			go d.runCheck()
		}
	}
}

// GetStatus returns daemon status
func (d *Daemon) GetStatus() map[string]interface{} {
	status := map[string]interface{}{
		"running":        true,
		"check_interval": d.checkInterval.String(),
		"next_check_in":  d.getNextCheckTime(),
	}

	// Get current day status
	date := dateutil.Today()
	workedMinutes, targetMinutes, err := d.manager.GetStatus(date)
	if err == nil && targetMinutes > 0 {
		status["today"] = map[string]interface{}{
			"date":            date.Format("2006-01-02"),
			"worked_minutes":  workedMinutes,
			"target_minutes":  targetMinutes,
			"remaining_minutes": targetMinutes - workedMinutes,
			"progress_percent":  (workedMinutes / targetMinutes) * 100,
		}
	}

	return status
}

func (d *Daemon) getNextCheckTime() string {
	// This is approximate - actual next check depends on ticker
	nextCheck := time.Now().Add(d.checkInterval)
	return fmt.Sprintf("%s (in %s)", nextCheck.Format("15:04:05"), d.checkInterval.String())
}

// calculateNextRun calculates the next scheduled run time (MSK timezone)
func (d *Daemon) calculateNextRun() time.Time {
	// MSK timezone (UTC+3)
	mskLocation := time.FixedZone("MSK", 3*60*60)
	now := time.Now().In(mskLocation)

	// Create target time for today
	today := time.Date(now.Year(), now.Month(), now.Day(),
		d.dailyHour, d.dailyMinute, 0, 0, mskLocation)

	// If target time already passed today, schedule for tomorrow
	if now.After(today) || now.Equal(today) {
		return today.AddDate(0, 0, 1)
	}

	return today
}

// shouldRunAt checks if sync should run at the given time
func (d *Daemon) shouldRunAt(now time.Time) bool {
	// MSK timezone (UTC+3)
	mskLocation := time.FixedZone("MSK", 3*60*60)
	nowMSK := now.In(mskLocation)

	// Check if current time matches scheduled time (within 1 minute window)
	return nowMSK.Hour() == d.dailyHour &&
		nowMSK.Minute() == d.dailyMinute
}

// runSync executes the time sync operation for today
// CRITICAL: Protected with mutex to prevent concurrent runs that could create duplicates
func (d *Daemon) runSync() error {
	// IDEMPOTENT PROTECTION: Lock to prevent concurrent sync
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if sync is already running
	if d.syncRunning {
		d.logger.Warn("Sync already running, skipping concurrent execution")
		return fmt.Errorf("sync already in progress")
	}

	// Check if already ran today
	today := dateutil.Today()
	todayStr := today.Format("2006-01-02")
	if d.lastRunDate == todayStr {
		d.logger.Info("Already ran sync today, skipping to prevent duplicates",
			zap.String("last_run_date", d.lastRunDate),
			zap.Time("last_run_time", d.lastRunTime))
		return nil
	}

	// Mark sync as running
	d.syncRunning = true
	defer func() {
		d.syncRunning = false
	}()

	d.logger.Info("Running sync for today", zap.Time("date", today))

	entries, err := d.manager.DistributeTimeForDate(today, false)
	if err != nil {
		return fmt.Errorf("failed to distribute time: %w", err)
	}

	if len(entries) == 0 {
		d.logger.Info("No time entries created (either non-workday or already worked enough)",
			zap.Time("date", today))
		// Still update lastRunDate to prevent retrying on non-workday
		d.lastRunDate = todayStr
		d.lastRunTime = time.Now()
		return nil
	}

	totalMinutes := 0.0
	for _, entry := range entries {
		totalMinutes += entry.Minutes
	}

	d.logger.Info("Sync completed",
		zap.Int("entries", len(entries)),
		zap.Float64("total_minutes", totalMinutes),
		zap.Float64("total_hours", totalMinutes/60),
		zap.Time("date", today))

	// Update last run info to prevent duplicate runs
	d.lastRunDate = todayStr
	d.lastRunTime = time.Now()

	return nil
}

// SyncNow triggers an immediate sync (called from tray menu)
func (d *Daemon) SyncNow() {
	d.logger.Info("Manual sync triggered from tray")
	if err := d.runSync(); err != nil {
		d.logger.Error("Manual sync failed", zap.Error(err))
		if d.trayApp != nil {
			d.trayApp.ShowNotification("Sync Failed", fmt.Sprintf("Error: %v", err))
		}
	} else {
		d.logger.Info("Manual sync completed successfully")
		if d.trayApp != nil {
			d.trayApp.ShowNotification("Sync Completed", "Time logged successfully")
		}
		// lastRunDate is updated inside runSync() - no need to update here
	}
}
