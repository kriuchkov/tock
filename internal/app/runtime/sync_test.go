package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-faster/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/ports"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
)

// loadWithSync loads a runtime for a file backend in a fresh home, with sync.enabled as given.
func loadWithSync(t *testing.T, enabled bool) (*Runtime, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"TOCK_BACKEND", "TOCK_FILE", "TOCK_FILE_PATH", "TOCK_SYNC_ENABLED", "TOCK_SYNC_DIR"} {
		t.Setenv(name, "")
	}

	configPath := filepath.Join(t.TempDir(), "tock.yaml")
	content := "backend: file\nfile:\n  path: " + filepath.Join(home, "data", "tock.txt") + "\n"
	if enabled {
		content += "sync:\n  enabled: true\n"
	}
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))

	rt, err := Load(context.Background(), Request{ConfigPath: configPath})
	require.NoError(t, err)
	return rt, home
}

// assertNothingCreated checks that loading created no sync state, sync folder or sidecar in home.
func assertNothingCreated(t *testing.T, rt *Runtime, home string) {
	t.Helper()
	assert.NoDirExists(t, filepath.Join(home, ".tock"))
	assert.NoDirExists(t, filepath.Join(home, "data"))
	assert.NoDirExists(t, rt.Config.Sync.Dir)
	assert.NoDirExists(t, filepath.Join(home, "Library"))
}

func TestLoadWithoutSyncBuildsNoSyncService(t *testing.T) {
	rt, home := loadWithSync(t, false)

	assert.Nil(t, rt.Sync)
	assertNothingCreated(t, rt, home)
}

func TestLoadWithSyncBuildsTheServiceWithoutTouchingDisk(t *testing.T) {
	rt, home := loadWithSync(t, true)

	assert.NotNil(t, rt.Sync)
	container := filepath.Join(home, "Library", "Mobile Documents", "iCloud~com~kriuchkov~tock")
	assert.Equal(t, filepath.Join(container, "Documents", "sync"), rt.Config.Sync.Dir)
	assertNothingCreated(t, rt, home)
}

func TestSyncStatePathIsOutsideTheSyncFolder(t *testing.T) {
	assert.Equal(t, filepath.Join("/home/u", ".tock", "sync", "state.db"), syncStatePath("/home/u"))
}

func TestDeviceProfile(t *testing.T) {
	profile := deviceProfile("2.4.0")
	assert.Equal(t, "tock 2.4.0", profile.App)
	assert.NotContains(t, profile.Name, ".local")
	assert.NotEmpty(t, profile.Platform)
	assert.Empty(t, profile.ID, "the sync service adds the ID")

	assert.Equal(t, "tock", deviceProfile("").App)
}

func TestLoadWithSyncMarksRecordedWrites(t *testing.T) {
	rt, _ := loadWithSync(t, true)

	require.NotNil(t, rt.SyncWrites)
	assert.False(t, rt.SyncWrites.Take())
	require.NotNil(t, rt.BuildSync)
	built, err := rt.BuildSync()
	require.NoError(t, err)
	assert.Same(t, rt.Sync, built, "with sync on, init uses the runtime's own service")
}

func TestLoadWithoutSyncBuildsItOnlyForInit(t *testing.T) {
	rt, home := loadWithSync(t, false)

	assert.Nil(t, rt.SyncWrites)
	assert.False(t, rt.SyncWrites.Take(), "a nil flag never reports a write")
	require.NotNil(t, rt.BuildSync)
	built, err := rt.BuildSync()
	require.NoError(t, err)
	assert.NotNil(t, built)
	assert.Nil(t, rt.Sync, "building it for init does not turn sync on")
	assertNothingCreated(t, rt, home)
}

func TestWriteFlag(t *testing.T) {
	var flag WriteFlag
	assert.False(t, flag.Take())
	flag.Mark()
	flag.Mark()
	assert.True(t, flag.Take())
	assert.False(t, flag.Take())
}

