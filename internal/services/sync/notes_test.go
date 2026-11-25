package sync_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// failingNotes fails every Save after the first ok ones, as a process killed or a disk error in the
// middle of the notes a write-back writes after its lines.
type failingNotes struct {
	ports.NotesRepository

	ok    int
	saves int
}

func (f *failingNotes) Save(ctx context.Context, id string, date time.Time, text string, tags []string) error {
	f.saves++
	if f.saves > f.ok {
		return errors.New("killed")
	}
	return f.NotesRepository.Save(ctx, id, date, text, tags)
}

// localNotes returns the notes tock shows on a device for each activity of its backend, by start.
func localNotes(t *testing.T, d *device) map[time.Time]string {
	t.Helper()
	out := make(map[time.Time]string)
	for _, a := range d.list(t) {
		notes, _, err := d.notes.Get(context.Background(), a.ID(), a.StartTime)
		require.NoError(t, err)
		out[a.StartTime.UTC()] = notes
	}
	return out
}

// A first sync that dies between the lines and their notes leaves lines without notes files: the next
// run adopts them without journaling their notes as cleared, and writes the notes.
func TestNotesLeftUnwrittenAreNotJournaledAsCleared(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	for i := range 5 {
		// A month ago, before the days whose notes a plain sync reads.
		start := at(-60 * 24 * 30).Add(time.Duration(i) * 24 * time.Hour)
		phoneWrite(t, mac.folder, clock, i, newTestUID(t), map[string]any{
			"start": start.Format(time.RFC3339), "end": start.Add(30 * time.Minute).Format(time.RFC3339),
			"project": "P", "description": "d", "notes": "phone note", "tags/x": true,
		})
	}
	clock.advance(time.Minute)

	stored := mac.notes
	mac.notes = &failingNotes{NotesRepository: stored, ok: 2}
	mac.build()
	_, err := mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorContains(t, err, "killed")
	require.Len(t, mac.list(t), 5, "the lines were written")

	mac.notes = stored
	mac.build()
	clock.advance(time.Minute)
	assert.Zero(t, mac.sync(t).Written)
	assert.Empty(t, mac.folder.lines(mac.deviceID()))
	for _, a := range visibleFrom(mac.folder, phone, mac.deviceID()) {
		assert.Equal(t, "phone note", a.Notes)
		assert.Equal(t, []string{"x"}, a.Tags)
	}
	for start, notes := range localNotes(t, mac) {
		assert.Equal(t, "phone note", notes, "the notes of the activity at %s are written", start)
	}
	clock.advance(time.Minute)
	assert.Zero(t, mac.full(t).Written)
	assertQuiet(t, mac)
}

// Notes cleared by tock, which saves them empty, are a change even from a process whose own sync did
// not run: only a notes file that is not there is left alone.
func TestEmptiedNotesFileIsJournaled(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "P", Description: "d", StartTime: at(0), EndTime: at(30), Notes: "note", Tags: []string{"x"},
	})
	require.NoError(t, err)
	clock.now = at(60)
	mac.sync(t)

	a := mac.list(t)[0]
	require.NoError(t, mac.notes.Save(context.Background(), a.ID(), a.StartTime, "", nil))
	clock.advance(time.Minute)
	assert.Equal(t, 1, mac.full(t).Written)
	visible := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, visible, 1)
	assert.Empty(t, visible[0].Notes)
	assert.Empty(t, visible[0].Tags)
}

// timewarrior keeps UTC, and notes are filed under the local start: after a zone change a full sync
// finds no notes file, and it neither journals the notes as cleared nor, back home, takes the notes
// left there for an edit of what the phone changed meanwhile.
func TestTimewarriorNotesSurviveAZoneChange(t *testing.T) {
	msk := inZone(t, "Europe/Moscow")
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, msk)
	clock := &testClock{now: start}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "w", StartTime: start, EndTime: start.Add(30 * time.Minute),
		Notes: "important note", Tags: []string{"client"},
	})
	require.NoError(t, err)
	clock.advance(time.Hour)
	mac.sync(t)
	lines := len(mac.folder.lines(mac.deviceID()))
	uid := uidOf(t, mac, "Mac")

	inZone(t, "Europe/London")
	mac.build()
	clock.advance(time.Hour)
	assert.Zero(t, mac.full(t).Written)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	assert.Equal(t, map[time.Time]string{start.UTC(): "important note"}, localNotes(t, mac), "the notes are filed where tock looks now")
	phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{"notes": "phone edit"})
	clock.advance(time.Minute)
	mac.sync(t)

	inZone(t, "Europe/Moscow")
	mac.build()
	clock.advance(time.Hour)
	assert.Zero(t, mac.full(t).Written, "the notes left in Moscow are no edit")
	visible := visibleFrom(mac.folder, phone, mac.deviceID())
	require.Len(t, visible, 1)
	assert.Equal(t, "phone edit", visible[0].Notes)
	assert.Equal(t, []string{"client"}, visible[0].Tags)
	assert.Equal(t, map[time.Time]string{start.UTC(): "phone edit"}, localNotes(t, mac))
	assertQuiet(t, mac)
}

