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
	assert.FileExists(t, configFile)
}

func TestLoadFromFile(t *testing.T) {
	// Clean env to prevent interference
	t.Setenv("TOCK_BACKEND", "")
	t.Setenv("TOCK_FILE", "")
	t.Setenv("TOCK_FILE_PATH", "")
	t.Setenv("TOCK_TIMEWARRIOR_DATA_PATH", "")
	t.Setenv("TOCK_THEME", "")
	t.Setenv("TOCK_THEME_NAME", "")
	t.Setenv("TOCK_COLOR_PRIMARY", "")

	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tock.yaml")

	configContent := `backend: timewarrior
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
	assert.Equal(t, "/custom/path/tock.txt", cfg.File.Path)
	assert.Equal(t, "/custom/timewarrior/data", cfg.Timewarrior.DataPath)
	assert.Equal(t, "custom", cfg.Theme.Name)
	assert.Equal(t, "#ff0000", cfg.Theme.Primary)
}

func TestEnvironmentOverrides(t *testing.T) {
	// Set environment variables
	t.Setenv("TOCK_LOG_LEVEL", "warn")
	t.Setenv("TOCK_BACKEND", "file")
	// Use the alias TOCK_FILE instead of TOCK_FILE_PATH to verify alias binding
	t.Setenv("TOCK_FILE", "/env/path/tock.txt")
	t.Setenv("TOCK_THEME", "light")

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
	assert.Equal(t, "/env/path/tock.txt", cfg.File.Path)
	assert.Equal(t, "light", cfg.Theme.Name)
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
