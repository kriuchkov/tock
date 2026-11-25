package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/config"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/ports"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// newSyncTestCommand returns a command whose runtime has service as its sync service; nil leaves
// sync off. Its stdout and stderr are returned.
func newSyncTestCommand(t *testing.T, service ports.SyncResolver) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := newTestCLICommand(&stubActivityResolver{})
	rt, ok := appruntime.FromContext(cmd.Context())
	require.True(t, ok)
	rt.Sync = service
	rt.Config.Sync.Dir = "/tmp/tock-sync"
	if service != nil {
		rt.Config.Sync.Enabled = true
		rt.SyncWrites = &appruntime.WriteFlag{}
	}

	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// loadSyncConfig loads a config file with sync off in a fresh home, as tock would.
func loadSyncConfig(t *testing.T) (*appruntime.Runtime, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("TOCK_SYNC_ENABLED", "")
	t.Setenv("TOCK_SYNC_DIR", "")
	configPath := filepath.Join(t.TempDir(), "tock.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("backend: file\n"), 0o600))

	cfg, v, err := config.Load(config.WithConfigFile(configPath))
	require.NoError(t, err)
	return &appruntime.Runtime{
		Config:        cfg,
		Viper:         v,
		TimeFormatter: timeutil.NewFormatter("24"),
		Localizer:     localization.MustNew(localization.LanguageEnglish),
	}, configPath
}

func commandWithRuntime(rt *appruntime.Runtime) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(rt.WithContext(context.Background()))
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

func TestSyncCmdRefusesWhileDisabled(t *testing.T) {
	cmd, out, _ := newSyncTestCommand(t, nil)

	err := runSyncCmd(cmd, &syncOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tock sync init")
	assert.Empty(t, out.String())
}

func TestSyncCmdRunsTheService(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, ports.SyncOptions{Full: true}).Return(ports.SyncReport{Read: 3, Written: 2, Applied: 1}, nil)

	cmd, out, _ := newSyncTestCommand(t, service)
	require.NoError(t, runSyncCmd(cmd, &syncOptions{Full: true}))
	assert.Equal(t, "Synced: 3 changes read, 2 written, 1 applied to the backend.\n", out.String())
}

func TestSyncCmdReportsWhatNeedsAttention(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, ports.SyncOptions{}).Return(ports.SyncReport{
		Device:         "0123456789abcdef",
		ReplacedDevice: "7f3a9c2e5b1d4a60",
		Pending:        2,
		Skipped:        1,
		Unreadable:     []string{"journal/7f3a9c2e5b1d4a60/2026-11.jsonl"},
	}, nil)

	cmd, out, _ := newSyncTestCommand(t, service)
	require.NoError(t, runSyncCmd(cmd, &syncOptions{}))
	assert.Contains(t, out.String(), "this Mac now writes as 0123456789abcdef")
	assert.Contains(t, out.String(), "2 files are still downloading")
	assert.Contains(t, out.String(), "1 invalid lines")
	assert.Contains(t, out.String(), "journal/7f3a9c2e5b1d4a60/2026-11.jsonl")
}

func TestSyncCmdJSON(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, ports.SyncOptions{}).
		Return(ports.SyncReport{Device: "7f3a9c2e5b1d4a60", Read: 4, Compared: true}, nil)

	cmd, out, _ := newSyncTestCommand(t, service)
	require.NoError(t, runSyncCmd(cmd, &syncOptions{JSONOutput: true}))

	var got map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, "7f3a9c2e5b1d4a60", got["device"])
	assert.InDelta(t, 4, got["read"], 0)
	assert.Equal(t, true, got["compared"])
	assert.NotContains(t, got, "unreadable")
}

func TestSyncCmdExplainsWhatToDo(t *testing.T) {
	cases := map[error]string{
		coreErrors.ErrSyncNotInitialized:    "run 'tock sync init'",
		coreErrors.ErrSyncFormatUnsupported: "Update tock",
		coreErrors.ErrSyncBackendChanged:    "another backend or file than the one sync was set up with (check the folder)",
		coreErrors.ErrSyncBackendMissing:    "the backend's data at check the folder is missing",
		coreErrors.ErrSyncTooManyDeletions:  "run 'tock sync --allow-deletions'",
	}
	for sentinel, want := range cases {
		service := portsmocks.NewMockSyncResolver(t)
		service.EXPECT().Run(mock.Anything, mock.Anything).Return(ports.SyncReport{}, errors.Wrap(sentinel, "check the folder"))

		cmd, _, _ := newSyncTestCommand(t, service)
		err := runSyncCmd(cmd, &syncOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), want)
	}
}

