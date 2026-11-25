package sync_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/file"
	"github.com/kriuchkov/tock/internal/adapters/repositories/notes"
	"github.com/kriuchkov/tock/internal/adapters/repositories/reminders"
	"github.com/kriuchkov/tock/internal/adapters/syncbackend"
	"github.com/kriuchkov/tock/internal/adapters/syncfolder"
	"github.com/kriuchkov/tock/internal/adapters/syncstate"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/services/activity"
	"github.com/kriuchkov/tock/internal/services/reminder"
	syncsvc "github.com/kriuchkov/tock/internal/services/sync"
	"github.com/kriuchkov/tock/internal/services/sync/recorder"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// The interop fixtures shared with tock-ios: sync folders one implementation wrote through its own
// code, each with the expected.json every device must read from it. interop_test.go writes tock's
// (make sync-interop OUT=<tock-ios>/Tests/Interop) and reads the app's, kept in testdata/ios.

const (
	// interopZone is the zone the scenarios run in and reminders are read in, on both sides.
	interopZone = "Europe/Berlin"
	// interopUIDPrefix starts every uid a scenario fixes. The file backend makes the other uids at
	// random; renameUIDs gives them fixed ones in this range too.
	interopUIDPrefix = "019a9b3c-0000-7000-8000-"
	// interopOutEnv names the absolute folder make sync-interop writes the fixtures to.
	interopOutEnv  = "SYNC_INTEROP_OUT"
	dateTimeLayout = "2006-01-02T15:04"
)

// interopUID is the uid of the n-th record of a group of a scenario, as tock-ios numbers its own:
// 019a9b3c-0000-7000-8000-0000000<group>00<nn>.
func interopUID(group, n int) string {
	return interopUIDPrefix + "0000000" + string(rune('0'+group)) + "00" + twoDigits(n)
}

func twoDigits(n int) string {
	return string([]byte{byte('0' + n/10%10), byte('0' + n%10)})
}

// interopWindow says which occurrences expected.json lists: those on the days [From, To), with their
// status at Now, all local to interopZone.
type interopWindow struct {
	From string
	To   string
	Now  string
}

// interopExpect is expected.json: what every device reads from a folder. tock-ios computes the same
// keys (Tests/TockStoreTests/Interop/InteropSupport.swift).
type interopExpect struct {
	Name string `json:"name"`
	TZ   string `json:"tz"`
	From string `json:"from"`
	To   string `json:"to"`
	Now  string `json:"now"`
	// Devices are the devices/*.json of the devices with a journal, by ID.
	Devices []interopDevice `json:"devices"`
	// Skipped counts the invalid lines.
	Skipped int `json:"skipped"`
	// Activities and Reminders are the visible records in uid order, with every merged field as
	// devices show them after the rules, as in the merge vectors.
	Activities []interopRecord `json:"activities"`
	Reminders  []interopRecord `json:"reminders"`
	// Writes are the ends each device with a journal writes after the rules, in uid order.
	Writes map[string][]interopWrite `json:"writes"`
	Read   interopRead               `json:"read"`
}

// interopKeys are the keys of expected.json both sides compute; the rest names the scenario.
func interopKeys() []string {
	return []string{"devices", "skipped", "activities", "reminders", "writes", "read"}
}

type interopDevice struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Platform string  `json:"platform"`
	App      string  `json:"app"`
	Seen     *string `json:"seen"`
}

type interopRecord struct {
	UID    string         `json:"uid"`
	Fields map[string]any `json:"fields"`
}

type interopWrite struct {
	Type string         `json:"type"`
	UID  string         `json:"uid"`
	Set  map[string]any `json:"set"`
}

// interopRead is what the records read as: the activities as devices show them, instants in UTC,
// and the occurrences of every visible reminder in the window.
type interopRead struct {
	Activities  []interopActivity   `json:"activities"`
	Occurrences []interopOccurrence `json:"occurrences"`
}

type interopActivity struct {
	UID         string   `json:"uid"`
	Start       string   `json:"start"`
	End         *string  `json:"end"`
	Project     string   `json:"project"`
	Description string   `json:"description"`
	Notes       string   `json:"notes"`
	Tags        []string `json:"tags"`
}

type interopOccurrence struct {
	UID    string `json:"uid"`
	Due    string `json:"due"`
	Status string `json:"status"`
}

// interopFolder is a sync folder as a device that owns nothing in it reads it, with syncfolder and
// journal only.
type interopFolder struct {
	state   *journal.State
	skipped int
	devices []ports.SyncDevice
	// writers are the devices with a journal.
	writers []string
}

