//go:build darwin && cgo

package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/app/localization"
	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	// traySyncInterval is how often the menu bar icon syncs on its own.
	traySyncInterval = time.Minute
	// traySyncFullInterval is how often its sync compares the whole backend and every note file.
	traySyncFullInterval = time.Hour
	// traySyncDebounce lets a burst of changes in the sync folder settle into one sync.
	traySyncDebounce = 2 * time.Second
	// traySyncTimeout bounds one sync, so a stalled folder never holds the menu's writes for long.
	traySyncTimeout = 2 * time.Minute
)

// traySyncResult is what one sync of the menu bar icon did; off is set when none ran because sync is
// off in the config.
type traySyncResult struct {
	report ports.SyncReport
	err    error
	off    bool
	at     time.Time
}

// traySync runs the menu bar icon's syncs on a goroutine of their own: at start, once a minute (a
// full one once an hour), soon after another device's journal in the sync folder changes, and when
// a menu action asks for one. Results go to the menu loop through results.
type traySync struct {
	service ports.SyncResolver
	writes  *appruntime.WriteFlag
	// enabled reads sync.enabled from the config before each sync: sync turned off while the icon runs
	// stops its syncs. Nil syncs always.
	enabled func() bool
	dir     string
	now     func() time.Time
	// mu keeps syncs and the menu's writes apart, so this process never records and syncs at once.
	mu      sync.Mutex
	trigger chan struct{}
	results chan traySyncResult

	lastFull time.Time
	// device is this Mac's ID from the last sync: changes in its own journal need no sync.
	device string
}

// newTraySync returns nil while sync is off.
func newTraySync(rt *appruntime.Runtime) *traySync {
	if rt.Sync == nil {
		return nil
	}
	dir := ""
	if rt.Config != nil {
		dir = rt.Config.Sync.Dir
	}
	return &traySync{
		service: rt.Sync,
		writes:  rt.SyncWrites,
		enabled: rt.SyncEnabled,
		dir:     dir,
		now:     time.Now,
		trigger: make(chan struct{}, 1),
		results: make(chan traySyncResult, 1),
	}
}

// request asks for a sync soon; requests made while one is waiting merge into it.
func (s *traySync) request() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// write runs a menu action that changes data apart from the syncs, then asks for a sync to send it.
// Without sync it just runs the action.
func (s *traySync) write(action func()) {
	if s == nil {
		action()
		return
	}
	s.mu.Lock()
	action()
	s.mu.Unlock()
	s.request()
}

// run syncs until ctx is done.
func (s *traySync) run(ctx context.Context) {
	watcher := newSyncWatcher(s.dir)
	defer watcher.close()
	ticker := time.NewTicker(traySyncInterval)
	defer ticker.Stop()
	settle := time.NewTimer(traySyncDebounce)
	settle.Stop()
	defer settle.Stop()

	for {
		s.syncOnce(ctx)
		watcher.refresh()

		if !s.wait(ctx, ticker.C, settle, watcher) {
			return
		}
		// Whatever woke it, this sync covers the changes still settling too.
		settle.Stop()
	}
}

// wait blocks until the next sync is due; false once ctx is done.
func (s *traySync) wait(ctx context.Context, tick <-chan time.Time, settle *time.Timer, watcher *syncWatcher) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-tick:
			return true
		case <-s.trigger:
			return true
		case <-settle.C:
			return true
		case event, ok := <-watcher.events():
			if !ok {
				watcher.close()
			} else if watcher.handle(event, s.device) {
				settle.Reset(traySyncDebounce)
			}
		case _, ok := <-watcher.errors():
			if !ok {
				watcher.close()
				continue
			}
			// An overflow may have lost events: sync to be sure.
			return true
		}
	}
}

// syncOnce runs one sync, a full one when the last full one is an hour old, and hands the result to
// the menu loop.
func (s *traySync) syncOnce(ctx context.Context) {
	if s.enabled != nil && !s.enabled() {
		select {
		case s.results <- traySyncResult{off: true, at: s.now()}:
		case <-ctx.Done():
		}
		return
	}
	full := s.lastFull.IsZero() || s.now().Sub(s.lastFull) >= traySyncFullInterval

	s.mu.Lock()
	s.writes.Take()
	runCtx, cancel := context.WithTimeout(ctx, traySyncTimeout)
	report, err := s.service.Run(runCtx, ports.SyncOptions{Full: full})
	cancel()
	s.mu.Unlock()

	if err == nil {
		s.device = report.Device
		if full {
			s.lastFull = s.now()
		}
	}
	select {
	case s.results <- traySyncResult{report: report, err: err, at: s.now()}:
	case <-ctx.Done():
	}
}

