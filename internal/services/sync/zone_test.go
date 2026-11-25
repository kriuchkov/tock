package sync_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// tock.txt lines journaled in Moscow are matched in London by the wall time they were written at, when
// state.db is rebuilt there (a new layout, or a damaged file and tock sync init): nothing is journaled
// again, and the lines keep their wall times.
func TestRebuildMatchesLinesWrittenInAnotherZone(t *testing.T) {
	msk := inZone(t, "Europe/Moscow")
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, msk)
	clock := &testClock{now: start}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	for i := range 3 {
		s := start.Add(time.Duration(i) * time.Hour)
		_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
			Project: "Mac", Description: "work", StartTime: s, EndTime: s.Add(30 * time.Minute),
		})
		require.NoError(t, err)
	}
	clock.now = start.Add(4 * time.Hour)
	mac.sync(t)
	before := fileText(t, mac)
	lines := len(mac.folder.lines(mac.deviceID()))

	inZone(t, "Europe/London")
	clock.now = start.Add(10 * time.Hour)
	meta := mac.state.meta
	mac.state = newMemState()
	mac.state.meta = ports.SyncMeta{Dir: meta.Dir, Device: meta.Device, Clock: meta.Clock}
	mac.build()
	report := mac.sync(t)
	assert.Zero(t, report.Written)
	assert.Zero(t, report.Applied)
	assert.Equal(t, before, fileText(t, mac))
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	assert.Len(t, visibleFrom(mac.folder, mac.deviceID()), 3)
	assertQuiet(t, mac)
	assert.Zero(t, mac.full(t).Written)
}

// A line a run wrote in Moscow before it could save its image is adopted in London by the wall time it
// was written at: no copy of the phone's activity is journaled.
func TestAdoptMatchesALineWrittenInAnotherZone(t *testing.T) {
	msk := inZone(t, "Europe/Moscow")
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, msk)
	clock := &testClock{now: start.Add(time.Hour)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	phoneWrite(t, mac.folder, clock, 0, alphaUID, map[string]any{
		"start": start.Format(time.RFC3339), "end": start.Add(30 * time.Minute).Format(time.RFC3339),
		"project": "Phone", "description": "call", "notes": "",
	})
	clock.advance(time.Minute)
	mac.state.failAt = len(mac.state.saves) + 1
	_, err := mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.Error(t, err)
	require.Contains(t, fileText(t, mac), "2026-11-18 09:00 - 2026-11-18 09:30 | Phone | call")
	mac.state.failAt = 0

	inZone(t, "Europe/London")
	mac.build()
	clock.advance(time.Hour)
	report := mac.sync(t)
	assert.Zero(t, report.Written, "the line is the phone's activity")
	assert.Empty(t, mac.folder.lines(mac.deviceID()))
	assert.Equal(t, "2026-11-18 09:00 - 2026-11-18 09:30 | Phone | call\n", fileText(t, mac))
	assertQuiet(t, mac)
	assert.Zero(t, mac.full(t).Written)
}

// Tokyo is east of UTC: tock files an activity at 08:00 on December 1 in 2026-12.data, while its UTC
// start, the key sync knows it by, lies on November 30. A recorded write at that key, weeks later, reads
// the activity where it is: a note added through tock is journaled as a note, not as a deletion.
func TestTimewarriorNoteAtAMonthBoundaryIsNoDeletion(t *testing.T) {
	tokyo := inZone(t, "Asia/Tokyo")
	start := time.Date(2026, 12, 1, 8, 0, 0, 0, tokyo)
	clock := &testClock{now: start.Add(2 * time.Hour)}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "early", StartTime: start, EndTime: start.Add(30 * time.Minute),
	})
	require.NoError(t, err)
	mac.sync(t)
	uid := uidOf(t, mac, "Mac")

	clock.advance(20 * 24 * time.Hour) // past the days whose notes a plain sync reads
	all, err := mac.activities.Find(context.Background(), models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	_, err = mac.tracker.AddNote(context.Background(), all[0], "a note added later")
	require.NoError(t, err)
	clock.advance(time.Minute)
	mac.sync(t)

	assert.Zero(t, deletions(mac))
	assert.Len(t, changesOf(mac, uid), len(mac.folder.lines(mac.deviceID())), "every line is of the one record")
	visible := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, visible, 1)
	assert.Equal(t, "a note added later", visible[0].Notes)
	clock.advance(time.Hour)
	assert.Zero(t, mac.full(t).Written)
	assertQuiet(t, mac)
}

