package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/viper"
)

type Config struct {
	Backend         string             `mapstructure:"backend"`
	Language        string             `mapstructure:"language"`
	File            FileConfig         `mapstructure:"file"`
	TodoTXT         TodoTXTConfig      `mapstructure:"todotxt"`
	Timewarrior     TimewarriorConfig  `mapstructure:"timewarrior"`
	Sqlite          SqliteConfig       `mapstructure:"sqlite"`
	WorkingHours    WorkingHoursConfig `mapstructure:"working_hours"`
	Tray            TrayConfig         `mapstructure:"tray"`
	Reminders       RemindersConfig    `mapstructure:"reminders"`
	MCP             MCPConfig          `mapstructure:"mcp"`
	Sync            SyncConfig         `mapstructure:"sync"`
	Theme           ThemeConfig        `mapstructure:"theme"`
	Calendar        CalendarConfig     `mapstructure:"calendar"`
	TimeFormat      string             `mapstructure:"time_format"`
	Export          ExportConfig       `mapstructure:"export"`
	WeeklyTarget    time.Duration      `mapstructure:"weekly_target"`
	CheckUpdates    bool               `mapstructure:"check_updates"`
	LastUpdateCheck time.Time          `mapstructure:"last_update_check"`
}

type CalendarConfig struct {
	TimeSpentFormat   string `mapstructure:"time_spent_format"`
	TimeStartFormat   string `mapstructure:"time_start_format"`
	TimeEndFormat     string `mapstructure:"time_end_format"`
	TimeTotalFormat   string `mapstructure:"time_total_format"`
	AlignDurationLeft bool   `mapstructure:"align_duration_left"`
	// UpcomingDays is how many days the Upcoming block of the calendar covers, today included.
	UpcomingDays int `mapstructure:"upcoming_days"`
}

type ExportConfig struct {
	ICal ICalConfig `mapstructure:"ical"`
}

type ICalConfig struct {
	FileName string `mapstructure:"file_name"`
}

type FileConfig struct {
	Path string `mapstructure:"path"`
}

type TodoTXTConfig struct {
	Path string `mapstructure:"path"`
}

type TimewarriorConfig struct {
	DataPath                       string `mapstructure:"data_path"`
	ConfigPath                     string `mapstructure:"config_path"`         // optional explicit path to timewarrior.cfg
	UseTockTagColors               bool   `mapstructure:"use_tock_tag_colors"` // global: ignore tags.*.color from timewarrior.cfg and use tock theme.tag_colors
	UseTockTagColorsCalendar       bool   `mapstructure:"use_tock_tag_colors_calendar"`
	UseTockTagColorsWeeklyActivity bool   `mapstructure:"use_tock_tag_colors_weekly_activity"`
	UseTockTagColorsTopProjects    bool   `mapstructure:"use_tock_tag_colors_top_projects"`
}

type SqliteConfig struct {
	Path string `mapstructure:"path"`
}

type WorkingHoursConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	StopAt   string `mapstructure:"stop_at"`
	Weekdays string `mapstructure:"weekdays"`
}

// TrayConfig controls the macOS menu bar (status bar) integration. It is
// disabled by default and only has an effect on macOS via the `tock tray`
// command. When AutoStart is set, `tock start` spawns the menu bar icon in the
// background and it closes itself once the activity is stopped.
type TrayConfig struct {
	Enabled   bool `mapstructure:"enabled"`
	AutoStart bool `mapstructure:"auto_start"`
}

// MCPConfig controls the local MCP server (`tock mcp`) that lets AI clients read and
// change tracked time. It is off until Enabled is set.
type MCPConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// ReadOnly leaves out every tool that changes data.
	ReadOnly bool `mapstructure:"read_only"`
	// DisabledTools are tool names to leave out, such as "remove".
	DisabledTools []string `mapstructure:"disabled_tools"`
}

// SyncConfig controls sync with the tock iOS app through a folder in iCloud Drive (docs/sync.md).
// It is off until Enabled is set.
type SyncConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// Dir is the sync folder, with a leading ~ expanded. The default is the iOS app's iCloud
	// container, which shows up as a tock folder in iCloud Drive.
	Dir string `mapstructure:"dir"`
}

// RemindersConfig controls reminders. Notifications are sent by the macOS menu bar
// icon (`tock tray`), so they only go out while it runs.
type RemindersConfig struct {
	Notifications bool          `mapstructure:"notifications"`
	Snooze        time.Duration `mapstructure:"snooze"`
}

