package sync_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3" // the sqlite3 driver for the sqlite backend
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/file"
	"github.com/kriuchkov/tock/internal/adapters/repositories/notes"
	"github.com/kriuchkov/tock/internal/adapters/repositories/reminders"
	"github.com/kriuchkov/tock/internal/adapters/repositories/sqlite"
	"github.com/kriuchkov/tock/internal/adapters/repositories/timewarrior"
	"github.com/kriuchkov/tock/internal/adapters/repositories/todotxt"
	"github.com/kriuchkov/tock/internal/adapters/syncbackend"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/services/activity"
	syncsvc "github.com/kriuchkov/tock/internal/services/sync"
)

// testClock is the time every simulated device sees.
type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time { return c.now }

func (c *testClock) advance(d time.Duration) { c.now = c.now.Add(d) }

// base is a Wednesday morning in local time, the day the tests track.
func base() time.Time {
	return time.Date(2026, 11, 18, 9, 0, 0, 0, time.Local)
}

// device is one simulated Mac: its backend, its state.db and the sync service over both.
type device struct {
	name       string
	folder     *memFolder
	state      *memState
	backend    *fileBackend
	activities ports.ActivityRepository
	notes      ports.NotesRepository
	reminders  ports.ReminderRepository
	clock      *testClock
	svc        *syncsvc.Service
	// tracker is the activity service over the recorded repositories, as tock runs it.
	tracker ports.ActivityResolver
	recRem  ports.ReminderRepository
	// recordWait is the service's RecordWait; zero for its default.
	recordWait time.Duration
}

func (d *device) build() {
	d.svc = syncsvc.NewService(syncsvc.Config{
		Dir:        "/sync",
		OpenFolder: d.folder.open,
		State:      d.state,
		Backend:    d.backend,
		Activities: d.activities,
		Notes:      d.notes,
		Reminders:  d.reminders,
		Profile:    ports.SyncDevice{Name: d.name, Platform: "macos", App: "tock test"},
		Now:        d.clock.Now,
		RecordWait: d.recordWait,
	})
	d.tracker = activity.NewService(
		&recActivities{repo: d.activities, sink: d.svc}, &recNotes{repo: d.notes, sink: d.svc}, activity.WithOwners(d.svc),
	)
	d.recRem = &recReminders{repo: d.reminders, sink: d.svc}
}

// newFileDevice is a Mac with the file backend: tock.txt at minute precision in local time, notes
// and reminders in sidecar files.
func newFileDevice(t *testing.T, name string, folder *memFolder, clock *testClock) *device {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tock.txt")
	remindersPath := filepath.Join(dir, ".tock", "reminders.yaml")
	d := &device{
		name:   name,
		folder: folder,
		state:  newMemState(),
		backend: &fileBackend{
			identity:  ports.SyncBackendIdentity{Name: "file", Path: path},
			precision: time.Minute,
			sidecar:   true,
			wall:      true,
			normalize: file.Normalize,
			files:     []string{path, remindersPath},
			backup:    syncbackend.NewFile(path, remindersPath),
		},
		activities: file.NewRepository(path),
		notes:      notes.NewRepository(filepath.Join(dir, ".tock", "notes")),
		reminders:  reminders.NewRepository(remindersPath),
		clock:      clock,
	}
	d.build()
	return d
}

// newTimewarriorDevice is a Mac with the timewarrior backend: UTC seconds.
func newTimewarriorDevice(t *testing.T, name string, folder *memFolder, clock *testClock) *device {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	remindersPath := filepath.Join(dir, ".tock", "reminders.yaml")
	d := &device{
		name:   name,
		folder: folder,
		state:  newMemState(),
		backend: &fileBackend{
			identity:  ports.SyncBackendIdentity{Name: "timewarrior", Path: data},
			precision: time.Second,
			sidecar:   true,
			lineTags:  true,
			normalize: timewarrior.Normalize,
			files:     []string{data, remindersPath},
			backup:    syncbackend.NewTimewarrior(data, remindersPath),
		},
		activities: timewarrior.NewRepository(data),
		notes:      notes.NewRepository(filepath.Join(dir, ".tock", "notes")),
		reminders:  reminders.NewRepository(remindersPath),
		clock:      clock,
	}
	d.build()
	return d
}

