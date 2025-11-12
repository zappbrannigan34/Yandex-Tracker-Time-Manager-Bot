package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/username/time-tracker-bot/internal/calendar"
	"github.com/username/time-tracker-bot/internal/config"
	"github.com/username/time-tracker-bot/internal/daemon"
	"github.com/username/time-tracker-bot/internal/timemanager"
	"github.com/username/time-tracker-bot/internal/tracker"
	"github.com/username/time-tracker-bot/pkg/dateutil"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	version    = "1.0.0"
	configPath string
	logger     *zap.Logger
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "time-tracker-bot",
		Short: "Yandex Tracker Time Manager",
		Long:  "Automatically distribute and log time in Yandex Tracker with production calendar integration",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Load config to get log file path
			cfg, err := config.Load(configPath)
			if err == nil && cfg.Daemon.LogFile != "" {
				logger, err = initFileLogger(cfg.Daemon.LogFile, cfg.Daemon.LogLevel)
				if err != nil {
					initLogger() // Fallback to console
				}
			} else {
				initLogger() // Default console logger
			}
		},
	}

	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "config.yaml", "Config file path")

	rootCmd.AddCommand(syncCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(reportCmd())
	rootCmd.AddCommand(backfillCmd())
	rootCmd.AddCommand(cleanupCmd())
	rootCmd.AddCommand(weeklyScheduleCmd())
	rootCmd.AddCommand(daemonCmd())
	rootCmd.AddCommand(versionCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func syncCmd() *cobra.Command {
	var dateStr string
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync time for a date",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse date
			var date time.Time
			var err error

			if dateStr == "today" {
				date = dateutil.Today()
			} else if dateStr == "yesterday" {
				date = dateutil.Yesterday()
			} else {
				date, err = dateutil.ParseDate(dateStr)
				if err != nil {
					return fmt.Errorf("invalid date format: %w", err)
				}
			}

			// Load config
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			cfg.ExpandEnvVars()

			// Initialize components
			manager, err := initializeManager(cfg)
			if err != nil {
				return err
			}

			logger.Info("Starting sync",
				zap.Time("date", date),
				zap.Bool("dry_run", dryRun))

			// Distribute time
			entries, err := manager.DistributeTimeForDate(date, dryRun)
			if err != nil {
				return fmt.Errorf("failed to distribute time: %w", err)
			}

			// Print results
			if len(entries) == 0 {
				fmt.Println("No time entries to log (either non-workday or already worked enough)")
				return nil
			}

			fmt.Printf("\n%s Time Entries for %s:\n", getIcon(dryRun), date.Format("2006-01-02"))
			fmt.Println("═══════════════════════════════════════════════════════")

			totalMinutes := 0.0
			for _, entry := range entries {
				hours := int(entry.Minutes / 60)
				mins := int(entry.Minutes) % 60
				fmt.Printf("  • %-15s  %2dh %2dm  %s\n",
					entry.IssueKey,
					hours, mins,
					entry.Comment)
				totalMinutes += entry.Minutes
			}

			fmt.Println("═══════════════════════════════════════════════════════")
			totalHours := int(totalMinutes / 60)
			totalMins := int(totalMinutes) % 60
			fmt.Printf("  Total: %dh %dm (%d entries)\n", totalHours, totalMins, len(entries))

			if dryRun {
				fmt.Println("\n[DRY RUN] No worklogs were created")
			} else {
				fmt.Println("\n✅ Time logged successfully")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&dateStr, "date", "d", "today", "Date to sync (today, yesterday, or YYYY-MM-DD)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without creating worklogs")

	return cmd
}

func statusCmd() *cobra.Command {
	var dateStr string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current time tracking status",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse date
			var date time.Time
			var err error

			if dateStr == "today" {
				date = dateutil.Today()
			} else {
				date, err = dateutil.ParseDate(dateStr)
				if err != nil {
					return fmt.Errorf("invalid date format: %w", err)
				}
			}

			// Load config
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			cfg.ExpandEnvVars()

			// Initialize components
			manager, err := initializeManager(cfg)
			if err != nil {
				return err
			}

			// Get status
			workedMinutes, targetMinutes, err := manager.GetStatus(date)
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}

			// Print status
			fmt.Printf("\nTime Tracking Status for %s:\n", date.Format("2006-01-02"))
			fmt.Println("═══════════════════════════════════════════════════════")

			if targetMinutes == 0 {
				fmt.Println("  Not a working day")
				return nil
			}

			workedHours := workedMinutes / 60
			targetHours := targetMinutes / 60
			remainingMinutes := targetMinutes - workedMinutes
			remainingHours := remainingMinutes / 60

			fmt.Printf("  Worked:    %.1fh (%.0f minutes)\n", workedHours, workedMinutes)
			fmt.Printf("  Target:    %.1fh (%.0f minutes)\n", targetHours, targetMinutes)

			if remainingMinutes > 0 {
				fmt.Printf("  Remaining: %.1fh (%.0f minutes)\n", remainingHours, remainingMinutes)
			} else {
				fmt.Printf("  ✅ Target reached!\n")
			}

			progress := (workedMinutes / targetMinutes) * 100
			fmt.Printf("  Progress:  %.1f%%\n", progress)

			return nil
		},
	}

	cmd.Flags().StringVarP(&dateStr, "date", "d", "today", "Date to check (today or YYYY-MM-DD)")

	return cmd
}