// readInteropFolder reads every segment of the folder at dir from its start. It writes nothing.
func readInteropFolder(t *testing.T, dir string) interopFolder {
	t.Helper()
	ctx := context.Background()
	folder := syncfolder.New(dir)
	segments, err := folder.Segments(ctx)
	require.NoError(t, err)

	read := interopFolder{state: journal.NewState()}
	for _, segment := range segments {
		data, readErr := folder.Read(ctx, segment, 0)
		require.NoError(t, readErr)
		seg := journal.ReadSegment(segment.Device, data)
		require.Equal(t, len(data), seg.Read, "%s/%s ends in a whole line", segment.Device, segment.Month)
		for _, change := range seg.Changes {
			read.state.Apply(change)
		}
		read.skipped += len(seg.Skipped)
		if !slices.Contains(read.writers, segment.Device) {
			read.writers = append(read.writers, segment.Device)
		}
	}
	read.devices, err = folder.Devices(ctx)
	require.NoError(t, err)
	return read
}

// expect computes expected.json of the folder; reminders are read in time.Local, which the caller
// sets to interopZone.
func (f interopFolder) expect(t *testing.T, name string, window interopWindow) interopExpect {
	t.Helper()
	out := interopExpect{
		Name: name, TZ: interopZone, From: window.From, To: window.To, Now: window.Now, Skipped: f.skipped,
		Devices: []interopDevice{}, Activities: []interopRecord{}, Reminders: []interopRecord{},
		Writes: map[string][]interopWrite{},
		Read:   interopRead{Activities: []interopActivity{}, Occurrences: []interopOccurrence{}},
	}
	for _, device := range f.devices {
		if slices.Contains(f.writers, device.ID) {
			out.Devices = append(out.Devices, interopDevice{
				ID: device.ID, Name: device.Name, Platform: device.Platform, App: device.App, Seen: utcInstant(device.Seen),
			})
		}
	}

	rules := f.state.ApplyRules("")
	for _, stored := range f.state.Records(journal.TypeActivity) {
		rec := rules.Show(stored)
		act, ok := rec.Activity()
		if !ok {
			continue
		}
		out.Activities = append(out.Activities, interopRecord{UID: rec.UID, Fields: fieldValues(t, rec)})
		out.Read.Activities = append(out.Read.Activities, readActivity(rec.UID, act))
	}

	from, to, now := localDay(t, window.From), localDay(t, window.To), localWall(t, window.Now)
	for _, rec := range f.state.Records(journal.TypeReminder) {
		rem, ok := rec.Reminder()
		if !ok {
			continue
		}
		out.Reminders = append(out.Reminders, interopRecord{UID: rec.UID, Fields: fieldValues(t, rec)})
		layout := dateTimeLayout
		if rem.AllDay {
			layout = time.DateOnly
		}
		for _, o := range rem.Occurrences(from, to, now) {
			out.Read.Occurrences = append(out.Read.Occurrences, interopOccurrence{
				UID: rec.UID, Due: o.Due.In(time.Local).Format(layout), Status: string(o.Status(now)),
			})
		}
	}

	for _, device := range f.writers {
		writes := []interopWrite{}
		for _, stop := range f.state.ApplyRules(device).Writes {
			var end any
			require.NoError(t, json.Unmarshal(stop.End, &end))
			writes = append(writes, interopWrite{Type: string(journal.TypeActivity), UID: stop.UID, Set: map[string]any{"end": end}})
		}
		slices.SortFunc(writes, func(a, b interopWrite) int { return strings.Compare(a.UID, b.UID) })
		out.Writes[device] = writes
	}
	return out
}

func fieldValues(t *testing.T, rec journal.Record) map[string]any {
	t.Helper()
	values := make(map[string]any, len(rec.Fields))
	for path, field := range rec.Fields {
		var value any
		require.NoError(t, json.Unmarshal(field.Value, &value))
		values[path] = value
	}
	return values
}

func readActivity(uid string, act models.Activity) interopActivity {
	tags := act.Tags
	if tags == nil {
		tags = []string{}
	}
	out := interopActivity{
		UID: uid, Start: act.StartTime.UTC().Format(time.RFC3339), Project: act.Project, Description: act.Description,
		Notes: act.Notes, Tags: tags,
	}
	if act.EndTime != nil {
		out.End = utcInstant(*act.EndTime)
	}
	return out
}

