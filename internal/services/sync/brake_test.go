package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/notes"
	"github.com/kriuchkov/tock/internal/adapters/repositories/sqlite"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// macWithHistory is a Mac whose tock.txt holds n synced activities, an hour apart.
func macWithHistory(t *testing.T, n int) (*device, *testClock) {
	t.Helper()
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	for i := range n {
		_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
			Project: "P", Description: "d", StartTime: at(i * 60), EndTime: at(i*60 + 30),
		})
		require.NoError(t, err)
	}
	clock.now = at(n * 60)
	mac.sync(t)
	require.Len(t, visibleFrom(mac.folder, mac.deviceID()), n)
	return mac, clock
}

// deletions counts the tombstones a device journaled.
func deletions(d *device) int {
	n := 0
	for _, change := range d.folder.lines(d.deviceID()) {
		if string(change.Set["deleted"]) == "true" {
			n++
		}
	}
	return n
}

// A tock.txt that went away (renamed, on a volume not mounted) is not compared: the sync stops and
// journals nothing, and once the file is back nothing was lost.
func TestMissingDataFileStopsTheSync(t *testing.T) {
	mac, clock := macWithHistory(t, 3)
	uids := map[string]bool{}
	for _, a := range visibleFrom(mac.folder, mac.deviceID()) {
		uids[a.UID] = true
	}
	lines := len(mac.folder.lines(mac.deviceID()))
	path := mac.backend.identity.Path
	require.NoError(t, os.Rename(path, path+".bak"))

	clock.advance(time.Hour)
	for _, opts := range []ports.SyncOptions{{}, {Full: true}} {
		_, err := mac.svc.Run(context.Background(), opts)
		require.ErrorIs(t, err, coreErrors.ErrSyncBackendMissing)
		assert.Contains(t, err.Error(), path)
	}
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines, "nothing journaled")
	assert.NoFileExists(t, path, "nothing written into the backend")

	require.NoError(t, os.Rename(path+".bak", path))
	clock.advance(time.Hour)
	assert.Zero(t, mac.full(t).Written)
	visible := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, visible, 3)
	for _, a := range visible {
		assert.True(t, uids[a.UID], "the records keep their uids")
	}
	assertQuiet(t, mac)
}

// tock sync init on a backend whose file is gone writes it again from the sync folder, deleting
// nothing.
func TestInitWritesAMissingDataFileAgain(t *testing.T) {
	mac, clock := macWithHistory(t, 3)
	require.NoError(t, os.Remove(mac.backend.identity.Path))

	clock.advance(time.Hour)
	report := mac.init(t)
	assert.Zero(t, report.Written)
	assert.Equal(t, 3, report.Applied)
	assert.Len(t, mac.list(t), 3)
	assert.Zero(t, deletions(mac))
	assertQuiet(t, mac)
}

// A tock.txt moved away and started again by a new activity ("archive the year, start fresh") makes
// every record look deleted: the brake stops the sync before it journals anything, and only
// tock sync --allow-deletions deletes them on every device.
func TestDeletionBrakeStopsAMassDeletion(t *testing.T) {
	mac, clock := macWithHistory(t, 6)
	lines := len(mac.folder.lines(mac.deviceID()))
	path := mac.backend.identity.Path
	require.NoError(t, os.Rename(path, path+".2025"))

	clock.advance(time.Hour)
	_, err := mac.tracker.Start(
		context.Background(), models.StartActivityRequest{Project: "New", Description: "year", StartTime: clock.now},
	)
	require.NoError(t, err)
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	assert.Contains(t, err.Error(), "6")
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{Full: true})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines, "nothing journaled, not even the new activity")
	assert.Len(t, visibleFrom(mac.folder, mac.deviceID()), 6)

	report, err := mac.svc.Run(context.Background(), ports.SyncOptions{AllowDeletions: true})
	require.NoError(t, err)
	assert.Equal(t, 7, report.Written)
	assert.Equal(t, 6, deletions(mac))
	assert.Equal(t, []string{"year"}, descriptions(visibleFrom(mac.folder, mac.deviceID())))
	assertQuiet(t, mac)
}

