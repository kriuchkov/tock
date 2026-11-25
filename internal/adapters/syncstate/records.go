package syncstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// fieldColumns are the columns readRecords scans, in its order.
const fieldColumns = "f.uid, f.path, f.value, f.hlc_wall, f.hlc_counter, f.dev"

func (s *state) Records(ctx context.Context, typ journal.RecordType, uids []string) ([]journal.Record, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	list, err := jsonList(uids)
	if err != nil {
		return nil, err
	}
	return s.records(ctx, typ,
		"SELECT "+fieldColumns+" FROM fields AS f WHERE f.type = ? AND f.uid IN (SELECT value FROM json_each(?)) ORDER BY f.uid, f.path",
		string(typ), list)
}

func (s *state) AllRecords(ctx context.Context, typ journal.RecordType) ([]journal.Record, error) {
	return s.records(ctx, typ, "SELECT "+fieldColumns+" FROM fields AS f WHERE f.type = ? ORDER BY f.uid, f.path", string(typ))
}

// RunningActivities returns the activities the running table lists as running: those that were
// visible with no shown end when they were saved. A record's row there is set every time the record
// is saved. CROSS JOIN keeps running the outer table, so the query reads only the running records.
func (s *state) RunningActivities(ctx context.Context) ([]journal.Record, error) {
	return s.records(ctx, journal.TypeActivity,
		"SELECT "+fieldColumns+" FROM running AS r CROSS JOIN fields AS f ON f.type = ? AND f.uid = r.uid "+
			"WHERE r.until IS NULL ORDER BY f.uid, f.path",
		string(journal.TypeActivity))
}

// AutoEndedActivities returns the activities the running table lists as stopped by an auto_end later
// than after, when they were saved.
func (s *state) AutoEndedActivities(ctx context.Context, after time.Time) ([]journal.Record, error) {
	return s.records(ctx, journal.TypeActivity,
		"SELECT "+fieldColumns+" FROM running AS r CROSS JOIN fields AS f ON f.type = ? AND f.uid = r.uid "+
			"WHERE r.until > ? ORDER BY f.uid, f.path",
		string(journal.TypeActivity), after.Unix())
}

// ActivitiesStartingIn returns the activity records the starts table lists in span: those whose
// merged start was in it when they were saved. A zero To has no end.
func (s *state) ActivitiesStartingIn(ctx context.Context, span ports.SyncSpan) ([]journal.Record, error) {
	to := int64(math.MaxInt64)
	if !span.To.IsZero() {
		to = span.To.Unix()
		if span.To.Nanosecond() > 0 {
			to++
		}
	}
	from := span.From.Unix()
	if span.From.Nanosecond() > 0 {
		from++
	}
	return s.records(ctx, journal.TypeActivity,
		"SELECT "+fieldColumns+" FROM starts AS s CROSS JOIN fields AS f ON f.type = ? AND f.uid = s.uid "+
			"WHERE s.start >= ? AND s.start < ? ORDER BY f.uid, f.path",
		string(journal.TypeActivity), from, to)
}

// records runs a query of fieldColumns ordered by uid and gathers its rows into records.
func (s *state) records(ctx context.Context, typ journal.RecordType, query string, args ...any) ([]journal.Record, error) {
	var records []journal.Record
	err := s.read(ctx, func(ctx context.Context, db *sql.DB) error {
		var err error
		records, err = readRecords(ctx, db, typ, query, args...)
		return err
	})
	return records, err
}

func readRecords(ctx context.Context, db *sql.DB, typ journal.RecordType, query string, args ...any) ([]journal.Record, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "read sync records")
	}
	defer rows.Close()

	var (
		records []journal.Record
		// names holds one copy of each path and device: most rows repeat a few of them.
		names = make(map[string]string)
	)
	for rows.Next() {
		var (
			uid, path, dev sql.RawBytes
			value          []byte
			field          journal.Field
		)
		if err = rows.Scan(&uid, &path, &value, &field.HLC.Wall, &field.HLC.Counter, &dev); err != nil {
			return nil, errors.Wrap(err, "read sync records")
		}
		if len(records) == 0 || records[len(records)-1].UID != string(uid) {
			records = append(records, journal.Record{Type: typ, UID: string(uid), Fields: make(map[string]journal.Field)})
		}
		field.Value = value
		field.Dev = intern(names, dev)
		records[len(records)-1].Fields[intern(names, path)] = field
	}
	if err = rows.Err(); err != nil {
		return nil, errors.Wrap(err, "read sync records")
	}
	return records, nil
}