func utcInstant(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// localDay is the first instant of a local date, the end of the gap where midnight is skipped.
func localDay(t *testing.T, value string) time.Time {
	t.Helper()
	day, err := time.Parse(time.DateOnly, value)
	require.NoError(t, err)
	return timeutil.WallTime(day.Year(), day.Month(), day.Day(), 0, 0, time.Local)
}

// localWall is a local wall time, YYYY-MM-DDTHH:MM.
func localWall(t *testing.T, value string) time.Time {
	t.Helper()
	wall, err := time.Parse(dateTimeLayout, value)
	require.NoError(t, err)
	return timeutil.WallTime(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), time.Local)
}

// checkInterop compares a computed expected.json with a written one key by key, so a failure names
// the part.
func checkInterop(t *testing.T, want []byte, got interopExpect) {
	t.Helper()
	data, err := json.Marshal(got)
	require.NoError(t, err)
	var wantKeys, gotKeys map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(want, &wantKeys))
	require.NoError(t, json.Unmarshal(data, &gotKeys))
	assert.JSONEq(t, string(wantKeys["tz"]), string(gotKeys["tz"]))
	for _, key := range interopKeys() {
		require.Contains(t, wantKeys, key)
		assert.JSONEq(t, string(wantKeys[key]), string(gotKeys[key]), key)
	}
}

// windowOf reads the name and window of an expected.json.
func windowOf(t *testing.T, data []byte) (string, interopWindow) {
	t.Helper()
	var head struct {
		Name string `json:"name"`
		From string `json:"from"`
		To   string `json:"to"`
		Now  string `json:"now"`
	}
	require.NoError(t, json.Unmarshal(data, &head))
	return head.Name, interopWindow{From: head.From, To: head.To, Now: head.Now}
}

// exportInterop writes a folder and its expected.json to <SYNC_INTEROP_OUT>/<name>, replacing what was
// there; without SYNC_INTEROP_OUT it does nothing.
func exportInterop(t *testing.T, name, dir string, expect interopExpect) {
	t.Helper()
	out := os.Getenv(interopOutEnv)
	if out == "" {
		return
	}
	require.True(t, filepath.IsAbs(out), "%s must be an absolute path", interopOutEnv)
	target := filepath.Join(out, name)
	require.NoError(t, os.RemoveAll(target))
	copyVisible(t, dir, filepath.Join(target, "folder"))
	data, err := json.MarshalIndent(expect, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(target, "expected.json"), append(data, '\n'), 0o600))
	t.Logf("wrote %s", target)
}

// copyFolder copies a fixture's folder into a new temporary one, without the hidden files a writer
// keeps next to its journal.
func copyFolder(t *testing.T, src string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sync")
	copyVisible(t, src, dir)
	return dir
}

// copyVisible copies the files of src to dst, leaving out every name that starts with ".", which
// readers ignore: the writer's lock files.
func copyVisible(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".") && rel != "." {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o600)
	})
	require.NoError(t, err)
}

// renameUIDs gives the activity uids the file backend made at random fixed ones, in the order the
// journals of devices first name them (devices and months in order), so the folder is the same on
// every run. A uid keeps its length, so no cursor moves. Uids that start with interopUIDPrefix stay.
func renameUIDs(t *testing.T, dir string, devices []string, group int) {
	t.Helper()
	uidPattern := regexp.MustCompile(`"uid":"([0-9a-f-]{36})"`)
	renamed := map[string]string{}
	type segment struct {
		path       string
		compressed bool
		content    []byte
	}
	var segments []segment
	for _, device := range devices {
		paths, err := filepath.Glob(filepath.Join(dir, "journal", device, "*.jsonl*"))
		require.NoError(t, err)
		slices.Sort(paths)
		for _, path := range paths {
			s := segment{path: path, compressed: strings.HasSuffix(path, ".gz")}
			s.content = readSegmentFile(t, path, s.compressed)
			for _, match := range uidPattern.FindAllSubmatch(s.content, -1) {
				uid := string(match[1])
				if _, done := renamed[uid]; !done && !strings.HasPrefix(uid, interopUIDPrefix) {
					renamed[uid] = interopUID(group, len(renamed)+1)
				}
			}
			segments = append(segments, s)
		}
	}
	for _, s := range segments {
		content := s.content
		for old, uid := range renamed {
			content = bytes.ReplaceAll(content, []byte(old), []byte(uid))
		}
		if s.compressed {
			content = gzipped(t, content)
		}
		require.NoError(t, os.WriteFile(s.path, content, 0o600))
	}
}

