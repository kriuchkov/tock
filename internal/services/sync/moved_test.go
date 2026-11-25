package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// A run that wrote another device's move and rename into the backend and stopped before it saved
// leaves the line at the new start and the image at the old one: the next run pairs them by the key
// the merged state moved the record to, and neither deletes the record nor takes the line for a new
// one.
func TestInterruptedMoveIsNotADeletion(t *testing.T) {
	devices := map[string]func(*testing.T, string, *memFolder, *testClock) *device{
		"file": newFileDevice, "timewarrior": newTimewarriorDevice, "sqlite": newSQLiteDevice,
	}
	for name, newDevice := range devices {
		t.Run(name, func(t *testing.T) {
			clock := &testClock{now: time.Date(2026, 12, 3, 9, 0, 0, 0, time.Local)}
			mac := newDevice(t, "mac", newMemFolder(), clock)
			mac.init(t)
			nov := time.Date(2026, 11, 30, 10, 0, 0, 0, time.Local)
			_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
				Project: "A", Description: "a", StartTime: nov, EndTime: nov.Add(time.Hour),
			})
			require.NoError(t, err)
			clock.advance(time.Minute)
			mac.sync(t)
			uid := uidOf(t, mac, "A")
			lines := len(mac.folder.lines(mac.deviceID()))

			clock.advance(time.Minute)
			dec := time.Date(2026, 12, 2, 10, 0, 0, 0, time.Local)
			phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{
				"start": dec.Format(time.RFC3339), "end": dec.Add(time.Hour).Format(time.RFC3339), "description": "renamed",
			})
			clock.advance(time.Minute)
			mac.state.failAt = len(mac.state.saves) + 1
			_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
			require.Error(t, err)
			mac.state.failAt = 0
			require.Equal(t, []string{"renamed"}, descriptions(mac.list(t)))

			mac.build()
			clock.advance(time.Minute)
			assert.Zero(t, mac.sync(t).Written)
			assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
			visible := visibleFrom(mac.folder, phone, mac.deviceID())
			require.Len(t, visible, 1)
			assert.Equal(t, "renamed", visible[0].Description)
			assert.Equal(t, []string{"renamed"}, descriptions(mac.list(t)))
			clock.advance(time.Minute)
			assert.Zero(t, mac.full(t).Written)
		})
	}
}

// handEdited runs a hand edit, as the timew CLI makes without tock's lock, right before the first batch
// a sync writes.
type handEdited struct {
	ports.ActivityRepository

	edit func()
}

func (r *handEdited) Apply(ctx context.Context, writes []ports.ActivityWrite) error {
	if r.edit != nil {
		r.edit()
		r.edit = nil
	}
	return r.ActivityRepository.(ports.ActivityBatchWriter).Apply(ctx, writes)
}

// A batch that moves an activity from one timewarrior month file to another and finds an activity of
// the other file changed by hand writes nothing: the moved activity stays where it was, and the next
// run neither deletes it nor loses the hand edit.
func TestTimewarriorBatchAcrossMonthsIsAllOrNothing(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 12, 3, 9, 0, 0, 0, time.UTC)}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	alpha, beta := newTestUID(t), newTestUID(t)
	nov := time.Date(2026, 11, 30, 10, 0, 0, 0, time.UTC)
	dec := time.Date(2026, 12, 2, 10, 0, 0, 0, time.UTC)
	phoneWrite(t, mac.folder, clock, 0, alpha, map[string]any{
		"start": nov.Format(time.RFC3339), "end": nov.Add(time.Hour).Format(time.RFC3339), "project": "A", "description": "moved",
	})
	phoneWrite(t, mac.folder, clock, 1, beta, map[string]any{
		"start": dec.Format(time.RFC3339), "end": dec.Add(time.Hour).Format(time.RFC3339), "project": "B", "description": "b",
	})
	clock.advance(time.Minute)
	mac.sync(t)

	clock.advance(time.Minute)
	moved := dec.Add(-24 * time.Hour)
	phoneWrite(t, mac.folder, clock, 0, alpha, map[string]any{
		"start": moved.Format(time.RFC3339), "end": moved.Add(time.Hour).Format(time.RFC3339),
	})
	phoneWrite(t, mac.folder, clock, 1, beta, map[string]any{"description": "b from the phone"})
	stored := mac.activities
	mac.activities = &handEdited{ActivityRepository: stored, edit: func() {
		all, err := stored.Find(context.Background(), models.ActivityFilter{})
		require.NoError(t, err)
		for _, a := range all {
			if a.Project == "B" {
				require.NoError(t, stored.Remove(context.Background(), a))
				a.Description = "b by hand"
				require.NoError(t, stored.Save(context.Background(), a))
			}
		}
	}}
	mac.build()
	clock.advance(time.Minute)
	_, err := mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrActivityNotFound)
	assert.ElementsMatch(t, []string{"moved", "b by hand"}, descriptions(mac.list(t)), "nothing was written")

	mac.activities = stored
	mac.build()
	clock.advance(time.Minute)
	mac.sync(t)
	assert.Zero(t, deletions(mac))
	visible := visibleFrom(mac.folder, phone, mac.deviceID())
	assert.ElementsMatch(t, []string{"moved", "b by hand"}, descriptions(visible))
	for _, a := range visible {
		if a.Project == "A" {
			assert.True(t, a.StartTime.Equal(moved), "the move stands")
		}
	}
	assert.ElementsMatch(t, []string{"moved", "b by hand"}, descriptions(mac.list(t)))
	assertQuiet(t, mac)
}

