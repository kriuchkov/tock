package sync_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// The interop tests with tock-ios (see interop_support_test.go). TestInteropGoFolder and
// TestInteropMixedFolder write a folder through the real code: the file backend, the activity and
// reminder services over the recorded repositories, this bridge, state.db and syncfolder. Each run
// checks it; with SYNC_INTEROP_OUT set they also write it for tock-ios:
//
//	make sync-interop OUT=<tock-ios>/Tests/Interop
//
// TestInteropSwiftFolders reads the folders the app wrote through its JournalSync, which tock-ios
// writes into testdata/ios (Tests/Interop/README.md there).

const (
	macBook   = "aa00000000000001"
	macMini   = "aa00000000000002"
	macStudio = "aa00000000000003"
	// seedPhone is the phone of testdata/ios/mixed-seed.
	seedPhone = "bb00000000000003"
)

func mustRepeat(t *testing.T, spec string) models.Repeat {
	t.Helper()
	r, err := models.ParseRepeat(spec)
	require.NoError(t, err)
	return r
}

// TestInteropGoFolder: two Macs with the file backend over a week across a month: activities with
// notes and tags, an edited start, a stop made on purpose by the device that does not own the
// activity, a removal, reminders with done, snoozed and skipped occurrences, closed months compressed,
// and an auto_end: one Mac starts while the other's activity runs and writes the end that gives the
// other one as its auto_end, which its owner, not synced since, shows without writing anything.
func TestInteropGoFolder(t *testing.T) {
	inZone(t, interopZone)
	folder := filepath.Join(t.TempDir(), "sync")
	clock := &testClock{}
	m1 := newInteropMac(t, macBook, "MacBook", folder, clock)
	m2 := newInteropMac(t, macMini, "Mac mini", folder, clock)
	r := func(n int) string { return interopUID(2, n) }

	// Wednesday 28 October.
	m1.at("2026-10-28T08:55")
	m1.init()
	m1.at("2026-10-28T09:00")
	m1.start("Backend", "API review", []string{"api"}, "first pass")
	m1.at("2026-10-28T10:30")
	m1.stop()
	m1.at("2026-10-28T10:45")
	m1.start("Docs", "Sync spec", nil, "")
	m1.at("2026-10-28T12:00")
	m1.stop()
	m1.at("2026-10-28T12:05")
	m1.note("Docs", "Sync spec", "Folder section done")
	m1.at("2026-10-28T12:10")
	m1.remind(models.Reminder{
		UID: r(1), Title: "Water plants", Due: localWall(t, "2026-10-29T08:00"), Repeat: mustRepeat(t, "daily"),
		Tags: []string{"home"},
	})
	m1.remind(models.Reminder{
		UID: r(2), Title: "Weekly review", Project: "Ops", Due: localWall(t, "2026-10-30T16:00"),
		Repeat: mustRepeat(t, "weekly"), Notify: 10 * time.Minute, Notes: "Look back at the week",
	})
	m1.remind(models.Reminder{UID: r(3), Title: "Renew domain", Project: "Ops", Due: localDay(t, "2026-11-05"), AllDay: true})

	// Thursday 29 October: the Mac mini joins, tracks, and moves the MacBook's start.
	m2.at("2026-10-29T09:00")
	m2.init()
	m2.at("2026-10-29T09:05")
	m2.start("Frontend", "Widget layout", []string{"ui"}, "")
	m2.at("2026-10-29T11:00")
	m2.stop()
	m2.at("2026-10-29T11:05")
	moved := localWall(t, "2026-10-28T08:45")
	m2.edit("Backend", "API review", models.UpdateActivityRequest{StartTime: &moved})
	m2.at("2026-10-29T11:10")
	m2.tag("Frontend", "Widget layout", "widgets")

	// Friday 30 October.
	m1.at("2026-10-30T09:00")
	m1.sync()
	m1.at("2026-10-30T09:05")
	m1.add("Admin", "Expenses", "2026-10-30T08:00", "2026-10-30T08:30")
	m1.at("2026-10-30T09:10")
	m1.add("Misc", "Scratch", "2026-10-30T08:30", "2026-10-30T08:40")
	m1.at("2026-10-30T09:11")
	m1.remove("Misc", "Scratch")

	// Monday 2 November: the MacBook writes in a new month, so its October closes.
	m1.at("2026-11-02T08:05")
	m1.done(r(1), "2026-11-02")
	m1.at("2026-11-02T08:06")
	m1.sync()

	// Tuesday 3 November: the MacBook stops the Mac mini's activity on purpose.
	m2.at("2026-11-03T08:10")
	m2.sync()
	m2.snooze(r(1), "2026-11-03", "2026-11-03T09:30")
	m2.at("2026-11-03T08:11")
	m2.skip(r(1), "2026-11-04")
	m2.sync()
	m2.at("2026-11-03T10:00")
	m2.start("Meetings", "Standup", nil, "")
	m1.at("2026-11-03T10:10")
	m1.sync()
	m1.at("2026-11-03T10:15")
	m1.stop()
	m2.at("2026-11-03T10:20")
	m2.sync()

	// Wednesday 4 November: the Mac mini starts while the MacBook's activity runs and writes the end
	// that gives it as its auto_end; the MacBook does not sync again.
	m1.at("2026-11-04T09:00")
	m1.start("Backend", "Merge", nil, "")
	m2.at("2026-11-04T09:30")
	m2.sync()
	m2.at("2026-11-04T10:00")
	m2.start("Review", "PRs", []string{"review"}, "")
	m2.at("2026-11-04T10:05")
	assert.Zero(t, m2.sync().Written)

	for _, device := range []string{macBook, macMini} {
		assert.FileExists(t, filepath.Join(folder, "journal", device, "2026-10.jsonl.gz"))
		assert.NoFileExists(t, filepath.Join(folder, "journal", device, "2026-10.jsonl"))
	}
	renameUIDs(t, folder, []string{macBook, macMini}, 1)
	expect := readInteropFolder(t, folder).expect(t, "Go writes: two Macs with the file backend", interopWindow{
		From: "2026-10-26", To: "2026-11-09", Now: "2026-11-04T12:00",
	})
	assert.Equal(t, readSummary(t, expect.Read.Activities), m2.backend(), "the Mac mini's backend holds what every device reads")
	merge := readEnd(t, expect, "Merge")
	require.NotNil(t, merge)
	assert.Equal(t, "2026-11-04T09:00:00Z", *merge, "shown stopped where the Mac mini started")
	assert.Equal(t, "2026-11-04T10:00:00+01:00", fieldOf(t, expect, "Merge", journal.FieldAutoEnd), "written by the Mac mini")
	assert.Empty(t, expect.Writes[macBook], "the MacBook shows the auto_end and leaves it as it is")
	assert.Empty(t, expect.Writes[macMini])
	exportInterop(t, "go", folder, expect)
}