func readSegmentFile(t *testing.T, path string, compressed bool) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	if !compressed {
		return data
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	return content
}

// gzipped compresses as syncfolder does.
func gzipped(t *testing.T, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	require.NoError(t, err)
	_, err = writer.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.Bytes()
}

// interopMac is a Mac of a scenario as tock runs it with sync on: the file backend in a temporary
// home, state.db, the sync folder through syncfolder, and the activity and reminder services over
// the recorded repositories. Every command syncs after it, as the CLI does after a command that
// changed data.
type interopMac struct {
	t          *testing.T
	clock      *testClock
	svc        *syncsvc.Service
	tracker    ports.ActivityResolver
	reminders  ports.ReminderResolver
	recRem     ports.ReminderRepository
	activities ports.ActivityRepository
	notes      ports.NotesRepository
}

// newInteropMac sets up a Mac whose sync state already holds its device ID, so the scenario's
// journals are the same on every run.
func newInteropMac(t *testing.T, id, name, folder string, clock *testClock) *interopMac {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "tock.txt")
	remindersPath := filepath.Join(home, ".tock", "reminders.yaml")
	state := syncstate.New(filepath.Join(home, ".tock", "sync", "state.db"))
	require.NoError(t, state.Save(context.Background(), ports.SyncDelta{Meta: &ports.SyncMeta{Device: id}}))

	m := &interopMac{
		t:          t,
		clock:      clock,
		activities: file.NewRepository(path),
		notes:      notes.NewRepository(filepath.Join(home, ".tock", "notes")),
	}
	reminderRepo := reminders.NewRepository(remindersPath)
	m.svc = syncsvc.NewService(syncsvc.Config{
		Dir:        folder,
		OpenFolder: syncfolder.New,
		State:      state,
		Backend:    syncbackend.NewFile(path, remindersPath),
		Activities: m.activities,
		Notes:      m.notes,
		Reminders:  reminderRepo,
		Profile:    ports.SyncDevice{Name: name, Platform: "macos", App: "tock interop"},
		Now:        clock.Now,
	})
	m.tracker = activity.NewService(
		recorder.NewActivityRepository(m.activities, m.svc), recorder.NewNotesRepository(m.notes, m.svc), activity.WithOwners(m.svc),
	)
	m.recRem = recorder.NewReminderRepository(reminderRepo, m.svc)
	m.reminders = reminder.NewService(m.recRem, m.tracker)
	return m
}

// at sets the time every Mac of the scenario sees to a local wall time.
func (m *interopMac) at(wall string) {
	m.clock.now = localWall(m.t, wall)
}

func (m *interopMac) init() {
	m.t.Helper()
	report, err := m.svc.Init(context.Background(), ports.SyncInitOptions{})
	require.NoError(m.t, err)
	require.Empty(m.t, report.Unreadable)
}

func (m *interopMac) sync() ports.SyncReport {
	m.t.Helper()
	report, err := m.svc.Run(context.Background(), ports.SyncOptions{})
	require.NoError(m.t, err)
	require.Empty(m.t, report.Unreadable)
	return report
}

// start runs tock start at now, then syncs.
func (m *interopMac) start(project, description string, tags []string, notes string) {
	m.t.Helper()
	_, err := m.tracker.Start(context.Background(), models.StartActivityRequest{
		Project: project, Description: description, StartTime: m.clock.now, Tags: tags, Notes: notes,
	})
	require.NoError(m.t, err)
	m.sync()
}

// stop runs tock stop at now, then syncs.
func (m *interopMac) stop() {
	m.t.Helper()
	_, err := m.tracker.Stop(context.Background(), models.StopActivityRequest{EndTime: m.clock.now})
	require.NoError(m.t, err)
	m.sync()
}

// add runs tock add, then syncs.
func (m *interopMac) add(project, description, start, end string) {
	m.t.Helper()
	_, err := m.tracker.Add(context.Background(), models.AddActivityRequest{
		Project: project, Description: description, StartTime: localWall(m.t, start), EndTime: localWall(m.t, end),
	})
	require.NoError(m.t, err)
	m.sync()
}

// find returns the activity of the backend with project and description that started last.
func (m *interopMac) find(project, description string) models.Activity {
	m.t.Helper()
	list, err := m.tracker.List(context.Background(), models.ActivityFilter{})
	require.NoError(m.t, err)
	var found *models.Activity
	for i := range list {
		if list[i].Project == project && list[i].Description == description &&
			(found == nil || list[i].StartTime.After(found.StartTime)) {
			found = &list[i]
		}
	}
	require.NotNil(m.t, found, "no %s / %s", project, description)
	return *found
}

