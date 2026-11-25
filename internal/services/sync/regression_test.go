package sync_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// inZone runs the rest of a test with time.Local set to the named zone.
func inZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	require.NoError(t, err)
	old := time.Local
	time.Local = loc                       //nolint:reassign // the backend reads and writes local time; no test runs in parallel
	t.Cleanup(func() { time.Local = old }) //nolint:reassign // restores the zone
	return loc
}

// reminderTitles merges the journals of devices and returns the titles of the reminders they show.
func reminderTitles(folder *memFolder, devices ...string) []string {
	state := journal.NewState()
	for _, device := range devices {
		for _, change := range folder.lines(device) {
			state.Apply(change)
		}
	}
	var out []string
	for _, record := range state.Records(journal.TypeReminder) {
		if reminder, ok := record.Reminder(); ok {
			out = append(out, reminder.Title)
		}
	}
	return out
}

// visibleFrom merges every journal of the folder, as another device sees it.
func visibleFrom(folder *memFolder, devices ...string) []models.Activity {
	state := journal.NewState()
	for _, device := range devices {
		for _, change := range folder.lines(device) {
			state.Apply(change)
		}
	}
	var out []models.Activity
	for _, record := range state.Records(journal.TypeActivity) {
		if a, ok := record.Activity(); ok {
			out = append(out, a)
		}
	}
	return out
}

func fileText(t *testing.T, d *device) string {
	t.Helper()
	raw, err := os.ReadFile(d.backend.identity.Path)
	require.NoError(t, err)
	return string(raw)
}

// Values tock.txt cannot hold, such as a "|" or a line break in a description, are written as the
// file keeps them, and what the file gives back is not journaled over the merged value.
func TestValuesTheFileCannotHoldAreNotJournaledBack(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	phoneWrite(t, mac.folder, clock, 0, alphaUID, map[string]any{
		"start": at(0).Format(time.RFC3339), "end": at(30).Format(time.RFC3339),
		"project": "Backend", "description": "Review PR | auth", "notes": "",
	})
	phoneWrite(t, mac.folder, clock, 1, betaUID, map[string]any{
		"start": at(40).Format(time.RFC3339), "end": at(50).Format(time.RFC3339),
		"project": "Docs", "description": "first line\nsecond line", "notes": "",
	})
	clock.advance(time.Hour)
	mac.sync(t)
	assert.Len(t, strings.Split(strings.TrimSpace(fileText(t, mac)), "\n"), 2, "one line each")

	clock.advance(time.Hour)
	report := mac.full(t)
	assert.Zero(t, report.Written)
	assert.Empty(t, changesOf(mac, alphaUID))
	assert.Empty(t, changesOf(mac, betaUID))
	assertQuiet(t, mac)
}

// Quotes and backslashes in a timewarrior annotation come back changed; the change is not journaled.
func TestTimewarriorQuotesAreNotJournaledBack(t *testing.T) {
	clock := &testClock{now: at(0)}
	studio := newTimewarriorDevice(t, "studio", newMemFolder(), clock)
	studio.init(t)
	phoneWrite(t, studio.folder, clock, 0, alphaUID, map[string]any{
		"start": at(0).Format(time.RFC3339), "end": at(30).Format(time.RFC3339),
		"project": "Backend", "description": `say "hi" to C:\temp`, "notes": "",
	})
	clock.advance(time.Hour)
	studio.sync(t)
	clock.advance(time.Hour)
	report := studio.full(t)
	assert.Zero(t, report.Written)
	assert.Empty(t, changesOf(studio, alphaUID))
	assertQuiet(t, studio)
}