// newTodoTXTDevice is a Mac with the todotxt backend: seconds, and tags in the line as well as in the
// notes sidecar.
func newTodoTXTDevice(t *testing.T, name string, folder *memFolder, clock *testClock) *device {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "todo.txt")
	remindersPath := filepath.Join(dir, ".tock", "reminders.yaml")
	d := &device{
		name:   name,
		folder: folder,
		state:  newMemState(),
		backend: &fileBackend{
			identity:  ports.SyncBackendIdentity{Name: "todotxt", Path: path},
			precision: time.Second,
			sidecar:   true,
			lineTags:  true,
			normalize: todotxt.Normalize,
			files:     []string{path, remindersPath},
			backup:    syncbackend.NewTodoTXT(path, remindersPath),
		},
		activities: todotxt.NewRepository(path),
		notes:      notes.NewRepository(filepath.Join(dir, ".tock", "notes")),
		reminders:  reminders.NewRepository(remindersPath),
		clock:      clock,
	}
	d.build()
	return d
}

// newSQLiteDevice is a Mac with the sqlite backend, which keeps uids.
func newSQLiteDevice(t *testing.T, name string, folder *memFolder, clock *testClock) *device {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tock.db")
	repo, err := sqlite.NewSQLiteActivityRepository(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.DB.Close() })
	reminderRepo, err := sqlite.NewReminderRepository(ctx, repo.DB)
	require.NoError(t, err)

	d := &device{
		name:   name,
		folder: folder,
		state:  newMemState(),
		backend: &fileBackend{
			identity:  ports.SyncBackendIdentity{Name: "sqlite", Path: path},
			precision: time.Second,
			keepsUIDs: true,
			lineTags:  true,
			token:     func() string { return dumpDB(repo.DB) },
			backup:    syncbackend.NewSQLite(repo.DB, path),
		},
		activities: repo,
		notes:      sqlite.NewNotesRepository(repo.DB),
		reminders:  reminderRepo,
		clock:      clock,
	}
	d.build()
	return d
}

// dumpDB is a change token for the sqlite backend: every row as text.
func dumpDB(db *sql.DB) string {
	var out strings.Builder
	for _, query := range []string{
		`SELECT description, project, start_time, COALESCE(end_time, ''), COALESCE(notes, ''), COALESCE(tags, ''), uid FROM activities ORDER BY id`,
		`SELECT id, title, due, notes, occurrences, uid FROM reminders ORDER BY id`,
	} {
		out.WriteString(dumpRows(db, query))
	}
	return out.String()
}

func dumpRows(db *sql.DB, query string) string {
	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		return err.Error()
	}
	defer rows.Close()

	var out strings.Builder
	columns, _ := rows.Columns()
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	for rows.Next() {
		_ = rows.Scan(pointers...)
		for _, v := range values {
			out.WriteString(toString(v))
			out.WriteByte('|')
		}
		out.WriteByte('\n')
	}
	if err = rows.Err(); err != nil {
		return err.Error()
	}
	return out.String()
}

func toString(v any) string {
	switch value := v.(type) {
	case []byte:
		return string(value)
	case time.Time:
		return value.UTC().Format(time.RFC3339Nano)
	case nil:
		return ""
	default:
		return fmt.Sprint(value)
	}
}

func (d *device) init(t *testing.T) ports.SyncReport {
	t.Helper()
	report, err := d.svc.Init(context.Background(), ports.SyncInitOptions{})
	require.NoError(t, err)
	return report
}

// restore runs tock sync init --restore.
func (d *device) restore(t *testing.T) ports.SyncReport {
	t.Helper()
	report, err := d.svc.Init(context.Background(), ports.SyncInitOptions{Restore: true})
	require.NoError(t, err)
	return report
}

func (d *device) sync(t *testing.T) ports.SyncReport {
	t.Helper()
	report, err := d.svc.Run(context.Background(), ports.SyncOptions{})
	require.NoError(t, err)
	return report
}

func (d *device) full(t *testing.T) ports.SyncReport {
	t.Helper()
	report, err := d.svc.Run(context.Background(), ports.SyncOptions{Full: true})
	require.NoError(t, err)
	return report
}

// list returns the backend's activities by start, with their notes and tags.
func (d *device) list(t *testing.T) []models.Activity {
	t.Helper()
	activities, err := d.activities.Find(context.Background(), models.ActivityFilter{})
	require.NoError(t, err)
	for i := range activities {
		n, tags, getErr := d.notes.Get(context.Background(), activities[i].ID(), activities[i].StartTime)
		require.NoError(t, getErr)
		if n != "" {
			activities[i].Notes = n
		}
		if len(tags) > 0 {
			activities[i].Tags = tags
		}
		activities[i].UID = ""
	}
	slices.SortFunc(activities, func(a, b models.Activity) int { return a.StartTime.Compare(b.StartTime) })
	return activities
}