func TestMarkingSinkMarksOnlyWritesThatHappened(t *testing.T) {
	sink := portsmocks.NewMockSyncChangeSink(t)
	sink.EXPECT().Record(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, write func(context.Context) (ports.SyncWrite, error)) error {
			_, err := write(ctx)
			return err
		})
	flag := &WriteFlag{}
	marking := markingSink{sink: sink, writes: flag}

	failed := errors.New("disk full")
	err := marking.Record(context.Background(), func(context.Context) (ports.SyncWrite, error) {
		return ports.SyncWrite{}, failed
	})
	require.ErrorIs(t, err, failed)
	assert.False(t, flag.Take())

	require.NoError(t, marking.Record(context.Background(), func(context.Context) (ports.SyncWrite, error) {
		return ports.SyncWrite{Kind: ports.SyncActivitySaved}, nil
	}))
	assert.True(t, flag.Take())
}

func TestEnableSyncWritesTheConfig(t *testing.T) {
	rt, _ := loadWithSync(t, false)
	dir := filepath.Join(t.TempDir(), "shared")

	require.NoError(t, rt.EnableSync(dir))
	assert.True(t, rt.Config.Sync.Enabled)
	assert.Equal(t, dir, rt.Config.Sync.Dir)

	cfg, _, err := config.Load(config.WithConfigFile(rt.Viper.ConfigFileUsed()))
	require.NoError(t, err)
	assert.True(t, cfg.Sync.Enabled)
	assert.Equal(t, dir, cfg.Sync.Dir)

	assert.Error(t, (&Runtime{}).EnableSync(dir), "without a config file there is nothing to write")
}

// SyncEnabled reads the config again: a process that outlives a change of sync.enabled sees it.
func TestSyncEnabledReadsTheConfigAgain(t *testing.T) {
	rt, _ := loadWithSync(t, true)
	require.True(t, rt.SyncEnabled())

	configPath := rt.Viper.ConfigFileUsed()
	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, []byte(strings.Replace(string(raw), "enabled: true", "enabled: false", 1)), 0o600))
	assert.False(t, rt.SyncEnabled())
	assert.True(t, rt.Config.Sync.Enabled, "the loaded config stays as it was")
}

// A sqlite database that once synced loses its change log while sync is off.
func TestLoadWithoutSyncDropsTheSQLiteChangeLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"TOCK_BACKEND", "TOCK_FILE", "TOCK_FILE_PATH", "TOCK_SQLITE_PATH", "TOCK_SYNC_ENABLED", "TOCK_SYNC_DIR"} {
		t.Setenv(name, "")
	}
	dbPath := filepath.Join(t.TempDir(), "tock.db")
	ctx := context.Background()
	repos, err := initRepositories(ctx, backendSqlite, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repos.db.Close() })
	_, err = newSyncBackend(backendSqlite, dbPath, repos).Token(ctx)
	require.NoError(t, err)
	countTriggers := func() int {
		var n int
		require.NoError(t, repos.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name LIKE 'sync_%'`).Scan(&n))
		return n
	}
	require.Positive(t, countTriggers())

	configPath := filepath.Join(t.TempDir(), "tock.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("backend: sqlite\nsqlite:\n  path: "+dbPath+"\n"), 0o600))
	rt, err := Load(ctx, Request{ConfigPath: configPath})
	require.NoError(t, err)
	assert.Nil(t, rt.Sync)
	assert.Zero(t, countTriggers())
}

// The backend's identity is its data path made absolute, with the links of the part that exists
// resolved: a relative path read from another directory, as by the menu bar icon that launchd starts
// in /, never passes for the file sync was set up with.
func TestSyncIdentityIsCanonical(t *testing.T) {
	dir := t.TempDir()
	resolvedDir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(dir, link))

	// A file that is not there yet keeps the identity it gets once it is.
	before := canonicalPath(filepath.Join(link, "data", "tock.txt"))
	assert.Equal(t, filepath.Join(resolvedDir, "data", "tock.txt"), before)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "data"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "data", "tock.txt"), nil, 0o600))
	assert.Equal(t, before, canonicalPath(filepath.Join(link, "data", "tock.txt")))

	t.Chdir(dir)
	here := newSyncBackend("file", "tock.txt", repositories{}).Identity()
	assert.Equal(t, filepath.Join(resolvedDir, "tock.txt"), here.Path)
	t.Chdir("/")
	assert.Equal(t, "/tock.txt", newSyncBackend("file", "tock.txt", repositories{}).Identity().Path,
		"the same relative path read from / is another file")
}