// Two lines of tock.txt in one minute stay apart: the phone's removal or edit of one leaves the
// other alone.
func TestTwoLinesInOneMinuteStayApart(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  map[string]any
		want string
	}{
		{"removal", map[string]any{"deleted": true}, "2026-11-18 09:00 - 2026-11-18 09:40 | B | second\n"},
		{"edit", map[string]any{"description": "first edited"},
			"2026-11-18 09:00 - 2026-11-18 09:00 | A | first edited\n2026-11-18 09:00 - 2026-11-18 09:40 | B | second\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := &testClock{now: at(60)}
			mac := newFileDevice(t, "mac", newMemFolder(), clock)
			require.NoError(t, os.WriteFile(mac.backend.identity.Path, []byte(
				"2026-11-18 09:00 - 2026-11-18 09:00 | A | first\n2026-11-18 09:00 - 2026-11-18 09:40 | B | second\n"), 0o600))
			mac.init(t)
			uidA, uidB := uidOf(t, mac, "A"), uidOf(t, mac, "B")

			clock.advance(time.Hour)
			phoneWrite(t, mac.folder, clock, 0, uidA, tc.set)
			clock.advance(time.Minute)
			mac.sync(t)
			assert.Equal(t, tc.want, fileText(t, mac))

			clock.advance(time.Hour)
			report := mac.full(t)
			assert.Zero(t, report.Written)
			assert.Len(t, changesOf(mac, uidB), 1, "only the line init journaled")
			assertQuiet(t, mac)
		})
	}
}

// tock start in the minute a phone activity runs in replaces its line in tock.txt. The phone's
// activity is neither renamed to the new one nor deleted: it waits for the minute, empty there.
func TestStartInThePhoneActivityMinuteKeepsBoth(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	clock.advance(20 * time.Second)
	phoneStart(t, mac, clock, alphaUID, "Alpha", clock.now)
	clock.advance(5 * time.Second)
	mac.sync(t)

	clock.advance(20 * time.Second)
	_, err := mac.tracker.Start(context.Background(), models.StartActivityRequest{Project: "Mac", Description: "X", StartTime: clock.now})
	require.NoError(t, err)
	mac.build() // the command's process exits before it syncs
	clock.advance(time.Minute)
	mac.sync(t)
	assert.Empty(t, changesOf(mac, alphaUID), "the phone's activity keeps its uid, title and end")

	clock.advance(time.Hour)
	report := mac.full(t)
	assert.Zero(t, report.Written)
	assert.Empty(t, changesOf(mac, alphaUID))
	visible := visibleFrom(mac.folder, mac.deviceID(), phone)
	assert.ElementsMatch(t, []string{"Alpha", "X"}, descriptions(visible))
}

// A stop tock writes because another activity starts is told from one made on purpose by a later
// sync in another process too: the Mac does not journal the end of the phone's activity.
func TestAutoStopOutlivesTheProcess(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	phoneStart(t, mac, clock, alphaUID, "Alpha", at(0))
	clock.advance(time.Minute)
	mac.sync(t)

	clock.now = at(60)
	_, err := mac.tracker.Start(context.Background(), models.StartActivityRequest{Project: "Mac", Description: "X", StartTime: at(60)})
	require.NoError(t, err)
	mac.build()
	clock.now = at(90)
	_, err = mac.tracker.Stop(context.Background(), models.StopActivityRequest{EndTime: at(90)})
	require.NoError(t, err)
	mac.sync(t)
	assert.Empty(t, endsWritten(mac, alphaUID))
	assert.Empty(t, mac.state.meta.AutoStops, "the marks go once a sync went through")
}

// The end of the working day leaves another device's activity to its owner: it is not stopped in
// tock.txt, so no command stops it there for the next sync to put it back as running.
func TestWorkingHoursStopIsNotJournaledForOthers(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	phoneStart(t, mac, clock, alphaUID, "Alpha", at(0))
	clock.now = at(1)
	mac.sync(t)
	before := fileText(t, mac)

	weekdays, err := models.ParseWorkingWeekdays("")
	require.NoError(t, err)
	require.True(t, weekdays[at(600).Weekday()])
	hours := models.WorkingHours{StopHour: 18, Weekdays: weekdays}
	for minute := 600; minute < 603; minute++ {
		clock.now = at(minute)
		stopped, stopErr := mac.tracker.StopAfterWorkingHours(context.Background(), hours, at(minute))
		require.NoError(t, stopErr)
		assert.Nil(t, stopped)
		assert.Equal(t, before, fileText(t, mac))
		assert.Zero(t, mac.sync(t).Applied, "nothing to put back")
	}
	assert.Empty(t, endsWritten(mac, alphaUID))
}

