package sync_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

const (
	alphaUID = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a10"
	betaUID  = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a11"
)

// phoneStart writes a running activity of the phone that starts at the given time.
func phoneStart(t *testing.T, d *device, clock *testClock, uid, description string, start time.Time) {
	t.Helper()
	phoneWrite(t, d.folder, clock, 0, uid, map[string]any{
		"start": start.Format(time.RFC3339), "end": nil, "project": "Work", "description": description, "notes": "",
	})
}

// Two activities the phone starts within one minute fall on one line of tock.txt. The backend holds
// the one still running, so the Mac shows what the phone shows; the other one waits.
func TestSameMinuteStartsShowTheRunningOne(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)

	clock.advance(5 * time.Second)
	phoneStart(t, mac, clock, alphaUID, "Alpha", clock.now)
	clock.advance(35 * time.Second)
	phoneStart(t, mac, clock, betaUID, "Beta", clock.now)
	clock.advance(time.Minute)
	mac.sync(t)

	got := mac.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, "Beta", got[0].Description)
	assert.Nil(t, got[0].EndTime, "Beta runs")
	assertAutoEndOnly(t, mac, alphaUID, at(0).Add(40*time.Second))
	assertQuiet(t, mac)
	assert.Zero(t, mac.full(t).Written)
}

// assertAutoEndOnly checks that all a device journaled is the auto_end of one activity it does not
// own, which rule 1 stops at end.
func assertAutoEndOnly(t *testing.T, d *device, uid string, end time.Time) {
	t.Helper()
	lines := d.folder.lines(d.deviceID())
	require.Len(t, lines, 1, "the Mac owns neither and writes only the auto_end")
	assert.Equal(t, uid, lines[0].UID)
	assert.Len(t, lines[0].Set, 1)
	assertInstants(t, []time.Time{end}, valuesWritten(d, uid, journal.FieldAutoEnd))
}

// An activity that arrives after another one at its minute was written takes the line when it ends
// later, and the one it replaces leaves the backend without a deletion and without leaving its notes.
func TestSameMinuteStartReplacesTheEarlierOne(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)

	clock.advance(5 * time.Second)
	phoneStart(t, mac, clock, alphaUID, "Alpha", clock.now)
	phoneWrite(t, mac.folder, clock, 1, alphaUID, map[string]any{"notes": "Alpha's notes", "tags/a": true})
	clock.advance(time.Minute)
	mac.sync(t)
	require.Len(t, mac.list(t), 1)
	assert.Equal(t, "Alpha's notes", mac.list(t)[0].Notes)

	phoneStart(t, mac, clock, betaUID, "Beta", at(0).Add(40*time.Second))
	clock.advance(time.Minute)
	mac.sync(t)

	got := mac.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, "Beta", got[0].Description)
	assert.Nil(t, got[0].EndTime)
	assert.Empty(t, got[0].Notes, "Alpha's notes left with it")
	assert.Empty(t, got[0].Tags)
	assertAutoEndOnly(t, mac, alphaUID, at(0).Add(40*time.Second))
	assertQuiet(t, mac)
	assert.Zero(t, mac.full(t).Written)

	// Once Beta moves on, Alpha comes back to its minute, stopped at its auto_end.
	clock.advance(time.Minute)
	phoneWrite(t, mac.folder, clock, 0, betaUID, map[string]any{"start": at(5).Format(time.RFC3339)})
	clock.advance(time.Minute)
	mac.sync(t)
	got = mac.list(t)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"Alpha", "Beta"}, descriptions(got))
	assert.Equal(t, "Alpha's notes", got[0].Notes)
	assert.NotNil(t, got[0].EndTime)
	assertAutoEndOnly(t, mac, alphaUID, at(0).Add(40*time.Second))
	assertQuiet(t, mac)
}

// Of two finished activities in one minute, the backend holds the one that ends later.
func TestSameMinuteKeepsTheLongerOne(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)

	clock.advance(3 * time.Hour)
	phoneWrite(t, mac.folder, clock, 0, alphaUID, map[string]any{
		"start": at(0).Add(5 * time.Second).Format(time.RFC3339), "end": at(120).Format(time.RFC3339),
		"project": "Work", "description": "Alpha", "notes": "",
	})
	phoneWrite(t, mac.folder, clock, 1, betaUID, map[string]any{
		"start": at(0).Add(40 * time.Second).Format(time.RFC3339), "end": at(0).Add(50 * time.Second).Format(time.RFC3339),
		"project": "Work", "description": "Beta", "notes": "",
	})
	clock.advance(time.Minute)
	mac.sync(t)

	got := mac.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, "Alpha", got[0].Description)
	assertQuiet(t, mac)
}

// An activity that keeps time at its minute keeps it: one written by hand since the last sync is
// journaled and stays, and the phone's activity at that minute waits rather than replace it.
func TestSameMinuteNeverReplacesAnActivityThatKeepsTime(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)

	save(t, mac.activities, models.Activity{Project: "Hand", Description: "Typed", StartTime: at(0), EndTime: new(at(30))})
	clock.advance(40 * time.Second)
	phoneStart(t, mac, clock, betaUID, "Beta", clock.now)
	clock.advance(time.Minute)
	mac.sync(t)

	got := mac.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, "Typed", got[0].Description)
	assert.Len(t, mac.folder.lines(mac.deviceID()), 1, "the hand-written activity is journaled")
	assertQuiet(t, mac)
}

// An activity that waited for its minute is written once the minute is free: by the next sync that
// reads it, and at the latest by a full one, which reads every record.
func TestSameMinuteWaitingActivityComesBack(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)

	typed := models.Activity{Project: "Hand", Description: "Typed", StartTime: at(0), EndTime: new(at(30))}
	save(t, mac.activities, typed)
	clock.advance(time.Hour)
	phoneWrite(t, mac.folder, clock, 0, betaUID, map[string]any{
		"start": at(0).Add(40 * time.Second).Format(time.RFC3339), "end": at(20).Format(time.RFC3339),
		"project": "Work", "description": "Beta", "notes": "",
	})
	clock.advance(time.Minute)
	mac.sync(t)
	require.Equal(t, []string{"Typed"}, descriptions(mac.list(t)))

	// Removed by hand: the minute is free, but nothing in the next sync names Beta.
	require.NoError(t, mac.activities.Remove(t.Context(), typed))
	clock.advance(time.Minute)
	mac.sync(t)
	mac.full(t)
	got := mac.list(t)
	require.Len(t, got, 1)
	assert.Equal(t, "Beta", got[0].Description)
	assert.True(t, got[0].EndTime.Equal(at(20)))
	assertQuiet(t, mac)
	assert.Zero(t, mac.full(t).Applied)
}
