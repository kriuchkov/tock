package sync_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

// uidOf returns the uid of the merged activity with the given project.
func uidOf(t *testing.T, d *device, project string) string {
	t.Helper()
	for _, record := range d.state.records {
		if a, ok := record.Activity(); ok && a.Project == project {
			return record.UID
		}
	}
	t.Fatalf("%s holds no activity of %s", d.name, project)
	return ""
}

// endsWritten returns the ends a device journaled for an activity, leaving out the null of a
// running one.
func endsWritten(d *device, uid string) []string {
	var out []string
	for _, value := range valuesWritten(d, uid, journal.FieldEnd) {
		if string(value) != "null" {
			out = append(out, string(value))
		}
	}
	return out
}

// valuesWritten returns the values a device journaled at one path of an activity, in its order.
func valuesWritten(d *device, uid, path string) []json.RawMessage {
	var out []json.RawMessage
	for _, change := range d.folder.lines(d.deviceID()) {
		if change.Type == journal.TypeActivity && change.UID == uid {
			if value, ok := change.Set[path]; ok {
				out = append(out, value)
			}
		}
	}
	return out
}

// assertInstants checks that values hold the given instants, in order.
func assertInstants(t *testing.T, want []time.Time, values []json.RawMessage, msgAndArgs ...any) {
	t.Helper()
	got := make([]time.Time, 0, len(values))
	for _, value := range values {
		var text string
		require.NoError(t, json.Unmarshal(value, &text), msgAndArgs...)
		instant, err := time.Parse(time.RFC3339, text)
		require.NoError(t, err, msgAndArgs...)
		got = append(got, instant.UTC())
	}
	utc := make([]time.Time, 0, len(want))
	for _, instant := range want {
		utc = append(utc, instant.UTC())
	}
	assert.Equal(t, utc, got, msgAndArgs...)
}

func endOf(t *testing.T, d *device, project string) *time.Time {
	t.Helper()
	for _, a := range d.list(t) {
		if a.Project == project {
			return a.EndTime
		}
	}
	t.Fatalf("%s has no activity of %s in its backend", d.name, project)
	return nil
}

// assertEnd checks the end the backend of a device holds for the activity of a project.
func assertEnd(t *testing.T, d *device, project string, want time.Time, msgAndArgs ...any) {
	t.Helper()
	end := endOf(t, d, project)
	require.NotNil(t, end, msgAndArgs...)
	assert.True(t, end.Equal(want), "%s: %s ends at %s, not %s", d.name, project, end, want)
}

func twoMacs(t *testing.T) (*device, *device, *testClock) {
	t.Helper()
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	studio := newTimewarriorDevice(t, "studio", folder, clock)
	mac.init(t)
	studio.init(t)
	return mac, studio, clock
}

func startOn(t *testing.T, d *device, project string, minute int) {
	t.Helper()
	_, err := d.tracker.Start(
		context.Background(),
		models.StartActivityRequest{Project: project, Description: "work", StartTime: at(minute)},
	)
	require.NoError(t, err)
}

func stopOn(t *testing.T, d *device, minute int) {
	t.Helper()
	_, err := d.tracker.Stop(context.Background(), models.StopActivityRequest{EndTime: at(minute)})
	require.NoError(t, err)
}

