package sync_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

func at(minutes int) time.Time {
	return base().Add(time.Duration(minutes) * time.Minute)
}

func save(t *testing.T, repo ports.ActivityRepository, a models.Activity) {
	t.Helper()
	require.NoError(t, repo.Save(context.Background(), a))
}

// assertQuiet asserts that a run wrote nothing anywhere: no journal line, no file in the folder, no
// record or image in the state and nothing in the backend.
func assertQuiet(t *testing.T, d *device) {
	t.Helper()
	before := d.list(t)
	writes := d.folder.writes()
	report := d.sync(t)
	assert.Zero(t, report.Written, "changes written")
	assert.Zero(t, report.Applied, "records written into the backend")
	assert.Equal(t, writes, d.folder.writes(), "writes to the folder")
	delta := d.state.lastSave()
	assert.Empty(t, delta.Records, "records saved")
	assert.Empty(t, delta.Images, "images saved")
	assert.Empty(t, delta.DropImages, "images dropped")
	assert.Empty(t, delta.Cursors, "cursors saved")
	assert.Empty(t, delta.ReminderIDs, "reminder numbers saved")
	assert.Equal(t, before, d.list(t), "the backend")
}

func TestRunBeforeInit(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)

	_, err := mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncNotInitialized)

	status, err := mac.svc.Status(context.Background())
	require.NoError(t, err)
	assert.False(t, status.Initialized)
	assert.Equal(t, "/sync", status.Dir)
	assert.Zero(t, mac.folder.writes())
}

// Before init a write made through tock goes through unrecorded and touches no sync state.
func TestRecordBeforeInitOnlyWrites(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)

	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Backend", Description: "API", StartTime: at(0), EndTime: at(30),
	})
	require.NoError(t, err)
	assert.Len(t, mac.list(t), 1)
	assert.Empty(t, mac.state.saves)
}

func TestInitJournalsTheBackendAndASecondRunWritesNothing(t *testing.T) {
	clock := &testClock{now: at(120)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	ctx := context.Background()

	save(t, mac.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	save(t, mac.activities, models.Activity{Project: "Docs", Description: "Sync spec", StartTime: at(60)})
	require.NoError(t, mac.notes.Save(ctx, at(0).Format("150405"), at(0), "first draft", []string{"api", "go"}))
	_, err := mac.reminders.Save(ctx, models.Reminder{Title: "Review", Due: at(240), Created: at(-60)})
	require.NoError(t, err)

	report := mac.init(t)
	assert.Equal(t, 3, report.Written)
	assert.Zero(t, report.Applied)
	assert.True(t, report.Compared)
	assert.Equal(t, mac.deviceID(), report.Device)
	assert.Len(t, report.Device, 16)

	lines := folder.lines(mac.deviceID())
	require.Len(t, lines, 3)
	for _, line := range lines {
		assert.Equal(t, mac.deviceID(), line.Dev)
	}
	var api journal.Record
	for _, record := range mac.state.records {
		if a, ok := record.Activity(); ok && a.Project == "Backend" {
			api = record
		}
	}
	activity, ok := api.Activity()
	require.True(t, ok)
	assert.Equal(t, "first draft", activity.Notes)
	assert.Equal(t, []string{"api", "go"}, activity.Tags)
	assert.True(t, activity.StartTime.Equal(at(0)))

	status, err := mac.svc.Status(ctx)
	require.NoError(t, err)
	assert.True(t, status.Initialized)
	assert.Equal(t, mac.deviceID(), status.Device)
	require.Len(t, status.Devices, 1)
	assert.Equal(t, "mac", status.Devices[0].Name)

	assertQuiet(t, mac)
	clock.advance(time.Minute)
	assertQuiet(t, mac)

	full := mac.full(t)
	assert.Zero(t, full.Written)
	assert.Zero(t, full.Applied)
}

func TestTwoDevicesConverge(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	studio := newTimewarriorDevice(t, "studio", folder, clock)
	ctx := context.Background()

	_, err := mac.tracker.Add(ctx, models.AddActivityRequest{
		Project: "Backend", Description: "API", StartTime: at(0), EndTime: at(45), Notes: "spec", Tags: []string{"go"},
	})
	require.NoError(t, err)
	mac.init(t)
	studio.init(t)
	assert.Equal(t, summary(mac.list(t)), summary(studio.list(t)))

	// The studio starts and later stops an activity; the Mac edits the first one.
	clock.advance(time.Hour)
	_, err = studio.tracker.Start(ctx, models.StartActivityRequest{Project: "Docs", Description: "README", StartTime: at(60)})
	require.NoError(t, err)
	studio.sync(t)
	mac.sync(t)
	first := mac.list(t)[0]
	_, err = mac.tracker.Update(ctx, first, models.UpdateActivityRequest{Description: new("API v2")})
	require.NoError(t, err)
	mac.sync(t)
	clock.advance(30 * time.Minute)
	_, err = studio.tracker.Stop(ctx, models.StopActivityRequest{EndTime: at(90)})
	require.NoError(t, err)
	studio.sync(t)
	mac.sync(t)
	studio.sync(t)

	want := summary([]models.Activity{
		{Project: "Backend", Description: "API v2", StartTime: at(0), EndTime: new(at(45)), Notes: "spec", Tags: []string{"go"}},
		{Project: "Docs", Description: "README", StartTime: at(60), EndTime: new(at(90))},
	})
	assert.Equal(t, want, summary(mac.list(t)))
	assert.Equal(t, want, summary(studio.list(t)))

	// A removal on one device removes the activity on the other.
	docs := studio.list(t)[1]
	require.NoError(t, studio.tracker.Remove(ctx, docs))
	studio.sync(t)
	mac.sync(t)
	assert.Equal(t, want[:1], summary(mac.list(t)))

	assertQuiet(t, mac)
	assertQuiet(t, studio)
}

func TestFormatUnsupportedWritesNothing(t *testing.T) {
	clock := &testClock{now: at(0)}
	folder := newMemFolder()
	mac := newFileDevice(t, "mac", folder, clock)
	save(t, mac.activities, models.Activity{Project: "Backend", Description: "API", StartTime: at(0), EndTime: new(at(45))})
	mac.init(t)

	folder.format = []byte(`{"format":"tock-sync","version":2}`)
	saves := len(mac.state.saves)
	writes := folder.writes()
	save(t, mac.activities, models.Activity{Project: "Docs", Description: "README", StartTime: at(60), EndTime: new(at(90))})
	tockFile, err := os.ReadFile(mac.backend.identity.Path)
	require.NoError(t, err)

	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncFormatUnsupported)
	assert.Len(t, mac.state.saves, saves)
	assert.Equal(t, writes, folder.writes())
	after, err := os.ReadFile(mac.backend.identity.Path)
	require.NoError(t, err)
	assert.Equal(t, tockFile, after)

	_, err = newFileDevice(t, "new", folder, clock).svc.Init(context.Background(), ports.SyncInitOptions{})
	require.ErrorIs(t, err, coreErrors.ErrSyncFormatUnsupported)
}
