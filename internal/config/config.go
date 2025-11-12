package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/viper"
)

// Config represents application configuration
type Config struct {
	Tracker  TrackerConfig  `mapstructure:"tracker"`
	Calendar CalendarConfig `mapstructure:"calendar"`
	TimeRules TimeRulesConfig `mapstructure:"time_rules"`
	Daemon   DaemonConfig   `mapstructure:"daemon"`
	IAM      IAMConfig      `mapstructure:"iam"`
	State    StateConfig    `mapstructure:"state"`
}

// TrackerConfig represents Yandex Tracker configuration
type TrackerConfig struct {
	OrgID       string `mapstructure:"org_id"`
	APIEndpoint string `mapstructure:"api_endpoint"`
	BoardID     int    `mapstructure:"board_id"`
	IssuesQuery string `mapstructure:"issues_query"`
}

// CalendarConfig represents calendar configuration
type CalendarConfig struct {
	Type         string `mapstructure:"type"` // "isdayoff" or "production-calendar"
	FallbackURL  string `mapstructure:"fallback_url"` // For isdayoff type (xmlcalendar.ru)
	CacheTTL     string `mapstructure:"cache_ttl"`

	// Legacy fields for production-calendar type (backward compatibility)
	APIURL       string `mapstructure:"api_url"`
	APIToken     string `mapstructure:"api_token"`
	FallbackFile string `mapstructure:"fallback_file"`
	Country      string `mapstructure:"country"`
}

// TimeRulesConfig represents time distribution rules
type TimeRulesConfig struct {
	TargetHoursPerDay     int                 `mapstructure:"target_hours_per_day"`
	DailyTasks            []DailyTaskConfig   `mapstructure:"daily_tasks"`
	WeeklyTasks           []WeeklyTaskConfig  `mapstructure:"weekly_tasks"`
	RandomizationPercent  float64             `mapstructure:"randomization_percent"`
}

// DailyTaskConfig represents a daily task
type DailyTaskConfig struct {
	Issue       string `mapstructure:"issue"`
	Minutes     int    `mapstructure:"minutes"`
	Description string `mapstructure:"description"`
}

// WeeklyTaskConfig represents a weekly task
type WeeklyTaskConfig struct {
	Issue         string  `mapstructure:"issue"`
	HoursPerWeek  float64 `mapstructure:"hours_per_week"`
	DaysPerWeek   int     `mapstructure:"days_per_week"`
	Description   string  `mapstructure:"description"`
}

// DaemonConfig represents daemon mode configuration
type DaemonConfig struct {
	CheckInterval string `mapstructure:"check_interval"` // Deprecated: use DailyTime instead
	DailyTime     string `mapstructure:"daily_time"`     // Time to run daily sync (HH:MM format, MSK timezone)
	LogFile       string `mapstructure:"log_file"`
	LogLevel      string `mapstructure:"log_level"`
	SystemTray    bool   `mapstructure:"system_tray"` // Show system tray icon (Windows only)
}

// IAMConfig represents IAM token configuration
type IAMConfig struct {
	RefreshInterval string `mapstructure:"refresh_interval"`
	CLICommand      string `mapstructure:"cli_command"`
}

// StateConfig represents state storage configuration
type StateConfig struct {
	WeeklyScheduleFile string `mapstructure:"weekly_schedule_file"`
}

// Load loads configuration from file
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.time-tracker-bot")
		v.AddConfigPath("/etc/time-tracker-bot")
	}

	// Read environment variables
	v.AutomaticEnv()

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate Tracker config
	if c.Tracker.OrgID == "" {
		return fmt.Errorf("tracker.org_id is required")
	}
	if c.Tracker.APIEndpoint == "" {
		return fmt.Errorf("tracker.api_endpoint is required")
	}
	if c.Tracker.BoardID <= 0 {
		return fmt.Errorf("tracker.board_id must be positive")
	}
	if c.Tracker.IssuesQuery == "" {
		return fmt.Errorf("tracker.issues_query is required")
	}

	// Validate Calendar config
	calType := c.Calendar.Type
	if calType == "" {
		calType = "isdayoff" // Default to isdayoff
	}

	switch calType {
	case "isdayoff":
		if c.Calendar.FallbackURL == "" {
			return fmt.Errorf("calendar.fallback_url is required for isdayoff type")
		}
	case "production-calendar":
		if c.Calendar.APIURL == "" {
			return fmt.Errorf("calendar.api_url is required for production-calendar type")
		}
		if c.Calendar.APIToken == "" {
			return fmt.Errorf("calendar.api_token is required for production-calendar type")
		}
		if c.Calendar.Country == "" {
			return fmt.Errorf("calendar.country is required for production-calendar type")
		}
	default:
		return fmt.Errorf("calendar.type must be 'isdayoff' or 'production-calendar', got '%s'", calType)
	}

	// Validate TimeRules config
	if c.TimeRules.TargetHoursPerDay <= 0 {
		return fmt.Errorf("time_rules.target_hours_per_day must be positive")
	}
	if c.TimeRules.RandomizationPercent < 0 || c.TimeRules.RandomizationPercent > 100 {
		return fmt.Errorf("time_rules.randomization_percent must be between 0 and 100")
	}

	// Validate IAM config
	if c.IAM.CLICommand == "" {
		return fmt.Errorf("iam.cli_command is required")
	}

	return nil
}

// GetCacheTTL returns cache TTL duration
func (c *CalendarConfig) GetCacheTTL() time.Duration {
	if c.CacheTTL == "" {
		return 24 * time.Hour
	}
	duration, err := time.ParseDuration(c.CacheTTL)
	if err != nil {
		return 24 * time.Hour
	}
	return duration
}

// GetCheckInterval returns daemon check interval duration
func (c *DaemonConfig) GetCheckInterval() time.Duration {
	if c.CheckInterval == "" {
		return 2 * time.Hour
	}
	duration, err := time.ParseDuration(c.CheckInterval)
	if err != nil {
		return 2 * time.Hour
	}
	return duration
}

// GetDailyTime returns the configured daily sync time (MSK timezone)
// Returns hour and minute (0-23, 0-59). Default: 20:00
func (c *DaemonConfig) GetDailyTime() (hour, minute int) {
	if c.DailyTime == "" {
		return 20, 0 // Default: 20:00 MSK
	}

	var h, m int
	_, err := fmt.Sscanf(c.DailyTime, "%d:%d", &h, &m)
	if err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 20, 0 // Fallback to default
	}
	return h, m
}

// GetRefreshInterval returns IAM token refresh interval duration
func (c *IAMConfig) GetRefreshInterval() time.Duration {
	if c.RefreshInterval == "" {
		return 1 * time.Hour
	}
	duration, err := time.ParseDuration(c.RefreshInterval)
	if err != nil {
		return 1 * time.Hour
	}
	return duration
}

// ExpandEnvVars expands environment variables in config strings
func (c *Config) ExpandEnvVars() {
	c.Tracker.OrgID = os.ExpandEnv(c.Tracker.OrgID)
	c.Calendar.APIToken = os.ExpandEnv(c.Calendar.APIToken)
}