// deviceID is the device's ID in its state.
func (d *device) deviceID() string {
	return d.state.meta.Device
}

// summary describes activities at minutes, to compare backends that keep different precisions.
func summary(activities []models.Activity) []string {
	const precision = time.Minute
	out := make([]string, 0, len(activities))
	for _, a := range activities {
		end := "running"
		if a.EndTime != nil {
			end = a.EndTime.Truncate(precision).UTC().Format(time.RFC3339)
		}
		tags := slices.Clone(a.Tags)
		slices.Sort(tags)
		out = append(out, strings.Join([]string{
			a.StartTime.Truncate(precision).UTC().Format(time.RFC3339), end, a.Project, a.Description,
			strings.TrimSpace(a.Notes), strings.Join(tags, ","),
		}, " | "))
	}
	return out
}

// recActivities records the saves and removals of an activity repository, as the recorder does.
type recActivities struct {
	repo ports.ActivityRepository
	sink ports.SyncChangeSink
}

func (r *recActivities) Save(ctx context.Context, a models.Activity) error {
	return r.sink.Record(ctx, func(ctx context.Context) (ports.SyncWrite, error) {
		return ports.SyncWrite{Kind: ports.SyncActivitySaved, Activity: a}, r.repo.Save(ctx, a)
	})
}

func (r *recActivities) Remove(ctx context.Context, a models.Activity) error {
	return r.sink.Record(ctx, func(ctx context.Context) (ports.SyncWrite, error) {
		return ports.SyncWrite{Kind: ports.SyncActivityRemoved, Activity: a}, r.repo.Remove(ctx, a)
	})
}

func (r *recActivities) FindLast(ctx context.Context) (*models.Activity, error) {
	return r.repo.FindLast(ctx)
}

func (r *recActivities) Find(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
	return r.repo.Find(ctx, filter)
}

// recNotes records the writes of a notes repository.
type recNotes struct {
	repo ports.NotesRepository
	sink ports.SyncChangeSink
}

func (r *recNotes) Save(ctx context.Context, id string, date time.Time, text string, tags []string) error {
	return r.sink.Record(ctx, func(ctx context.Context) (ports.SyncWrite, error) {
		w := ports.SyncWrite{
			Kind:       ports.SyncNotesSaved,
			ActivityID: id,
			Activity:   models.Activity{StartTime: date, Notes: text, Tags: tags},
		}
		return w, r.repo.Save(ctx, id, date, text, tags)
	})
}

func (r *recNotes) Delete(ctx context.Context, id string, date time.Time) error {
	return r.sink.Record(ctx, func(ctx context.Context) (ports.SyncWrite, error) {
		return ports.SyncWrite{
			Kind:       ports.SyncNotesDeleted,
			ActivityID: id,
			Activity:   models.Activity{StartTime: date},
		}, r.repo.Delete(
			ctx,
			id,
			date,
		)
	})
}

func (r *recNotes) Get(ctx context.Context, id string, date time.Time) (string, []string, error) {
	return r.repo.Get(ctx, id, date)
}

// recReminders records the writes of a reminder repository.
type recReminders struct {
	repo ports.ReminderRepository
	sink ports.SyncChangeSink
}

func (r *recReminders) List(ctx context.Context) ([]models.Reminder, error) { return r.repo.List(ctx) }

func (r *recReminders) Save(ctx context.Context, reminder models.Reminder) (*models.Reminder, error) {
	var saved *models.Reminder
	err := r.sink.Record(ctx, func(ctx context.Context) (ports.SyncWrite, error) {
		var err error
		saved, err = r.repo.Save(ctx, reminder)
		if err != nil {
			return ports.SyncWrite{}, err
		}
		return ports.SyncWrite{Kind: ports.SyncReminderSaved, Reminder: *saved}, nil
	})
	return saved, err
}

func (r *recReminders) Remove(ctx context.Context, id int) error {
	return r.sink.Record(ctx, func(ctx context.Context) (ports.SyncWrite, error) {
		return ports.SyncWrite{Kind: ports.SyncReminderRemoved, Reminder: models.Reminder{ID: id}}, r.repo.Remove(ctx, id)
	})
}
