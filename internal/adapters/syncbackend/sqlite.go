package syncbackend

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	// partLog is the only part of the sqlite token: the last sequence number of the change log.
	partLog = "log"
	// keptChanges is how many changes the log keeps once it is pruned; a token older than that
	// makes Changed cover everything.
	keptChanges = 10000
	// maxSpans caps the start times a compare reads one by one; more changes cover every activity.
	maxSpans     = 1000
	kindActivity = "activity"
	kindReminder = "reminder"
	// kindGap marks where the log stopped while sync was off: the changes made then were not logged.
	kindGap = "gap"
)

// triggerNames are the triggers of changeLogSchema.
const triggerNames = `'sync_activities_insert', 'sync_activities_update', 'sync_activities_delete',
	'sync_reminders_insert', 'sync_reminders_update', 'sync_reminders_delete'`

// changeLogSchema is the change log and the triggers that fill it: a row per changed reminder, and a
// row per start time an activity change touched, as Unix seconds (NULL when start_time does not
// parse). An update logs the old and the new start, so a moved activity is compared at both.
const changeLogSchema = `
CREATE TABLE IF NOT EXISTS sync_changes (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	kind TEXT NOT NULL,
	start INTEGER
);
CREATE TRIGGER IF NOT EXISTS sync_activities_insert AFTER INSERT ON activities BEGIN
	INSERT INTO sync_changes (kind, start) VALUES ('activity', CAST(strftime('%s', NEW.start_time) AS INTEGER));
END;
CREATE TRIGGER IF NOT EXISTS sync_activities_update AFTER UPDATE ON activities BEGIN
	INSERT INTO sync_changes (kind, start) VALUES
		('activity', CAST(strftime('%s', OLD.start_time) AS INTEGER)),
		('activity', CAST(strftime('%s', NEW.start_time) AS INTEGER));
END;
CREATE TRIGGER IF NOT EXISTS sync_activities_delete AFTER DELETE ON activities BEGIN
	INSERT INTO sync_changes (kind, start) VALUES ('activity', CAST(strftime('%s', OLD.start_time) AS INTEGER));
END;
CREATE TRIGGER IF NOT EXISTS sync_reminders_insert AFTER INSERT ON reminders BEGIN
	INSERT INTO sync_changes (kind) VALUES ('reminder');
END;
CREATE TRIGGER IF NOT EXISTS sync_reminders_update AFTER UPDATE ON reminders BEGIN
	INSERT INTO sync_changes (kind) VALUES ('reminder');
END;
CREATE TRIGGER IF NOT EXISTS sync_reminders_delete AFTER DELETE ON reminders BEGIN
	INSERT INTO sync_changes (kind) VALUES ('reminder');
END;
`

// changeLogObjects counts the table and the triggers of changeLogSchema.
const changeLogObjects = 7

// Exists reports whether the database file is there. The repositories create it when they open it,
// so a database that went away comes back empty: the deletion brake of the sync service, not this,
// keeps that from deleting every record.
func (d *database) Exists(_ context.Context) (bool, error) {
	return exists(d.path)
}

// Token returns the last sequence number of the change log, creating the log and its triggers first
// when the database has none.
func (d *database) Token(ctx context.Context) (ports.SyncToken, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	var last int64
	if err := d.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) FROM sync_changes`).Scan(&last); err != nil {
		return nil, errors.Wrap(err, "read the sync change log")
	}
	return ports.SyncToken{partLog: strconv.FormatInt(last, 10)}, nil
}

// Changed reads the changes logged after since up to now: the start times of the activities they
// touched, one second each, and the reminders when any changed. It covers everything when since is
// empty or not a log position, when the log went back (a database replaced by an older one), when
// the changes after since were pruned, and when they touched more than maxSpans start times.
func (d *database) Changed(ctx context.Context, since, now ports.SyncToken) (ports.SyncScope, error) {
	from, okFrom := logPosition(since)
	to, okTo := logPosition(now)
	switch {
	case !okFrom || !okTo || to < from:
		return everything(), nil
	case to == from:
		return ports.SyncScope{}, nil
	}

	if err := d.ensure(ctx); err != nil {
		return ports.SyncScope{}, err
	}
	kept, err := d.keeps(ctx, from)
	if err != nil {
		return ports.SyncScope{}, err
	}
	if !kept {
		return everything(), nil
	}
	return d.scopeBetween(ctx, from, to)
}

// ensure creates the change log and its triggers once per process when they are missing, and prunes
// the log down to keptChanges once it holds twice as many.
func (d *database) ensure(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ready {
		return nil
	}
	if d.db == nil {
		return errors.New("sync backend: no sqlite database")
	}

	var present int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name IN (
		'sync_changes', 'sync_activities_insert', 'sync_activities_update', 'sync_activities_delete',
		'sync_reminders_insert', 'sync_reminders_update', 'sync_reminders_delete')`).Scan(&present)
	if err != nil {
		return errors.Wrap(err, "inspect the sync change log")
	}
	if present < changeLogObjects {
		if err = d.createLog(ctx); err != nil {
			return err
		}
	}
	if err = d.prune(ctx); err != nil {
		return err
	}
	d.ready = true
	return nil
}

