package syncstate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/mattn/go-sqlite3"

	"github.com/kriuchkov/tock/internal/core/journal"
)

const (
	// schemaVersion is the layout of the tables, kept in PRAGMA user_version. A database of an older
	// version is rebuilt; one of a newer version is left alone (errNewerState). Version 2 keeps activity
	// times in images with their offset and the file backend's keys as wall times, and adds starts.
	// Version 3 reads the shown end (end, else auto_end) and lists the activities an auto_end stops in
	// running, with that auto_end: a tock from before it would put them back as running in the backend.
	schemaVersion = 3
	// busyTimeout is how long a write waits for another process's transaction.
	busyTimeout = 10 * time.Second
	// walLimit is the size the WAL is cut back to after a checkpoint; a full save leaves it large.
	walLimit = 4 << 20
)

// schema is the layout of version 3.
//
//	meta          name -> value, see metaKeys; dir, device and clock outlive a rebuild
//	fields        one row per merged field: the value as it was read, its clock and device
//	running       uids of the visible activity records whose end holds no instant: those that run, with
//	              until null, and those an auto_end stops, with that auto_end in Unix seconds as until
//	starts        the merged start of every activity record whose start parses, in Unix seconds
//	cursors       one row per segment read
//	images        the last-sync image per record: its backend key and its fields as a JSON object
//	reminder_ids  this device's #N per reminder uid
const schema = `
CREATE TABLE meta (
	name TEXT PRIMARY KEY NOT NULL,
	value TEXT NOT NULL
) WITHOUT ROWID;
CREATE TABLE fields (
	type TEXT NOT NULL,
	uid TEXT NOT NULL,
	path TEXT NOT NULL,
	value TEXT NOT NULL,
	hlc_wall INTEGER NOT NULL,
	hlc_counter INTEGER NOT NULL,
	dev TEXT NOT NULL,
	PRIMARY KEY (type, uid, path)
) WITHOUT ROWID;
CREATE TABLE running (
	uid TEXT PRIMARY KEY NOT NULL,
	until INTEGER
) WITHOUT ROWID;
CREATE INDEX running_by_until ON running (until);
CREATE TABLE starts (
	uid TEXT PRIMARY KEY NOT NULL,
	start INTEGER NOT NULL
) WITHOUT ROWID;
CREATE INDEX starts_by_start ON starts (start);
CREATE TABLE cursors (
	device TEXT NOT NULL,
	month TEXT NOT NULL,
	cursor_offset INTEGER NOT NULL,
	file_size INTEGER NOT NULL,
	compressed INTEGER NOT NULL,
	PRIMARY KEY (device, month)
) WITHOUT ROWID;
CREATE TABLE images (
	type TEXT NOT NULL,
	uid TEXT NOT NULL,
	backend_key TEXT NOT NULL,
	fields TEXT NOT NULL,
	PRIMARY KEY (type, uid)
) WITHOUT ROWID;
CREATE INDEX images_by_key ON images (type, backend_key);
CREATE TABLE reminder_ids (
	uid TEXT PRIMARY KEY NOT NULL,
	number INTEGER NOT NULL
) WITHOUT ROWID;
`

// connector opens connections with the sqlite3 driver itself, so the state does not depend on a
// driver registered under a name.
type connector struct {
	driver *sqlite3.SQLiteDriver
	dsn    string
}

func (c connector) Connect(context.Context) (driver.Conn, error) {
	return c.driver.Open(c.dsn)
}

func (c connector) Driver() driver.Driver {
	return c.driver
}

// openDB returns the database at path; it connects on first use. One connection serves the process:
// the methods run one at a time anyway, and the pragmas of the DSN and the hook apply to it.
func openDB(path string) *sql.DB {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	query := url.Values{}
	query.Set("_busy_timeout", strconv.FormatInt(busyTimeout.Milliseconds(), 10))
	query.Set("_journal_mode", "WAL")
	query.Set("_synchronous", "FULL")
	query.Set("_txlock", "immediate")
	// A file: URI escapes what the path holds, such as "?" or "#".
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()

	drv := &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		_, err := conn.Exec("PRAGMA journal_size_limit = "+strconv.Itoa(walLimit), nil)
		return err
	}}
	db := sql.OpenDB(connector{driver: drv, dsn: dsn})
	db.SetMaxOpenConns(1)
	return db
}

