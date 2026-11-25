package sync_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// changesOf returns the changes a device journaled for one record.
func changesOf(d *device, uid string) []journal.Change {
	var out []journal.Change
	for _, change := range d.folder.lines(d.deviceID()) {
		if change.UID == uid {
			out = append(out, change)
		}
	}
	return out
}

// A sync on another backend than the one sync was set up with, as tock -f other.txt runs it, leaves
// both alone: it neither merges the other file into the folder nor records its writes.
func TestOtherBackendIsLeftAlone(t *testing.T) {
	clock := &testClock{now: at(120)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	save(t, mac.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	mac.init(t)
	lines := len(folder.lines(mac.deviceID()))
	saves := len(mac.state.saves)

	other := newFileDevice(t, "mac", folder, clock)
	other.state = mac.state
	other.build()
	_, err := other.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Scratch", Description: "one-off", StartTime: at(60), EndTime: at(70),
	})
	require.NoError(t, err)

	clock.advance(time.Minute)
	_, err = other.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncBackendChanged)
	_, err = other.svc.Run(context.Background(), ports.SyncOptions{Full: true})
	require.ErrorIs(t, err, coreErrors.ErrSyncBackendChanged)
	assert.Len(t, folder.lines(mac.deviceID()), lines, "nothing of the other file is journaled")
	assert.Len(t, mac.state.saves, saves, "the state is left alone")
	assert.Equal(t, []string{"Scratch"}, projects(other.list(t)), "nothing is written into the other file")

	report := mac.sync(t)
	assert.Zero(t, report.Written)
	assert.Equal(t, []string{"Backend"}, projects(mac.list(t)))
}

