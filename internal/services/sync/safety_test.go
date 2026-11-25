package sync_test

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// phone is a device that writes its journal by hand, like the iOS app.
const phone = "aaaaaaaaaaaaaaaa"

// phoneWrite appends a change of the phone at the clock's time.
func phoneWrite(t *testing.T, folder *memFolder, clock *testClock, counter int, uid string, set map[string]any) {
	t.Helper()
	values := make(map[string]json.RawMessage, len(set))
	for path, value := range set {
		raw, err := json.Marshal(value)
		require.NoError(t, err)
		values[path] = raw
	}
	hlc := journal.HLC{Wall: clock.now.UnixMilli(), Counter: counter}
	line, err := json.Marshal(
		map[string]any{"v": 1, "hlc": hlc.String(), "dev": phone, "type": journal.TypeActivity, "uid": uid, "set": values},
	)
	require.NoError(t, err)
	month := clock.now.UTC().Format("2006-01")
	require.NoError(t, folder.Append(context.Background(), phone, month, append(line, '\n')))
}

func TestPrecisionRoundTripsMakeNoChurn(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	studio := newTimewarriorDevice(t, "studio", folder, clock)
	mac.init(t)
	studio.init(t)

	start := at(0).Add(37 * time.Second)
	end := at(45).Add(12 * time.Second)
	_, err := studio.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Backend", Description: "API", StartTime: start, EndTime: end, Notes: "seconds", Tags: []string{"go"},
	})
	require.NoError(t, err)
	clock.advance(time.Hour)
	studio.sync(t)
	mac.sync(t)

	got := mac.list(t)
	require.Len(t, got, 1)
	assert.True(t, got[0].StartTime.Equal(at(0)), "the file backend keeps minutes")
	assert.True(t, got[0].EndTime.Equal(at(45)))
	assert.Equal(t, "seconds", got[0].Notes)

	for range 3 {
		clock.advance(time.Minute)
		assertQuiet(t, mac)
		assertQuiet(t, studio)
		assert.Zero(t, mac.full(t).Written)
		assert.Zero(t, studio.full(t).Written)
	}

	// An edit on the Mac changes the description only: the studio keeps its seconds.
	_, err = mac.tracker.Update(context.Background(), got[0], models.UpdateActivityRequest{Description: new("API v2")})
	require.NoError(t, err)
	mac.sync(t)
	studio.sync(t)
	back := studio.list(t)
	require.Len(t, back, 1)
	assert.Equal(t, "API v2", back[0].Description)
	assert.True(t, back[0].StartTime.Equal(start))
	assert.True(t, back[0].EndTime.Equal(end))
	for _, change := range folder.lines(mac.deviceID()) {
		if change.Type == journal.TypeActivity {
			assert.NotContains(t, change.Set, "start", "the Mac never rewrites the start it holds at minutes")
			assert.NotContains(t, change.Set, "end")
		}
	}
	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

// A record that is incomplete, or holds a value of the wrong type, hides it in the merged state but
// never removes it from the backend; only a tombstone does, and deleted: false brings it back.
func TestOnlyATombstoneRemovesFromTheBackend(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	save(t, mac.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	mac.init(t)
	uid := uidOf(t, mac, "Backend")

	clock.advance(time.Minute)
	phoneWrite(t, folder, clock, 0, "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a10", map[string]any{"description": "half"})
	phoneWrite(t, folder, clock, 1, uid, map[string]any{"project": 5})
	mac.sync(t)
	require.Len(t, mac.list(t), 1, "a hidden record stays in the backend")
	assert.Equal(t, "Backend", mac.list(t)[0].Project)
	assertQuiet(t, mac)

	clock.advance(time.Minute)
	phoneWrite(t, folder, clock, 0, uid, map[string]any{"project": "Backend", "deleted": true})
	mac.sync(t)
	assert.Empty(t, mac.list(t), "a tombstone removes it")
	assertQuiet(t, mac)

	clock.advance(time.Minute)
	phoneWrite(t, folder, clock, 0, uid, map[string]any{"deleted": false})
	mac.sync(t)
	require.Len(t, mac.list(t), 1, "an undo brings it back")
	assert.Equal(t, "API", mac.list(t)[0].Description)
	assertQuiet(t, mac)
}

// A journal that goes away makes the device read everything again; the records only it held leave
// the merged state but stay in the backend.
func TestRebuildKeepsTheBackend(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	_, err := studio.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Docs", Description: "README", StartTime: at(0), EndTime: at(30),
	})
	require.NoError(t, err)
	studio.sync(t)
	mac.sync(t)
	require.Len(t, mac.list(t), 1)

	clock.advance(time.Minute)
	for key := range mac.folder.plain {
		if key[0] == studio.deviceID() {
			delete(mac.folder.plain, key)
		}
	}
	uid := uidOf(t, mac, "Docs")
	report := mac.sync(t)
	assert.True(t, report.Rebuilt)
	assert.Len(t, mac.list(t), 1)
	assert.True(t, mac.state.lastSave().Reset)
	// The record left the merged state with the journal, so this device journals it again, whole and
	// under its uid, for the other devices.
	assert.Equal(t, 1, report.Written)
	sent := changesOf(mac, uid)
	require.Len(t, sent, 1)
	assert.JSONEq(t, `"README"`, string(sent[0].Set["description"]))
	assert.Contains(t, sent[0].Set, "start")
	assert.Contains(t, sent[0].Set, "end")
	assertQuiet(t, mac)
}