func reportCmd() *cobra.Command {
	var dateStr string
	var week bool
	var month bool
	var fromStr string
	var toStr string

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Show detailed worklog report",
		Long:  "Show detailed worklog report with list of tasks and time spent",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse date range
			var from, to time.Time
			var err error

			// Determine date range based on flags
			if week {
				// Current week (Monday to Sunday)
				now := dateutil.Today()
				weekday := int(now.Weekday())
				if weekday == 0 {
					weekday = 7 // Sunday = 7
				}
				from = now.AddDate(0, 0, -(weekday - 1)) // Monday
				to = from.AddDate(0, 0, 6)               // Sunday
			} else if month {
				// Current month
				now := dateutil.Today()
				from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
				to = time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.Local)
			} else if fromStr != "" && toStr != "" {
				// Custom range
				from, err = dateutil.ParseDate(fromStr)
				if err != nil {
					return fmt.Errorf("invalid from date: %w", err)
				}
				to, err = dateutil.ParseDate(toStr)
				if err != nil {
					return fmt.Errorf("invalid to date: %w", err)
				}
			} else if dateStr != "" {
				// Single day (detailed)
				if dateStr == "today" {
					from = dateutil.Today()
				} else if dateStr == "yesterday" {
					from = dateutil.Yesterday()
				} else {
					from, err = dateutil.ParseDate(dateStr)
					if err != nil {
						return fmt.Errorf("invalid date format: %w", err)
					}
				}
				to = from
			} else {
				return fmt.Errorf("specify --date, --week, --month, or --from/--to")
			}

			// Load config
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			cfg.ExpandEnvVars()

			// Initialize token manager
			tokenManager := tracker.NewTokenManager(
				cfg.IAM.GetRefreshInterval(),
				cfg.IAM.CLICommand,
				logger,
			)
			if err := tokenManager.Start(); err != nil {
				return fmt.Errorf("failed to start token manager: %w", err)
			}

			// Initialize Tracker API client
			trackerClient := tracker.NewClient(
				cfg.Tracker.APIEndpoint,
				cfg.Tracker.OrgID,
				tokenManager,
				logger,
			)

			var worklogs []tracker.Worklog
			if from.Equal(to) {
				// Single day
				worklogs, err = trackerClient.GetWorklogsForToday(from)
			} else {
				// Range
				worklogs, err = trackerClient.GetWorklogsForRange(from, to)
			}

			if err != nil {
				return fmt.Errorf("failed to get worklogs: %w", err)
			}

			// Print report in compact format
			if len(worklogs) == 0 {
				fmt.Printf("\nNo worklogs found for period %s - %s\n",
					from.Format("2006-01-02"),
					to.Format("2006-01-02"))
				return nil
			}

			fmt.Printf("\nWorklogs for %s - %s:\n\n",
				from.Format("2006-01-02"),
				to.Format("2006-01-02"))

			// Print worklogs
			totalMinutes := 0.0
			for _, wl := range worklogs {
				minutes, _ := tracker.ParseISO8601Duration(wl.Duration)
				totalMinutes += minutes
				hours := int(minutes / 60)
				mins := int(minutes) % 60

				comment := wl.Comment
				if comment == "" {
					comment = "-"
				}

				fmt.Printf("%s  %-15s  %2dh %2dm  %s\n",
					wl.Start.In(time.Local).Format("2006-01-02"),
					wl.Issue.Key,
					hours, mins,
					comment)
			}

			// Print total
			totalHours := int(totalMinutes / 60)
			totalMins := int(totalMinutes) % 60
			fmt.Printf("\nTotal: %dh %dm (%d worklogs)\n", totalHours, totalMins, len(worklogs))

			return nil
		},
	}

	cmd.Flags().StringVarP(&dateStr, "date", "d", "", "Single date (today, yesterday, or YYYY-MM-DD)")
	cmd.Flags().BoolVarP(&week, "week", "w", false, "Current week (Monday-Sunday)")
	cmd.Flags().BoolVarP(&month, "month", "m", false, "Current month")
	cmd.Flags().StringVar(&fromStr, "from", "", "Start date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&toStr, "to", "", "End date (YYYY-MM-DD)")

	return cmd
}

func weeklyScheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "weekly-schedule",
		Short: "Show weekly schedule for random tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Load weekly state
			stateManager := timemanager.NewWeeklyStateManager(cfg.State.WeeklyScheduleFile, logger)
			if err := stateManager.Load(); err != nil {
				return fmt.Errorf("failed to load weekly state: %w", err)
			}

			state := stateManager.GetCurrentState()

			if state == nil || state.Year == 0 {
				fmt.Println("\nNo weekly schedule yet")
				fmt.Println("Run 'sync' command to generate schedule for current week")
				return nil
			}

			// Print schedule
			fmt.Printf("\nWeekly Schedule (Week %d, %d):\n", state.Week, state.Year)
			fmt.Printf("Period: %s - %s\n", state.StartDate, state.EndDate)
			fmt.Println("═══════════════════════════════════════════════════════")

			for task, dates := range state.SelectedDays {
				fmt.Printf("\n  %s:\n", task)
				for _, dateStr := range dates {
					date, _ := time.Parse("2006-01-02", dateStr)
					weekday := date.Weekday().String()
					fmt.Printf("    • %s (%s)\n", dateStr, weekday)
				}
			}

			fmt.Println()

			return nil
		},
	}

	return cmd
}

func daemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run in daemon mode (continuous background process)",
		Long:  "Start daemon that automatically distributes time every N hours",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			cfg.ExpandEnvVars()

			// Initialize components
			manager, err := initializeManager(cfg)
			if err != nil {
				return err
			}

			// Create daemon with scheduled or interval mode
			if cfg.Daemon.DailyTime != "" {
				// New scheduled mode: run at specific time daily
				hour, minute := cfg.Daemon.GetDailyTime()
				d := daemon.NewScheduledDaemon(manager, hour, minute, cfg.Daemon.SystemTray, logger)

				logger.Info("Starting daemon in scheduled mode",
					zap.Int("daily_hour", hour),
					zap.Int("daily_minute", minute),
					zap.Bool("system_tray", cfg.Daemon.SystemTray))

				if !cfg.Daemon.SystemTray {
					fmt.Printf("🤖 Daemon started in scheduled mode\n")
					fmt.Printf("   Daily sync at: %02d:%02d MSK (UTC+3)\n", hour, minute)
					fmt.Println("Press Ctrl+C to stop")
				}

				if err := d.Start(); err != nil {
					return fmt.Errorf("daemon failed: %w", err)
				}
			} else {
				// Legacy interval mode: check every N hours
				d := daemon.NewDaemon(manager, cfg.Daemon.GetCheckInterval(), logger)

				logger.Info("Starting daemon in interval mode",
					zap.Duration("check_interval", cfg.Daemon.GetCheckInterval()))

				fmt.Printf("🤖 Daemon started in interval mode (checking every %s)\n", cfg.Daemon.GetCheckInterval())
				fmt.Println("Press Ctrl+C to stop")

				if err := d.Start(); err != nil {
					return fmt.Errorf("daemon failed: %w", err)
				}
			}

			return nil
		},
	}

	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("time-tracker-bot version %s\n", version)
		},
	}
}