// TestInteropMixedFolder: a Mac with the file backend joins the folder of tock-ios's phone
// (testdata/ios/mixed-seed), starts its own activity while the phone's runs and writes the end that
// gives the phone's as its auto_end, adds a note to the phone's activity, marks the phone's reminder
// done and adds its own.
func TestInteropMixedFolder(t *testing.T) {
	inZone(t, interopZone)
	folder := copyFolder(t, filepath.Join("testdata", "ios", "mixed-seed", "folder"))
	clock := &testClock{}
	mac := newInteropMac(t, macStudio, "Mac Studio", folder, clock)

	mac.at("2026-11-09T10:00")
	mac.init()
	isRunning := true
	running, err := mac.tracker.List(context.Background(), models.ActivityFilter{IsRunning: &isRunning})
	require.NoError(t, err)
	require.Len(t, running, 1, "the phone's activity runs in the backend")
	mac.start("Backend", "Go side", []string{"go"}, "")
	mac.at("2026-11-09T10:05")
	mac.note("Ops", "Inbox", "Checked on the Mac")
	mac.at("2026-11-09T10:06")
	mac.done(interopUID(6, 1), "2026-11-09")
	mac.at("2026-11-09T10:10")
	mac.remind(models.Reminder{
		UID: interopUID(8, 1), Title: "Review PRs", Project: "Backend", Due: localWall(t, "2026-11-10T10:00"),
		Repeat: mustRepeat(t, "weekly"), Notify: 15 * time.Minute, Tags: []string{"team"},
	})
	mac.at("2026-11-09T10:15")
	assert.Zero(t, mac.sync().Written)

	renameUIDs(t, folder, []string{macStudio}, 7)
	expect := readInteropFolder(t, folder).expect(t, "Mixed: the phone's folder and a Mac with the file backend", interopWindow{
		From: "2026-11-09", To: "2026-11-16", Now: "2026-11-09T12:00",
	})
	assert.Equal(t, readSummary(t, expect.Read.Activities), mac.backend(), "the Mac's backend holds what every device reads")
	assert.Equal(t, "2026-11-09T10:00:00+01:00", fieldOf(t, expect, "Interop check", journal.FieldAutoEnd), "written by the Mac")
	assert.Empty(t, expect.Writes[seedPhone], "the phone shows the auto_end and leaves it as it is")
	assert.Empty(t, expect.Writes[macStudio])
	exportInterop(t, "mixed", folder, expect)
}

// TestInteropSwiftFolders reads the folders tock-ios wrote with syncfolder and journal, and expects
// what the app read from them; then a Mac with the file backend joins each and holds the same
// activities, journaling only the auto_end of every stop rule 1 gives.
func TestInteropSwiftFolders(t *testing.T) {
	inZone(t, interopZone)
	for _, name := range []string{"swift", "mixed-seed"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("testdata", "ios", name)
			want, err := os.ReadFile(filepath.Join(dir, "expected.json"))
			require.NoError(t, err)
			title, window := windowOf(t, want)
			read := readInteropFolder(t, filepath.Join(dir, "folder"))
			expect := read.expect(t, title, window)
			checkInterop(t, want, expect)

			clock := &testClock{now: localWall(t, window.Now)}
			mac := newInteropMac(t, macStudio, "Mac Studio", copyFolder(t, filepath.Join(dir, "folder")), clock)
			report, err := mac.svc.Init(context.Background(), ports.SyncInitOptions{})
			require.NoError(t, err)
			assert.Equal(t, len(read.state.Stops()), report.Written, "a Mac that owns nothing writes the auto_end of every stop")
			assert.Equal(t, readSummary(t, expect.Read.Activities), mac.backend())
			clock.advance(time.Minute)
			report = mac.sync()
			assert.Zero(t, report.Written)
			assert.Zero(t, report.Applied)
		})
	}
}

// fieldOf is a merged field of the activity with a description, as expected.json lists it; "" when it
// has none.
func fieldOf(t *testing.T, expect interopExpect, description, path string) any {
	t.Helper()
	for _, a := range expect.Read.Activities {
		if a.Description != description {
			continue
		}
		for _, record := range expect.Activities {
			if record.UID == a.UID {
				if value, ok := record.Fields[path]; ok {
					return value
				}
				return ""
			}
		}
	}
	require.Failf(t, "no activity", "%s", description)
	return nil
}

// readEnd is the end expected.json reads for the activity with a description.
func readEnd(t *testing.T, expect interopExpect, description string) *string {
	t.Helper()
	for _, a := range expect.Read.Activities {
		if a.Description == description {
			return a.End
		}
	}
	require.Failf(t, "no activity", "%s", description)
	return nil
}