// traySyncStatus is the menu's sync line and its tooltip for a result.
func traySyncStatus(loc *localization.Localizer, tf *timeutil.Formatter, result traySyncResult) (string, string) {
	if tf == nil {
		tf = timeutil.NewFormatter("24")
	}
	clock := result.at.Local().Format(tf.GetDisplayFormat())
	switch {
	case result.off:
		return loc.Text("tray.sync.off"), loc.Text("sync.disabled")
	case result.err == nil && result.report.Pending > 0:
		return loc.Format("tray.sync.synced_pending", clock, result.report.Pending), syncReportLine(loc, result.report)
	case result.err == nil:
		return loc.Format("tray.sync.synced", clock), syncReportLine(loc, result.report)
	case errors.Is(result.err, coreErrors.ErrSyncNotInitialized):
		return loc.Text("tray.sync.not_initialized"), loc.Text("sync.not_initialized")
	case errors.Is(result.err, coreErrors.ErrSyncFormatUnsupported):
		return loc.Text("tray.sync.format_unsupported"), loc.Text("sync.format_unsupported")
	case errors.Is(result.err, coreErrors.ErrSyncBackendChanged):
		return loc.Text("tray.sync.backend_changed"), oneLine(result.err.Error())
	case errors.Is(result.err, coreErrors.ErrSyncBackendMissing):
		return loc.Text("tray.sync.backend_missing"), oneLine(result.err.Error())
	case errors.Is(result.err, coreErrors.ErrSyncTooManyDeletions):
		return loc.Text("tray.sync.too_many_deletions"), oneLine(result.err.Error())
	default:
		return loc.Format("tray.sync.failed", clock), oneLine(result.err.Error())
	}
}

func syncReportLine(loc *localization.Localizer, report ports.SyncReport) string {
	return strings.TrimSpace(loc.Format("sync.report", report.Read, report.Written, report.Applied))
}

// syncWatcher watches the journal folders of the sync folder, so the menu bar icon syncs soon after
// another device's changes arrive. A nil watcher watches nothing; the icon then syncs once a minute.
type syncWatcher struct {
	watcher *fsnotify.Watcher
	// eventCh and errorCh are the watcher's channels.
	eventCh <-chan fsnotify.Event
	errorCh <-chan error
	journal string
	watched map[string]bool
	closed  bool
}

// newSyncWatcher returns nil when the folder cannot be watched.
func newSyncWatcher(dir string) *syncWatcher {
	if dir == "" {
		return nil
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}
	w := &syncWatcher{
		watcher: watcher,
		eventCh: watcher.Events,
		errorCh: watcher.Errors,
		journal: filepath.Join(dir, "journal"),
		watched: make(map[string]bool),
	}
	w.refresh()
	return w
}

// refresh watches the journal folder and each device's folder in it not watched yet. A folder that
// does not exist yet, before tock sync init or before a device's first change, is tried again on the
// next refresh.
func (w *syncWatcher) refresh() {
	if w == nil || w.closed {
		return
	}
	w.add(w.journal)
	entries, err := os.ReadDir(w.journal)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			w.add(filepath.Join(w.journal, entry.Name()))
		}
	}
}

func (w *syncWatcher) add(path string) {
	if w.watched[path] {
		return
	}
	if err := w.watcher.Add(path); err == nil {
		w.watched[path] = true
	}
}

// handle forgets a watched folder that went away and reports whether event may bring another
// device's changes: anything under the journal folder but own's journal, which only this Mac writes.
func (w *syncWatcher) handle(event fsnotify.Event, own string) bool {
	if w == nil {
		return false
	}
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		delete(w.watched, event.Name)
	}
	if event.Op == fsnotify.Chmod {
		return false
	}
	rel, err := filepath.Rel(w.journal, event.Name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	device, _, _ := strings.Cut(rel, string(filepath.Separator))
	return own == "" || device != own
}

// events and errors are nil for a nil or closed watcher: a nil channel never delivers.
func (w *syncWatcher) events() <-chan fsnotify.Event {
	if w == nil || w.closed {
		return nil
	}
	return w.eventCh
}

func (w *syncWatcher) errors() <-chan error {
	if w == nil || w.closed {
		return nil
	}
	return w.errorCh
}

// close stops watching; the icon then syncs once a minute.
func (w *syncWatcher) close() {
	if w != nil && !w.closed {
		w.closed = true
		if w.watcher != nil {
			_ = w.watcher.Close()
		}
	}
}