// The brake offers the two ways out: tock sync init --restore, which writes the data again from the
// folder and keeps a copy of what it rewrites, and --allow-deletions, which deletes the records
// everywhere.
func TestSyncCmdOffersRestoreAndDeletingEverywhere(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, mock.Anything).Return(ports.SyncReport{}, errors.Wrap(coreErrors.ErrSyncTooManyDeletions, "120"))

	cmd, _, _ := newSyncTestCommand(t, service)
	err := runSyncCmd(cmd, &syncOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "120 records are gone")
	assert.Contains(t, err.Error(), "Nothing was written")
	assert.Contains(t, err.Error(), "run 'tock sync init --restore'")
	assert.Contains(t, err.Error(), ".before-restore-")
	assert.Contains(t, err.Error(), "stay only in that copy")
	assert.Contains(t, err.Error(), "run 'tock sync --allow-deletions'")
}

// tock sync --allow-deletions lets the run past the deletion brake.
func TestSyncCmdAllowsDeletions(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, ports.SyncOptions{AllowDeletions: true}).Return(ports.SyncReport{Written: 40}, nil)

	cmd := NewSyncCmd()
	syncCmd, _, _ := newSyncTestCommand(t, service)
	cmd.SetContext(syncCmd.Context())
	cmd.SetOut(syncCmd.OutOrStdout())
	cmd.SetArgs([]string{"--allow-deletions"})
	require.NoError(t, cmd.Execute())
}

// The deletion brake and a missing data file say what to do after a command, once, as no retry
// fixes them.
func TestSyncRecordedWritesExplainsTheBrake(t *testing.T) {
	for sentinel, want := range map[error]string{
		coreErrors.ErrSyncTooManyDeletions: "Sync stopped: 120 records are gone from the backend",
		coreErrors.ErrSyncBackendMissing:   "Sync stopped: the backend's data at /data/tock.txt is missing",
	} {
		detail := "120"
		if errors.Is(sentinel, coreErrors.ErrSyncBackendMissing) {
			detail = "/data/tock.txt"
		}
		service := portsmocks.NewMockSyncResolver(t)
		service.EXPECT().Run(mock.Anything, mock.Anything).Return(ports.SyncReport{}, errors.Wrap(sentinel, detail)).Once()
		cmd, _, errOut := newSyncTestCommand(t, service)

		getRuntime(cmd).SyncWrites.Mark()
		syncRecordedWrites(cmd.Context(), cmd)
		assert.Contains(t, errOut.String(), want)
		assert.NotContains(t, errOut.String(), "sync failed")
	}
}

func TestSyncCmdTakesTheRecordedWrites(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, mock.Anything).Return(ports.SyncReport{}, nil).Once()

	cmd, _, _ := newSyncTestCommand(t, service)
	getRuntime(cmd).SyncWrites.Mark()
	require.NoError(t, runSyncCmd(cmd, &syncOptions{}))

	// The hook after the command finds nothing left to send.
	syncRecordedWrites(cmd.Context(), cmd)
}

func TestSyncInitCmdPassesTheDir(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Init(mock.Anything, ports.SyncInitOptions{Dir: "/tmp/tock-sync"}).
		Return(ports.SyncReport{Device: "7f3a9c2e5b1d4a60"}, nil)

	cmd, out, errOut := newSyncTestCommand(t, service)
	require.NoError(t, runSyncInitCmd(cmd, &syncInitOptions{Dir: "/tmp/tock-sync"}))
	assert.Contains(t, out.String(), "Sync is on: /tmp/tock-sync (this device: 7f3a9c2e5b1d4a60)")
	assert.Empty(t, errOut.String(), "sync is on in that folder already, so the config stays as it is")
}