// One process that starts an activity and then removes an older one, as the list TUI does when it
// continues one and deletes another before its sync, made a deletion: only the removal that ends an
// update moving an activity's start is a move. The record removed is deleted everywhere, and the new one
// does not take its notes and tags.
func TestRemovalAfterAStartIsADeletion(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	ctx := context.Background()
	_, err := mac.tracker.Add(ctx, models.AddActivityRequest{
		Project: "Old", Description: "z", StartTime: at(-24 * 60), EndTime: at(-24*60 + 30), Notes: "z notes", Tags: []string{"ztag"},
	})
	require.NoError(t, err)
	clock.advance(time.Hour)
	mac.sync(t)
	old := uidOf(t, mac, "Old")

	_, err = mac.tracker.Start(ctx, models.StartActivityRequest{Project: "New", Description: "x", StartTime: clock.now})
	require.NoError(t, err)
	for _, a := range mac.list(t) {
		if a.Project == "Old" {
			require.NoError(t, mac.tracker.Remove(ctx, a))
		}
	}
	clock.advance(time.Minute)
	mac.sync(t)

	assert.Equal(t, 1, deletions(mac))
	removal := changesOf(mac, old)
	assert.Equal(t, "true", string(removal[len(removal)-1].Set["deleted"]))
	visible := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, visible, 1)
	assert.Equal(t, "New", visible[0].Project)
	assert.Empty(t, visible[0].Notes)
	assert.Empty(t, visible[0].Tags)
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// A timewarrior batch writes the month file that gains a moved activity before the one that loses it:
// a crash between the two renames leaves the activity in both. The next run takes the line left at the
// old start for what it is, removes it, and journals nothing: no copy of the record reaches the others.
func TestTimewarriorMoveLeftInBothMonthsMakesNoCopy(t *testing.T) {
	inZone(t, "UTC")
	clock := &testClock{now: time.Date(2026, 12, 3, 9, 0, 0, 0, time.UTC)}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	uid := newTestUID(t)
	nov := time.Date(2026, 11, 30, 10, 0, 0, 0, time.UTC)
	phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{
		"start": nov.Format(time.RFC3339), "end": nov.Add(time.Hour).Format(time.RFC3339), "project": "A", "description": "moved",
	})
	clock.advance(time.Minute)
	mac.sync(t)
	novFile := filepath.Join(mac.backend.identity.Path, "2026-11.data")
	novData, err := os.ReadFile(novFile)
	require.NoError(t, err)
	lines := len(mac.folder.lines(mac.deviceID()))

	dec := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	clock.advance(time.Minute)
	phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{
		"start": dec.Format(time.RFC3339), "end": dec.Add(time.Hour).Format(time.RFC3339),
	})
	clock.advance(time.Minute)
	mac.state.failAt = len(mac.state.saves) + 1
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.Error(t, err)
	mac.state.failAt = 0
	// December got the interval, and the crash came before November lost it.
	require.NoError(t, os.WriteFile(novFile, novData, 0o600))
	require.Len(t, mac.list(t), 2)

	mac.build()
	clock.advance(time.Minute)
	assert.Zero(t, mac.sync(t).Written)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	got := mac.list(t)
	require.Len(t, got, 1)
	assert.True(t, got[0].StartTime.Equal(dec))
	visible := visibleFrom(mac.folder, phone, mac.deviceID())
	require.Len(t, visible, 1)
	assert.True(t, visible[0].StartTime.Equal(dec))
	clock.advance(time.Minute)
	assert.Zero(t, mac.full(t).Written)
	assertQuiet(t, mac)
}