// A move from another device whose notes the write-back could not write under the new start leaves the
// activity without a notes file there: the next run writes them, and journals nothing.
func TestMovedActivityKeepsNotesLeftUnwritten(t *testing.T) {
	devices := map[string]func(*testing.T, string, *memFolder, *testClock) *device{
		"file": newFileDevice, "timewarrior": newTimewarriorDevice,
	}
	for name, newDevice := range devices {
		t.Run(name, func(t *testing.T) {
			clock := &testClock{now: at(0)}
			mac := newDevice(t, "mac", newMemFolder(), clock)
			mac.init(t)
			_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
				Project: "P", Description: "d", StartTime: at(0), EndTime: at(30), Notes: "note",
			})
			require.NoError(t, err)
			clock.now = at(60)
			mac.sync(t)
			lines := len(mac.folder.lines(mac.deviceID()))
			clock.advance(time.Minute)
			phoneWrite(t, mac.folder, clock, 0, uidOf(t, mac, "P"), map[string]any{"start": at(5).Format(time.RFC3339)})

			stored := mac.notes
			mac.notes = &failingNotes{NotesRepository: stored}
			mac.build()
			clock.advance(time.Minute)
			_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
			require.ErrorContains(t, err, "killed")

			mac.notes = stored
			mac.build()
			clock.advance(time.Minute)
			assert.Zero(t, mac.sync(t).Written)
			assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
			visible := visibleFrom(mac.folder, phone, mac.deviceID())
			require.Len(t, visible, 1)
			assert.Equal(t, "note", visible[0].Notes)
			assert.Equal(t, map[time.Time]string{at(5).UTC(): "note"}, localNotes(t, mac))
			assertQuiet(t, mac)
		})
	}
}

// timewarrior and todotxt keep an activity's tags in its line: timew untag, or an edit of a todo.txt
// line, that removes the last tag of an activity without a notes file is a change, journaled, and not
// written back.
func TestLastTagRemovedFromALineIsJournaled(t *testing.T) {
	for name, newDevice := range map[string]func(*testing.T, string, *memFolder, *testClock) *device{
		"timewarrior": newTimewarriorDevice,
		"todotxt":     newTodoTXTDevice,
	} {
		t.Run(name, func(t *testing.T) {
			clock := &testClock{now: at(120)}
			mac := newDevice(t, "mac", newMemFolder(), clock)
			mac.init(t)
			tagged := models.Activity{Project: "Mac", Description: "work", StartTime: at(0), EndTime: new(at(60)), Tags: []string{"client"}}
			save(t, mac.activities, tagged)
			mac.sync(t)
			visible := visibleFrom(mac.folder, mac.deviceID())
			require.Len(t, visible, 1)
			require.Equal(t, []string{"client"}, visible[0].Tags)

			untagged := tagged
			untagged.Tags = nil
			save(t, mac.activities, untagged)
			clock.advance(time.Minute)
			mac.sync(t)

			visible = visibleFrom(mac.folder, mac.deviceID())
			require.Len(t, visible, 1)
			assert.Empty(t, visible[0].Tags)
			local := mac.list(t)
			require.Len(t, local, 1)
			assert.Empty(t, local[0].Tags, "the tag is not written back")
			clock.advance(time.Minute)
			assertQuiet(t, mac)
		})
	}
}

// Notes edited by hand in an old activity's notes file, which a plain sync does not read, are not
// overwritten with another device's change: the write-back leaves the file, and the next full sync
// journals the edit.
func TestNotesEditedByHandAreNotOverwritten(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	ctx := context.Background()
	start := at(-30 * 24 * 60) // a month ago, before the days whose notes a plain sync reads
	_, err := mac.tracker.Add(ctx, models.AddActivityRequest{
		Project: "P", Description: "d", StartTime: start, EndTime: start.Add(30 * time.Minute), Notes: "first",
	})
	require.NoError(t, err)
	mac.sync(t)
	uid := uidOf(t, mac, "P")

	a := mac.list(t)[0]
	require.NoError(t, mac.notes.Save(ctx, a.ID(), a.StartTime, "by hand", nil))
	clock.advance(time.Minute)
	phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{"notes": "from the phone"})
	clock.advance(time.Minute)
	mac.sync(t)
	assert.Equal(t, map[time.Time]string{start.UTC(): "by hand"}, localNotes(t, mac), "the hand edit stays")

	clock.advance(time.Minute)
	mac.full(t)
	visible := visibleFrom(mac.folder, phone, mac.deviceID())
	require.Len(t, visible, 1)
	assert.Equal(t, "by hand", visible[0].Notes)
	assert.Equal(t, map[time.Time]string{start.UTC(): "by hand"}, localNotes(t, mac))
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}