// tock sync init --restore asks for the restore and names each copy it made of the backend's files.
func TestSyncInitCmdRestores(t *testing.T) {
	backups := []string{"/data/tock.txt.before-restore-20261125-101500", "/data/.tock/notes.before-restore-20261125-101500"}
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Init(mock.Anything, ports.SyncInitOptions{Dir: "/tmp/tock-sync", Restore: true}).
		Return(ports.SyncReport{Device: "7f3a9c2e5b1d4a60", Applied: 3, Backups: backups}, nil).Twice()

	cmd, out, _ := newSyncTestCommand(t, service)
	require.NoError(t, runSyncInitCmd(cmd, &syncInitOptions{Dir: "/tmp/tock-sync", Restore: true}))
	for _, path := range backups {
		assert.Contains(t, out.String(), "A copy of the data as it was before sync wrote it again: "+path+"\n")
	}

	cmd, out, _ = newSyncTestCommand(t, service)
	require.NoError(t, runSyncInitCmd(cmd, &syncInitOptions{Dir: "/tmp/tock-sync", Restore: true, JSONOutput: true}))
	var got syncInitJSON
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, backups, got.Report.Backups)
}

// tock sync init --restore into a folder without records says why it did nothing and what to run.
func TestSyncInitCmdRefusesToRestoreFromAnEmptyFolder(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Init(mock.Anything, ports.SyncInitOptions{Dir: "/tmp/tock-sync", Restore: true}).
		Return(ports.SyncReport{}, coreErrors.ErrSyncNothingToRestore)

	cmd, _, _ := newSyncTestCommand(t, service)
	err := runSyncInitCmd(cmd, &syncInitOptions{Dir: "/tmp/tock-sync", Restore: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Nothing was restored")
	assert.Contains(t, err.Error(), "run 'tock sync init' without --restore")
}

func TestSyncInitCmdExplainsARefusedRestore(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{coreErrors.ErrSyncRestoreNotSynced, "Run 'tock sync init' without --restore first"},
		{coreErrors.ErrSyncRestorePending, "still downloading"},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			service := portsmocks.NewMockSyncResolver(t)
			service.EXPECT().Init(mock.Anything, ports.SyncInitOptions{Dir: "/tmp/tock-sync", Restore: true}).
				Return(ports.SyncReport{}, tc.err)

			cmd, _, _ := newSyncTestCommand(t, service)
			err := runSyncInitCmd(cmd, &syncInitOptions{Dir: "/tmp/tock-sync", Restore: true})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "Nothing was restored")
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), "Nothing was written.")
		})
	}
}

func TestSyncInitTurnsSyncOnInTheConfig(t *testing.T) {
	rt, configPath := loadSyncConfig(t)
	dir := filepath.Join(t.TempDir(), "shared")
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Init(mock.Anything, ports.SyncInitOptions{Dir: dir}).
		Return(ports.SyncReport{Device: "7f3a9c2e5b1d4a60", Written: 5}, nil)
	rt.BuildSync = func() (ports.SyncResolver, error) { return service, nil }

	cmd, out, errOut := commandWithRuntime(rt)
	require.NoError(t, runSyncInitCmd(cmd, &syncInitOptions{Dir: dir, JSONOutput: true}))
	assert.Empty(t, errOut.String())

	var got syncInitJSON
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.Equal(t, dir, got.Dir)
	assert.True(t, got.ConfigUpdated)
	assert.Equal(t, 5, got.Report.Written)

	cfg, _, err := config.Load(config.WithConfigFile(configPath))
	require.NoError(t, err)
	assert.True(t, cfg.Sync.Enabled)
	assert.Equal(t, dir, cfg.Sync.Dir)
	assert.Equal(t, "file", cfg.Backend, "the rest of the config stays")
}