// The end of the working day stops an activity this Mac owns, and that stop is journaled.
func TestWorkingHoursStopsOwnActivity(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	startOn(t, mac, "Mac", 0)
	clock.now = at(1)
	mac.sync(t)

	clock.now = at(600)
	weekdays, err := models.ParseWorkingWeekdays("")
	require.NoError(t, err)
	stopped, err := mac.tracker.StopAfterWorkingHours(context.Background(), models.WorkingHours{StopHour: 18, Weekdays: weekdays}, at(600))
	require.NoError(t, err)
	require.NotNil(t, stopped)
	mac.sync(t)
	uid := uidOf(t, mac, "Mac")
	assert.Len(t, endsWritten(mac, uid), 1)
	visible := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, visible, 1)
	require.NotNil(t, visible[0].EndTime)
	assert.True(t, visible[0].EndTime.Equal(*stopped.EndTime))
}

// A change of the Mac's time zone leaves tock.txt's wall times alone and journals nothing.
func TestTimeZoneChangeJournalsNothing(t *testing.T) {
	msk := inZone(t, "Europe/Moscow")
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, msk)
	clock := &testClock{now: start}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	for i := range 3 {
		clock.now = start.Add(time.Duration(i) * time.Hour)
		_, err := mac.tracker.Start(
			context.Background(),
			models.StartActivityRequest{Project: "Mac", Description: "work", StartTime: clock.now},
		)
		require.NoError(t, err)
		mac.sync(t)
	}
	_, err := mac.tracker.Stop(context.Background(), models.StopActivityRequest{EndTime: start.Add(150 * time.Minute)})
	require.NoError(t, err)
	mac.sync(t)
	before := fileText(t, mac)
	lines := len(mac.folder.lines(mac.deviceID()))

	inZone(t, "Europe/London")
	clock.now = start.Add(10 * time.Hour)
	mac.build()
	assert.Zero(t, mac.sync(t).Written)
	assert.Zero(t, mac.full(t).Written)
	assert.Equal(t, before, fileText(t, mac))
	assert.Len(t, mac.folder.lines(mac.deviceID()), lines)
	visible := visibleFrom(mac.folder, mac.deviceID())
	require.Len(t, visible, 3)
	for _, a := range visible {
		assert.Equal(t, 0, a.StartTime.UTC().Minute())
		assert.Contains(t, []int{6, 7, 8}, a.StartTime.UTC().Hour(), "the instants written in Moscow")
	}

	// An edit from the phone keeps the line's wall time; a new activity gets London's.
	phoneWrite(t, mac.folder, clock, 0, uidOfStart(t, mac, start), map[string]any{"description": "edited"})
	clock.advance(time.Minute)
	mac.sync(t)
	assert.Contains(t, fileText(t, mac), "2026-11-18 09:00 - 2026-11-18 10:00 | Mac | edited")
	assertQuiet(t, mac)
	assert.Zero(t, mac.full(t).Written)
}

func newTestUID(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	require.NoError(t, err)
	return id.String()
}

// uidOfStart returns the uid of the merged activity that starts at start.
func uidOfStart(t *testing.T, d *device, start time.Time) string {
	t.Helper()
	for _, record := range d.state.records {
		if a, ok := record.Activity(); ok && a.StartTime.Equal(start) {
			return record.UID
		}
	}
	t.Fatalf("%s holds no activity at %s", d.name, start)
	return ""
}

// An activity in the hour repeated when daylight saving time ends keeps its instant: tock.txt holds
// its wall time, which reads as the first of the two.
func TestRepeatedHourKeepsItsInstant(t *testing.T) {
	ny := inZone(t, "America/New_York")
	second := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) // 01:30 EST, the second 01:30
	clock := &testClock{now: second.Add(3 * time.Hour)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	phoneWrite(t, mac.folder, clock, 0, alphaUID, map[string]any{
		"start": second.In(ny).Format(time.RFC3339), "end": second.Add(20 * time.Minute).In(ny).Format(time.RFC3339),
		"project": "Phone", "description": "late", "notes": "",
	})
	clock.advance(time.Minute)
	mac.sync(t)
	assert.Contains(t, fileText(t, mac), "2026-11-01 01:30 - 2026-11-01 01:50 | Phone | late")

	clock.advance(time.Hour)
	assert.Zero(t, mac.full(t).Written)
	assert.Empty(t, changesOf(mac, alphaUID))
	assertQuiet(t, mac)
}