func initializeManager(cfg *config.Config) (*timemanager.Manager, error) {
	// Initialize IAM token manager
	tokenManager := tracker.NewTokenManager(
		cfg.IAM.GetRefreshInterval(),
		cfg.IAM.CLICommand,
		logger,
	)

	if err := tokenManager.Start(); err != nil {
		return nil, fmt.Errorf("failed to start token manager: %w", err)
	}

	// Initialize Tracker API client
	trackerClient := tracker.NewClient(
		cfg.Tracker.APIEndpoint,
		cfg.Tracker.OrgID,
		tokenManager,
		logger,
	)

	// Initialize calendar based on type
	var cal calendar.Calendar

	calType := cfg.Calendar.Type
	if calType == "" {
		calType = "isdayoff" // Default
	}

	switch calType {
	case "isdayoff":
		logger.Info("Using isdayoff.ru calendar API")
		cal = calendar.NewIsDayOffCalendar(
			cfg.Calendar.FallbackURL,
			cfg.Calendar.GetCacheTTL(),
			logger,
		)

	case "production-calendar":
		logger.Info("Using production-calendar.ru API (legacy)")
		primaryCal := calendar.NewProductionCalendar(
			cfg.Calendar.APIURL,
			cfg.Calendar.APIToken,
			cfg.Calendar.Country,
			cfg.Calendar.GetCacheTTL(),
			logger,
		)

		fallbackCal := calendar.NewFileCalendar(cfg.Calendar.FallbackFile, logger)
		compositeCal := calendar.NewCompositeCalendar(primaryCal, fallbackCal, logger)

		// Load fallback calendar
		if err := compositeCal.LoadFallback(); err != nil {
			logger.Warn("Failed to load fallback calendar, continuing with API only",
				zap.Error(err))
		}

		cal = compositeCal

	default:
		return nil, fmt.Errorf("unknown calendar type: %s", calType)
	}

	// Initialize weekly state manager
	weeklyState := timemanager.NewWeeklyStateManager(cfg.State.WeeklyScheduleFile, logger)
	if err := weeklyState.Load(); err != nil {
		return nil, fmt.Errorf("failed to load weekly state: %w", err)
	}

	// Initialize time manager
	manager := timemanager.NewManager(cfg, trackerClient, cal, weeklyState, logger)

	return manager, nil
}

func initLogger() {
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var err error
	logger, err = config.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to initialize logger: %v", err))
	}
}

func initFileLogger(logFile string, level string) (*zap.Logger, error) {
	config := zap.NewProductionConfig()
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.OutputPaths = []string{logFile}
	config.ErrorOutputPaths = []string{logFile}

	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel
	}
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	return config.Build()
}

func getIcon(dryRun bool) string {
	if dryRun {
		return "📋"
	}
	return "✅"
}
