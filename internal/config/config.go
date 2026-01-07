package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/go-faster/errors"
	"github.com/spf13/viper"
)

type Config struct {
	Backend     string            `mapstructure:"backend"`
	File        FileConfig        `mapstructure:"file"`
	Timewarrior TimewarriorConfig `mapstructure:"timewarrior"`
	Theme       ThemeConfig       `mapstructure:"theme"`
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

func Load(opts ...Option) (*Config, error) {
	var err error
	v := viper.New()

	v.SetConfigName("tock")
	v.SetConfigType("yaml")

	var homeDir string
	if homeDir, err = os.UserHomeDir(); err == nil {
		configDir := filepath.Join(homeDir, ".config", "tock")

		if err := os.MkdirAll(configDir, 0755); err == nil {
			v.SetConfigFile(filepath.Join(configDir, "tock.yaml"))
		}
	}

	v.AddConfigPath(".")
	v.AutomaticEnv()

	v.SetDefault("backend", "file")
	v.SetDefault("file.path", filepath.Join(homeDir, ".tock.txt"))

	// Enable environment variable overrides
	v.SetEnvPrefix("TOCK")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind legacy/specific environment variables
	_ = v.BindEnv("theme.name", "TOCK_THEME", "TOCK_THEME_NAME")
	_ = v.BindEnv("theme.primary", "TOCK_COLOR_PRIMARY")
	_ = v.BindEnv("theme.secondary", "TOCK_COLOR_SECONDARY")
	_ = v.BindEnv("theme.text", "TOCK_COLOR_TEXT")
	_ = v.BindEnv("theme.sub_text", "TOCK_COLOR_SUBTEXT")
	_ = v.BindEnv("theme.faint", "TOCK_COLOR_FAINT")
	_ = v.BindEnv("theme.highlight", "TOCK_COLOR_HIGHLIGHT")

	for _, opt := range opts {
		opt(v)
	}

	if err := v.ReadInConfig(); err != nil {
		if err := v.WriteConfigAs(v.ConfigFileUsed()); err != nil {
			return nil, errors.Wrap(err, "write default config")
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