// Two devices start an activity before they see each other's: the other device writes the end rule 1
// gives the earlier one as its auto_end, and its owner, which sees it there, writes nothing.
func TestConcurrentStartsWriteTheAutoEnd(t *testing.T) {
	mac, studio, clock := twoMacs(t)

	startOn(t, mac, "mac", 0)
	mac.sync(t)
	clock.advance(10 * time.Minute)
	startOn(t, studio, "studio", 10)
	studio.sync(t)

	x := uidOf(t, studio, "mac")
	assert.Empty(t, endsWritten(studio, x), "the studio does not own the Mac's activity")
	assertInstants(t, []time.Time{at(10)}, valuesWritten(studio, x, journal.FieldAutoEnd))
	assertEnd(t, studio, "mac", at(10))

	clock.advance(time.Minute)
	mac.sync(t)
	assert.Len(t, changesOf(mac, x), 1, "the owner leaves the auto_end as it is")
	assertEnd(t, mac, "mac", at(10))

	clock.advance(time.Minute)
	studio.sync(t)
	assertEnd(t, studio, "mac", at(10))
	assert.Nil(t, endOf(t, studio, "studio"))
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

// The owner stopped its activity at 10:30, and another device that had not read that yet started
// one at 11:00: it writes that end as the auto_end, even before it read every segment, and the real
// end shows over it on every device.
func TestOwnersEndShowsOverTheAutoEnd(t *testing.T) {
	mac, studio, clock := twoMacs(t)

	startOn(t, mac, "mac", 0)
	mac.sync(t)
	studio.sync(t)

	clock.advance(30 * time.Minute)
	stopOn(t, mac, 30)
	mac.sync(t)

	// The studio cannot read the Mac's journal yet and starts an activity at 11:00, which stops the
	// Mac's in its own backend.
	month := clock.now.UTC().Format("2006-01")
	mac.folder.broken[mac.deviceID()+"/"+month] = true
	clock.advance(30 * time.Minute)
	startOn(t, studio, "studio", 60)
	report := studio.sync(t)
	assert.NotEmpty(t, report.Unreadable)
	x := uidOf(t, studio, "mac")
	assert.Empty(t, endsWritten(studio, x))
	assertInstants(t, []time.Time{at(60)}, valuesWritten(studio, x, journal.FieldAutoEnd), "written with the start")

	delete(mac.folder.broken, mac.deviceID()+"/"+month)
	clock.advance(time.Minute)
	studio.sync(t)
	mac.sync(t)
	studio.sync(t)

	assertEnd(t, mac, "mac", at(30))
	assertEnd(t, studio, "mac", at(30))
	assert.Empty(t, endsWritten(studio, x))
	assert.Len(t, valuesWritten(studio, x, journal.FieldAutoEnd), 1)
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

// Starting an activity while another device's runs journals the end that stops the other one as its
// auto_end, with the start. Stopping the new one leaves it stopped, before and after its owner syncs,
// which writes nothing and gets the end in its backend (docs/sync.md, rule 1).
func TestStartThenStopLeavesAnotherDevicesActivityStopped(t *testing.T) {
	for _, tc := range []struct {
		name      string
		newDevice func(*testing.T, string, *memFolder, *testClock) *device
	}{
		{"file", newFileDevice},
		{"todotxt", newTodoTXTDevice},
		{"timewarrior", newTimewarriorDevice},
		{"sqlite", newSQLiteDevice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &testClock{now: at(0)}
			folder := newMemFolder()
			mac := newFileDevice(t, "mac", folder, clock)
			studio := tc.newDevice(t, "studio", folder, clock)
			mac.init(t)
			studio.init(t)

			startOn(t, mac, "mac", 0)
			mac.sync(t)
			studio.sync(t)
			clock.advance(30 * time.Minute)
			startOn(t, studio, "studio", 30)
			studio.sync(t)
			x := uidOf(t, studio, "mac")
			assert.Empty(t, endsWritten(studio, x))
			assertInstants(t, []time.Time{at(30)}, valuesWritten(studio, x, journal.FieldAutoEnd))

			clock.advance(15 * time.Minute)
			stopOn(t, studio, 45)
			studio.sync(t)
			assertEnd(t, studio, "mac", at(30), "the Mac's activity stays stopped")
			assertEnd(t, studio, "studio", at(45))

			clock.advance(time.Minute)
			mac.sync(t)
			assert.Len(t, changesOf(mac, x), 1, "the owner writes nothing")
			assertEnd(t, mac, "mac", at(30))
			assertEnd(t, mac, "studio", at(45))
			assertQuiet(t, mac)
			assertQuiet(t, studio)
			assert.Len(t, valuesWritten(studio, x, journal.FieldAutoEnd), 1)
		})
	}
}

// A device that starts an activity while its backend still shows another device's running, which a
// third device has stopped with an auto_end since, leaves that auto_end as it is: the start no longer
// stops it. It writes the auto_end of the third device's activity instead, which its start stops.
func TestStartKeepsTheAutoEndAThirdDeviceWrote(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	phoneStart(t, mac, clock, alphaUID, "Alpha", at(0))
	clock.advance(time.Minute)
	mac.sync(t)
	studio.sync(t)

	clock.advance(20 * time.Minute)
	startOn(t, mac, "mac", 20)
	mac.sync(t)
	assertInstants(t, []time.Time{at(20)}, valuesWritten(mac, alphaUID, journal.FieldAutoEnd))

	clock.advance(20 * time.Minute)
	startOn(t, studio, "studio", 40)
	studio.sync(t)
	assert.Empty(t, valuesWritten(studio, alphaUID, journal.FieldAutoEnd))
	assert.Empty(t, endsWritten(studio, alphaUID))
	assertEnd(t, studio, "Work", at(20), "the phone's activity keeps the auto_end the Mac wrote")
	x := uidOf(t, studio, "mac")
	assertInstants(t, []time.Time{at(40)}, valuesWritten(studio, x, journal.FieldAutoEnd))
	assertEnd(t, studio, "mac", at(40))

	clock.advance(time.Minute)
	mac.sync(t)
	assertEnd(t, mac, "Work", at(20))
	assertEnd(t, mac, "mac", at(40))
	assert.Nil(t, endOf(t, mac, "studio"))
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

// A start before the running activity's own, while another device owns that one, stops it with an
// end, now: the new activity runs (docs/sync.md, rule 1).
func TestStartBeforeAnotherDevicesActivityWritesItsEnd(t *testing.T) {
	mac, studio, clock := twoMacs(t)

	startOn(t, mac, "mac", 30)
	clock.advance(31 * time.Minute)
	mac.sync(t)
	studio.sync(t)
	startOn(t, studio, "studio", 10)
	studio.sync(t)

	x := uidOf(t, studio, "mac")
	end := endOf(t, studio, "mac")
	require.NotNil(t, end)
	assert.False(t, end.Before(at(30)), "never before the activity's start")
	assertInstants(t, []time.Time{*end}, valuesWritten(studio, x, journal.FieldEnd))
	assert.Empty(t, valuesWritten(studio, x, journal.FieldAutoEnd))
	assert.Nil(t, endOf(t, studio, "studio"), "the new activity runs")

	clock.advance(time.Minute)
	mac.sync(t)
	assertEnd(t, mac, "mac", end.Truncate(time.Minute), "tock.txt keeps minutes")
	assert.Nil(t, endOf(t, mac, "studio"))
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

// An end found by comparing the backend that is the one rule 1 gives an activity another device
// owns goes to the journal as its auto_end; another one, which the user typed, as its end.
func TestFoundEndsOfAnotherDevicesActivity(t *testing.T) {
	mac, studio, clock := twoMacs(t)

	startOn(t, mac, "mac", 0)
	mac.sync(t)
	studio.sync(t)
	running := studio.list(t)[0]

	// By hand: the Mac's activity stops where a new one starts.
	clock.advance(time.Hour)
	running.EndTime = new(at(40))
	save(t, studio.activities, running)
	save(t, studio.activities, models.Activity{Project: "studio", Description: "by hand", StartTime: at(40)})
	studio.sync(t)
	x := uidOf(t, studio, "mac")
	assert.Empty(t, endsWritten(studio, x))
	assertInstants(t, []time.Time{at(40)}, valuesWritten(studio, x, journal.FieldAutoEnd))

	// By hand again: the user moves the end to 9:20, which is not that one.
	running.EndTime = new(at(20))
	save(t, studio.activities, running)
	studio.sync(t)
	assert.Len(t, endsWritten(studio, x), 1)

	mac.sync(t)
	assertEnd(t, mac, "mac", at(20))
	assertQuiet(t, studio)
}

// A stop the user makes on purpose is written as the end by whichever device makes it.
func TestDeliberateStopOfAnotherDevicesActivity(t *testing.T) {
	mac, studio, clock := twoMacs(t)

	startOn(t, mac, "mac", 0)
	mac.sync(t)
	studio.sync(t)
	clock.advance(20 * time.Minute)
	stopOn(t, studio, 20)
	studio.sync(t)
	x := uidOf(t, studio, "mac")
	assert.Len(t, endsWritten(studio, x), 1)
	assert.Empty(t, valuesWritten(studio, x, journal.FieldAutoEnd))

	mac.sync(t)
	assertEnd(t, mac, "mac", at(20))
}

// tock sync init --restore over an older copy of tock.txt, where an activity still runs that another
// device stopped with an auto_end since, writes that end back and journals nothing.
func TestRestoreWritesTheAutoEndBack(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	startOn(t, mac, "mac", 0)
	mac.sync(t)
	studio.sync(t)
	older := fileText(t, mac)

	clock.advance(30 * time.Minute)
	startOn(t, studio, "studio", 30)
	clock.advance(15 * time.Minute)
	stopOn(t, studio, 45)
	studio.sync(t)
	clock.advance(time.Minute)
	mac.sync(t)
	assertEnd(t, mac, "mac", at(30))
	lines := len(mac.folder.lines(mac.deviceID()))

	require.NoError(t, os.WriteFile(mac.backend.identity.Path, []byte(older), 0o600))
	mac.build()
	clock.advance(time.Minute)
	report := mac.restore(t)
	assert.Zero(t, report.Written)
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines, "nothing journaled")
	assertEnd(t, mac, "mac", at(30))
	assertEnd(t, mac, "studio", at(45))
	clock.advance(time.Minute)
	assertQuiet(t, mac)
}

// Reopening an activity stopped at its auto_end, with tock edit --clear-end or by removing the end
// from its line, journals end and auto_end cleared in one change: it runs again on every device.
func TestReopenClearsTheAutoEnd(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reopen func(t *testing.T, mac, studio *device) *device
	}{
		{"edit on the owner", func(t *testing.T, mac, _ *device) *device {
			t.Helper()
			stopped := mac.list(t)[0]
			_, err := mac.tracker.Update(context.Background(), stopped, models.UpdateActivityRequest{ClearEndTime: true})
			require.NoError(t, err)
			return mac
		}},
		{"by hand on another device", func(t *testing.T, _, studio *device) *device {
			t.Helper()
			stopped := studio.list(t)[0]
			stopped.EndTime = nil
			save(t, studio.activities, stopped)
			return studio
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mac, studio, clock := twoMacs(t)
			startOn(t, mac, "mac", 0)
			mac.sync(t)
			studio.sync(t)
			clock.advance(30 * time.Minute)
			startOn(t, studio, "studio", 30)
			studio.sync(t)
			clock.advance(15 * time.Minute)
			stopOn(t, studio, 45)
			studio.sync(t)
			mac.sync(t)
			x := uidOf(t, mac, "mac")
			assertEnd(t, mac, "mac", at(30))

			clock.advance(time.Hour)
			reopened := tc.reopen(t, mac, studio)
			reopened.sync(t)
			changes := changesOf(reopened, x)
			require.NotEmpty(t, changes)
			assert.Equal(t, journal.Reopen(), changes[len(changes)-1].Set, "one change clears both")

			clock.advance(time.Minute)
			mac.sync(t)
			studio.sync(t)
			assert.Nil(t, endOf(t, mac, "mac"))
			assert.Nil(t, endOf(t, studio, "mac"))
			assertQuiet(t, mac)
			assertQuiet(t, studio)
		})
	}
}

// Two devices that start an activity before they read each other's both write the auto_end of a third
// device's activity, each at its own start; the later clock's value wins. Rule 1 still stops that
// activity where the first of the two starts, and the first one where the second starts
// (docs/sync.md, rule 1).
func TestConcurrentStartsStopAtTheEarlierOne(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	phoneStart(t, mac, clock, alphaUID, "Alpha", at(0))
	clock.advance(time.Minute)
	mac.sync(t)
	studio.sync(t)

	clock.now = at(10)
	startOn(t, mac, "mac", 10)
	mac.sync(t)
	assertInstants(t, []time.Time{at(10)}, valuesWritten(mac, alphaUID, journal.FieldAutoEnd))

	// The studio cannot read the Mac's journal yet: iCloud has not brought it down.
	segment := mac.deviceID() + "/" + clock.now.UTC().Format("2006-01")
	mac.folder.broken[segment] = true
	clock.now = at(30)
	startOn(t, studio, "studio", 30)
	studio.sync(t)
	assertInstants(t, []time.Time{at(30)}, valuesWritten(studio, alphaUID, journal.FieldAutoEnd))
	delete(mac.folder.broken, segment)

	for range 2 {
		clock.advance(time.Minute)
		mac.sync(t)
		studio.sync(t)
	}
	for _, d := range []*device{mac, studio} {
		assertEnd(t, d, "Work", at(10), "the phone's activity stops where the Mac's starts")
		assertEnd(t, d, "mac", at(30), "the Mac's activity stops where the studio's starts")
		assert.Nil(t, endOf(t, d, "studio"))
	}
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

// A start before another device's running activity stops that one with an end, now. When the new
// activity is stopped too before a sync goes through, the end still goes to the journal: rule 1 no
// longer gives any, and without it the write-back would put the other activity back as running.
func TestStartBeforeThenStopBeforeTheSyncKeepsTheEnd(t *testing.T) {
	t.Run("another Mac's activity", func(t *testing.T) {
		mac, studio, clock := twoMacs(t)
		startOn(t, mac, "mac", 30)
		clock.advance(31 * time.Minute)
		mac.sync(t)
		studio.sync(t)

		startOn(t, studio, "studio", 10)
		end := endOf(t, studio, "mac")
		require.NotNil(t, end, "the start stops the Mac's activity in the backend")
		clock.advance(2 * time.Minute)
		stopOn(t, studio, 33)
		studio.sync(t)

		x := uidOf(t, studio, "mac")
		assertInstants(t, []time.Time{*end}, valuesWritten(studio, x, journal.FieldEnd))
		assert.Empty(t, valuesWritten(studio, x, journal.FieldAutoEnd))
		assertEnd(t, studio, "mac", *end)
		clock.advance(time.Minute)
		mac.sync(t)
		assertEnd(t, mac, "mac", end.Truncate(time.Minute))
		assertQuiet(t, mac)
		assertQuiet(t, studio)
	})
	t.Run("the phone's activity", func(t *testing.T) {
		clock := &testClock{now: at(31)}
		mac := newFileDevice(t, "mac", newMemFolder(), clock)
		mac.init(t)
		phoneStart(t, mac, clock, alphaUID, "Alpha", at(30))
		clock.advance(time.Minute)
		mac.sync(t)

		startOn(t, mac, "mac", 10)
		require.NotNil(t, endOf(t, mac, "Work"))
		clock.advance(5 * time.Minute)
		stopOn(t, mac, 40)
		mac.sync(t)

		assert.NotNil(t, endOf(t, mac, "Work"), "the sync does not restart the phone's activity")
		assert.Len(t, endsWritten(mac, alphaUID), 1)
		assertQuiet(t, mac)
	})
}

// A stop made on purpose and a start at the same instant, with no sync in between, journal the stop as
// the end, not as the auto_end the start would give: a reader that does not know auto_end sees it.
func TestDeliberateStopThenStartWritesTheEnd(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	phoneStart(t, mac, clock, alphaUID, "Alpha", at(0))
	clock.advance(time.Minute)
	mac.sync(t)

	clock.now = at(20)
	stopOn(t, mac, 20)
	startOn(t, mac, "mac", 20)
	mac.sync(t)

	assertInstants(t, []time.Time{at(20)}, valuesWritten(mac, alphaUID, journal.FieldEnd))
	assert.Empty(t, valuesWritten(mac, alphaUID, journal.FieldAutoEnd))
	assertQuiet(t, mac)
}

// The owner moves its running activity's start past the auto_end another device wrote before it read
// that auto_end. The auto_end, now before the start, counts as missing, so the activity runs from its
// new start and rule 1 stops the other device's activity there (docs/sync.md, Activity fields).
func TestStartMovedPastTheAutoEnd(t *testing.T) {
	for _, tc := range []struct {
		name      string
		newDevice func(*testing.T, string, *memFolder, *testClock) *device
	}{
		{"file", newFileDevice},
		{"timewarrior", newTimewarriorDevice},
		{"sqlite", newSQLiteDevice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &testClock{now: at(0)}
			mac := tc.newDevice(t, "mac", newMemFolder(), clock)
			mac.init(t)
			phoneStart(t, mac, clock, alphaUID, "Alpha", at(0))
			clock.advance(time.Minute)
			mac.sync(t)

			clock.now = at(10)
			startOn(t, mac, "mac", 10)
			mac.sync(t)
			assertInstants(t, []time.Time{at(10)}, valuesWritten(mac, alphaUID, journal.FieldAutoEnd))

			clock.now = at(25)
			phoneWrite(t, mac.folder, clock, 0, alphaUID, map[string]any{"start": at(20).Format(time.RFC3339)})
			clock.advance(time.Minute)
			mac.sync(t)
			assert.Nil(t, endOf(t, mac, "Work"), "the phone's activity runs from its new start")
			assertEnd(t, mac, "mac", at(20), "rule 1 stops the Mac's activity where the phone's now starts")
			ends := endsWritten(mac, uidOf(t, mac, "mac"))
			require.Len(t, ends, 1, "the Mac writes the end of its own activity")
			assertInstants(t, []time.Time{at(20)}, []json.RawMessage{json.RawMessage(ends[0])})
			assert.Len(t, valuesWritten(mac, alphaUID, journal.FieldAutoEnd), 1, "the stale auto_end is left as it is")
			assertQuiet(t, mac)
		})
	}
}

// A start journals the auto_end that stops another device's activity even when the sync has not read
// every segment and rule 1 stops that activity at another one's start, which this device had not read
// when it started: that one's start is the earlier end. Once both stop, the activity stays stopped.
func TestStartStopsAnotherDevicesActivityInAnIncompleteSync(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	mac.init(t)
	const older = "dddddddddddddddd"
	const tabletUID = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a20"
	phoneStart(t, mac, clock, alphaUID, "Alpha", at(0))
	clock.advance(time.Minute)
	mac.sync(t)
	require.Nil(t, endOf(t, mac, "Work"))

	// A segment the Mac cannot read keeps its syncs from being complete.
	deviceWrite(t, folder, clock, older, 0, "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a21", map[string]any{
		"start": at(-600).Format(time.RFC3339), "end": at(-590).Format(time.RFC3339), "project": "Old",
	})
	folder.broken[older+"/"+clock.now.UTC().Format("2006-01")] = true

	clock.advance(9 * time.Minute)
	deviceWrite(t, folder, clock, tablet, 0, tabletUID, map[string]any{
		"start": at(10).Format(time.RFC3339), "end": nil, "project": "Tablet", "description": "", "notes": "",
	})
	clock.advance(10 * time.Minute)
	startOn(t, mac, "mac", 20)
	report := mac.sync(t)
	require.NotEmpty(t, report.Unreadable)
	assertInstants(t, []time.Time{at(10)}, valuesWritten(mac, alphaUID, journal.FieldAutoEnd),
		"the tablet's start, which rule 1 stops it at, is the earlier end")
	assertEnd(t, mac, "Work", at(10))

	clock.advance(5 * time.Minute)
	deviceWrite(t, folder, clock, tablet, 0, tabletUID, map[string]any{"end": at(25).Format(time.RFC3339)})
	stopOn(t, mac, 25)
	mac.sync(t)
	assertEnd(t, mac, "Work", at(10), "the phone's activity stays stopped once the others stop")
	assertEnd(t, mac, "mac", at(25))
}

// An activity stopped by its auto_end stops earlier at the start of a running activity that starts
// inside it, which a device reads later, as a backdated start: rule 1 then cuts it there, though the
// run read nothing of it and it starts hours before (docs/sync.md, rule 1).
func TestBackdatedStartCutsAnAutoEnd(t *testing.T) {
	for _, tc := range []struct {
		name      string
		newDevice func(*testing.T, string, *memFolder, *testClock) *device
	}{
		{"file", newFileDevice},
		{"timewarrior", newTimewarriorDevice},
		{"sqlite", newSQLiteDevice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &testClock{now: at(0)}
			mac := tc.newDevice(t, "mac", newMemFolder(), clock)
			mac.init(t)
			const zUID = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a30"
			const yUID = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a31"
			phoneStart(t, mac, clock, alphaUID, "Alpha", at(0))
			clock.now = at(200)
			deviceWrite(t, mac.folder, clock, tablet, 0, alphaUID, map[string]any{"auto_end": at(200).Format(time.RFC3339)})
			deviceWrite(t, mac.folder, clock, tablet, 1, zUID, map[string]any{
				"start": at(200).Format(time.RFC3339), "end": at(210).Format(time.RFC3339), "project": "Tablet", "description": "",
			})
			clock.advance(15 * time.Minute)
			mac.sync(t)
			assertEnd(t, mac, "Work", at(200))

			deviceWrite(t, mac.folder, clock, tablet, 0, yUID, map[string]any{
				"start": at(150).Format(time.RFC3339), "end": nil, "project": "Backdated", "description": "",
			})
			clock.advance(time.Minute)
			mac.sync(t)
			assertEnd(t, mac, "Work", at(150), "the backdated start stops the phone's activity")
			assertInstants(t, []time.Time{at(150)}, valuesWritten(mac, alphaUID, journal.FieldAutoEnd))
			assert.Nil(t, endOf(t, mac, "Backdated"))
			assertQuiet(t, mac)
		})
	}
}
