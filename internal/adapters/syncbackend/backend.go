// Package syncbackend tells the sync bridge what it needs to know about each backend beyond its
// repositories: the time precision, where activity uids live, and the change token that says whether
// tock's data changed since tock last wrote it (docs/sync.md, Mac: the CLI's backends).
//
//	backend      uids                  precision           change token
//	file         state.db, by start    minute, local time  size and modification time of tock.txt
//	todotxt      state.db, by start    second              size and modification time of the file
//	timewarrior  state.db, by start    second, UTC         size and modification time of each YYYY-MM.data
//	sqlite       uid columns           second              a change log the triggers fill
//
// The file, todotxt and timewarrior backends keep reminders in .tock/reminders.yaml, whose size and
// modification time are a part of their token, and notes in .tock/notes/, which no token covers.
// The sqlite triggers and their change log table are created on the first Token call, so a database
// gets them only once sync runs.
package syncbackend

import (
	"database/sql"
	"sync"
	"time"

	"github.com/kriuchkov/tock/internal/adapters/repositories/file"
	"github.com/kriuchkov/tock/internal/adapters/repositories/timewarrior"
	"github.com/kriuchkov/tock/internal/adapters/repositories/todotxt"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// Backend names, as in the backend config key.
const (
	NameFile        = "file"
	NameTodoTXT     = "todotxt"
	NameTimewarrior = "timewarrior"
	NameSQLite      = "sqlite"
)

// files is a backend kept in plain files, with reminders in a sidecar YAML file: the token is the
// size and modification time of each file it holds.
type files struct {
	identity  ports.SyncBackendIdentity
	precision time.Duration
	// wall is set for the file backend, whose times are local wall times with no offset.
	wall bool
	// lineTags is set for todotxt and timewarrior, whose lines hold an activity's tags.
	lineTags bool
	// normalize is the round trip of an activity through the backend's format.
	normalize func(models.Activity) models.Activity
	// reminders is the path of .tock/reminders.yaml.
	reminders string
	// monthly is set for timewarrior: the path is a directory of YYYY-MM.data files, one token part
	// each, instead of one data file.
	monthly bool
}

// NewFile returns the probe of the file backend (Bartib's tock.txt at path), with its reminders in
// the YAML file at reminders.
func NewFile(path, reminders string) ports.SyncBackend {
	return &files{
		identity:  ports.SyncBackendIdentity{Name: NameFile, Path: path},
		precision: time.Minute,
		wall:      true,
		normalize: file.Normalize,
		reminders: reminders,
	}
}

// NewTodoTXT returns the probe of the todotxt backend at path, with its reminders in the YAML file at
// reminders.
func NewTodoTXT(path, reminders string) ports.SyncBackend {
	return &files{
		identity:  ports.SyncBackendIdentity{Name: NameTodoTXT, Path: path},
		precision: time.Second,
		lineTags:  true,
		normalize: todotxt.Normalize,
		reminders: reminders,
	}
}

// NewTimewarrior returns the probe of the timewarrior data directory dataDir, with its reminders in
// the YAML file at reminders. Its token has a part per YYYY-MM.data, so only the months that changed
// are compared.
func NewTimewarrior(dataDir, reminders string) ports.SyncBackend {
	return &files{
		identity:  ports.SyncBackendIdentity{Name: NameTimewarrior, Path: dataDir},
		precision: time.Second,
		lineTags:  true,
		normalize: timewarrior.Normalize,
		reminders: reminders,
		monthly:   true,
	}
}

func (f *files) Identity() ports.SyncBackendIdentity {
	return f.identity
}

func (f *files) Precision() time.Duration {
	return f.precision
}

func (f *files) WallClock() bool {
	return f.wall
}

func (f *files) Normalize(activity models.Activity) models.Activity {
	return f.normalize(activity)
}

func (f *files) KeepsUIDs() bool {
	return false
}

func (f *files) SidecarNotes() bool {
	return true
}

func (f *files) LineTags() bool {
	return f.lineTags
}

// database is the sqlite backend: triggers on activities and reminders log every changed row in a
// change log table, and the token is the log's last sequence number.
type database struct {
	db   *sql.DB
	path string
	// mu guards ready, set once this process made sure the change log and its triggers exist.
	mu    sync.Mutex
	ready bool
}

// NewSQLite returns the probe of the sqlite backend: db is the database at path the repositories
// use.
func NewSQLite(db *sql.DB, path string) ports.SyncBackend {
	return &database{db: db, path: path}
}

func (d *database) Identity() ports.SyncBackendIdentity {
	return ports.SyncBackendIdentity{Name: NameSQLite, Path: d.path}
}

func (d *database) Precision() time.Duration {
	return time.Second
}

func (d *database) WallClock() bool {
	return false
}

// Normalize returns the activity as it is: a row holds every value, and the times are compared at the
// second.
func (d *database) Normalize(activity models.Activity) models.Activity {
	return activity
}

func (d *database) KeepsUIDs() bool {
	return true
}

func (d *database) SidecarNotes() bool {
	return false
}

// LineTags is true: a row holds the activity's tags.
func (d *database) LineTags() bool {
	return true
}
