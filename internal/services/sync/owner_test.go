package sync_test

import (
	"context"
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
	for _, change := range d.folder.lines(d.deviceID()) {
		if change.Type == journal.TypeActivity && change.UID == uid {
			if end, ok := change.Set["end"]; ok && string(end) != "null" {
				out = append(out, string(end))
			}
		}
	}
	return out
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

// Two devices start an activity before they see each other's: the owner of the earlier one writes
// its end; the other device only shows it.
func TestOwnerOnlyStopsConcurrentStarts(t *testing.T) {
	mac, studio, clock := twoMacs(t)

	startOn(t, mac, "mac", 0)
	mac.sync(t)
	clock.advance(10 * time.Minute)
	startOn(t, studio, "studio", 10)
	studio.sync(t)

	x := uidOf(t, studio, "mac")
	assert.Empty(t, endsWritten(studio, x), "the studio does not own the Mac's activity")
	require.NotNil(t, endOf(t, studio, "mac"), "the studio shows the computed end")
	assert.True(t, endOf(t, studio, "mac").Equal(at(10)))

	clock.advance(time.Minute)
	mac.sync(t)
	assert.Len(t, endsWritten(mac, x), 1, "the Mac owns its activity and writes its end")
	assert.True(t, endOf(t, mac, "mac").Equal(at(10)))

	clock.advance(time.Minute)
	studio.sync(t)
	assert.Empty(t, endsWritten(studio, x))
	assert.True(t, endOf(t, studio, "mac").Equal(at(10)))
	assert.Nil(t, endOf(t, studio, "studio"))
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

// The owner stopped its activity at 10:30, and another device that had not read that yet started
// one at 11:00: the end it computes is shown, never written, so the real end wins.
func TestOwnerOnlyStopsRealEndWins(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	ctx := context.Background()

	startOn(t, mac, "mac", 0)
	mac.sync(t)
	studio.sync(t)

	clock.advance(30 * time.Minute)
	_, err := mac.tracker.Stop(ctx, models.StopActivityRequest{EndTime: at(30)})
	require.NoError(t, err)
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

	delete(mac.folder.broken, mac.deviceID()+"/"+month)
	clock.advance(time.Minute)
	studio.sync(t)
	mac.sync(t)
	studio.sync(t)

	assert.True(t, endOf(t, mac, "mac").Equal(at(30)))
	assert.True(t, endOf(t, studio, "mac").Equal(at(30)))
	assert.Empty(t, endsWritten(studio, x))
}

// Starting an activity stops another device's running one in the backend, but only its owner
// journals that end.
func TestOwnerOnlyStopsAutoStopNotWritten(t *testing.T) {
	mac, studio, clock := twoMacs(t)

	startOn(t, mac, "mac", 0)
	mac.sync(t)
	studio.sync(t)
	clock.advance(30 * time.Minute)
	startOn(t, studio, "studio", 30)
	studio.sync(t)

	x := uidOf(t, studio, "mac")
	assert.Empty(t, endsWritten(studio, x))
	assert.True(t, endOf(t, studio, "mac").Equal(at(30)))

	mac.sync(t)
	assert.Len(t, endsWritten(mac, x), 1)
	assert.True(t, endOf(t, mac, "mac").Equal(at(30)))
	studio.sync(t)
	assertQuiet(t, studio)
	assertQuiet(t, mac)
}

// An end found by comparing the backend that equals the computed end is not journaled by a device
// that does not own the activity; another end, which the user typed, is.
func TestOwnerOnlyStopsFoundEnds(t *testing.T) {
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

	// By hand again: the user moves the end to 10:20, which is not the computed end.
	running.EndTime = new(at(20))
	save(t, studio.activities, running)
	studio.sync(t)
	assert.Len(t, endsWritten(studio, x), 1)

	mac.sync(t)
	assert.True(t, endOf(t, mac, "mac").Equal(at(20)))
}

// A stop the user makes on purpose is written by whichever device makes it.
func TestDeliberateStopOfAnotherDevicesActivity(t *testing.T) {
	mac, studio, clock := twoMacs(t)

	startOn(t, mac, "mac", 0)
	mac.sync(t)
	studio.sync(t)
	clock.advance(20 * time.Minute)
	_, err := studio.tracker.Stop(context.Background(), models.StopActivityRequest{EndTime: at(20)})
	require.NoError(t, err)
	studio.sync(t)
	assert.Len(t, endsWritten(studio, uidOf(t, studio, "mac")), 1)

	mac.sync(t)
	assert.True(t, endOf(t, mac, "mac").Equal(at(20)))
}
