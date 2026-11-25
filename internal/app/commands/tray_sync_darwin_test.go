//go:build darwin && cgo

package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/go-faster/errors"
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

func TestTraySyncStatus(t *testing.T) {
	loc := localization.MustNew(localization.LanguageEnglish)
	tf := timeutil.NewFormatter("24")
	at := time.Date(2026, 11, 18, 14, 5, 0, 0, time.Local)

	cases := map[string]struct {
		result  traySyncResult
		title   string
		tooltip string
	}{
		"synced": {
			result:  traySyncResult{report: ports.SyncReport{Read: 2, Written: 1}, at: at},
			title:   "Synced at 14:05",
			tooltip: "Synced: 2 changes read, 1 written, 0 applied to the backend.",
		},
		"downloading": {
			result: traySyncResult{report: ports.SyncReport{Pending: 3}, at: at},
			title:  "Synced at 14:05, 3 files downloading",
		},
		"not set up": {
			result: traySyncResult{err: coreErrors.ErrSyncNotInitialized, at: at},
			title:  "Sync: run 'tock sync init' to set it up",
		},
		"newer format": {
			result: traySyncResult{err: errors.Wrap(coreErrors.ErrSyncFormatUnsupported, "format.json"), at: at},
			title:  "Sync stopped: update tock",
		},
		"data missing": {
			result: traySyncResult{err: errors.Wrap(coreErrors.ErrSyncBackendMissing, "/data/tock.txt"), at: at},
			title:  "Sync paused: the data file is missing",
		},
		"too many deletions": {
			result: traySyncResult{err: errors.Wrap(coreErrors.ErrSyncTooManyDeletions, "120"), at: at},
			title:  "Sync paused: many records are gone",
		},
		"failed": {
			result:  traySyncResult{err: errors.New("read journal:\npermission denied"), at: at},
			title:   "Sync failed at 14:05",
			tooltip: "read journal: permission denied",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			title, tooltip := traySyncStatus(loc, tf, tc.result)
			assert.Equal(t, tc.title, title)
			if tc.tooltip != "" {
				assert.Equal(t, tc.tooltip, tooltip)
			}
		})
	}
}

func newTestTraySync(t *testing.T, service ports.SyncResolver, now *time.Time) *traySync {
	t.Helper()
	rt := &appruntime.Runtime{Sync: service, SyncWrites: &appruntime.WriteFlag{}, Config: &config.Config{}}
	s := newTraySync(rt)
	require.NotNil(t, s)
	s.now = func() time.Time { return *now }
	return s
}

func TestTraySyncIsOffWithoutSync(t *testing.T) {
	assert.Nil(t, newTraySync(&appruntime.Runtime{}))

	var s *traySync
	ran := false
	s.write(func() { ran = true })
	assert.True(t, ran, "without sync a menu action just runs")
}

func TestTraySyncRunsAFullSyncOnceAnHour(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	service := portsmocks.NewMockSyncResolver(t)
	s := newTestTraySync(t, service, &now)
	ctx := context.Background()

	service.EXPECT().Run(mock.Anything, ports.SyncOptions{Full: true}).Return(ports.SyncReport{Device: "7f3a9c2e5b1d4a60"}, nil).Once()
	s.syncOnce(ctx)
	result := <-s.results
	require.NoError(t, result.err)
	assert.Equal(t, now, result.at)
	assert.Equal(t, "7f3a9c2e5b1d4a60", s.device)

	now = now.Add(30 * time.Minute)
	service.EXPECT().Run(mock.Anything, ports.SyncOptions{}).Return(ports.SyncReport{}, nil).Once()
	s.writes.Mark()
	s.syncOnce(ctx)
	<-s.results
	assert.False(t, s.writes.Take(), "the sync sends the writes recorded so far")

	now = now.Add(31 * time.Minute)
	service.EXPECT().Run(mock.Anything, ports.SyncOptions{Full: true}).Return(ports.SyncReport{}, errors.New("offline")).Once()
	s.syncOnce(ctx)
	require.Error(t, (<-s.results).err)

	service.EXPECT().Run(mock.Anything, ports.SyncOptions{Full: true}).Return(ports.SyncReport{}, nil).Once()
	s.syncOnce(ctx)
	<-s.results
}

// Sync turned off in the config while the menu bar icon runs stops its syncs, and the menu says so;
// turned on again, they go on.
func TestTraySyncStopsWhileSyncIsOff(t *testing.T) {
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	service := portsmocks.NewMockSyncResolver(t)
	enabled := false
	rt := &appruntime.Runtime{
		Sync: service, SyncWrites: &appruntime.WriteFlag{}, Config: &config.Config{},
		SyncEnabled: func() bool { return enabled },
	}
	s := newTraySync(rt)
	require.NotNil(t, s)
	s.now = func() time.Time { return now }
	ctx := context.Background()

	s.syncOnce(ctx)
	title, _ := traySyncStatus(localization.MustNew(localization.LanguageEnglish), nil, <-s.results)
	assert.Equal(t, "Sync is off", title)

	enabled = true
	service.EXPECT().Run(mock.Anything, ports.SyncOptions{Full: true}).Return(ports.SyncReport{}, nil).Once()
	s.syncOnce(ctx)
	require.NoError(t, (<-s.results).err)
}

