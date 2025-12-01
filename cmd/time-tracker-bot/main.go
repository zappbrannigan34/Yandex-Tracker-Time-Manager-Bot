package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/username/time-tracker-bot/internal/calendar"
	"github.com/username/time-tracker-bot/internal/config"
	"github.com/username/time-tracker-bot/internal/timemanager"
	"github.com/username/time-tracker-bot/internal/tracker"
	"github.com/username/time-tracker-bot/pkg/dateutil"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	configPath string
	logger     *zap.Logger
	syncWriter io.Writer = os.Stdout
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

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func syncCmd() *cobra.Command {
	var dryRun bool
	var teeOutput string

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Backfill месяц и полностью заполнить сегодняшний день",
		RunE: func(cmd *cobra.Command, args []string) error {
			syncWriter = os.Stdout
			if teeOutput != "" {
				if err := os.MkdirAll(filepath.Dir(teeOutput), 0o755); err != nil {
					return fmt.Errorf("failed to create tee path: %w", err)
				}
				f, err := os.OpenFile(teeOutput, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
				if err != nil {
					return fmt.Errorf("failed to open tee-output file: %w", err)
				}
				defer f.Close()
				syncWriter = io.MultiWriter(os.Stdout, f)
				syncPrintf("📝 Output is mirrored to %s\n", teeOutput)
			}
			defer func() {
				syncWriter = os.Stdout
			}()

			today := dateutil.Today()
			monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.Local)

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

			logger.Info("Starting full sync",
				zap.Time("month_start", monthStart),
				zap.Time("today", today),
				zap.Bool("dry_run", dryRun))

			syncPrintf("⏳ Step 1/3: normalizing %s .. %s\n",
				monthStart.Format("2006-01-02"),
				today.AddDate(0, 0, -1).Format("2006-01-02"))
			// Step 1: normalize historic days (до сегодняшнего)
			normalizeSummary, err := manager.NormalizeWorkdaysRange(monthStart, today.AddDate(0, 0, -1), dryRun)
			if err != nil {
				return fmt.Errorf("normalization failed: %w", err)
			}
			if normalizeSummary != nil {
				syncPrintf("   • Processed %d days, normalized %d (%.1fh removed) in %s\n",
					normalizeSummary.ProcessedDays,
					normalizeSummary.NormalizedDays,
					normalizeSummary.TotalMinutesTrimmed/60,
					normalizeSummary.Duration.Round(time.Millisecond))
			}

			syncPrintf("⏳ Step 2/3: backfill month-to-date\n")
			// Step 2: Backfill entire month-to-date (excluding future days)
			backfillResult, timelines, err := manager.BackfillPeriod(monthStart, today, dryRun, nil)
			if err != nil {
				return fmt.Errorf("backfill failed: %w", err)
			}
			syncPrintf("   • Backfill processed %d day(s), %.1fh planned, took %s\n",
				backfillResult.ProcessedDays,
				backfillResult.TotalMinutes/60,
				backfillResult.Duration.Round(time.Millisecond))

			monthlyStatus, err := manager.GetMonthlyStatus(monthStart, today)
			if err != nil {
				logger.Warn("Failed to calculate month-to-date status", zap.Error(err))
			} else {
				syncPrintf("\n📊 Month-to-date (%s to %s)\n",
					monthStart.Format("2006-01-02"),
					today.Format("2006-01-02"))
				syncPrintln("═══════════════════════════════════════════════════════")
				syncPrintf("  Working days:   %d  - рабочие дни по графику\n", monthlyStatus.WorkingDays)
				syncPrintf("  Target hours:   %.1fh (%.0f minutes)  - норматив по календарю\n", monthlyStatus.TargetMinutes/60, monthlyStatus.TargetMinutes)
				syncPrintf("  Logged hours:   %.1fh (%.0f minutes)  - уже списано в Tracker\n", monthlyStatus.WorkedMinutes/60, monthlyStatus.WorkedMinutes)
				cleanupDays := 0
				cleanupHours := 0.0
				if normalizeSummary != nil {
					cleanupDays = normalizeSummary.NormalizedDays
					cleanupHours = normalizeSummary.TotalMinutesTrimmed / 60
				}
				syncPrintf("  Cleanup days:   %d (%.1fh removed)  - переработка снята автоматически\n", cleanupDays, cleanupHours)
				syncPrintf("  Backfill days:  %d (%.1fh planned)  - найдено незакрытых рабочих дней\n", backfillResult.ProcessedDays, backfillResult.TotalMinutes/60)
				remaining := monthlyStatus.RemainingMinutes()
				label := "Remaining"
				statusExplanation := "ещё нужно списать, чтобы совпасть с нормативом"
				if remaining < 0 {
					label = "Overage"
					statusExplanation = "переработка относительно норматива"
				}
				syncPrintf("  %s:        %.1fh (%.0f minutes)  - %s\n", label, math.Abs(remaining)/60, math.Abs(remaining), statusExplanation)

				if len(monthlyStatus.Daily) > 0 {
					syncPrintln("\n📅 Per-day breakdown:")
					syncPrintln("═══════════════════════════════════════════════════════")
					syncPrintln("  Date         | Target  | Logged  | Diff | Status")
					syncPrintln("---------------+---------+---------+---------+----------------")
					for _, day := range monthlyStatus.Daily {
						diff := day.WorkedMinutes - day.TargetMinutes
						statusText := dayStatusLabel(diff)
						syncPrintf("  %s | %5.1fh | %5.1fh | %s%5.1fh | %s\n",
							day.Date.Format("2006-01-02"),
							day.TargetMinutes/60,
							day.WorkedMinutes/60,
							signLabel(diff),
							math.Abs(diff)/60,
							statusText)
					}
					syncPrintln("\nLegend: '+' = лишнего залогировано, '-' = не хватает; Status=detailed text.")
				}
			}

			if !dryRun {
				syncPrintf("⏳ Step 3/3: filling today (%s)\n", today.Format("2006-01-02"))
				if _, err := manager.DistributeTimeForDate(today, false, timelines); err != nil {
					return fmt.Errorf("failed to distribute time: %w", err)
				}
				syncPrintln("\n✅ Sync completed: month-to-date backfilled and today logged")
			} else {
				syncPrintln("\n[DRY RUN] No worklogs were created")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview actions without creating worklogs")
	cmd.Flags().StringVar(&teeOutput, "tee-output", "logs/cli-sync.log", "Mirror sync output to file (empty to disable)")

	return cmd
}

func syncPrintf(format string, a ...interface{}) {
	if syncWriter == nil {
		syncWriter = os.Stdout
	}
	fmt.Fprintf(syncWriter, format, a...)
}

func syncPrintln(a ...interface{}) {
	if syncWriter == nil {
		syncWriter = os.Stdout
	}
	fmt.Fprintln(syncWriter, a...)
}

func initializeManager(cfg *config.Config) (*timemanager.Manager, error) {
	// Initialize IAM token manager
	tokenManager := tracker.NewTokenManager(
		cfg.IAM.GetRefreshInterval(),
		cfg.IAM.CLICommand,
		cfg.IAM.InitCommand,
		cfg.IAM.FederationID,
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
	// Setup lumberjack for log rotation
	logWriter := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    100,  // MB
		MaxBackups: 3,    // Keep max 3 old log files
		MaxAge:     28,   // days
		Compress:   true, // Compress old logs with gzip
	}

	// Setup encoder
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Parse log level
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		zapLevel = zapcore.InfoLevel
	}

	// Create core with lumberjack writer
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(logWriter),
		zapLevel,
	)

	return zap.New(core), nil
}

func getIcon(dryRun bool) string {
	if dryRun {
		return "📋"
	}
	return "✅"
}

func signLabel(value float64) string {
	if value >= 0 {
		return "+"
	}
	return "-"
}

func dayStatusLabel(diff float64) string {
	if math.Abs(diff) < 0.01 {
		return "ok"
	}
	if diff > 0 {
		return fmt.Sprintf("лишнее +%.1fh", diff/60)
	}
	return fmt.Sprintf("не хватает %.1fh", math.Abs(diff)/60)
}
