package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "nonexistent.yaml")

	cfg, _, err := Load(WithConfigFile(configFile))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check defaults
	assert.Equal(t, "file", cfg.Backend)
	assert.Equal(t, "eng", cfg.Language)
	assert.Equal(t, 7, cfg.Calendar.UpcomingDays)
	assert.False(t, cfg.MCP.Enabled)
	assert.False(t, cfg.MCP.ReadOnly)
	assert.Empty(t, cfg.MCP.DisabledTools)
	assert.FileExists(t, configFile)
}

func TestLoadFromFile(t *testing.T) {
	// Clean env to prevent interference
	t.Setenv("TOCK_BACKEND", "")
	t.Setenv("TOCK_FILE", "")
	t.Setenv("TOCK_FILE_PATH", "")
	t.Setenv("TOCK_TODOTXT_PATH", "")
	t.Setenv("TOCK_TIMEWARRIOR_DATA_PATH", "")
	t.Setenv("TOCK_THEME", "")
	t.Setenv("TOCK_THEME_NAME", "")
	t.Setenv("TOCK_COLOR_PRIMARY", "")

	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tock.yaml")

	configContent := `backend: timewarrior
language: eng
file:
  path: /custom/path/tock.txt
timewarrior:
  data_path: /custom/timewarrior/data
theme:
  name: custom
  primary: "#ff0000"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load config using the explicit file
	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check loaded values
	assert.Equal(t, "timewarrior", cfg.Backend)
	assert.Equal(t, "eng", cfg.Language)
	assert.Equal(t, "/custom/path/tock.txt", cfg.File.Path)
	assert.Equal(t, "/custom/timewarrior/data", cfg.Timewarrior.DataPath)
	assert.Equal(t, "custom", cfg.Theme.Name)
	assert.Equal(t, "#ff0000", cfg.Theme.Primary)
}

func TestEnvironmentOverrides(t *testing.T) {
	// Set environment variables
	t.Setenv("TOCK_LOG_LEVEL", "warn")
	t.Setenv("TOCK_BACKEND", "file")
	t.Setenv("TOCK_LANG", "eng")
	// Use the alias TOCK_FILE instead of TOCK_FILE_PATH to verify alias binding
	t.Setenv("TOCK_FILE", "/env/path/tock.txt")
	t.Setenv("TOCK_THEME", "light")
	t.Setenv("TOCK_CALENDAR_UPCOMING_DAYS", "3")

	// Create a config file with conflicting values
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tock.yaml")

	configContent := `log_level: debug
backend: timewarrior
file:
  path: /config/path/tock.txt
theme:
  name: dark
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Load config - env vars should override file values
	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)

	assert.Equal(t, "file", cfg.Backend)
	assert.Equal(t, "eng", cfg.Language)
	assert.Equal(t, "/env/path/tock.txt", cfg.File.Path)
	assert.Equal(t, "light", cfg.Theme.Name)
	assert.Equal(t, 3, cfg.Calendar.UpcomingDays)
}

func TestInitialCreationFromEnv(t *testing.T) {
	t.Setenv("TOCK_BACKEND", "timewarrior")
	t.Setenv("TOCK_THEME", "matrix")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "init.yaml")

	require.NoFileExists(t, configPath)

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)

	// Check in-memory config
	assert.Equal(t, "timewarrior", cfg.Backend)
	assert.Equal(t, "matrix", cfg.Theme.Name)

	// Check file content
	assert.FileExists(t, configPath)
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)

	sContent := string(content)
	assert.Contains(t, sContent, "backend: timewarrior")
	assert.Contains(t, sContent, "name: matrix")
}

func TestLoadWorkingHoursFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tock.yaml")

	configContent := `working_hours:
  enabled: true
  stop_at: "17:30"
  weekdays: "mon,tue,wed,thu,fri"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)

	assert.True(t, cfg.WorkingHours.Enabled)
	assert.Equal(t, "17:30", cfg.WorkingHours.StopAt)
	assert.Equal(t, "mon,tue,wed,thu,fri", cfg.WorkingHours.Weekdays)
}

func TestWorkingHoursEnvironmentOverrides(t *testing.T) {
	t.Setenv("TOCK_WORKING_HOURS_ENABLED", "true")
	t.Setenv("TOCK_WORKING_HOURS_STOP_AT", "18:15")
	t.Setenv("TOCK_WORKING_HOURS_WEEKDAYS", "mon,wed,fri")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tock.yaml")

	configContent := `working_hours:
  enabled: false
  stop_at: "17:30"
  weekdays: "mon,tue,wed,thu,fri"
`
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)

	assert.True(t, cfg.WorkingHours.Enabled)
	assert.Equal(t, "18:15", cfg.WorkingHours.StopAt)
	assert.Equal(t, "mon,wed,fri", cfg.WorkingHours.Weekdays)
}

func TestTimewarriorUseTockTagColorsEnvOverride(t *testing.T) {
	t.Setenv("TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS", "true")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tock.yaml")
	configContent := `timewarrior:
  use_tock_tag_colors: false
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)
	assert.True(t, cfg.Timewarrior.UseTockTagColors)
}

func TestTimewarriorUseTockTagColorsScopedEnvOverrides(t *testing.T) {
	t.Setenv("TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_CALENDAR", "true")
	t.Setenv("TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_WEEKLY_ACTIVITY", "true")
	t.Setenv("TOCK_TIMEWARRIOR_USE_TOCK_TAG_COLORS_TOP_PROJECTS", "false")

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tock.yaml")
	configContent := `timewarrior:
  use_tock_tag_colors_calendar: false
  use_tock_tag_colors_weekly_activity: false
  use_tock_tag_colors_top_projects: true
`
	err := os.WriteFile(configPath, []byte(configContent), 0o644)
	require.NoError(t, err)

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)
	assert.True(t, cfg.Timewarrior.UseTockTagColorsCalendar)
	assert.True(t, cfg.Timewarrior.UseTockTagColorsWeeklyActivity)
	assert.False(t, cfg.Timewarrior.UseTockTagColorsTopProjects)
}

func TestThemeColorEnvironment(t *testing.T) {
	t.Setenv("TOCK_THEME", "custom")
	t.Setenv("TOCK_THEME_BASE", "mocha")
	t.Setenv("TOCK_COLOR_TAG", "34")
	t.Setenv("TOCK_COLOR_SURFACE", "236")
	t.Setenv("TOCK_COLOR_OK", "120")
	t.Setenv("TOCK_COLOR_WARN", "208")

	cfg, _, err := Load(WithConfigFile(filepath.Join(t.TempDir(), "tock.yaml")))
	require.NoError(t, err)

	assert.Equal(t, "custom", cfg.Theme.Name)
	assert.Equal(t, "mocha", cfg.Theme.Base)
	assert.Equal(t, "34", cfg.Theme.Tag)
	assert.Equal(t, "236", cfg.Theme.Surface)
	assert.Equal(t, "120", cfg.Theme.OK)
	assert.Equal(t, "208", cfg.Theme.Warn)
}

func TestLoadMCPFromFileAndEnv(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "tock.yaml")
	configContent := `mcp:
  enabled: false
  disabled_tools: [remove, remind_remove]
`
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))
	t.Setenv("TOCK_MCP_ENABLED", "true")
	t.Setenv("TOCK_MCP_READ_ONLY", "true")

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)

	assert.True(t, cfg.MCP.Enabled)
	assert.True(t, cfg.MCP.ReadOnly)
	assert.Equal(t, []string{"remove", "remind_remove"}, cfg.MCP.DisabledTools)
}