// Lines this device journaled that its segment lost are sent again once the state is rebuilt.
func TestLostOwnLinesAreSentAgain(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "kept", StartTime: at(0), EndTime: at(30),
	})
	require.NoError(t, err)
	mac.sync(t)
	require.Len(t, visibleFrom(mac.folder, mac.deviceID()), 1)

	mac.folder.mu.Lock()
	for key := range mac.folder.plain {
		if key[0] == mac.deviceID() {
			mac.folder.plain[key] = nil
		}
	}
	mac.folder.mu.Unlock()
	clock.advance(time.Hour)
	report := mac.sync(t)
	assert.True(t, report.Rebuilt)
	assert.Equal(t, 1, report.Written)
	assert.Equal(t, []string{"kept"}, descriptions(visibleFrom(mac.folder, mac.deviceID())))
	assertQuiet(t, mac)
}

// handEditing is a reminders repository that, once armed, appends a line to tock.txt by hand when the
// sync lists the reminders before writing them: between two of its writes into the backend.
type handEditing struct {
	ports.ReminderRepository

	path  string
	armed bool
}

func (h *handEditing) List(ctx context.Context) ([]models.Reminder, error) {
	if h.armed {
		h.armed = false
		f, err := os.OpenFile(h.path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		_, err = f.WriteString("2026-10-01 12:00 - 2026-10-01 12:30 | Hand | edit\n") // older than the notes window
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
	}
	return h.ReminderRepository.List(ctx)
}

// A line written by hand while a sync writes into the backend is not hidden by the token the sync
// saves: the next sync finds it.
func TestHandEditBetweenWritesIsFound(t *testing.T) {
	mac, studio, clock := twoMacs(t)
	hand := &handEditing{ReminderRepository: mac.reminders, path: mac.backend.identity.Path}
	mac.reminders = hand
	mac.build()
	_, err := studio.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Docs", Description: "README", StartTime: at(0), EndTime: at(30),
	})
	require.NoError(t, err)
	_, err = studio.recRem.Save(context.Background(), models.Reminder{Title: "Review", Due: at(240), Created: at(-60)})
	require.NoError(t, err)
	studio.sync(t)

	clock.advance(time.Minute)
	hand.armed = true
	mac.sync(t)
	require.False(t, hand.armed, "the hand edit ran")
	require.Contains(t, fileText(t, mac), "| Hand | edit")

	clock.advance(time.Minute)
	report := mac.sync(t)
	assert.Equal(t, 1, report.Written, "the hand-edited line is journaled")
	assert.Contains(t, descriptions(visibleFrom(mac.folder, mac.deviceID(), studio.deviceID())), "edit")
}

// counting counts the writes an activity repository gets.
type counting struct {
	ports.ActivityRepository

	saves, applies int
}

func (c *counting) Save(ctx context.Context, a models.Activity) error {
	c.saves++
	return c.ActivityRepository.Save(ctx, a)
}

func (c *counting) Apply(ctx context.Context, writes []ports.ActivityWrite) error {
	c.applies++
	return c.ActivityRepository.(ports.ActivityBatchWriter).Apply(ctx, writes)
}

// A sync that writes many activities into tock.txt writes the file once.
func TestManyWritesGoInOneBatch(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	const n = 300
	for i := range n {
		uid := newTestUID(t)
		phoneWrite(t, mac.folder, clock, i, uid, map[string]any{
			"start": at(2 * i).Format(time.RFC3339), "end": at(2*i + 1).Format(time.RFC3339),
			"project": "Phone", "description": "item", "notes": "",
		})
	}
	repo := &counting{ActivityRepository: mac.activities}
	mac.activities = repo
	mac.build()
	clock.advance(time.Minute)
	report := mac.sync(t)
	assert.Equal(t, n, report.Applied)
	assert.Equal(t, 1, repo.applies)
	assert.Zero(t, repo.saves)
	assert.Len(t, mac.list(t), n)
	assertQuiet(t, mac)
}