// isDamaged reports whether err says the file is not a database or a damaged one.
func isDamaged(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrNotADB || sqliteErr.Code == sqlite3.ErrCorrupt)
}

// errNewerState is returned for a database a newer tock made: rebuilding it would drop what that tock
// keeps, and it would rebuild it again in turn, each time matching the backend anew.
var errNewerState = errors.New("the sync state was made by a newer version of tock; update tock")

// ensureSchema rebuilds the database when it has an older layout, and refuses one with a newer layout.
func ensureSchema(ctx context.Context, db *sql.DB) error {
	version, err := userVersion(ctx, db)
	if err != nil {
		return err
	}
	switch {
	case version == schemaVersion:
		return nil
	case version > schemaVersion:
		return errors.Wrapf(errNewerState, "layout %d, this tock knows %d", version, schemaVersion)
	}
	return rebuild(ctx, db)
}

// queryer is what reads both outside and inside a transaction.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func userVersion(ctx context.Context, q queryer) (int, error) {
	var version int
	if err := q.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, errors.Wrap(err, "read sync state version")
	}
	return version, nil
}

// rebuild drops every table and makes the current ones in one transaction, keeping the sync folder,
// the device ID and its clock, and the backend, from a meta table of name and value columns, which
// every layout keeps. Another process may have rebuilt it first; then it leaves it alone.
func rebuild(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin sync state rebuild")
	}
	defer func() { _ = tx.Rollback() }()

	version, err := userVersion(ctx, tx)
	if err != nil {
		return err
	}
	switch {
	case version == schemaVersion:
		return nil
	case version > schemaVersion:
		return errors.Wrapf(errNewerState, "layout %d, this tock knows %d", version, schemaVersion)
	}

	kept := keptMeta(ctx, tx)
	if err = dropAll(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return errors.Wrap(err, "create sync state tables")
	}
	if err = putMeta(ctx, tx, kept); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(schemaVersion)); err != nil {
		return errors.Wrap(err, "write sync state version")
	}
	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "commit sync state rebuild")
	}
	return nil
}

// keptMeta reads what outlives a rebuild, as far as it can: the folder; the backend sync was set up
// with, marked to be matched again as after a backend switch, since its images go, so that a command on
// another data file after the rebuild is still told apart from it; and the device with its clock only
// together, since a device ID without its clock would take its own journal for a clone's.
func keptMeta(ctx context.Context, q queryer) map[string]string {
	values, err := readMeta(ctx, q)
	if err != nil {
		return nil
	}
	kept := make(map[string]string, len(values))
	if dir := values[metaDir]; dir != "" {
		kept[metaDir] = dir
	}
	if name := values[metaBackendName]; name != "" {
		kept[metaBackendName], kept[metaBackendPath], kept[metaMatchBackend] = name, values[metaBackendPath], "1"
	}
	device, clock := values[metaDevice], values[metaClock]
	if device == "" {
		return kept
	}
	if _, clockErr := journal.ParseHLC(clock); clock == "" || clockErr == nil {
		kept[metaDevice], kept[metaClock] = device, clock
	}
	return kept
}

// dropAll drops every table and view; their indexes and triggers go with them.
func dropAll(ctx context.Context, tx *sql.Tx) error {
	drops, err := dropStatements(ctx, tx)
	if err != nil {
		return err
	}
	for _, drop := range drops {
		if _, err = tx.ExecContext(ctx, drop); err != nil {
			return errors.Wrap(err, "drop sync state table")
		}
	}
	return nil
}

func dropStatements(ctx context.Context, q queryer) ([]string, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT type, name FROM sqlite_schema WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite\_%' ESCAPE '\'`)
	if err != nil {
		return nil, errors.Wrap(err, "list sync state tables")
	}
	defer rows.Close()

	var drops []string
	for rows.Next() {
		var kind, name string
		if err = rows.Scan(&kind, &name); err != nil {
			return nil, errors.Wrap(err, "list sync state tables")
		}
		drops = append(drops, "DROP "+strings.ToUpper(kind)+" IF EXISTS "+quoteIdent(name))
	}
	if err = rows.Err(); err != nil {
		return nil, errors.Wrap(err, "list sync state tables")
	}
	return drops, nil
}

// quoteIdent quotes an SQL identifier.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