func TestSyncInitKeepsTheConfiguredDir(t *testing.T) {
	rt, configPath := loadSyncConfig(t)
	// iCloud Drive holds the tock app's container already.
	require.NoError(t, os.MkdirAll(iCloudContainer(config.DefaultSyncDir(os.Getenv("HOME"))), 0o750))
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Init(mock.Anything, ports.SyncInitOptions{}).Return(ports.SyncReport{Device: "7f3a9c2e5b1d4a60"}, nil)
	rt.BuildSync = func() (ports.SyncResolver, error) { return service, nil }

	cmd, out, _ := commandWithRuntime(rt)
	require.NoError(t, runSyncInitCmd(cmd, &syncInitOptions{}))
	assert.Contains(t, out.String(), config.DefaultSyncDir(os.Getenv("HOME")))

	cfg, _, err := config.Load(config.WithConfigFile(configPath))
	require.NoError(t, err)
	assert.True(t, cfg.Sync.Enabled)
	assert.Equal(t, config.DefaultSyncDir(os.Getenv("HOME")), cfg.Sync.Dir)
}

func TestSyncInitPrintsTheLinesWhenTheConfigCannotBeWritten(t *testing.T) {
	rt, _ := loadSyncConfig(t)
	rt.Viper = nil
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Init(mock.Anything, ports.SyncInitOptions{Dir: "/tmp/elsewhere"}).Return(ports.SyncReport{}, nil)
	rt.BuildSync = func() (ports.SyncResolver, error) { return service, nil }

	cmd, _, errOut := commandWithRuntime(rt)
	require.NoError(t, runSyncInitCmd(cmd, &syncInitOptions{Dir: "/tmp/elsewhere"}))
	assert.Contains(t, errOut.String(), "sync:\n  enabled: true\n  dir: /tmp/elsewhere\n")
}