// An activity a run wrote into the backend without saving its image, as a full run interrupted
// before its save leaves it, is taken for its record by a later run that has not loaded that record.
func TestUnsavedImageIsAdoptedByAPlainRun(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	phoneWrite(t, mac.folder, clock, 0, alphaUID, map[string]any{
		"start": at(0).Format(time.RFC3339), "end": at(30).Format(time.RFC3339),
		"project": "Phone", "description": "written", "notes": "",
	})
	clock.advance(time.Minute)
	mac.sync(t)
	require.Len(t, mac.list(t), 1)

	delete(mac.state.images, [2]string{string(journal.TypeActivity), alphaUID})
	mac.state.meta.Token = nil
	mac.build()
	clock.advance(time.Minute)
	report := mac.sync(t)
	assert.Zero(t, report.Written)
	assert.Empty(t, mac.folder.lines(mac.deviceID()))
	assert.Len(t, visibleFrom(mac.folder, mac.deviceID(), phone), 1)
	assertQuiet(t, mac)
}

// failingBatch fails every batch while failing is set.
type failingBatch struct {
	ports.ActivityRepository

	failing bool
}

func (f *failingBatch) Apply(ctx context.Context, writes []ports.ActivityWrite) error {
	if f.failing {
		return os.ErrPermission
	}
	return f.ActivityRepository.(ports.ActivityBatchWriter).Apply(ctx, writes)
}

// A reminder without a uid keeps the one a run gave it even when the run fails after journaling it.
func TestNewReminderUIDIsStoredBeforeItIsJournaled(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.init(t)
	_, err := mac.reminders.Save(context.Background(), models.Reminder{Title: "Water plants", Due: at(240), Created: at(-60)})
	require.NoError(t, err)
	path := mac.backend.files[1]
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var kept []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if !strings.Contains(line, "uid:") {
			kept = append(kept, line)
		}
	}
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600))

	phoneWrite(t, mac.folder, clock, 0, alphaUID, map[string]any{
		"start": at(0).Format(time.RFC3339), "end": at(30).Format(time.RFC3339),
		"project": "Phone", "description": "p", "notes": "",
	})
	failing := &failingBatch{ActivityRepository: mac.activities, failing: true}
	mac.activities = failing
	mac.build()
	clock.advance(time.Minute)
	_, err = mac.svc.Run(context.Background(), ports.SyncOptions{Full: true})
	require.Error(t, err)

	failing.failing = false
	clock.advance(time.Minute)
	mac.full(t)
	uids := map[string]bool{}
	for _, change := range mac.folder.lines(mac.deviceID()) {
		if change.Type == journal.TypeReminder {
			uids[change.UID] = true
		}
	}
	assert.Len(t, uids, 1)
}

// A write made while a sync holds the state waits for it only so long, then goes on unrecorded; the
// next sync finds it by comparing the backend. An auto-stop waits for the lock: only its record tells
// it from a stop made on purpose.
func TestWriteDuringALongSyncGoesOnUnrecorded(t *testing.T) {
	clock := &testClock{now: at(0)}
	mac := newFileDevice(t, "mac", newMemFolder(), clock)
	mac.recordWait = 50 * time.Millisecond
	mac.build()
	mac.init(t)

	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- mac.state.Exclusive(context.Background(), func(context.Context) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	_, err := mac.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: "Mac", Description: "during", StartTime: at(0), EndTime: at(30),
	})
	require.NoError(t, err)
	assert.Len(t, mac.list(t), 1, "written without waiting for the sync")
	close(release)
	require.NoError(t, <-done)

	clock.advance(time.Minute)
	report := mac.sync(t)
	assert.Equal(t, 1, report.Written)
	assert.Equal(t, []string{"during"}, descriptions(visibleFrom(mac.folder, mac.deviceID())))
}