// The same activity renamed on the phone weeks later: the plain sync reads it where it is and renames
// it, instead of adding a second line beside it.
func TestTimewarriorRenameAtAMonthBoundaryMakesNoCopy(t *testing.T) {
	tokyo := inZone(t, "Asia/Tokyo")
	start := time.Date(2026, 12, 1, 8, 0, 0, 0, tokyo)
	clock := &testClock{now: start.Add(2 * time.Hour)}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "early", StartTime: start, EndTime: start.Add(30 * time.Minute),
	})
	require.NoError(t, err)
	mac.sync(t)
	uid := uidOf(t, mac, "Mac")
	lines := len(mac.folder.lines(mac.deviceID()))

	clock.advance(20 * 24 * time.Hour)
	phoneWrite(t, mac.folder, clock, 0, uid, map[string]any{"description": "renamed on the phone"})
	clock.advance(time.Minute)
	mac.sync(t)
	assert.Equal(t, []string{"renamed on the phone"}, descriptions(mac.list(t)))

	clock.advance(time.Hour)
	assert.Zero(t, mac.full(t).Written)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	assert.Equal(t, []string{"renamed on the phone"}, descriptions(visibleFrom(mac.folder, phone, mac.deviceID())))
	assertQuiet(t, mac)
}

// An activity at 01:30 on December 1 in Moscow is filed in 2026-12.data; in London it reads as November
// 30, 22:30. Every activity with notes is written again there (its notes go under the London start),
// and the write finds this one in the file it is in: syncs go on, and the notes stay.
func TestTimewarriorActivityFiledInAnotherZonesMonth(t *testing.T) {
	msk := inZone(t, "Europe/Moscow")
	start := time.Date(2026, 12, 1, 1, 30, 0, 0, msk)
	clock := &testClock{now: start.Add(2 * time.Hour)}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "late", StartTime: start, EndTime: start.Add(30 * time.Minute),
		Notes: "keep me", Tags: []string{"client"},
	})
	require.NoError(t, err)
	other := time.Date(2026, 12, 2, 10, 0, 0, 0, msk)
	_, err = mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "other", StartTime: other, EndTime: other.Add(time.Hour),
	})
	require.NoError(t, err)
	clock.advance(time.Hour)
	mac.sync(t)
	lines := len(mac.folder.lines(mac.deviceID()))

	inZone(t, "Europe/London")
	mac.build()
	clock.advance(time.Hour)
	assert.Zero(t, mac.sync(t).Written)
	clock.advance(time.Hour)
	assert.Zero(t, mac.full(t).Written)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	assert.Equal(t, map[time.Time]string{start.UTC(): "keep me", other.UTC(): ""}, localNotes(t, mac))
	for _, a := range visibleFrom(mac.folder, mac.deviceID()) {
		if a.Description == "late" {
			assert.Equal(t, "keep me", a.Notes)
			assert.Equal(t, []string{"client"}, a.Tags)
		}
	}
	assertQuiet(t, mac)
}

// After a zone change, the first run reads the notes of an activity without notes under the start as
// it reads in the new zone, and its image takes that offset: a note added there later, by hand or by a
// tock note that wrote without recording, is an edit. It is journaled and stays.
func TestTimewarriorNoteAddedAfterAZoneChangeIsKept(t *testing.T) {
	msk := inZone(t, "Europe/Moscow")
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, msk)
	clock := &testClock{now: start.Add(2 * time.Hour)}
	mac := newTimewarriorDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "w", StartTime: start, EndTime: start.Add(30 * time.Minute),
	})
	require.NoError(t, err)
	mac.sync(t)

	inZone(t, "Europe/London")
	mac.build()
	clock.advance(time.Hour)
	assert.Zero(t, mac.sync(t).Written)

	a := mac.list(t)[0]
	require.NoError(t, mac.notes.Save(context.Background(), a.ID(), a.StartTime, "added in London", nil))
	clock.advance(time.Minute)
	assert.Equal(t, 1, mac.sync(t).Written)
	assert.Equal(t, map[time.Time]string{start.UTC(): "added in London"}, localNotes(t, mac))
	visible := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, visible, 1)
	assert.Equal(t, "added in London", visible[0].Notes)
	clock.advance(time.Hour)
	assert.Zero(t, mac.full(t).Written)
	assertQuiet(t, mac)
}

// Four tock.txt lines alike but for their hour, written in Moscow, read in London: every merged start
// has another wall time there, three hours earlier, where the line of another record sits with the same
// project, description and length. None is taken for a line a move left behind: a full sync removes
// none, and deletes no record.
func TestZoneChangeTakesNoLineForOneLeftBehind(t *testing.T) {
	msk := inZone(t, "Europe/Moscow")
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, msk)
	clock := &testClock{now: start}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	for i := range 4 {
		s := start.Add(time.Duration(i) * time.Hour)
		_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
			Project: "Mac", Description: "w", StartTime: s, EndTime: s.Add(30 * time.Minute),
		})
		require.NoError(t, err)
	}
	clock.now = start.Add(5 * time.Hour)
	mac.sync(t)
	before := fileText(t, mac)
	lines := len(mac.folder.lines(mac.deviceID()))

	inZone(t, "Europe/London")
	mac.build()
	clock.advance(time.Hour)
	report := mac.full(t)
	assert.Zero(t, report.Written)
	assert.Zero(t, report.Applied)
	assert.Equal(t, before, fileText(t, mac))
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	assert.Len(t, visibleFrom(mac.folder, mac.deviceID()), 4)
	assertQuiet(t, mac)
}