func TestTraySyncWritesThenAsksForASync(t *testing.T) {
	now := time.Now()
	s := newTestTraySync(t, portsmocks.NewMockSyncResolver(t), &now)

	s.write(func() {})
	s.write(func() {})
	assert.Len(t, s.trigger, 1, "requests made while one waits merge into it")
}

func TestTraySyncRunSyncsAtStartAndOnRequest(t *testing.T) {
	now := time.Now()
	service := portsmocks.NewMockSyncResolver(t)
	service.EXPECT().Run(mock.Anything, mock.Anything).Return(ports.SyncReport{}, nil).Twice()
	s := newTestTraySync(t, service, &now)
	s.dir = t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.run(ctx)
		close(done)
	}()

	<-s.results
	s.request()
	<-s.results
	cancel()
	<-done
}

func TestSyncWatcherHandle(t *testing.T) {
	journal := filepath.Join("/sync", "journal")
	w := &syncWatcher{journal: journal, watched: map[string]bool{filepath.Join(journal, "0123456789abcdef"): true}}
	own := "7f3a9c2e5b1d4a60"

	other := fsnotify.Event{Name: filepath.Join(journal, "0123456789abcdef", "2026-11.jsonl"), Op: fsnotify.Write}
	assert.True(t, w.handle(other, own))
	assert.True(t, w.handle(fsnotify.Event{Name: filepath.Join(journal, "89abcdef01234567"), Op: fsnotify.Create}, own))
	assert.True(t, w.handle(other, ""), "before the first sync every change counts")

	mine := fsnotify.Event{Name: filepath.Join(journal, own, "2026-11.jsonl"), Op: fsnotify.Write}
	assert.False(t, w.handle(mine, own), "this Mac's own journal changes only when it writes")
	assert.False(t, w.handle(fsnotify.Event{Name: other.Name, Op: fsnotify.Chmod}, own))
	assert.False(t, w.handle(fsnotify.Event{Name: filepath.Join("/sync", "devices", "x.json"), Op: fsnotify.Write}, own))

	gone := fsnotify.Event{Name: filepath.Join(journal, "0123456789abcdef"), Op: fsnotify.Remove}
	w.handle(gone, own)
	assert.NotContains(t, w.watched, gone.Name, "a folder that went away is watched again once it is back")

	var none *syncWatcher
	assert.False(t, none.handle(other, own))
	assert.Nil(t, none.events())
	none.refresh()
	none.close()
}

func TestSyncWatcherClosedDeliversNothing(t *testing.T) {
	w := newSyncWatcher(t.TempDir())
	require.NotNil(t, w)
	w.close()
	w.close()
	assert.Nil(t, w.events())
	assert.Nil(t, w.errors())
	w.refresh()
}

func TestSyncWatcherSeesAnotherDevicesJournal(t *testing.T) {
	dir := t.TempDir()
	assert.Nil(t, newSyncWatcher(""))

	w := newSyncWatcher(dir)
	require.NotNil(t, w)
	defer w.close()
	assert.Empty(t, w.watched, "before tock sync init there is no journal folder to watch")

	device := filepath.Join(dir, "journal", "0123456789abcdef")
	require.NoError(t, os.MkdirAll(device, 0o750))
	w.refresh()
	assert.Contains(t, w.watched, device)

	require.NoError(t, os.WriteFile(filepath.Join(device, "2026-11.jsonl"), []byte("{}\n"), 0o600))
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-w.events():
			if w.handle(event, "7f3a9c2e5b1d4a60") {
				return
			}
		case <-deadline:
			t.Fatal("no event for a new segment of another device")
		}
	}
}

func TestTraySyncWaitsForTheNextSync(t *testing.T) {
	now := time.Now()
	s := newTestTraySync(t, portsmocks.NewMockSyncResolver(t), &now)
	s.device = "7f3a9c2e5b1d4a60"
	events := make(chan fsnotify.Event, 1)
	errs := make(chan error, 1)
	w := &syncWatcher{eventCh: events, errorCh: errs, journal: "/sync/journal", watched: map[string]bool{}}
	settle := time.NewTimer(time.Hour)
	settle.Stop()
	tick := make(chan time.Time, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errs <- fsnotify.ErrEventOverflow
	assert.True(t, s.wait(ctx, tick, settle, w), "lost events make it sync to be sure")

	s.request()
	assert.True(t, s.wait(ctx, tick, settle, w))

	tick <- time.Now()
	assert.True(t, s.wait(ctx, tick, settle, w))

	events <- fsnotify.Event{Name: "/sync/journal/7f3a9c2e5b1d4a60/2026-11.jsonl", Op: fsnotify.Write}
	settle.Reset(time.Millisecond)
	assert.True(t, s.wait(ctx, tick, settle, w), "changes that settled")

	close(events)
	close(errs)
	cancel()
	assert.False(t, s.wait(ctx, tick, settle, w), "closed channels never spin the wait")
}