// After a backend switch the new backend's records are matched to the merged ones by start, project
// and description: the matches are not journaled again, and the rest of the merged state is written
// into the new backend.
func TestBackendSwitchJournalsOnlyWhatIsNew(t *testing.T) {
	clock := &testClock{now: at(120)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	save(t, mac.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	save(t, mac.activities, models.Activity{Project: "Docs", Description: "README", StartTime: at(60), EndTime: new(at(90))})
	mac.init(t)
	lines := len(folder.lines(mac.deviceID()))

	// The same data moved to timewarrior, without the README, plus one activity only it has.
	switched := newTimewarriorDevice(t, "mac", folder, clock)
	switched.state = mac.state
	switched.build()
	save(t, switched.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	save(t, switched.activities, models.Activity{Project: "Ops", Description: "deploy", StartTime: at(100), EndTime: new(at(110))})

	clock.advance(time.Minute)
	report := switched.init(t)
	assert.Equal(t, 1, report.Written, "only the activity the folder does not hold")
	assert.Equal(t, 1, report.Applied, "the README is written into the new backend")
	assert.Len(t, folder.lines(mac.deviceID()), lines+1)
	assert.Equal(t, []string{"Backend", "Docs", "Ops"}, projects(switched.list(t)))
	assert.Equal(t, switched.backend.identity, switched.state.meta.Backend)
	assertQuiet(t, switched)
}

func projects(activities []models.Activity) []string {
	out := make([]string, 0, len(activities))
	for _, a := range activities {
		out = append(out, a.Project)
	}
	return out
}

// Reminder numbers are local: one arriving from another device gets the next free number here.
func TestReminderNumbersAreLocal(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	studio := newTimewarriorDevice(t, "studio", folder, clock)
	ctx := context.Background()

	for _, title := range []string{"Water plants", "Review"} {
		_, err := mac.reminders.Save(ctx, models.Reminder{Title: title, Due: at(240), Created: at(-60)})
		require.NoError(t, err)
	}
	_, err := studio.reminders.Save(ctx, models.Reminder{Title: "Call", Due: at(300), AllDay: true, Created: at(-30)})
	require.NoError(t, err)
	mac.init(t)
	studio.init(t)
	mac.sync(t)

	numbers := func(d *device) map[string]int {
		list, listErr := d.reminders.List(ctx)
		require.NoError(t, listErr)
		out := make(map[string]int)
		for _, r := range list {
			out[r.Title] = r.ID
		}
		return out
	}
	assert.Equal(t, map[string]int{"Water plants": 1, "Review": 2, "Call": 3}, numbers(mac))
	assert.Equal(t, map[string]int{"Call": 1, "Water plants": 2, "Review": 3}, numbers(studio))

	// Done on the studio, done on the Mac; removed on the Mac, removed on the studio.
	list, err := studio.reminders.List(ctx)
	require.NoError(t, err)
	review := list[slices.IndexFunc(list, func(r models.Reminder) bool { return r.Title == "Review" })]
	done := at(250)
	review = review.WithState(review.Due, models.OccurrenceState{DoneAt: &done})
	_, err = studio.recRem.Save(ctx, review)
	require.NoError(t, err)
	studio.sync(t)
	mac.sync(t)
	macList, err := mac.reminders.List(ctx)
	require.NoError(t, err)
	macReview := macList[slices.IndexFunc(macList, func(r models.Reminder) bool { return r.Title == "Review" })]
	require.NotNil(t, macReview.State(macReview.Due).DoneAt)
	assert.True(t, macReview.State(macReview.Due).DoneAt.Equal(done))

	require.NoError(t, mac.recRem.Remove(ctx, 1))
	mac.sync(t)
	studio.sync(t)
	assert.Equal(t, map[string]int{"Call": 1, "Review": 3}, numbers(studio))
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

// Writes made through tock name what to compare: while nothing else changed the backend, a run
// compares only them and does not ask what changed.
func TestRecordedWritesSkipTheChangedScope(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	ctx := context.Background()

	_, err := mac.tracker.Add(ctx, models.AddActivityRequest{Project: "Backend", Description: "API", StartTime: at(0), EndTime: at(30)})
	require.NoError(t, err)
	report := mac.sync(t)
	assert.Equal(t, 1, report.Written)
	assert.Zero(t, mac.backend.changedCalls)

	// A change by hand moves the token: the next run asks what changed.
	save(t, mac.activities, models.Activity{Project: "Docs", Description: "by hand", StartTime: at(60), EndTime: new(at(70))})
	report = mac.sync(t)
	assert.Equal(t, 1, report.Written)
	assert.Equal(t, 1, mac.backend.changedCalls)

	// A change by hand between two writes through tock is found too.
	_, err = mac.tracker.Add(ctx, models.AddActivityRequest{Project: "One", Description: "tock", StartTime: at(80), EndTime: at(85)})
	require.NoError(t, err)
	save(t, mac.activities, models.Activity{Project: "Two", Description: "by hand", StartTime: at(90), EndTime: new(at(95))})
	_, err = mac.tracker.Add(ctx, models.AddActivityRequest{Project: "Three", Description: "tock", StartTime: at(100), EndTime: at(105)})
	require.NoError(t, err)
	report = mac.sync(t)
	assert.Equal(t, 3, report.Written)
	assertQuiet(t, mac)
}

// An activity whose start moved is one activity, whether it moved by hand (same project and
// description) or through tock (an update that also renames it).
func TestMovedActivityKeepsItsUID(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	ctx := context.Background()
	save(t, mac.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	mac.full(t)
	uid := uidOf(t, mac, "Backend")
	studio.sync(t)

	clock.advance(time.Hour)
	moved := models.Activity{Project: "Backend", Description: "API", StartTime: at(5), EndTime: new(at(45))}
	require.NoError(t, mac.activities.Remove(ctx, mac.list(t)[0]))
	save(t, mac.activities, moved)
	mac.sync(t)
	changes := changesOf(mac, uid)
	require.Len(t, changes, 2)
	assert.Equal(t, []string{"start"}, keys(changes[1].Set))

	clock.advance(time.Minute)
	_, err := mac.tracker.Update(ctx, mac.list(t)[0], models.UpdateActivityRequest{StartTime: new(at(10)), Description: new("API v2")})
	require.NoError(t, err)
	mac.sync(t)
	changes = changesOf(mac, uid)
	require.Len(t, changes, 3)
	assert.Equal(t, []string{"description", "start"}, keys(changes[2].Set))

	studio.sync(t)
	got := studio.list(t)
	require.Len(t, got, 1)
	assert.True(t, got[0].StartTime.Equal(at(10)))
	assert.Equal(t, "API v2", got[0].Description)
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

func keys(set map[string]json.RawMessage) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

// The sqlite backend keeps uids: they are the journal's, and other devices' records get theirs.
func TestSQLiteBackendKeepsUIDs(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	db := newSQLiteDevice(t, "db", folder, clock)
	mac := newFileDevice(t, "mac", folder, clock)
	ctx := context.Background()

	_, err := db.tracker.Add(ctx, models.AddActivityRequest{
		Project:     "Backend",
		Description: "API",
		StartTime:   at(0).Add(7 * time.Second),
		EndTime:     at(30),
		Notes:       "inline",
		Tags:        []string{"sql"},
	})
	require.NoError(t, err)
	db.init(t)
	mac.init(t)

	stored, err := db.activities.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, uidOf(t, db, "Backend"), stored[0].UID)

	_, err = mac.tracker.Add(ctx, models.AddActivityRequest{Project: "Docs", Description: "README", StartTime: at(60), EndTime: at(90)})
	require.NoError(t, err)
	clock.advance(time.Hour)
	mac.sync(t)
	db.sync(t)
	stored, err = db.activities.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	assert.Equal(t, uidOf(t, mac, "Docs"), stored[1].UID)
	assert.Equal(t, summary(mac.list(t)), summary(db.list(t)))

	// Edited with sqlite3 by hand: found by uid.
	stored[0].Description = "API by hand"
	stored[0].StartTime = stored[0].StartTime.Add(2 * time.Minute)
	require.NoError(t, db.activities.Remove(ctx, models.Activity{StartTime: at(0).Add(7 * time.Second)}))
	save(t, db.activities, stored[0])
	db.sync(t)
	mac.sync(t)
	assert.Equal(t, []string{"API by hand", "README"}, descriptions(mac.list(t)))
	assertQuiet(t, db)
	assertQuiet(t, mac)
	assert.Zero(t, db.full(t).Written)
}

func descriptions(activities []models.Activity) []string {
	out := make([]string, 0, len(activities))
	for _, a := range activities {
		out = append(out, a.Description)
	}
	return out
}

func TestStatusDescribesTheFolder(t *testing.T) {
	mac, studio, _ := twoMacs(t)
	status, err := mac.svc.Status(context.Background())
	require.NoError(t, err)
	assert.True(t, status.Initialized)
	assert.Equal(t, "/sync", status.Dir)
	assert.Equal(t, mac.deviceID(), status.Device)
	assert.Equal(t, "file", status.Backend.Name)
	assert.False(t, status.LastSync.IsZero())
	ids := []string{status.Devices[0].ID, status.Devices[1].ID}
	assert.ElementsMatch(t, []string{mac.deviceID(), studio.deviceID()}, ids)
}

// editedReminders runs a hand edit, as an editor saving reminders.yaml makes without tock's lock, right
// before the List call it is set for.
type editedReminders struct {
	ports.ReminderRepository

	calls int
	at    int
	edit  func()
}

func (r *editedReminders) List(ctx context.Context) ([]models.Reminder, error) {
	r.calls++
	if r.calls == r.at {
		r.edit()
	}
	return r.ReminderRepository.List(ctx)
}

// A reminder edited by hand after the run read it is not overwritten with another device's change:
// the write-back leaves it, and the next run journals the edit.
func TestReminderEditedDuringASyncIsNotOverwritten(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	studio := newTimewarriorDevice(t, "studio", folder, clock)
	ctx := context.Background()
	_, err := mac.reminders.Save(ctx, models.Reminder{Title: "Call", Due: at(240), Created: at(-60)})
	require.NoError(t, err)
	mac.init(t)
	studio.init(t)
	notesOf := func(d *device) string {
		list, listErr := d.reminders.List(ctx)
		require.NoError(t, listErr)
		require.Len(t, list, 1)
		return list[0].Notes
	}

	list, err := studio.reminders.List(ctx)
	require.NoError(t, err)
	list[0].Notes = "from the studio"
	_, err = studio.recRem.Save(ctx, list[0])
	require.NoError(t, err)
	clock.advance(time.Minute)
	studio.sync(t)

	stored := mac.reminders
	mac.reminders = &editedReminders{ReminderRepository: stored, at: 2, edit: func() {
		mine, listErr := stored.List(ctx)
		require.NoError(t, listErr)
		mine[0].Notes = "by hand"
		_, saveErr := stored.Save(ctx, mine[0])
		require.NoError(t, saveErr)
	}}
	mac.build()
	clock.advance(time.Minute)
	mac.full(t)
	assert.Equal(t, "by hand", notesOf(mac), "the hand edit stays")

	mac.reminders = stored
	mac.build()
	clock.advance(time.Minute)
	mac.full(t)
	studio.sync(t)
	assert.Equal(t, "by hand", notesOf(mac))
	assert.Equal(t, "by hand", notesOf(studio))
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}