func TestSyncInitLeavesTheConfigWhenInitFails(t *testing.T) {
	rt, configPath := loadSyncConfig(t)
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Init(mock.Anything, mock.Anything).Return(ports.SyncReport{}, coreErrors.ErrSyncFormatUnsupported)
	rt.BuildSync = func() (ports.SyncResolver, error) { return service, nil }

	cmd, _, _ := commandWithRuntime(rt)
	err := runSyncInitCmd(cmd, &syncInitOptions{Dir: "/tmp/newer"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer version of tock")

	cfg, _, err := config.Load(config.WithConfigFile(configPath))
	require.NoError(t, err)
	assert.False(t, cfg.Sync.Enabled)
}

func TestSyncStatusCmdWhileDisabled(t *testing.T) {
	cmd, out, _ := newSyncTestCommand(t, nil)
	require.NoError(t, runSyncStatusCmd(cmd, &syncStatusOptions{}))
	assert.Contains(t, out.String(), "off (run 'tock sync init'")
	assert.Contains(t, out.String(), "/tmp/tock-sync")

	cmd, out, _ = newSyncTestCommand(t, nil)
	require.NoError(t, runSyncStatusCmd(cmd, &syncStatusOptions{JSONOutput: true}))
	var got syncStatusJSON
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.False(t, got.Enabled)
	assert.Equal(t, "/tmp/tock-sync", got.Dir)
	assert.Empty(t, got.Devices)
}

func TestSyncStatusCmdPrintsTheDevices(t *testing.T) {
	seen := time.Date(2026, 11, 18, 19, 0, 0, 0, time.UTC)
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Status(mock.Anything).Return(ports.SyncStatus{
		Dir:         "/tmp/tock-sync",
		Initialized: true,
		Device:      "7f3a9c2e5b1d4a60",
		LastSync:    seen,
		Pending:     3,
		Devices: []ports.SyncDevice{
			{ID: "0123456789abcdef", Name: "iPhone", Platform: "ios", App: "tock 1.0", Seen: seen},
			{ID: "7f3a9c2e5b1d4a60", Name: "MacBook", Platform: "macos", App: "tock 2.4.0", Seen: seen},
		},
	}, nil)

	cmd, out, _ := newSyncTestCommand(t, service)
	require.NoError(t, runSyncStatusCmd(cmd, &syncStatusOptions{}))
	text := out.String()
	assert.Contains(t, text, "Sync:         on\n")
	assert.Contains(t, text, "Folder:       /tmp/tock-sync\n")
	assert.Contains(t, text, "This device:  7f3a9c2e5b1d4a60\n")
	shown := seen.Local().Format("2006-01-02 15:04")
	assert.Contains(t, text, "Last sync:    "+shown)
	assert.Contains(t, text, "Downloading:  3 files")
	assert.Contains(t, text, "Devices:      2\n")
	assert.Contains(t, text, "  0123456789abcdef  iPhone, ios, tock 1.0, last seen")
	assert.Contains(t, text, "  7f3a9c2e5b1d4a60  MacBook, macos, tock 2.4.0, last seen "+shown+" (this device)\n")
}

func TestSyncStatusCmdBeforeInit(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Status(mock.Anything).Return(ports.SyncStatus{Dir: "/tmp/tock-sync"}, nil)

	cmd, out, _ := newSyncTestCommand(t, service)
	require.NoError(t, runSyncStatusCmd(cmd, &syncStatusOptions{JSONOutput: true}))
	var got syncStatusJSON
	require.NoError(t, json.Unmarshal(out.Bytes(), &got))
	assert.True(t, got.Enabled)
	assert.False(t, got.Initialized)
	assert.Nil(t, got.LastSync)

	cmd, out, _ = newSyncTestCommand(t, service)
	require.NoError(t, runSyncStatusCmd(cmd, &syncStatusOptions{}))
	assert.Contains(t, out.String(), "This device:  not set up (run 'tock sync init')")
	assert.Contains(t, out.String(), "Last sync:    never")
}

func TestSyncRecordedWritesRunsOnlyAfterAWrite(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	cmd, _, errOut := newSyncTestCommand(t, service)

	syncRecordedWrites(cmd.Context(), cmd)

	service.EXPECT().Run(mock.Anything, ports.SyncOptions{}).Return(ports.SyncReport{Written: 1}, nil).Once()
	getRuntime(cmd).SyncWrites.Mark()
	syncRecordedWrites(cmd.Context(), cmd)
	syncRecordedWrites(cmd.Context(), cmd)
	assert.Empty(t, errOut.String())
}

func TestSyncRecordedWritesWarnsOnOneLine(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, mock.Anything).Return(ports.SyncReport{}, errors.New("read journal:\npermission denied")).Once()
	cmd, out, errOut := newSyncTestCommand(t, service)

	getRuntime(cmd).SyncWrites.Mark()
	syncRecordedWrites(cmd.Context(), cmd)
	assert.Empty(t, out.String())
	assert.Equal(t, "Warning: sync failed: read journal: permission denied (run 'tock sync' to retry)\n", errOut.String())
}

// A command on another backend than sync was set up with says once that sync left it alone; it is no
// failure to retry.
func TestSyncRecordedWritesOnAnotherBackend(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, mock.Anything).
		Return(ports.SyncReport{}, errors.Wrap(coreErrors.ErrSyncBackendChanged, "file at /data/tock.txt")).Once()
	cmd, out, errOut := newSyncTestCommand(t, service)

	getRuntime(cmd).SyncWrites.Mark()
	syncRecordedWrites(cmd.Context(), cmd)
	assert.Empty(t, out.String())
	assert.Contains(t, errOut.String(), "(file at /data/tock.txt)")
	assert.Contains(t, errOut.String(), "tock sync init")
	assert.NotContains(t, errOut.String(), "sync failed")
}

// Sync on in the config while this Mac is not set up, as after its sync state was lost, says what to
// do after a command that changed data, instead of leaving the change unsynced without a word.
func TestSyncRecordedWritesSaysToInitWhenNotSetUp(t *testing.T) {
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, mock.Anything).Return(ports.SyncReport{}, coreErrors.ErrSyncNotInitialized).Once()
	cmd, out, errOut := newSyncTestCommand(t, service)

	getRuntime(cmd).SyncWrites.Mark()
	syncRecordedWrites(cmd.Context(), cmd)
	assert.Empty(t, out.String())
	assert.Equal(t, "Sync is on, but this Mac is not set up yet: run 'tock sync init'.\n", errOut.String())
}

func TestSyncRecordedWritesWithoutSync(t *testing.T) {
	cmd, _, errOut := newSyncTestCommand(t, nil)
	syncRecordedWrites(cmd.Context(), cmd)
	assert.Empty(t, errOut.String())

	bare := &cobra.Command{Use: "bare"}
	bare.SetContext(context.Background())
	syncRecordedWrites(bare.Context(), bare)
}