type ThemeConfig struct {
	Name string `mapstructure:"name"`
	// Base is the built-in theme a custom one starts from (dark by default).
	Base      string            `mapstructure:"base"`
	Primary   string            `mapstructure:"primary"`
	Secondary string            `mapstructure:"secondary"`
	Text      string            `mapstructure:"text"`
	SubText   string            `mapstructure:"sub_text"`
	Faint     string            `mapstructure:"faint"`
	Highlight string            `mapstructure:"highlight"`
	Tag       string            `mapstructure:"tag"`
	Reminder  string            `mapstructure:"reminder"`
	Surface   string            `mapstructure:"surface"`
	OK        string            `mapstructure:"ok"`
	Warn      string            `mapstructure:"warn"`
	TagColors map[string]string `mapstructure:"tag_colors"`
}

type Option func(*viper.Viper)

func WithConfigPath(path string) Option {
	return func(v *viper.Viper) {
		v.AddConfigPath(path)
	}
}

func WithConfigName(name string) Option {
	return func(v *viper.Viper) {
		v.SetConfigName(name)
	}
}

func WithConfigFile(file string) Option {
	return func(v *viper.Viper) {
		v.SetConfigFile(file)
	}
}

//nolint:funlen // load and setup configuration
func Load(opts ...Option) (*Config, *viper.Viper, error) {
	v := viper.New()
	var err error

	v.SetConfigName("tock")
	v.SetConfigType("yaml")

	var homeDir string
	var configPath string

	if homeDir, err = os.UserHomeDir(); err == nil {
		configDir := filepath.Join(homeDir, ".config", "tock")

		_ = os.MkdirAll(configDir, 0750)
		configPath = filepath.Join(configDir, "tock.yaml")
		v.SetConfigFile(configPath)
	}

	v.AddConfigPath(".")

	// Defaults
	v.SetDefault("backend", "file")
	v.SetDefault("language", "eng")
	v.SetDefault("time_format", "24")
	v.SetDefault("export.ical.file_name", "tock_export.ics")
	v.SetDefault("check_updates", true)
	v.SetDefault("working_hours.enabled", false)
	v.SetDefault("working_hours.stop_at", "")
	v.SetDefault("working_hours.weekdays", "mon,tue,wed,thu,fri")
	v.SetDefault("tray.enabled", false)
	v.SetDefault("tray.auto_start", false)
	v.SetDefault("mcp.enabled", false)
	v.SetDefault("mcp.read_only", false)
	v.SetDefault("mcp.disabled_tools", []string{})
	v.SetDefault("sync.enabled", false)
	v.SetDefault("reminders.notifications", true)
	v.SetDefault("reminders.snooze", "10m")
	v.SetDefault("calendar.upcoming_days", 7)
	v.SetDefault("timewarrior.use_tock_tag_colors", false)
	v.SetDefault("timewarrior.use_tock_tag_colors_calendar", false)
	v.SetDefault("timewarrior.use_tock_tag_colors_weekly_activity", false)
	v.SetDefault("timewarrior.use_tock_tag_colors_top_projects", false)

	if homeDir != "" {
		v.SetDefault("file.path", filepath.Join(homeDir, ".tock.txt"))
		v.SetDefault("sqlite.path", filepath.Join(homeDir, ".tock.db"))
		v.SetDefault("sync.dir", DefaultSyncDir(homeDir))
	}

	// Explicit Bindings for all supported variables
	_ = v.BindEnv("backend", "TOCK_BACKEND")
	_ = v.BindEnv("language", "TOCK_LANG")
	_ = v.BindEnv("todotxt.path", "TOCK_TODOTXT_PATH")
	_ = v.BindEnv("timewarrior.data_path", "TOCK_TIMEWARRIOR_DATA_PATH")
	_ = v.BindEnv("timewarrior.use_tock_tag_colors", "TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS")
	_ = v.BindEnv("timewarrior.use_tock_tag_colors_calendar", "TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_CALENDAR")
	_ = v.BindEnv("timewarrior.use_tock_tag_colors_weekly_activity", "TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_WEEKLY_ACTIVITY")
	_ = v.BindEnv("timewarrior.use_tock_tag_colors_top_projects", "TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_TOP_PROJECTS")
	_ = v.BindEnv("sqlite.path", "TOCK_SQLITE_PATH")
	_ = v.BindEnv("file.path", "TOCK_FILE", "TOCK_FILE_PATH")
	_ = v.BindEnv("time_format", "TOCK_TIME_FORMAT")
	_ = v.BindEnv("export.ical.file_name", "TOCK_EXPORT_ICAL_FILE_NAME")
	_ = v.BindEnv("theme.name", "TOCK_THEME", "TOCK_THEME_NAME")
	_ = v.BindEnv("theme.primary", "TOCK_COLOR_PRIMARY")
	_ = v.BindEnv("theme.secondary", "TOCK_COLOR_SECONDARY")
	_ = v.BindEnv("theme.text", "TOCK_COLOR_TEXT")
	_ = v.BindEnv("theme.sub_text", "TOCK_COLOR_SUBTEXT")
	_ = v.BindEnv("theme.faint", "TOCK_COLOR_FAINT")
	_ = v.BindEnv("theme.highlight", "TOCK_COLOR_HIGHLIGHT")
	_ = v.BindEnv("theme.tag", "TOCK_COLOR_TAG")
	_ = v.BindEnv("theme.reminder", "TOCK_COLOR_REMINDER")
	_ = v.BindEnv("theme.base", "TOCK_THEME_BASE")
	_ = v.BindEnv("theme.surface", "TOCK_COLOR_SURFACE")
	_ = v.BindEnv("theme.ok", "TOCK_COLOR_OK")
	_ = v.BindEnv("theme.warn", "TOCK_COLOR_WARN")
	_ = v.BindEnv("working_hours.enabled", "TOCK_WORKING_HOURS_ENABLED")
	_ = v.BindEnv("working_hours.stop_at", "TOCK_WORKING_HOURS_STOP_AT")
	_ = v.BindEnv("working_hours.weekdays", "TOCK_WORKING_HOURS_WEEKDAYS")
	_ = v.BindEnv("tray.enabled", "TOCK_TRAY_ENABLED")
	_ = v.BindEnv("tray.auto_start", "TOCK_TRAY_AUTO_START")
	_ = v.BindEnv("mcp.enabled", "TOCK_MCP_ENABLED")
	_ = v.BindEnv("mcp.read_only", "TOCK_MCP_READ_ONLY")
	_ = v.BindEnv("sync.enabled", "TOCK_SYNC_ENABLED")
	_ = v.BindEnv("sync.dir", "TOCK_SYNC_DIR")
	_ = v.BindEnv("reminders.notifications", "TOCK_REMINDERS_NOTIFICATIONS")
	_ = v.BindEnv("reminders.snooze", "TOCK_REMINDERS_SNOOZE")
	_ = v.BindEnv("calendar.upcoming_days", "TOCK_CALENDAR_UPCOMING_DAYS")
	_ = v.BindEnv("weekly_target", "TOCK_WEEKLY_TARGET")
	_ = v.BindEnv("check_updates", "TOCK_CHECK_UPDATES")

	for _, opt := range opts {
		opt(v)
	}

	//nolint:nestif // config file handling
	if err = v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if errors.As(err, &configFileNotFoundError) || os.IsNotExist(err) {
			writePath := v.ConfigFileUsed()
			if writePath == "" {
				writePath = configPath
			}

			if writePath != "" {
				for _, key := range v.AllKeys() {
					val := v.Get(key)
					v.Set(key, val)
				}
				if err = v.WriteConfigAs(writePath); err != nil {
					return nil, nil, errors.Wrap(err, "write default config")
				}
			}
		} else {
			return nil, nil, err
		}
	} else {
		for _, key := range v.AllKeys() {
			val := v.Get(key)
			v.Set(key, val)
		}
	}

	var cfg Config
	if err = v.Unmarshal(&cfg); err != nil {
		return nil, nil, err
	}
	cfg.Sync.Dir = expandHome(cfg.Sync.Dir, homeDir)
	return &cfg, v, nil
}

// DefaultSyncDir is the default sync folder under home: the sync folder in the iOS app's iCloud
// Drive container, iCloud.com.kriuchkov.tock, which macOS keeps in ~/Library/Mobile Documents.
func DefaultSyncDir(home string) string {
	return filepath.Join(home, "Library", "Mobile Documents", "iCloud~com~kriuchkov~tock", "Documents", "sync")
}

// expandHome replaces a leading ~ of path with home; without a home the path stays as it is.
func expandHome(path, home string) string {
	if home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(home, rest)
	}
	return path
}

// EnableSync sets sync.enabled, and sync.dir unless dir is empty, in v and writes v back to the
// config file it was loaded from, as the update check does with its time.
func EnableSync(v *viper.Viper, dir string) error {
	v.Set("sync.enabled", true)
	if dir != "" {
		v.Set("sync.dir", dir)
	}
	if err := v.WriteConfig(); err != nil {
		return errors.Wrap(err, "write the config file")
	}
	return nil
}
