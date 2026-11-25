package sync

import (
	"cmp"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// ref names one record.
type ref struct {
	typ journal.RecordType
	uid string
}

// store is the part of the merged state one run works on: the records its changes touch, the
// running activities and the records the compare finds changed, loaded from SyncState on demand.
// It merges by the rule of docs/sync.md, as journal.State does: every field holds the value of the
// latest change that set it, by clock and then by device, and of two changes with the same clock and
// device the one applied later.
type store struct {
	state   ports.SyncState
	records map[ref]*journal.Record
	// loaded holds the records asked of SyncState, found or not.
	loaded map[ref]bool
	// complete is set when records holds every record: nothing is asked of SyncState.
	complete bool
	// dirty holds the records changed since they were loaded, to save.
	dirty map[ref]bool
	// autoEndedAfter is the instant the activities an auto_end stops were last loaded from; zero
	// before.
	autoEndedAfter time.Time
}

func newStore(state ports.SyncState) *store {
	return &store{state: state, records: make(map[ref]*journal.Record), loaded: make(map[ref]bool), dirty: make(map[ref]bool)}
}

// loadAll loads every record of SyncState and marks the store complete.
func (s *store) loadAll(ctx context.Context) error {
	for _, typ := range []journal.RecordType{journal.TypeActivity, journal.TypeReminder} {
		records, err := s.state.AllRecords(ctx, typ)
		if err != nil {
			return errors.Wrapf(err, "load the %s records", typ)
		}
		for _, record := range records {
			s.seed(record)
		}
	}
	s.complete = true
	return nil
}

// load loads the records named that are not loaded yet.
func (s *store) load(ctx context.Context, refs []ref) error {
	if s.complete {
		return nil
	}
	byType := make(map[journal.RecordType][]string)
	for _, r := range refs {
		if !s.loaded[r] {
			s.loaded[r] = true
			byType[r.typ] = append(byType[r.typ], r.uid)
		}
	}
	for typ, uids := range byType {
		records, err := s.state.Records(ctx, typ, uids)
		if err != nil {
			return errors.Wrapf(err, "load %d %s records", len(uids), typ)
		}
		for _, record := range records {
			s.seed(record)
		}
	}
	return nil
}

// loadRunning loads the activities that may be running.
func (s *store) loadRunning(ctx context.Context) error {
	if s.complete {
		return nil
	}
	records, err := s.state.RunningActivities(ctx)
	if err != nil {
		return errors.Wrap(err, "load the running activities")
	}
	for _, record := range records {
		r := ref{typ: record.Type, uid: record.UID}
		s.loaded[r] = true
		s.seed(record)
	}
	return nil
}

// loadAutoEnded loads the activities an auto_end stops later than the earliest start among the
// running activities the store holds: rule 1 stops such an activity earlier when one that runs starts
// inside it (journal.RulesFor). Call it once what the run read or found is merged, as that can add an
// earlier running one.
func (s *store) loadAutoEnded(ctx context.Context) error {
	if s.complete {
		return nil
	}
	earliest, ok := s.earliestRunning()
	if !ok || (!s.autoEndedAfter.IsZero() && !earliest.Before(s.autoEndedAfter)) {
		return nil
	}
	records, err := s.state.AutoEndedActivities(ctx, earliest)
	if err != nil {
		return errors.Wrap(err, "load the activities an auto_end stops")
	}
	for _, record := range records {
		record.Type = journal.TypeActivity
		s.loaded[ref{typ: journal.TypeActivity, uid: record.UID}] = true
		s.seed(record)
	}
	s.autoEndedAfter = earliest
	return nil
}

// earliestRunning returns the earliest start among the running activities the store holds; ok is
// false when none runs.
func (s *store) earliestRunning() (time.Time, bool) {
	var earliest time.Time
	found := false
	for r, record := range s.records {
		if r.typ != journal.TypeActivity {
			continue
		}
		if a, visible := record.Activity(); visible && a.EndTime == nil && (!found || a.StartTime.Before(earliest)) {
			earliest, found = a.StartTime, true
		}
	}
	return earliest, found
}

// loadStartingIn loads the activity records state.db holds with a start in span and returns those the
// store did not hold.
func (s *store) loadStartingIn(ctx context.Context, span ports.SyncSpan) ([]ref, error) {
	if s.complete {
		return nil, nil
	}
	records, err := s.state.ActivitiesStartingIn(ctx, span)
	if err != nil {
		return nil, errors.Wrap(err, "load the activities that start near a key")
	}
	var out []ref
	for _, record := range records {
		r := ref{typ: journal.TypeActivity, uid: record.UID}
		s.loaded[r] = true
		if _, ok := s.records[r]; ok {
			continue
		}
		record.Type = journal.TypeActivity
		s.seed(record)
		out = append(out, r)
	}
	return out, nil
}

// seed adds a record as SyncState holds it, unless the store holds it already.
func (s *store) seed(record journal.Record) {
	r := ref{typ: record.Type, uid: record.UID}
	if _, ok := s.records[r]; ok {
		return
	}
	copied := journal.Record{Type: record.Type, UID: record.UID, Fields: make(map[string]journal.Field, len(record.Fields))}
	maps.Copy(copied.Fields, record.Fields)
	s.records[r] = &copied
}

// apply merges a change.
func (s *store) apply(change journal.Change) {
	r := ref{typ: change.Type, uid: change.UID}
	record, ok := s.records[r]
	if !ok {
		record = &journal.Record{Type: change.Type, UID: change.UID, Fields: make(map[string]journal.Field, len(change.Set))}
		s.records[r] = record
	}
	changed := false
	for path, value := range change.Set {
		if old, set := record.Fields[path]; set && compareChanges(change.HLC, change.Dev, old.HLC, old.Dev) < 0 {
			continue
		}
		record.Fields[path] = journal.Field{Value: value, HLC: change.HLC, Dev: change.Dev}
		changed = true
	}
	if changed || !ok {
		s.dirty[r] = true
	}
}

// get returns a record the store holds.
func (s *store) get(r ref) (journal.Record, bool) {
	record, ok := s.records[r]
	if !ok {
		return journal.Record{}, false
	}
	return *record, true
}

// holdsRecords reports whether the store holds an activity or a reminder that is not deleted.
func (s *store) holdsRecords() bool {
	for _, record := range s.records {
		if _, ok := record.Activity(); ok {
			return true
		}
		if _, ok := record.Reminder(); ok {
			return true
		}
	}
	return false
}

// refs returns the records of one type the store holds, by uid.
func (s *store) refs(typ journal.RecordType) []ref {
	var out []ref
	for r := range s.records {
		if r.typ == typ {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b ref) int { return cmp.Compare(a.uid, b.uid) })
	return out
}

// changed returns the records changed since they were loaded, all of them when the store is
// complete and replaces the saved ones.
func (s *store) changed(all bool) []journal.Record {
	var out []journal.Record
	for r, record := range s.records {
		if all || s.dirty[r] {
			out = append(out, *record)
		}
	}
	slices.SortFunc(out, func(a, b journal.Record) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.UID, b.UID))
	})
	return out
}

// rules applies the rules after the merge, as device dev does, to the activities the store holds
// that run or are stopped by their auto_end: the ends every device shows, and those dev writes.
func (s *store) rules(dev string) journal.Rules {
	var open []journal.Record
	for r, record := range s.records {
		if r.typ == journal.TypeActivity && record.ShownEndField() != journal.FieldEnd {
			open = append(open, *record)
		}
	}
	return journal.RulesFor(open, dev)
}

// stopEnds returns the ends of stops by uid.
func stopEnds(stops []journal.Stop) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(stops))
	for _, stop := range stops {
		out[stop.UID] = stop.End
	}
	return out
}

// compareChanges orders changes by clock, then by device.
func compareChanges(hlc journal.HLC, dev string, otherHLC journal.HLC, otherDev string) int {
	if c := hlc.Compare(otherHLC); c != 0 {
		return c
	}
	return cmp.Compare(dev, otherDev)
}
