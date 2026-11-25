package runtime

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/adapters/syncbackend"
	"github.com/kriuchkov/tock/internal/adapters/syncfolder"
	"github.com/kriuchkov/tock/internal/adapters/syncstate"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/ports"
	syncservice "github.com/kriuchkov/tock/internal/services/sync"
	"github.com/kriuchkov/tock/internal/services/sync/recorder"
)

// syncWiring is what withSync builds: the sync service, the repositories the other services use,
// the flag their recorded writes mark and what tells the activities other devices own.
type syncWiring struct {
	service ports.SyncResolver
	repos   repositories
	writes  *WriteFlag
	owners  ports.ActivityOwners
}

// withSync builds the sync service on the backend's repositories and returns it with the
// repositories the other services use: the same ones wrapped by the recorder, so every write made
// through tock reaches the journal, and marks the returned flag. The sync service itself keeps the
// unwrapped ones. Nothing is created on disk here; state.db and the sync folder are opened on first
// use.
func withSync(cfg config.SyncConfig, backend, filePath string, repos repositories, version string) (syncWiring, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return syncWiring{}, errors.Wrap(err, "find the home directory for the sync state")
	}

	service := syncservice.NewService(syncservice.Config{
		Dir:        cfg.Dir,
		OpenFolder: syncfolder.New,
		State:      syncstate.New(syncStatePath(home)),
		Backend:    newSyncBackend(backend, filePath, repos),
		Activities: repos.activities,
		Notes:      repos.notes,
		Reminders:  repos.reminders,
		Profile:    deviceProfile(version),
	})

	writes := &WriteFlag{}
	sink := markingSink{sink: service, writes: writes}
	recorded := repos
	recorded.activities = recorder.NewActivityRepository(repos.activities, sink)
	recorded.notes = recorder.NewNotesRepository(repos.notes, sink)
	recorded.reminders = recorder.NewReminderRepository(repos.reminders, sink)
	return syncWiring{service: service, repos: recorded, writes: writes, owners: service}, nil
}

// WriteFlag remembers that a write happened until it is taken. It is safe for concurrent use, and a
// nil flag never reports one.
type WriteFlag struct {
	set atomic.Bool
}

// Mark remembers a write.
func (f *WriteFlag) Mark() {
	f.set.Store(true)
}

// Take reports whether a write happened since the last Take and forgets it.
func (f *WriteFlag) Take() bool {
	return f != nil && f.set.Swap(false)
}

// markingSink passes every write on to the sync service and marks writes once one succeeded, so the
// delivery layer knows a sync has something to send.
type markingSink struct {
	sink   ports.SyncChangeSink
	writes *WriteFlag
}

func (s markingSink) Record(ctx context.Context, write func(ctx context.Context) (ports.SyncWrite, error)) error {
	return s.sink.Record(ctx, func(ctx context.Context) (ports.SyncWrite, error) {
		done, err := write(ctx)
		if err == nil {
			s.writes.Mark()
		}
		return done, err
	})
}

// EnableSync turns sync on in the config file, the way tock writes its config elsewhere, and sets
// sync.dir to dir unless it is empty. The running runtime keeps its services; the next command
// syncs.
func (rt *Runtime) EnableSync(dir string) error {
	if rt.Viper == nil {
		return errors.New("no config file to write")
	}
	if err := config.EnableSync(rt.Viper, dir); err != nil {
		return err
	}
	if rt.Config != nil {
		rt.Config.Sync.Enabled = true
		if dir != "" {
			rt.Config.Sync.Dir = dir
		}
	}
	return nil
}

// syncStatePath is state.db, kept outside the sync folder in ~/.tock/sync/.
func syncStatePath(home string) string {
	return filepath.Join(home, ".tock", "sync", "state.db")
}

// newSyncBackend returns what sync needs to know about the backend beyond its repositories. Its
// identity holds the data path made canonical, so state.db names one file however tock reaches it.
func newSyncBackend(backend, filePath string, repos repositories) ports.SyncBackend {
	filePath = canonicalPath(filePath)
	switch backend {
	case backendTodoTXT:
		return syncbackend.NewTodoTXT(filePath, repos.remindersPath)
	case backendTimewarrior:
		return syncbackend.NewTimewarrior(filePath, repos.remindersPath)
	case backendSqlite:
		return syncbackend.NewSQLite(repos.db, filePath)
	default:
		return syncbackend.NewFile(filePath, repos.remindersPath)
	}
}

// canonicalPath returns path absolute, with the symbolic links of its longest part that exists
// resolved, so one data file has one identity whatever directory tock runs from and whatever link
// names it. A relative path read by the menu bar icon, which launchd starts in /, then never passes
// for the file sync was set up with: sync stops with coreErrors.ErrSyncBackendChanged instead of
// comparing a file that is not there. Resolving only the part that exists keeps the identity of a
// file not created yet the same once it is.
func canonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	rest := ""
	for dir := abs; ; {
		if resolved, evalErr := filepath.EvalSymlinks(dir); evalErr == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return abs
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		dir = parent
	}
}

// deviceProfile describes this Mac in devices/<id>.json: its host name without .local, the
// platform and tock's version. The sync service adds the ID and the time it was seen.
func deviceProfile(version string) ports.SyncDevice {
	name, err := os.Hostname()
	if err != nil {
		name = ""
	}
	platform := goruntime.GOOS
	if platform == "darwin" {
		platform = "macos"
	}
	app := "tock"
	if version != "" {
		app += " " + version
	}
	return ports.SyncDevice{Name: strings.TrimSuffix(name, ".local"), Platform: platform, App: app}
}
