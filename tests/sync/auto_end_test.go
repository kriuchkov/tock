package sync_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/file"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

// phoneDevice is the device ID of the phone whose journal TestSyncPhoneActivityStaysStopped writes.
const phoneDevice = "bb00000000000009"

// phoneActivity is the uid of the phone's activity.
const phoneActivity = "019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a10"

// minutesAgo returns the minutes before now, at whole minutes, as start -t and stop -t take them; ok
// is false when the earliest is on another day.
func minutesAgo(minutes ...int) ([]time.Time, bool) {
	now := time.Now().Truncate(time.Minute)
	out := make([]time.Time, 0, len(minutes))
	for _, m := range minutes {
		t := now.Add(-time.Duration(m) * time.Minute)
		if t.Day() != now.Day() {
			return nil, false
		}
		out = append(out, t)
	}
	return out, true
}

// activitiesOn returns the activities a device reports for a date, with their times.
func (d *device) activitiesOn(date time.Time) []models.Activity {
	d.t.Helper()
	var activities []models.Activity
	require.NoError(d.t, json.Unmarshal([]byte(d.run("report", "--date", date.Format(time.DateOnly), "--json")), &activities))
	return activities
}

// endOn returns the end a device reports for the activity of a project on a date; nil while it runs.
func (d *device) endOn(date time.Time, project string) *time.Time {
	d.t.Helper()
	for _, a := range d.activitiesOn(date) {
		if a.Project == project {
			return a.EndTime
		}
	}
	require.Failf(d.t, "no activity", "%s on %s", project, date.Format(time.DateOnly))
	return nil
}

// assertEndOn checks the end a device reports for the activity of a project.
func (d *device) assertEndOn(date time.Time, project string, want time.Time, msg string) {
	d.t.Helper()
	end := d.endOn(date, project)
	require.NotNil(d.t, end, "%s: %s", project, msg)
	assert.True(d.t, end.Equal(want), "%s ends at %s, not %s: %s", project, end, want, msg)
}

// lineOf returns the activity of a project in a device's tock.txt, as the file holds it.
func (d *device) lineOf(project string) models.Activity {
	d.t.Helper()
	raw, err := os.ReadFile(d.data)
	require.NoError(d.t, err)
	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		a, parseErr := file.ParseActivity(line)
		require.NoError(d.t, parseErr, "%q", line)
		if a.Project == project {
			return *a
		}
	}
	require.Failf(d.t, "no line", "%s in %s", project, d.data)
	return models.Activity{}
}

// assertQuiet checks that a sync of a device writes nothing, to the folder or to its backend.
func (d *device) assertQuiet() {
	d.t.Helper()
	report := d.sync()
	assert.Zero(d.t, report.Written, "changes written")
	assert.Zero(d.t, report.Applied, "records written into the backend")
}

// setsOf returns the changes in a device's journal that set path, in its order.
func setsOf(t *testing.T, folder, device, path string) []journal.Change {
	t.Helper()
	segments, err := filepath.Glob(filepath.Join(folder, "journal", device, "*.jsonl"))
	require.NoError(t, err)
	var out []journal.Change
	for _, segment := range segments {
		raw, readErr := os.ReadFile(segment)
		require.NoError(t, readErr)
		for line := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\n"), "\n") {
			change, parseErr := journal.ParseChange([]byte(line))
			require.NoError(t, parseErr, "%s: %q", segment, line)
			if _, ok := change.Set[path]; ok {
				out = append(out, change)
			}
		}
	}
	return out
}

// instantIn reads a journal value as an instant.
func instantIn(t *testing.T, value json.RawMessage) time.Time {
	t.Helper()
	var text string
	require.NoError(t, json.Unmarshal(value, &text))
	instant, err := time.Parse(time.RFC3339, text)
	require.NoError(t, err)
	return instant
}

