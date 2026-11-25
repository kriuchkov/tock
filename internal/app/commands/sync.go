package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// syncHookTimeout bounds the sync after a command, so a stalled sync folder never hangs the shell.
const syncHookTimeout = 30 * time.Second

type syncOptions struct {
	Full           bool
	AllowDeletions bool
	JSONOutput     bool
}

type syncInitOptions struct {
	Dir        string
	Restore    bool
	JSONOutput bool
}

type syncStatusOptions struct {
	JSONOutput bool
}

// NewSyncCmd returns tock sync, which syncs with the tock iOS app through a folder in iCloud Drive
// (docs/sync.md), with its init and status subcommands. Sync is off until tock sync init turns it on.
func NewSyncCmd() *cobra.Command {
	var opts syncOptions

	cmd := &cobra.Command{
		Use:   "sync",
		Short: defaultText("sync.short"),
		Long:  defaultText("sync.long"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSyncCmd(cmd, &opts) },
	}
	cmd.Flags().BoolVar(&opts.Full, "full", false, defaultText("sync.flag.full"))
	cmd.Flags().BoolVar(&opts.AllowDeletions, "allow-deletions", false, defaultText("sync.flag.allow_deletions"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("sync.flag.json"))

	cmd.AddCommand(newSyncInitCmd())
	cmd.AddCommand(newSyncStatusCmd())
	return cmd
}

func newSyncInitCmd() *cobra.Command {
	var opts syncInitOptions

	cmd := &cobra.Command{
		Use:   "init",
		Short: defaultText("sync.init.short"),
		Long:  defaultText("sync.init.long"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSyncInitCmd(cmd, &opts) },
	}
	cmd.Flags().StringVar(&opts.Dir, "dir", "", defaultText("sync.init.flag.dir"))
	cmd.Flags().BoolVar(&opts.Restore, "restore", false, defaultText("sync.init.flag.restore"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("sync.flag.json"))
	return cmd
}

func newSyncStatusCmd() *cobra.Command {
	var opts syncStatusOptions

	cmd := &cobra.Command{
		Use:   "status",
		Short: defaultText("sync.status.short"),
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSyncStatusCmd(cmd, &opts) },
	}
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("sync.flag.json"))
	return cmd
}

// syncResolver returns the runtime's sync service, or an error telling how to turn sync on.
func syncResolver(cmd *cobra.Command) (ports.SyncResolver, error) {
	rt := getRuntime(cmd)
	if rt.Sync == nil {
		return nil, errors.New(text(cmd, "sync.disabled"))
	}
	return rt.Sync, nil
}

func runSyncCmd(cmd *cobra.Command, opts *syncOptions) error {
	service, err := syncResolver(cmd)
	if err != nil {
		return err
	}

	// This run sends what the command recorded so far, so the hook after it has nothing left to do.
	getRuntime(cmd).SyncWrites.Take()
	report, err := service.Run(cmd.Context(), ports.SyncOptions{Full: opts.Full, AllowDeletions: opts.AllowDeletions})
	if err != nil {
		return syncError(cmd, err)
	}
	if opts.JSONOutput {
		return writeJSONTo(cmd.OutOrStdout(), newSyncReportJSON(report))
	}
	return writeSyncReport(cmd, report)
}

// runSyncInitCmd sets the sync folder up for this Mac (the one --dir names, else sync.dir), runs the
// first sync and turns sync on in the config file. When the config cannot be written, it prints the
// lines to add instead.
func runSyncInitCmd(cmd *cobra.Command, opts *syncInitOptions) error {
	rt := getRuntime(cmd)
	dir, err := resolveSyncDir(opts.Dir)
	if err != nil {
		return err
	}
	folder := dir
	if folder == "" {
		folder = rt.Config.Sync.Dir
	}
	if err = checkICloudContainer(cmd, folder); err != nil {
		return err
	}

	service := rt.Sync
	if service == nil {
		if rt.BuildSync == nil {
			return errors.New(text(cmd, "sync.disabled"))
		}
		if service, err = rt.BuildSync(); err != nil {
			return errors.Wrap(err, "set up sync")
		}
	}

	rt.SyncWrites.Take()
	report, err := service.Init(cmd.Context(), ports.SyncInitOptions{Dir: dir, Restore: opts.Restore})
	if err != nil {
		return syncError(cmd, err)
	}

	configUpdated := false
	if !rt.Config.Sync.Enabled || folder != rt.Config.Sync.Dir {
		if configErr := rt.EnableSync(dir); configErr != nil {
			cmd.PrintErr(text(cmd, "sync.init.config_manual", oneLine(configErr.Error()), folder))
		} else {
			configUpdated = true
		}
	}

	if opts.JSONOutput {
		return writeJSONTo(cmd.OutOrStdout(), syncInitJSON{Dir: folder, ConfigUpdated: configUpdated, Report: newSyncReportJSON(report)})
	}
	if _, err = fmt.Fprintf(cmd.OutOrStdout(), text(cmd, "sync.init.done"), folder, report.Device); err != nil {
		return err
	}
	return writeSyncReport(cmd, report)
}

func runSyncStatusCmd(cmd *cobra.Command, opts *syncStatusOptions) error {
	rt := getRuntime(cmd)
	status := ports.SyncStatus{}
	if rt.Config != nil {
		status.Dir = rt.Config.Sync.Dir
	}
	if rt.Sync != nil {
		var err error
		if status, err = rt.Sync.Status(cmd.Context()); err != nil {
			return syncError(cmd, err)
		}
	}

	if opts.JSONOutput {
		return writeJSONTo(cmd.OutOrStdout(), newSyncStatusJSON(rt.Sync != nil, status))
	}
	return writeSyncStatus(cmd, rt.Sync != nil, status)
}