func TestRootSyncsAfterACommandThatWrote(t *testing.T) {
	loader := loadRuntime
	t.Cleanup(func() { loadRuntime = loader })

	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, ports.SyncOptions{}).Return(ports.SyncReport{}, errors.New("folder gone")).Once()
	rt := &appruntime.Runtime{
		ActivityService: &stubActivityResolver{},
		Config:          &config.Config{},
		TimeFormatter:   timeutil.NewFormatter("24"),
		Localizer:       localization.MustNew(localization.LanguageEnglish),
		Sync:            service,
		SyncWrites:      &appruntime.WriteFlag{},
	}
	var gotVersion string
	loadRuntime = func(_ context.Context, req appruntime.Request) (*appruntime.Runtime, error) {
		gotVersion = req.Version
		return rt, nil
	}

	root := NewRootCmd()
	root.AddCommand(&cobra.Command{Use: "write", RunE: func(*cobra.Command, []string) error {
		rt.SyncWrites.Mark()
		return nil
	}})
	root.AddCommand(&cobra.Command{Use: "read", RunE: func(*cobra.Command, []string) error { return nil }})
	var errOut bytes.Buffer
	root.SetErr(&errOut)

	root.SetArgs([]string{"read"})
	require.NoError(t, root.Execute())
	assert.Empty(t, errOut.String())

	root.SetArgs([]string{"write"})
	require.NoError(t, root.Execute(), "a failed sync never fails the command")
	assert.Equal(t, "Warning: sync failed: folder gone (run 'tock sync' to retry)\n", errOut.String())
	assert.Equal(t, version, gotVersion)
}

func TestSyncInitWaitsForTheICloudContainer(t *testing.T) {
	rt, configPath := loadSyncConfig(t)
	rt.BuildSync = func() (ports.SyncResolver, error) { return portsmocks.NewMockSyncResolver(t), nil }

	cmd, _, _ := commandWithRuntime(rt)
	err := runSyncInitCmd(cmd, &syncInitOptions{})
	require.Error(t, err)
	container := filepath.Join(os.Getenv("HOME"), "Library", "Mobile Documents", "iCloud~com~kriuchkov~tock")
	assert.Contains(t, err.Error(), "The iCloud Drive folder "+container+" is not on this Mac yet")
	assert.NoDirExists(t, container, "nothing is made in iCloud Drive by hand")

	cfg, _, err := config.Load(config.WithConfigFile(configPath))
	require.NoError(t, err)
	assert.False(t, cfg.Sync.Enabled)
}

func TestICloudContainer(t *testing.T) {
	assert.Equal(t, "/home/someone/Library/Mobile Documents/iCloud~com~kriuchkov~tock",
		iCloudContainer("/home/someone/Library/Mobile Documents/iCloud~com~kriuchkov~tock/Documents/sync"))
	assert.Equal(t, "/home/someone/Library/Mobile Documents/com~apple~CloudDocs",
		iCloudContainer("/home/someone/Library/Mobile Documents/com~apple~CloudDocs/tock"))
	assert.Empty(t, iCloudContainer("/home/someone/Dropbox/tock"))
	assert.Empty(t, iCloudContainer("/home/someone/Library/Mobile Documents/"))
}

func TestResolveSyncDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := resolveSyncDir("")
	require.NoError(t, err)
	assert.Empty(t, got)

	got, err = resolveSyncDir("~/tock-sync")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "tock-sync"), got)

	got, err = resolveSyncDir("relative/sync")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(got))
	assert.Equal(t, "sync", filepath.Base(got))
}

func TestRootRegistersSync(t *testing.T) {
	root := NewRootCmd()
	cmd, _, err := root.Find([]string{"sync", "status"})
	require.NoError(t, err)
	assert.Equal(t, "status", cmd.Name())
	assert.NotNil(t, cmd.Flags().Lookup("json"))

	cmd, _, err = root.Find([]string{"sync", "init"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Flags().Lookup("dir"))
	assert.NotNil(t, cmd.Flags().Lookup("json"))

	cmd, _, err = root.Find([]string{"sync"})
	require.NoError(t, err)
	assert.NotNil(t, cmd.Flags().Lookup("full"))
	assert.NotNil(t, cmd.Flags().Lookup("json"))
}
