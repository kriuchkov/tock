package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/viper"
)

type Config struct {
	Backend      string            `mapstructure:"backend"`
	File         FileConfig        `mapstructure:"file"`
	Timewarrior  TimewarriorConfig `mapstructure:"timewarrior"`
	Theme        ThemeConfig       `mapstructure:"theme"`
	TimeFormat   string            `mapstructure:"time_format"`
	Export       ExportConfig      `mapstructure:"export"`
	WeeklyTarget time.Duration     `mapstructure:"weekly_target"`
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

type TimewarriorConfig struct {
	DataPath string `mapstructure:"data_path"`
}

type ThemeConfig struct {
	Name      string `mapstructure:"name"`
	Primary   string `mapstructure:"primary"`
	Secondary string `mapstructure:"secondary"`
	Text      string `mapstructure:"text"`
	SubText   string `mapstructure:"sub_text"`
	Faint     string `mapstructure:"faint"`
	Highlight string `mapstructure:"highlight"`
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

func Load(opts ...Option) (*Config, error) {
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
	v.SetDefault("time_format", "24")
	v.SetDefault("export.ical.file_name", "tock_export.ics")

	if homeDir != "" {
		v.SetDefault("file.path", filepath.Join(homeDir, ".tock.txt"))
	}

	// Explicit Bindings for all supported variables
	_ = v.BindEnv("backend", "TOCK_BACKEND")
	_ = v.BindEnv("timewarrior.data_path", "TOCK_TIMEWARRIOR_DATA_PATH")
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
	_ = v.BindEnv("weekly_target", "TOCK_WEEKLY_TARGET")

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
					return nil, errors.Wrap(err, "write default config")
				}
			}
		} else {
			return nil, err
		}
	} else {
		for _, key := range v.AllKeys() {
			val := v.Get(key)
			v.Set(key, val)
		}
	}

	var cfg Config
	if err = v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