// A segment that cannot be read waits for a later run; nothing is removed meanwhile, and this device
// writes no stops.
func TestUnreadableSegmentWaits(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	_, err := studio.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Docs", Description: "README", StartTime: at(0), EndTime: at(30),
	})
	require.NoError(t, err)
	studio.sync(t)
	month := clock.now.UTC().Format("2006-01")
	mac.folder.broken[studio.deviceID()+"/"+month] = true

	report := mac.sync(t)
	assert.Equal(t, []string{"journal/" + studio.deviceID() + "/" + month + ".jsonl"}, report.Unreadable)
	assert.Empty(t, mac.list(t))

	delete(mac.folder.broken, studio.deviceID()+"/"+month)
	report = mac.sync(t)
	assert.Empty(t, report.Unreadable)
	assert.Len(t, mac.list(t), 1)
}

func TestClonedDeviceTakesANewID(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	save(t, mac.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	mac.init(t)
	old := mac.deviceID()

	// A restored backup writes under the same ID, later than this device's clock.
	clone := newFileDevice(t, "clone", folder, clock)
	clone.state.meta = mac.state.meta
	clone.state.cursors = maps.Clone(mac.state.cursors)
	clone.state.records = maps.Clone(mac.state.records)
	clone.state.images = maps.Clone(mac.state.images)
	clone.state.meta.Backend = clone.backend.identity
	save(t, clone.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	clock.advance(time.Hour)
	save(t, clone.activities, models.Activity{Project: "Clone", Description: "copy", StartTime: at(50), EndTime: new(at(55))})
	clone.full(t)

	clock.advance(time.Minute)
	report := mac.sync(t)
	assert.Equal(t, old, report.ReplacedDevice)
	assert.NotEqual(t, old, report.Device)
	assert.Equal(t, report.Device, mac.deviceID())
	assert.Contains(t, folder.devices, report.Device)

	save(t, mac.activities, models.Activity{Project: "Mine", Description: "new", StartTime: at(70), EndTime: new(at(80))})
	mac.sync(t)
	assert.NotEmpty(t, folder.lines(report.Device))
	assert.Len(t, mac.list(t), 3)
}

// A run that wrote into the backend but could not save its state finds nothing new the next time:
// no new uids, no duplicates, no changes journaled.
func TestInterruptedRunFindsNothingNew(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	_, err := studio.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Docs", Description: "README", StartTime: at(0), EndTime: at(30), Notes: "draft",
	})
	require.NoError(t, err)
	startOn(t, studio, "Backend", 40)
	studio.sync(t)

	clock.advance(time.Minute)
	mac.state.failAt = len(mac.state.saves) + 1
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.Error(t, err)
	require.Len(t, mac.list(t), 2, "the backend was written")

	mac.state.failAt = 0
	report := mac.sync(t)
	assert.Zero(t, report.Written)
	assert.Len(t, mac.list(t), 2)
	assert.Len(t, mac.state.records, 2)
	assertQuiet(t, mac)
}

// A month closed by a change in a later month is compressed once read to its end; other devices go
// on reading it.
func TestClosedMonthsAreCompressed(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	studio := newTimewarriorDevice(t, "studio", folder, clock)
	mac.init(t)
	studio.init(t)

	october := clock.now.Add(-time.Hour)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "October", Description: "work", StartTime: october, EndTime: october.Add(30 * time.Minute),
	})
	require.NoError(t, err)
	mac.sync(t)
	studio.sync(t)

	clock.now = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	_, err = mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "November", Description: "work", StartTime: clock.now.Add(-time.Hour), EndTime: clock.now,
	})
	require.NoError(t, err)
	mac.sync(t)
	assert.Contains(t, folder.gz, [2]string{mac.deviceID(), "2026-10"})
	assert.NotContains(t, folder.plain, [2]string{mac.deviceID(), "2026-10"})

	studio.sync(t)
	assert.Len(t, studio.list(t), 2)
	assertQuiet(t, mac)
	assertQuiet(t, studio)

	late := newFileDevice(t, "late", folder, clock)
	late.init(t)
	assert.Len(t, late.list(t), 2)
}