// The phone moves an activity into another month, onto the start of one exactly like it. The line there
// is that other record's, not one a move left behind: the old line stays the moved record's, which waits
// there for its start to be free, and no record is deleted.
func TestTimewarriorMoveOntoALikeActivityRemovesNoLine(t *testing.T) {
	inZone(t, "UTC")
	clock := &testClock{now: time.Date(2026, 12, 3, 9, 0, 0, 0, time.UTC)}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	nov := time.Date(2026, 11, 30, 10, 0, 0, 0, time.UTC)
	dec := time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	moved, other := newTestUID(t), newTestUID(t)
	for i, w := range []struct {
		uid   string
		start time.Time
	}{{moved, nov}, {other, dec}} {
		phoneWrite(t, mac.folder, clock, i, w.uid, map[string]any{
			"start": w.start.Format(time.RFC3339), "end": w.start.Add(time.Hour).Format(time.RFC3339),
			"project": "A", "description": "a",
		})
	}
	clock.advance(time.Minute)
	mac.sync(t)
	require.Len(t, mac.list(t), 2)

	clock.advance(time.Minute)
	phoneWrite(t, mac.folder, clock, 0, moved, map[string]any{
		"start": dec.Format(time.RFC3339), "end": dec.Add(time.Hour).Format(time.RFC3339),
	})
	clock.advance(time.Minute)
	mac.sync(t)
	clock.advance(time.Minute)
	mac.full(t)
	assert.Zero(t, deletions(mac))
	assert.Len(t, visibleFrom(mac.folder, phone, mac.deviceID()), 2)
	assert.Len(t, mac.list(t), 2, "the start it moved to is taken: the moved activity waits where it was")
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// The same for an activity filed in Tokyo, under December, and moved by the phone half an hour later
// while the Mac is in London: both starts are in November there and in UTC, yet the batch moves it from
// December's file to November's, and a crash between the renames leaves it in both.
func TestTimewarriorMoveFiledInAnotherZoneLeftInBothMonthsMakesNoCopy(t *testing.T) {
	inZone(t, "Asia/Tokyo")
	clock := &testClock{now: time.Date(2026, 12, 3, 9, 0, 0, 0, time.UTC)}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	uid := newTestUID(t)
	was := time.Date(2026, 11, 30, 23, 0, 0, 0, time.UTC)
	phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{
		"start": was.Format(time.RFC3339), "end": was.Add(20 * time.Minute).Format(time.RFC3339), "project": "A", "description": "moved",
	})
	clock.advance(time.Minute)
	mac.sync(t)
	decFile := filepath.Join(mac.backend.identity.Path, "2026-12.data")
	decData, err := os.ReadFile(decFile)
	require.NoError(t, err)
	lines := len(mac.folder.lines(mac.deviceID()))

	inZone(t, "Europe/London")
	mac.build()
	moved := was.Add(30 * time.Minute)
	clock.advance(time.Minute)
	phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{
		"start": moved.Format(time.RFC3339), "end": moved.Add(20 * time.Minute).Format(time.RFC3339),
	})
	clock.advance(time.Minute)
	mac.state.failAt = len(mac.state.saves) + 1
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.Error(t, err)
	mac.state.failAt = 0
	// November got the interval, and the crash came before December lost it.
	require.NoError(t, os.WriteFile(decFile, decData, 0o600))
	require.Len(t, mac.list(t), 2)

	mac.build()
	clock.advance(time.Minute)
	assert.Zero(t, mac.sync(t).Written)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	got := mac.list(t)
	require.Len(t, got, 1)
	assert.True(t, got[0].StartTime.Equal(moved))
	visible := visibleFrom(mac.folder, phone, mac.deviceID())
	require.Len(t, visible, 1)
	assert.True(t, visible[0].StartTime.Equal(moved))
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}