// The brake lets ordinary deletions through: one record removed by hand, and removals made through
// tock however many.
func TestDeletionBrakeLetsOrdinaryDeletionsThrough(t *testing.T) {
	mac, clock := macWithHistory(t, 6)
	all, err := mac.activities.Find(context.Background(), models.ActivityFilter{})
	require.NoError(t, err)

	require.NoError(t, mac.activities.Remove(context.Background(), all[0]))
	clock.advance(time.Minute)
	assert.Equal(t, 1, mac.sync(t).Written, "one deletion by hand")

	for _, a := range all[1:5] {
		require.NoError(t, mac.tracker.Remove(context.Background(), a))
	}
	clock.advance(time.Minute)
	assert.Equal(t, 4, mac.sync(t).Written, "four removals through tock")
	assert.Equal(t, 5, deletions(mac))
	assert.Len(t, visibleFrom(mac.folder, mac.deviceID()), 1)
}

// An emptied sqlite database (deleted and created again when tock opened it) trips the brake too.
func TestDeletionBrakeStopsAnEmptiedDatabase(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newSQLiteDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	for i := range 4 {
		_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
			Project: "P", Description: "d", StartTime: at(i * 60), EndTime: at(i*60 + 30),
		})
		require.NoError(t, err)
	}
	mac.sync(t)
	all, err := mac.activities.Find(context.Background(), models.ActivityFilter{})
	require.NoError(t, err)
	for _, a := range all {
		require.NoError(t, mac.activities.Remove(context.Background(), a))
	}

	clock.advance(time.Minute)
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{Full: true})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	assert.Zero(t, deletions(mac))
	for _, change := range mac.folder.lines(mac.deviceID()) {
		assert.Equal(t, journal.TypeActivity, change.Type)
	}
}

// tock sync init is the way out of the brake that deletes nothing: on a tock.txt moved away and started
// again by tock start, it writes the records gone back into the file from the sync folder and journals
// the new activity.
func TestInitRestoresAFileStartedAgain(t *testing.T) {
	mac, clock := macWithHistory(t, 6)
	path := mac.backend.identity.Path
	require.NoError(t, os.Rename(path, path+".bak"))

	clock.advance(time.Hour)
	_, err := mac.tracker.Start(
		context.Background(), models.StartActivityRequest{Project: "New", Description: "year", StartTime: clock.now},
	)
	require.NoError(t, err)
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)

	report := mac.init(t)
	assert.Equal(t, 6, report.Applied)
	assert.Equal(t, 1, report.Written)
	assert.Len(t, mac.list(t), 7)
	assert.Zero(t, deletions(mac))
	assert.Len(t, visibleFrom(mac.folder, mac.deviceID()), 7)
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// The same for a sqlite database emptied or created again: it is always there, so init writes the
// records back from the sync folder instead of deleting them everywhere.
func TestInitRestoresAnEmptiedDatabase(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newSQLiteDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	for i := range 4 {
		_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
			Project: "P", Description: "d", StartTime: at(i * 60), EndTime: at(i*60 + 30), Notes: "n", Tags: []string{"x"},
		})
		require.NoError(t, err)
	}
	mac.sync(t)
	all, err := mac.activities.Find(context.Background(), models.ActivityFilter{})
	require.NoError(t, err)
	for _, a := range all {
		require.NoError(t, mac.activities.Remove(context.Background(), a))
	}

	clock.advance(time.Minute)
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	report := mac.init(t)
	assert.Equal(t, 4, report.Applied)
	assert.Zero(t, report.Written)
	assert.Zero(t, deletions(mac))
	restored := mac.list(t)
	require.Len(t, restored, 4)
	for _, a := range restored {
		assert.Equal(t, "n", a.Notes)
		assert.Equal(t, []string{"x"}, a.Tags)
	}
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// untouched is what an init stopped by the brake leaves as it was: the data file, this device's journal
// and state.db.
type untouched struct {
	data  string
	lines int
	saves int
	meta  ports.SyncMeta
}

func untouchedOf(t *testing.T, d *device) untouched {
	t.Helper()
	return untouched{data: fileText(t, d), lines: len(d.folder.lines(d.deviceID())), saves: len(d.state.saves), meta: d.state.meta}
}