// writePhoneStart appends to the folder, as the phone writes it, the start of an activity that runs.
func writePhoneStart(t *testing.T, folder string, start time.Time, project, description string) {
	t.Helper()
	now := time.Now()
	set := map[string]any{"start": start.Format(time.RFC3339), "end": nil, "project": project, "description": description, "notes": ""}
	line, err := json.Marshal(map[string]any{
		"v": 1, "hlc": journal.HLC{Wall: now.UnixMilli()}.String(), "dev": phoneDevice,
		"type": journal.TypeActivity, "uid": phoneActivity, "set": set,
	})
	require.NoError(t, err)
	dir := filepath.Join(folder, "journal", phoneDevice)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	segment, err := os.OpenFile(filepath.Join(dir, now.UTC().Format("2006-01")+".jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = segment.Write(append(line, '\n'))
	require.NoError(t, err)
	require.NoError(t, segment.Close())
}

// TestSyncOtherMacsActivityStaysStopped: Mac A owns an activity that runs. Mac B starts one, which
// stops A's in its backend, and stops it again before A syncs. B journals the end that gave A's
// activity as its auto_end, so it stays stopped where B's started, on B and, once A syncs, on A, whose
// tock.txt gets the end. A writes nothing, and the syncs after are quiet (docs/sync.md, rule 1).
func TestSyncOtherMacsActivityStaysStopped(t *testing.T) {
	times, ok := minutesAgo(10, 6, 3)
	if !ok {
		t.Skip("too close to midnight for start -t")
	}
	planning, build, stop := times[0], times[1], times[2]

	root := t.TempDir()
	bin := buildTock(t, root)
	folder := filepath.Join(root, "sync")
	a := newDevice(t, bin, filepath.Join(root, "a"), "file")
	b := newDevice(t, bin, filepath.Join(root, "b"), "sqlite")
	a.run("sync", "init", "--dir", folder)
	b.run("sync", "init", "--dir", folder)

	a.run("start", "-p", "Planning", "-d", "Started on A", "-t", planning.Format("15:04"))
	b.sync()
	require.Len(t, b.running(), 1, "A's activity runs on B")

	b.run("start", "-p", "Build", "-d", "Started on B", "-t", build.Format("15:04"))
	b.run("stop", "-t", stop.Format("15:04"))
	assert.Empty(t, b.running())
	b.assertEndOn(planning, "Planning", build, "B shows A's activity stopped where its own started")
	b.assertEndOn(planning, "Build", stop, "B's own stop")

	bID := b.sync().Device
	autoEnds := setsOf(t, folder, bID, journal.FieldAutoEnd)
	require.Len(t, autoEnds, 1, "B journals the auto_end of A's activity")
	assert.True(t, instantIn(t, autoEnds[0].Set[journal.FieldAutoEnd]).Equal(build))
	planningUID := autoEnds[0].UID

	aID := a.sync().Device
	assert.Empty(t, a.running())
	a.assertEndOn(planning, "Planning", build, "A shows the same")
	line := a.lineOf("Planning")
	require.NotNil(t, line.EndTime, "A's tock.txt gets the end")
	assert.True(t, line.EndTime.Equal(build))
	for _, path := range []string{journal.FieldEnd, journal.FieldAutoEnd} {
		for _, change := range setsOf(t, folder, aID, path) {
			if change.UID == planningUID {
				assert.JSONEq(t, "null", string(change.Set[path]), "A writes no end of its own activity")
			}
		}
	}

	a.assertQuiet()
	b.assertQuiet()
	a.assertQuiet()
}

// TestSyncPhoneActivityStaysStopped: the same with an activity the phone started, written into the
// folder as the app writes it. The Mac starts and stops its own, and the phone's stays stopped, on the
// Mac and on another Mac that joins the folder afterwards.
func TestSyncPhoneActivityStaysStopped(t *testing.T) {
	times, ok := minutesAgo(10, 6, 3)
	if !ok {
		t.Skip("too close to midnight for start -t")
	}
	phone, build, stop := times[0], times[1], times[2]

	root := t.TempDir()
	bin := buildTock(t, root)
	folder := filepath.Join(root, "sync")
	mac := newDevice(t, bin, filepath.Join(root, "mac"), "file")
	mac.run("sync", "init", "--dir", folder)
	writePhoneStart(t, folder, phone, "Phone", "Started on the phone")
	mac.sync()
	require.Len(t, mac.running(), 1, "the phone's activity runs on the Mac")

	mac.run("start", "-p", "Build", "-d", "Started on the Mac", "-t", build.Format("15:04"))
	mac.run("stop", "-t", stop.Format("15:04"))
	assert.Empty(t, mac.running())
	mac.assertEndOn(phone, "Phone", build, "the Mac shows the phone's activity stopped where its own started")
	line := mac.lineOf("Phone")
	require.NotNil(t, line.EndTime)
	assert.True(t, line.EndTime.Equal(build))

	autoEnds := setsOf(t, folder, mac.sync().Device, journal.FieldAutoEnd)
	require.Len(t, autoEnds, 1, "the Mac journals the auto_end of the phone's activity")
	assert.Equal(t, phoneActivity, autoEnds[0].UID)
	assert.True(t, instantIn(t, autoEnds[0].Set[journal.FieldAutoEnd]).Equal(build))
	mac.assertQuiet()

	other := newDevice(t, bin, filepath.Join(root, "other"), "file")
	other.run("sync", "init", "--dir", folder)
	assert.Empty(t, other.running())
	other.assertEndOn(phone, "Phone", build, "a Mac that joins shows it stopped too")
	other.assertQuiet()
	mac.assertQuiet()
}
