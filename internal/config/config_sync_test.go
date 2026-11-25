package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncHome points HOME at a fresh directory and clears the sync variables, so Load sees neither the
// real home nor the caller's environment.
func syncHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TOCK_SYNC_ENABLED", "")
	t.Setenv("TOCK_SYNC_DIR", "")
	return home
}

func TestLoadSyncDefaults(t *testing.T) {
	home := syncHome(t)

	cfg, _, err := Load(WithConfigFile(filepath.Join(t.TempDir(), "tock.yaml")))
	require.NoError(t, err)

	assert.False(t, cfg.Sync.Enabled)
	assert.Equal(t, filepath.Join(home, "Library", "Mobile Documents", "iCloud~com~kriuchkov~tock", "Documents", "sync"), cfg.Sync.Dir)
	assert.Equal(t, DefaultSyncDir(home), cfg.Sync.Dir)
	assert.NoDirExists(t, cfg.Sync.Dir, "loading the config must not create the sync folder")
}

func TestLoadSyncFromFileExpandsTilde(t *testing.T) {
	home := syncHome(t)
	configPath := filepath.Join(t.TempDir(), "tock.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("sync:\n  enabled: true\n  dir: ~/tock-sync\n"), 0o600))

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)

	assert.True(t, cfg.Sync.Enabled)
	assert.Equal(t, filepath.Join(home, "tock-sync"), cfg.Sync.Dir)
}

func TestLoadSyncFromEnv(t *testing.T) {
	home := syncHome(t)
	configPath := filepath.Join(t.TempDir(), "tock.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("sync:\n  enabled: false\n  dir: /elsewhere\n"), 0o600))
	t.Setenv("TOCK_SYNC_ENABLED", "true")
	t.Setenv("TOCK_SYNC_DIR", "~/from-env")

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)

	assert.True(t, cfg.Sync.Enabled)
	assert.Equal(t, filepath.Join(home, "from-env"), cfg.Sync.Dir)
}

func TestExpandHome(t *testing.T) {
	tests := []struct {
		path string
		home string
		want string
	}{
		{"~", "/home/u", "/home/u"},
		{"~/a/b", "/home/u", filepath.Join("/home/u", "a", "b")},
		{"/abs/path", "/home/u", "/abs/path"},
		{"~other/x", "/home/u", "~other/x"},
		{"relative", "/home/u", "relative"},
		{"~/a", "", "~/a"},
		{"", "/home/u", ""},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, expandHome(tt.path, tt.home))
		})
	}
}

func TestEnableSyncWritesTheConfigFile(t *testing.T) {
	syncHome(t)
	configPath := filepath.Join(t.TempDir(), "tock.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("backend: sqlite\nmcp:\n  enabled: true\n"), 0o600))

	_, v, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)
	require.NoError(t, EnableSync(v, "/srv/tock-sync"))

	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)
	assert.True(t, cfg.Sync.Enabled)
	assert.Equal(t, "/srv/tock-sync", cfg.Sync.Dir)
	assert.Equal(t, "sqlite", cfg.Backend)
	assert.True(t, cfg.MCP.Enabled)
}

func TestEnableSyncKeepsTheDirWhenNoneIsGiven(t *testing.T) {
	syncHome(t)
	configPath := filepath.Join(t.TempDir(), "tock.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("sync:\n  dir: ~/tock-sync\n"), 0o600))

	_, v, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)
	require.NoError(t, EnableSync(v, ""))

	content, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "~/tock-sync", "the dir is written back as the user wrote it")
	cfg, _, err := Load(WithConfigFile(configPath))
	require.NoError(t, err)
	assert.True(t, cfg.Sync.Enabled)
}
