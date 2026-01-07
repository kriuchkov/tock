package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	// Load config without any file
	cfg, err := Load(WithConfigPath("/nonexistent"))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check defaults
	assert.Equal(t, "file", cfg.Backend)
	assert.Equal(t, "dark", cfg.Theme.Name)
}

func TestLoadFromFile(t *testing.T) {
	// Ensure no env vars interfere
	oldTockFile := os.Getenv("TOCK_FILE")
	os.Unsetenv("TOCK_FILE")
	defer func() {
		if oldTockFile != "" {
			os.Setenv("TOCK_FILE", oldTockFile)
		}
	}()

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

	// Verify file was created
	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	t.Logf("File content:\n%s", string(content))
	t.Logf("Config path: %s", tmpDir)

	// Load config from temp directory
	cfg, err := Load(WithConfigPath(tmpDir))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	t.Logf("Loaded config: %+v", cfg)

	// Check loaded values
	assert.Equal(t, "timewarrior", cfg.Backend)
	assert.Equal(t, "/custom/path/tock.txt", cfg.File.Path)
	assert.Equal(t, "/custom/timewarrior/data", cfg.Timewarrior.DataPath)
	assert.Equal(t, "custom", cfg.Theme.Name)
	assert.Equal(t, "#ff0000", cfg.Theme.Primary)
}

func TestEnvironmentOverrides(t *testing.T) {
	// Set environment variables
	os.Setenv("TOCK_LOG_LEVEL", "warn")
	os.Setenv("TOCK_BACKEND", "file")
	os.Setenv("TOCK_FILE_PATH", "/env/path/tock.txt")
	os.Setenv("TOCK_THEME_NAME", "light")
	defer func() {
		os.Unsetenv("TOCK_LOG_LEVEL")
		os.Unsetenv("TOCK_BACKEND")
		os.Unsetenv("TOCK_FILE_PATH")
		os.Unsetenv("TOCK_THEME_NAME")
	}()

	// Create a config file with different values
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
	cfg, err := Load(WithConfigPath(tmpDir))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Environment variables should override config file
	assert.Equal(t, "file", cfg.Backend)
	assert.Equal(t, "/env/path/tock.txt", cfg.File.Path)
	assert.Equal(t, "light", cfg.Theme.Name)
}

func TestCustomConfigName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom.yaml")

	configContent := `log_level: error
backend: file
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(
		WithConfigPath(tmpDir),
		WithConfigName("custom"),
	)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, "file", cfg.Backend)
}