// syncRecordedWrites syncs when a write was recorded through tock since the last sync: after every
// command that changed data and after every MCP tool that did. The runtime comes from cmd, ctx only
// bounds the run. A failure prints one line on stderr and never fails the caller; with sync on but
// this Mac not set up, as after its sync state was lost, that line says to run tock sync init.
func syncRecordedWrites(ctx context.Context, cmd *cobra.Command) {
	rt, ok := appruntime.FromContext(cmd.Context())
	if !ok || rt.Sync == nil || !rt.SyncWrites.Take() {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, syncHookTimeout)
	defer cancel()
	report, err := rt.Sync.Run(ctx, ports.SyncOptions{})
	switch {
	case errors.Is(err, coreErrors.ErrSyncNotInitialized), errors.Is(err, coreErrors.ErrSyncBackendChanged),
		errors.Is(err, coreErrors.ErrSyncBackendMissing), errors.Is(err, coreErrors.ErrSyncTooManyDeletions):
		// Nothing a retry fixes: the text says what to do.
		cmd.PrintErrln(syncErrorText(cmd, err))
	case err != nil:
		cmd.PrintErrln(text(cmd, "sync.warning", oneLine(syncErrorText(cmd, err))))
	case report.ReplacedDevice != "":
		cmd.PrintErr(text(cmd, "sync.report.replaced", report.ReplacedDevice, report.Device))
	}
}

// syncError turns the sync errors a user can act on into what to do about them.
func syncError(cmd *cobra.Command, err error) error {
	for _, sentinel := range []error{
		coreErrors.ErrSyncNotInitialized, coreErrors.ErrSyncFormatUnsupported, coreErrors.ErrSyncBackendChanged,
		coreErrors.ErrSyncBackendMissing, coreErrors.ErrSyncTooManyDeletions, coreErrors.ErrSyncNothingToRestore,
		coreErrors.ErrSyncRestoreNotSynced, coreErrors.ErrSyncRestorePending,
	} {
		if errors.Is(err, sentinel) {
			return errors.New(syncErrorText(cmd, err))
		}
	}
	return errors.Wrap(err, "sync")
}

func syncErrorText(cmd *cobra.Command, err error) string {
	switch {
	case errors.Is(err, coreErrors.ErrSyncNotInitialized):
		return text(cmd, "sync.not_initialized")
	case errors.Is(err, coreErrors.ErrSyncFormatUnsupported):
		return text(cmd, "sync.format_unsupported")
	case errors.Is(err, coreErrors.ErrSyncBackendChanged):
		return text(cmd, "sync.backend_changed", errorDetail(err, coreErrors.ErrSyncBackendChanged))
	case errors.Is(err, coreErrors.ErrSyncBackendMissing):
		return text(cmd, "sync.backend_missing", errorDetail(err, coreErrors.ErrSyncBackendMissing))
	case errors.Is(err, coreErrors.ErrSyncTooManyDeletions):
		return text(cmd, "sync.too_many_deletions", errorDetail(err, coreErrors.ErrSyncTooManyDeletions))
	case errors.Is(err, coreErrors.ErrSyncNothingToRestore):
		return text(cmd, "sync.nothing_to_restore")
	case errors.Is(err, coreErrors.ErrSyncRestoreNotSynced):
		return text(cmd, "sync.restore_not_synced")
	case errors.Is(err, coreErrors.ErrSyncRestorePending):
		return text(cmd, "sync.restore_pending")
	default:
		return err.Error()
	}
}

// errorDetail is what a sync sentinel was wrapped with: the backend sync was set up with for
// ErrSyncBackendChanged, the data path for ErrSyncBackendMissing, the number of deletions for
// ErrSyncTooManyDeletions.
func errorDetail(err, sentinel error) string {
	detail, _ := strings.CutSuffix(err.Error(), ": "+sentinel.Error())
	if detail == err.Error() {
		return "?"
	}
	return detail
}

// resolveSyncDir makes the --dir of tock sync init absolute, expanding a leading ~; empty stays empty.
func resolveSyncDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "find the home directory")
		}
		dir = filepath.Join(home, strings.TrimPrefix(dir, "~"))
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", errors.Wrap(err, "resolve the sync folder")
	}
	return abs, nil
}

// iCloudContainer returns the iCloud Drive container dir lies in, ~/Library/Mobile Documents/<container>,
// or "" when dir is not in iCloud Drive.
func iCloudContainer(dir string) string {
	sep := string(filepath.Separator)
	marker := sep + filepath.Join("Library", "Mobile Documents") + sep
	i := strings.Index(dir, marker)
	if i < 0 {
		return ""
	}
	container, _, _ := strings.Cut(dir[i+len(marker):], sep)
	if container == "" {
		return ""
	}
	return dir[:i+len(marker)] + container
}

// checkICloudContainer refuses a sync folder in an iCloud Drive container that is not on this Mac yet,
// such as the tock app's before the app first wrote to it: init would make the folders by hand, and
// iCloud may never sync a container made that way.
func checkICloudContainer(cmd *cobra.Command, dir string) error {
	container := iCloudContainer(dir)
	if container == "" {
		return nil
	}
	if info, err := os.Stat(container); err == nil && info.IsDir() {
		return nil
	}
	return errors.New(text(cmd, "sync.icloud_missing", container))
}