// saveRecords replaces the fields of each record and its row in running; the last of two records
// with the same type and uid wins. After a reset there is nothing to remove first.
func saveRecords(ctx context.Context, tx *sql.Tx, records []journal.Record, reset bool) error {
	if len(records) == 0 {
		return nil
	}
	records = lastOfEach(records)
	byType := make(map[journal.RecordType][]string)
	for _, record := range records {
		if record.Type == "" || record.UID == "" {
			return errors.Errorf("save sync record %q of type %q: no type or uid", record.UID, record.Type)
		}
		byType[record.Type] = append(byType[record.Type], record.UID)
	}
	if !reset {
		for typ, uids := range byType {
			if err := execList(ctx, tx, "DELETE FROM fields WHERE type = ? AND uid IN (SELECT value FROM json_each(?))",
				uids, string(typ)); err != nil {
				return errors.Wrap(err, "replace sync records")
			}
		}
		for _, table := range []string{"running", "starts"} {
			if err := execList(ctx, tx, "DELETE FROM "+table+" WHERE uid IN (SELECT value FROM json_each(?))",
				byType[journal.TypeActivity]); err != nil {
				return errors.Wrapf(err, "replace %s activities", table)
			}
		}
	}

	fields := newBatch(tx, "INSERT INTO fields (type, uid, path, value, hlc_wall, hlc_counter, dev)", 7)
	defer fields.close()
	running := newBatch(tx, "INSERT INTO running (uid, until)", 2)
	defer running.close()
	starts := newBatch(tx, "INSERT INTO starts (uid, start)", 2)
	defer starts.close()
	for _, record := range records {
		for path, field := range record.Fields {
			if err := fields.add(ctx, string(record.Type), record.UID, path, valueText(field.Value),
				field.HLC.Wall, field.HLC.Counter, field.Dev); err != nil {
				return errors.Wrap(err, "save sync records")
			}
		}
		if record.Type != journal.TypeActivity {
			continue
		}
		if open, until := openUntil(record); open {
			if err := running.add(ctx, record.UID, until); err != nil {
				return errors.Wrap(err, "save running activities")
			}
		}
		if start, ok := startOf(record); ok {
			if err := starts.add(ctx, record.UID, start.Unix()); err != nil {
				return errors.Wrap(err, "save activity starts")
			}
		}
	}
	for _, b := range []*batch{fields, running, starts} {
		if err := b.flush(ctx); err != nil {
			return errors.Wrap(err, "save sync records")
		}
	}
	return nil
}

// startOf returns the merged start of an activity record, hidden or not, when it is an RFC 3339
// instant.
func startOf(record journal.Record) (time.Time, bool) {
	var text string
	if json.Unmarshal(record.Fields["start"].Value, &text) != nil {
		return time.Time{}, false
	}
	start, err := time.Parse(time.RFC3339, text)
	return start, err == nil
}

// lastOfEach drops every record that a later one of the same type and uid replaces.
func lastOfEach(records []journal.Record) []journal.Record {
	type key struct {
		typ journal.RecordType
		uid string
	}
	last := make(map[key]int, len(records))
	for i, record := range records {
		last[key{record.Type, record.UID}] = i
	}
	if len(last) == len(records) {
		return records
	}
	kept := make([]journal.Record, 0, len(last))
	for i, record := range records {
		if last[key{record.Type, record.UID}] == i {
			kept = append(kept, record)
		}
	}
	return kept
}

// execList runs a statement whose last parameter is a list of values for json_each; it does nothing
// for an empty list.
func execList(ctx context.Context, tx *sql.Tx, query string, values []string, args ...any) error {
	if len(values) == 0 {
		return nil
	}
	list, err := jsonList(values)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, query, append(args, list)...)
	return err
}

// openUntil reports whether an activity record is visible and its end holds no instant, as rule 1
// takes them: one that runs, with until nil, or one an auto_end stops, with until that auto_end in
// Unix seconds.
func openUntil(record journal.Record) (bool, any) {
	activity, ok := record.Activity()
	switch {
	case !ok:
		return false, nil
	case activity.EndTime == nil:
		return true, nil
	case record.ShownEndField() == journal.FieldAutoEnd:
		return true, activity.EndTime.Unix()
	}
	return false, nil
}

// valueText is a field value as stored: the JSON as it was read, null for none.
func valueText(value json.RawMessage) string {
	if len(value) == 0 {
		return "null"
	}
	return string(value)
}

// jsonList encodes values as a JSON array for json_each, so a list of any length is one parameter.
func jsonList(values []string) (string, error) {
	list, err := json.Marshal(values)
	if err != nil {
		return "", errors.Wrap(err, "encode sync state query")
	}
	return string(list), nil
}

// intern returns b as a string, the same copy for equal contents.
func intern(seen map[string]string, b []byte) string {
	if s, ok := seen[string(b)]; ok {
		return s
	}
	s := string(b)
	seen[s] = s
	return s
}

// wrapRowsErr returns the error that ended rows, wrapped with msg, or nil.
func wrapRowsErr(rows *sql.Rows, msg string) error {
	if err := rows.Err(); err != nil {
		return errors.Wrap(err, msg)
	}
	return nil
}
