package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
)

func configWithTimewarriorPath(p string) *config.Config {
	return &config.Config{Timewarrior: config.TimewarriorConfig{DataPath: p}}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		input string
		want  string
	}{
		{"~", home},
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"~/.timewarrior/data", filepath.Join(home, ".timewarrior/data")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, expandTilde(tt.input))
		})
	}
}

func TestResolveFilePathExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	cfg := configWithTimewarriorPath("~/.timewarrior/data")
	got := resolveFilePath(backendTimewarrior, "", cfg)
	assert.Equal(t, filepath.Join(home, ".timewarrior/data"), got)
}

func TestResolveFilePathExplicitOverrideExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	cfg := configWithTimewarriorPath("/some/path")
	got := resolveFilePath(backendTimewarrior, "~/.local/share/timewarrior/data", cfg)
	assert.Equal(t, filepath.Join(home, ".local/share/timewarrior/data"), got)
}

func TestBuildTagColors_ConfigOnly(t *testing.T) {
	cfgColors := map[string]string{"Work": "3", "Coding": "33"}
	got := buildTagColors(cfgColors, "file", "/irrelevant/path", "", false)
	assert.Equal(t, "3", got["Work"].FG)
	assert.Equal(t, "33", got["Coding"].FG)
	assert.Len(t, got, 2)
}

func TestBuildTagColors_TimewarriorOverridesConfig(t *testing.T) {
	// Build a temporary directory tree that mirrors ~/.timewarrior/:
	//   <tmp>/timewarrior.cfg   ← parsed by ParseTagColors(filepath.Dir(dataDir))
	//   <tmp>/data/             ← passed as dataDir
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o700))

	cfgContent := "tags.Coding.color = color33\n" +
		"tags.Development.color = color2\n" +
		"tags.Meetings.color = color196\n" +
		"tags.Lunch.color = gray8\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "timewarrior.cfg"), []byte(cfgContent), 0o600))

	cfgColors := map[string]string{
		"Work":  "3",  // config-only, no timewarrior entry
		"Extra": "99", // config-only, no timewarrior entry
	}
	got := buildTagColors(cfgColors, backendTimewarrior, dataDir, "", false)

	assert.Equal(t, "3", got["Work"].FG)
	assert.Equal(t, "33", got["Coding"].FG)
	assert.Equal(t, "2", got["Development"].FG)
	assert.Equal(t, "196", got["Meetings"].FG)
	assert.Equal(t, "240", got["Lunch"].FG) // gray8 = 232+8
	assert.Equal(t, "99", got["Extra"].FG)  // config-only entry survives
}

func TestBuildTagColors_TimewarriorCfgMissing(t *testing.T) {
	cfgColors := map[string]string{"Work": "3"}
	got := buildTagColors(cfgColors, backendTimewarrior, "/nonexistent/data", "", false)
	assert.Equal(t, map[string]models.TagColor{"Work": {FG: "3"}}, got)
}

func TestBuildTagColors_UseTockTagColors_DisablesTimewarriorOverride(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o700))

	// Would normally override Coding from config.
	cfgContent := "tags.Coding.color = color196\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "timewarrior.cfg"), []byte(cfgContent), 0o600))

	cfgColors := map[string]string{"Coding": "33", "Work": "3"}
	got := buildTagColors(cfgColors, backendTimewarrior, dataDir, "", true)

	assert.Equal(t, "33", got["Coding"].FG)
	assert.Equal(t, "3", got["Work"].FG)
	assert.Len(t, got, 2)
}

func TestBuildTagColors_BothEmpty(t *testing.T) {
	got := buildTagColors(nil, "file", "", "", false)
	assert.Nil(t, got)
}