func (d *database) createLog(ctx context.Context) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin creating the sync change log")
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, changeLogSchema); err != nil {
		return errors.Wrap(err, "create the sync change log")
	}
	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "commit the sync change log")
	}
	return nil
}

func (d *database) prune(ctx context.Context) error {
	var first, last int64
	err := d.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq), 0), COALESCE(MAX(seq), 0) FROM sync_changes`).Scan(&first, &last)
	if err != nil {
		return errors.Wrap(err, "measure the sync change log")
	}
	if last-first+1 <= 2*keptChanges {
		return nil
	}
	if _, err = d.db.ExecContext(ctx, `DELETE FROM sync_changes WHERE seq <= ?`, last-keptChanges); err != nil {
		return errors.Wrap(err, "prune the sync change log")
	}
	return nil
}

// keeps reports whether the log still holds every change after from.
func (d *database) keeps(ctx context.Context, from int64) (bool, error) {
	var first int64
	if err := d.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(seq), 0) FROM sync_changes`).Scan(&first); err != nil {
		return false, errors.Wrap(err, "read the start of the sync change log")
	}
	return first > 0 && from >= first-1, nil
}

func (d *database) scopeBetween(ctx context.Context, from, to int64) (ports.SyncScope, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT kind, start FROM sync_changes WHERE seq > ? AND seq <= ?`, from, to)
	if err != nil {
		return ports.SyncScope{}, errors.Wrap(err, "read the sync change log")
	}
	defer rows.Close()

	var scope ports.SyncScope
	starts := map[int64]struct{}{}
	for rows.Next() {
		var kind string
		var start sql.NullInt64
		if err = rows.Scan(&kind, &start); err != nil {
			return ports.SyncScope{}, errors.Wrap(err, "scan the sync change log")
		}
		switch {
		case kind == kindGap:
			return everything(), nil
		case kind == kindReminder:
			scope.Reminders = true
		case kind == kindActivity && start.Valid:
			starts[start.Int64] = struct{}{}
		default:
			// An activity whose start did not parse, or a kind this probe does not write.
			scope.AllActivities = true
		}
	}
	if err = rows.Err(); err != nil {
		return ports.SyncScope{}, errors.Wrap(err, "iterate the sync change log")
	}

	if len(starts) > maxSpans {
		scope.AllActivities = true
	}
	spans := make([]ports.SyncSpan, 0, len(starts))
	for start := range starts {
		at := time.Unix(start, 0).UTC()
		spans = append(spans, ports.SyncSpan{From: at, To: at.Add(time.Second)})
	}
	return withSpans(scope, spans), nil
}

// DisableSQLite takes sync's change log out of the sqlite database db while sync is off, so a database
// that once synced does not log every write forever: it drops the triggers and empties the log. The
// log keeps one row that marks the gap, and its sequence goes on from where it was, so a sync after
// sync is turned on again compares everything: nothing logged the changes made in between. It does
// nothing when the database has no sync triggers.
func DisableSQLite(ctx context.Context, db *sql.DB) error {
	var present int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger' AND name IN (`+
		triggerNames+`)`).Scan(&present); err != nil {
		return errors.Wrap(err, "inspect the sync change log")
	}
	if present == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "begin removing the sync change log")
	}
	defer func() { _ = tx.Rollback() }()
	for _, trigger := range []string{
		"sync_activities_insert", "sync_activities_update", "sync_activities_delete",
		"sync_reminders_insert", "sync_reminders_update", "sync_reminders_delete",
	} {
		if _, err = tx.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+trigger); err != nil {
			return errors.Wrap(err, "drop a sync trigger")
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sync_changes; INSERT INTO sync_changes (kind) VALUES ('`+kindGap+`')`); err != nil {
		return errors.Wrap(err, "empty the sync change log")
	}
	if err = tx.Commit(); err != nil {
		return errors.Wrap(err, "commit removing the sync change log")
	}
	return nil
}

// logPosition reads the sequence number of a sqlite token; ok is false for any other token.
func logPosition(token ports.SyncToken) (int64, bool) {
	if len(token) != 1 {
		return 0, false
	}
	value, ok := token[partLog]
	if !ok {
		return 0, false
	}
	position, err := strconv.ParseInt(value, 10, 64)
	return position, err == nil && position >= 0
}