// assertInitStops runs tock sync init on a backend that holds edits it cannot tell from stale values:
// it stops at the brake and changes nothing.
func assertInitStops(t *testing.T, d *device) {
	t.Helper()
	before := untouchedOf(t, d)
	_, err := d.svc.Init(context.Background(), ports.SyncInitOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	assert.Equal(t, before, untouchedOf(t, d))
}

// backupSuffix is the suffix of the copies a restore makes now.
func backupSuffix(clock *testClock) string {
	return ".before-restore-" + clock.now.Format("20060102-150405")
}

// matchSuffix is the suffix of the copies the match of tock sync init past the brake makes now.
func matchSuffix(clock *testClock) string {
	return ".before-match-" + clock.now.Format("20060102-150405")
}

// tock.txt replaced by an older copy: since the copy, the running activity was stopped, an activity
// moved and six added. Init stops at the brake: the copy's stale values look like edits. Restore writes
// the file again from the folder, keeping the copy next to it, journals nothing and loses no record.
func TestRestoreOverAnOlderCopyOfTheDataFile(t *testing.T) {
	mac, clock := macWithHistory(t, 8)
	ctx := context.Background()
	_, err := mac.tracker.Start(ctx, models.StartActivityRequest{Project: "Run", Description: "r", StartTime: clock.now})
	require.NoError(t, err)
	clock.advance(time.Minute)
	mac.sync(t)
	path := mac.backend.identity.Path
	older, err := os.ReadFile(path)
	require.NoError(t, err)

	clock.advance(30 * time.Minute)
	_, err = mac.tracker.Stop(ctx, models.StopActivityRequest{EndTime: clock.now})
	require.NoError(t, err)
	moved := mac.list(t)[3]
	to := moved.StartTime.Add(10 * time.Minute)
	_, err = mac.tracker.Update(ctx, moved, models.UpdateActivityRequest{StartTime: &to})
	require.NoError(t, err)
	for i := range 6 {
		start := clock.now.Add(time.Duration(i+1) * time.Hour)
		_, err = mac.tracker.Add(ctx, models.AddActivityRequest{
			Project: "New", Description: "n", StartTime: start, EndTime: start.Add(10 * time.Minute),
		})
		require.NoError(t, err)
	}
	clock.advance(8 * time.Hour)
	mac.sync(t)
	synced := mac.list(t)
	others := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, others, 15)
	lines := len(mac.folder.lines(mac.deviceID()))

	require.NoError(t, os.WriteFile(path, older, 0o600))
	clock.advance(time.Minute)
	_, err = mac.svc.Run(ctx, ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	mac.build()
	clock.advance(time.Minute)
	assertInitStops(t, mac)

	report := mac.restore(t)
	notesDir := filepath.Join(filepath.Dir(path), ".tock", "notes")
	require.Equal(t, []string{path + backupSuffix(clock), notesDir + backupSuffix(clock)}, report.Backups)
	kept, err := os.ReadFile(report.Backups[0])
	require.NoError(t, err)
	assert.Equal(t, string(older), string(kept))
	assert.Equal(t, synced, mac.list(t))
	assert.Zero(t, report.Written)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	assert.Equal(t, others, visibleFrom(mac.folder, mac.deviceID()))
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// Six lines deleted by hand and one moved: init stops at the brake, as the move cannot be told from a
// stale value. Restore undoes both, the edited file kept next to it.
func TestRestoreAfterAHandEditOfTheDataFile(t *testing.T) {
	mac, clock := macWithHistory(t, 10)
	synced := mac.list(t)
	others := visibleFrom(mac.folder, mac.deviceID())
	lines := len(mac.folder.lines(mac.deviceID()))
	path := mac.backend.identity.Path
	rows := strings.Split(strings.TrimSpace(fileText(t, mac)), "\n")
	edited := strings.ReplaceAll(strings.Join(rows[6:], "\n")+"\n", "15:00 - ", "15:10 - ")
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o600))
	clock.advance(time.Minute)
	_, err := mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	mac.build()
	clock.advance(time.Minute)
	assertInitStops(t, mac)

	report := mac.restore(t)
	require.Equal(t, []string{path + backupSuffix(clock)}, report.Backups, "no notes to copy")
	kept, err := os.ReadFile(report.Backups[0])
	require.NoError(t, err)
	assert.Equal(t, edited, string(kept))
	assert.Equal(t, synced, mac.list(t))
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	assert.Equal(t, others, visibleFrom(mac.folder, mac.deviceID()))
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// A stop and a note made while every sync stopped at the brake, in a process whose recorded writes are
// gone, are edits init cannot tell from stale values: it stops. Restore lets the folder win, and they
// stay in the copies of tock.txt and of the notes it makes first.
func TestRestoreKeepsEditsMadeWhileBrakedOnlyInTheBackup(t *testing.T) {
	mac, clock := macWithHistory(t, 10)
	ctx := context.Background()
	_, err := mac.tracker.Start(ctx, models.StartActivityRequest{Project: "Run", Description: "r", StartTime: clock.now})
	require.NoError(t, err)
	clock.advance(time.Minute)
	mac.sync(t)
	others := visibleFrom(mac.folder, mac.deviceID())
	lines := len(mac.folder.lines(mac.deviceID()))

	path := mac.backend.identity.Path
	rows := strings.Split(strings.TrimSpace(fileText(t, mac)), "\n")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(rows[6:], "\n")+"\n"), 0o600))
	clock.advance(time.Minute)
	_, err = mac.svc.Run(ctx, ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	clock.advance(30 * time.Minute)
	_, err = mac.tracker.Stop(ctx, models.StopActivityRequest{EndTime: clock.now})
	require.NoError(t, err)
	noted := mac.list(t)[0]
	_, err = mac.tracker.AddNote(ctx, noted, "note while braked")
	require.NoError(t, err)
	_, err = mac.svc.Run(ctx, ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	stopped := fileText(t, mac)

	mac.build()
	clock.advance(time.Minute)
	assertInitStops(t, mac)
	report := mac.restore(t)
	notesDir := filepath.Join(filepath.Dir(path), ".tock", "notes")
	require.Equal(t, []string{path + backupSuffix(clock), notesDir + backupSuffix(clock)}, report.Backups)
	kept, err := os.ReadFile(report.Backups[0])
	require.NoError(t, err)
	assert.Equal(t, stopped, string(kept))
	keptNote, _, err := notes.NewRepository(report.Backups[1]).Get(ctx, noted.ID(), noted.StartTime)
	require.NoError(t, err)
	assert.Equal(t, "note while braked", keptNote)

	assert.Len(t, mac.list(t), 11)
	assert.Empty(t, localNotes(t, mac)[noted.StartTime.UTC()])
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	assert.Equal(t, others, visibleFrom(mac.folder, mac.deviceID()))
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// Restore on sqlite backs the database up with a copy that opens as a database of its own and holds
// what the backend held before.
func TestRestoreBacksUpTheSQLiteDatabase(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newSQLiteDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	ctx := context.Background()
	for i := range 4 {
		_, err := mac.tracker.Add(ctx, models.AddActivityRequest{
			Project: "P", Description: "d", StartTime: at(i * 60), EndTime: at(i*60 + 30),
		})
		require.NoError(t, err)
	}
	clock.advance(4 * time.Hour)
	mac.sync(t)
	synced := mac.list(t)
	for _, a := range synced[:3] {
		require.NoError(t, mac.activities.Remove(ctx, a))
	}
	edited := synced[3]
	edited.Description = "by hand"
	require.NoError(t, mac.activities.Save(ctx, edited))
	clock.advance(time.Minute)
	_, err := mac.svc.Run(ctx, ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)
	_, err = mac.svc.Init(ctx, ports.SyncInitOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)

	report := mac.restore(t)
	path := mac.backend.identity.Path
	require.Equal(t, []string{path + backupSuffix(clock)}, report.Backups)
	copied, err := sqlite.NewSQLiteActivityRepository(ctx, report.Backups[0])
	require.NoError(t, err)
	t.Cleanup(func() { _ = copied.DB.Close() })
	held, err := copied.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, "by hand", held[0].Description)
	assert.Equal(t, descriptions(synced), descriptions(mac.list(t)))
	assert.Len(t, visibleFrom(mac.folder, mac.deviceID()), 4)
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// Restore makes the reminders those of the folder too: one added outside tock since the last sync is
// removed, one removed outside tock comes back, and reminders.yaml is kept as it was next to it.
func TestRestoreWritesTheRemindersOfTheFolder(t *testing.T) {
	mac, clock := macWithHistory(t, 1)
	ctx := context.Background()
	_, err := mac.recRem.Save(ctx, models.Reminder{Title: "Standup", Due: at(60)})
	require.NoError(t, err)
	clock.advance(time.Minute)
	mac.sync(t)
	synced, err := mac.reminders.List(ctx)
	require.NoError(t, err)
	require.Len(t, synced, 1)
	lines := len(mac.folder.lines(mac.deviceID()))

	require.NoError(t, mac.reminders.Remove(ctx, synced[0].ID))
	_, err = mac.reminders.Save(ctx, models.Reminder{Title: "Dentist", Due: at(120)})
	require.NoError(t, err)
	edited, err := os.ReadFile(mac.backend.files[1])
	require.NoError(t, err)

	mac.build()
	clock.advance(time.Minute)
	report := mac.restore(t)
	require.Equal(t, []string{mac.backend.files[1] + backupSuffix(clock)}, report.Backups)
	kept, err := os.ReadFile(report.Backups[0])
	require.NoError(t, err)
	assert.Equal(t, string(edited), string(kept))
	held, err := mac.reminders.List(ctx)
	require.NoError(t, err)
	require.Len(t, held, 1)
	assert.Equal(t, "Standup", held[0].Title)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// assertRestoreRefused runs tock sync init --restore and checks that it fails with want and changes
// nothing: no backup, no journal line, no state.
func assertRestoreRefused(t *testing.T, d *device, opts ports.SyncInitOptions, want error) {
	t.Helper()
	before := untouchedOf(t, d)
	opts.Restore = true
	_, err := d.svc.Init(context.Background(), opts)
	require.ErrorIs(t, err, want)
	assert.Equal(t, before, untouchedOf(t, d))
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(d.backend.identity.Path), "*.before-restore-*"))
	require.NoError(t, err)
	assert.Empty(t, backups)
}

// Restore lets the folder win, so it refuses a folder that holds no record, as one emptied or made
// again since this Mac synced with it: the backend would be emptied. It writes not even format.json.
func TestRestoreRefusesAnEmptyFolder(t *testing.T) {
	mac, clock := macWithHistory(t, 3)
	mac.folder = newMemFolder()
	mac.build()
	clock.advance(time.Minute)
	assertRestoreRefused(t, mac, ports.SyncInitOptions{}, coreErrors.ErrSyncNothingToRestore)
	segments, err := mac.folder.Segments(context.Background())
	require.NoError(t, err)
	assert.Empty(t, segments)
	assert.Nil(t, mac.folder.format)
	assert.Len(t, mac.list(t), 3)
}

// A folder whose records the phone has all deleted would empty the backend as well: restore refuses it
// too, once it read them.
func TestRestoreRefusesAFolderOfDeletedRecords(t *testing.T) {
	mac, clock := macWithHistory(t, 3)
	for _, record := range mac.state.records {
		clock.advance(time.Minute)
		phoneWrite(t, mac.folder, clock, 0, record.UID, map[string]any{"deleted": true})
	}
	clock.advance(time.Minute)
	assertRestoreRefused(t, mac, ports.SyncInitOptions{}, coreErrors.ErrSyncNothingToRestore)
	assert.Len(t, mac.list(t), 3)
}

// Restore replaces what the backend holds with what the folder holds, so it refuses a backend this Mac
// has not synced with this folder yet: its history is in no journal, and would be lost.
func TestRestoreRefusesWhatThisMacNeverSynced(t *testing.T) {
	t.Run("first init on another device's folder", func(t *testing.T) {
		clock := &testClock{now: at(0)}
		mac := newFileDevice(t, "mac", newMemFolder(), clock)
		phoneWrite(t, mac.folder, clock, 0, newTestUID(t), map[string]any{
			"start": at(60).Format(time.RFC3339), "end": at(90).Format(time.RFC3339), "project": "Phone", "description": "p",
		})
		for i := range 3 {
			saved := models.Activity{Project: "Local", Description: "history", StartTime: at(i * 60), EndTime: new(at(i*60 + 30))}
			require.NoError(t, mac.activities.Save(context.Background(), saved))
		}
		clock.advance(time.Hour)
		assertRestoreRefused(t, mac, ports.SyncInitOptions{}, coreErrors.ErrSyncRestoreNotSynced)
		assert.Nil(t, mac.folder.format)
		assert.Len(t, mac.list(t), 3)
	})
	t.Run("another folder", func(t *testing.T) {
		mac, clock := macWithHistory(t, 3)
		clock.advance(time.Minute)
		assertRestoreRefused(t, mac, ports.SyncInitOptions{Dir: "/elsewhere"}, coreErrors.ErrSyncRestoreNotSynced)
	})
}

// Restore waits for the folder's downloads: a journal not downloaded yet may hold records it would
// otherwise remove from the backend.
func TestRestoreWaitsForDownloads(t *testing.T) {
	mac, clock := macWithHistory(t, 3)
	mac.folder.pending = []string{"journal/" + phone + "/2026-11.jsonl"}
	clock.advance(time.Minute)
	assertRestoreRefused(t, mac, ports.SyncInitOptions{}, coreErrors.ErrSyncRestorePending)
	mac.folder.pending = nil
	report := mac.restore(t)
	assert.Empty(t, report.Backups, "nothing to rewrite")
}

// tock.txt moved away and started again by tock start while reminders.yaml stays: no activity pairs
// with an image, so init matches the activities on its own, writing the records gone back and
// journaling the new one, while the reminders, which pair, are compared as in any sync and keep an edit.
func TestInitMatchesActivitiesStartedOverWhileRemindersPair(t *testing.T) {
	mac, clock := macWithHistory(t, 10)
	ctx := context.Background()
	_, err := mac.recRem.Save(ctx, models.Reminder{Title: "Pay rent", Due: at(240)})
	require.NoError(t, err)
	clock.advance(time.Minute)
	mac.sync(t)
	require.NoError(t, os.Remove(mac.backend.identity.Path))
	_, err = mac.tracker.Start(ctx, models.StartActivityRequest{Project: "New", Description: "n", StartTime: clock.now})
	require.NoError(t, err)
	held, err := mac.reminders.List(ctx)
	require.NoError(t, err)
	held[0].Title = "Pay rent by hand"
	_, err = mac.reminders.Save(ctx, held[0])
	require.NoError(t, err)
	_, err = mac.svc.Run(ctx, ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)

	mac.build()
	clock.advance(time.Minute)
	report := mac.init(t)
	require.Equal(t, []string{mac.backend.identity.Path + matchSuffix(clock)}, report.Backups, "no notes, no reminder written")
	kept, err := os.ReadFile(report.Backups[0])
	require.NoError(t, err)
	assert.Contains(t, string(kept), "| New | n")
	assert.Zero(t, deletions(mac))
	assert.Len(t, mac.list(t), 11)
	others := visibleFrom(mac.folder, mac.deviceID())
	assert.Len(t, others, 11)
	assert.Contains(t, descriptions(others), "n")
	assert.Contains(t, reminderTitles(mac.folder, mac.deviceID()), "Pay rent by hand")
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// tock.txt moved away and started again after a note was edited by hand and another one added through
// tock. The match past the brake writes the activities back from the folder, the synced note over the
// hand edit, so it first copies the files it rewrites, as a restore does; the note the folder does not
// hold yet stays and is journaled by the next sync.
func TestInitMatchBacksUpWhatItRewrites(t *testing.T) {
	mac, clock := macWithHistory(t, 10)
	ctx := context.Background()
	startingAt := func(start time.Time) models.Activity {
		t.Helper()
		for _, a := range mac.list(t) {
			if a.StartTime.Equal(start) {
				return a
			}
		}
		t.Fatalf("no activity at %s", start)
		return models.Activity{}
	}
	_, err := mac.tracker.AddNote(ctx, startingAt(at(360)), "synced")
	require.NoError(t, err)
	clock.advance(time.Minute)
	mac.sync(t)

	_, err = mac.tracker.AddNote(ctx, startingAt(at(420)), "unsynced note")
	require.NoError(t, err)
	edited := startingAt(at(360))
	require.NoError(t, mac.notes.Save(ctx, edited.ID(), edited.StartTime, "hand edit", nil))
	require.NoError(t, os.Remove(mac.backend.identity.Path))
	_, err = mac.tracker.Start(ctx, models.StartActivityRequest{Project: "New", Description: "n", StartTime: clock.now})
	require.NoError(t, err)
	_, err = mac.svc.Run(ctx, ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncTooManyDeletions)

	mac.build()
	clock.advance(time.Minute)
	report := mac.init(t)
	path := mac.backend.identity.Path
	notesDir := filepath.Join(filepath.Dir(path), ".tock", "notes")
	require.Equal(t, []string{path + matchSuffix(clock), notesDir + matchSuffix(clock)}, report.Backups)
	kept, _, err := notes.NewRepository(report.Backups[1]).Get(ctx, edited.ID(), edited.StartTime)
	require.NoError(t, err)
	assert.Equal(t, "hand edit", kept)
	held := localNotes(t, mac)
	assert.Equal(t, "synced", held[at(360).UTC()])
	assert.Equal(t, "unsynced note", held[at(420).UTC()])

	clock.advance(time.Minute)
	mac.full(t)
	assert.Equal(t, "unsynced note", localNotes(t, mac)[at(420).UTC()])
	var seen []string
	for _, a := range visibleFrom(mac.folder, mac.deviceID()) {
		seen = append(seen, a.Notes)
	}
	assert.Contains(t, seen, "unsynced note")
}

// A first init of a Mac whose tock.txt and notes hold data, on a folder the phone already uses with
// records for some of the same activities: the match writes the folder's values into the backend, so
// it first copies the files it rewrites as they were. The folder loses nothing.
func TestFirstInitWithDataBacksUpWhatItRewrites(t *testing.T) {
	ctx := context.Background()
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	for i := range 3 {
		saved := models.Activity{Project: "Local", Description: "history", StartTime: at(i * 60), EndTime: new(at(i*60 + 30))}
		require.NoError(t, mac.activities.Save(ctx, saved))
	}
	noted := mac.list(t)[0]
	require.NoError(t, mac.notes.Save(ctx, noted.ID(), noted.StartTime, "mac note", nil))
	for i, notes := range []string{"phone note", ""} {
		phoneWrite(t, mac.folder, clock, i, newTestUID(t), map[string]any{
			"start": at(i * 60).Format(time.RFC3339), "end": at(i*60 + 30).Format(time.RFC3339),
			"project": "Local", "description": "history", "notes": notes,
		})
	}
	phoneWrite(t, mac.folder, clock, 2, newTestUID(t), map[string]any{
		"start": at(240).Format(time.RFC3339), "end": at(270).Format(time.RFC3339), "project": "Phone", "description": "p",
	})
	before := fileText(t, mac)

	clock.advance(time.Hour)
	report := mac.init(t)
	path := mac.backend.identity.Path
	notesDir := filepath.Join(filepath.Dir(path), ".tock", "notes")
	require.Equal(t, []string{path + matchSuffix(clock), notesDir + matchSuffix(clock)}, report.Backups)
	kept, err := os.ReadFile(report.Backups[0])
	require.NoError(t, err)
	assert.Equal(t, before, string(kept))
	keptNote, _, err := notes.NewRepository(report.Backups[1]).Get(ctx, noted.ID(), noted.StartTime)
	require.NoError(t, err)
	assert.Equal(t, "mac note", keptNote)

	others := visibleFrom(mac.folder, phone, mac.deviceID())
	assert.Len(t, others, 4)
	var seen []string
	for _, a := range others {
		seen = append(seen, a.Notes)
	}
	assert.Contains(t, seen, "phone note")
	assert.Contains(t, descriptions(others), "p")
	assert.Len(t, mac.list(t), 4)
}