// edit runs tock edit on an activity, then syncs.
func (m *interopMac) edit(project, description string, req models.UpdateActivityRequest) {
	m.t.Helper()
	_, err := m.tracker.Update(context.Background(), m.find(project, description), req)
	require.NoError(m.t, err)
	m.sync()
}

// note runs tock note on an activity, then syncs.
func (m *interopMac) note(project, description, note string) {
	m.t.Helper()
	_, err := m.tracker.AddNote(context.Background(), m.find(project, description), note)
	require.NoError(m.t, err)
	m.sync()
}

// tag runs tock tag on an activity, then syncs.
func (m *interopMac) tag(project, description string, tags ...string) {
	m.t.Helper()
	_, err := m.tracker.AddTags(context.Background(), m.find(project, description), tags)
	require.NoError(m.t, err)
	m.sync()
}

// remove runs tock remove on an activity, then syncs.
func (m *interopMac) remove(project, description string) {
	m.t.Helper()
	require.NoError(m.t, m.tracker.Remove(context.Background(), m.find(project, description)))
	m.sync()
}

// remind saves a reminder as tock remind add keeps it, made now with a fixed uid, then syncs. tock
// remind add itself stamps created with the real time.
func (m *interopMac) remind(r models.Reminder) {
	m.t.Helper()
	r.Created = m.clock.now
	_, err := m.recRem.Save(context.Background(), r)
	require.NoError(m.t, err)
	m.sync()
}

// occurrence names the occurrence of a reminder on a local date by this Mac's #N.
func (m *interopMac) occurrence(uid, day string) models.OccurrenceRef {
	m.t.Helper()
	list, err := m.reminders.List(context.Background())
	require.NoError(m.t, err)
	for _, r := range list {
		if r.UID == uid {
			return models.OccurrenceRef{ID: r.ID, Day: localDay(m.t, day)}
		}
	}
	require.Failf(m.t, "no reminder", "%s", uid)
	return models.OccurrenceRef{}
}

// done runs tock remind done at now, then syncs.
func (m *interopMac) done(uid, day string) {
	m.t.Helper()
	_, err := m.reminders.SetDone(context.Background(), m.occurrence(uid, day), true, m.clock.now)
	require.NoError(m.t, err)
	m.sync()
}

// snooze runs tock remind snooze until a local wall time, then syncs.
func (m *interopMac) snooze(uid, day, until string) {
	m.t.Helper()
	_, err := m.reminders.Snooze(context.Background(), m.occurrence(uid, day), localWall(m.t, until))
	require.NoError(m.t, err)
	m.sync()
}

// skip runs tock remind skip, then syncs.
func (m *interopMac) skip(uid, day string) {
	m.t.Helper()
	require.NoError(m.t, m.reminders.Skip(context.Background(), m.occurrence(uid, day)))
	m.sync()
}

// backend lists the activities of the backend as the summary of the harness: minutes, notes and tags.
func (m *interopMac) backend() []string {
	m.t.Helper()
	ctx := context.Background()
	list, err := m.activities.Find(ctx, models.ActivityFilter{})
	require.NoError(m.t, err)
	for i := range list {
		n, tags, getErr := m.notes.Get(ctx, list[i].ID(), list[i].StartTime)
		require.NoError(m.t, getErr)
		list[i].Notes, list[i].Tags = n, tags
	}
	slices.SortFunc(list, func(a, b models.Activity) int { return a.StartTime.Compare(b.StartTime) })
	return summary(list)
}

// readSummary is the summary of the activities of expected.json, as backend gives it.
func readSummary(t *testing.T, activities []interopActivity) []string {
	t.Helper()
	list := make([]models.Activity, 0, len(activities))
	for _, a := range activities {
		start, err := time.Parse(time.RFC3339, a.Start)
		require.NoError(t, err)
		act := models.Activity{StartTime: start, Project: a.Project, Description: a.Description, Notes: a.Notes, Tags: a.Tags}
		if a.End != nil {
			end, endErr := time.Parse(time.RFC3339, *a.End)
			require.NoError(t, endErr)
			act.EndTime = &end
		}
		list = append(list, act)
	}
	slices.SortFunc(list, func(a, b models.Activity) int { return a.StartTime.Compare(b.StartTime) })
	return summary(list)
}
