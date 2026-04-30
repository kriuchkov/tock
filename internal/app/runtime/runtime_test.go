package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/config"
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
