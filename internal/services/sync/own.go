package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strconv"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// withheldEnd is an end found for an activity another device owns, decided once the rest of what the
// compare found is merged.
type withheldEnd struct {
	uid string
	key string
	end json.RawMessage
	// cause is why tock wrote the end, as a recorded write or an auto-stop mark of the meta tells;
	// recorded is false for an end tock did not record, found by comparing the backend.
	cause    stopCause
	recorded bool
}

// ownChanges turns what the compare found into this device's changes and applies rule 1
// (docs/sync.md, Rules after the merge):
//
//   - a value found that the merged state already holds, the shown end and the ends rule 1 gives
//     included, is no change: it is what this or another device wrote, or what a run interrupted
//     before saving wrote into the backend;
//   - an end cleared is a reopen, journaled with auto_end cleared too (see reopen);
//   - an end found for an activity another device owns is journaled as othersEnds says: the stop of a
//     start as rule 1 says, the end rule 1 gives it as its auto_end, a stop made on purpose as its
//     end;
//   - what rule 1 then gives this device to write is journaled, while every segment was read: the end
//     of the activities it owns and the auto_end of the others.
func (r *run) ownChanges(ctx context.Context, finds []found, scope compareScope) error {
	refs := make([]ref, 0, len(finds))
	for _, f := range finds {
		refs = append(refs, f.ref)
	}
	if err := r.store.load(ctx, refs); err != nil {
		return err
	}
	if err := r.store.loadAutoEnded(ctx); err != nil {
		return err
	}

	before := stopEnds(r.store.rules(r.device).Stops)
	sets := make([]fields, len(finds))
	for i, f := range finds {
		sets[i] = r.foundSet(f, before, scope)
		f.keepMergedNotes(sets[i], scope)
	}
	if err := r.checkDeletions(ctx, finds, sets, scope); err != nil {
		return err
	}

	var withheld []withheldEnd
	for i, f := range finds {
		set := sets[i]
		if f.ref.typ == journal.TypeActivity && r.othersEnd(f.ref, set) {
			w := withheldEnd{uid: f.ref.uid, key: f.key, end: set[fieldEnd]}
			w.cause, w.recorded = r.causeOf(scope, w)
			withheld = append(withheld, w)
			delete(set, fieldEnd)
		}
		r.addOwn(f.ref.typ, f.ref.uid, set)
	}
	// What the compare found can start earlier than every activity that ran.
	if err := r.store.loadAutoEnded(ctx); err != nil {
		return err
	}
	r.othersEnds(withheld)
	r.stops = r.ownStops()
	return nil
}

// othersEnds journals the ends found for activities other devices own, once the rest of what the
// compare found is merged (docs/sync.md, rule 1). They are written at once, whether every segment was
// read or not, as the changes of a start are:
//
//   - the stop tock wrote because another activity started goes to the journal as startStop says;
//   - the end of the working day is not journaled: it leaves the activity to its owner;
//   - an end found by comparing the backend that is the one rule 1 gives the activity goes to the
//     journal as its auto_end, which an end its owner writes shows over;
//   - any other end, recorded through tock or found, is a stop made on purpose and goes to the journal
//     as its end.
func (r *run) othersEnds(withheld []withheldEnd) {
	if len(withheld) == 0 {
		return
	}
	rules := r.store.rules(r.device)
	writes := make(map[string]journal.Write, len(rules.Writes))
	for _, w := range rules.Writes {
		writes[w.UID] = w
	}
	for _, w := range withheld {
		write, stopped := writes[w.uid]
		switch {
		case w.cause.workday():
		case w.cause.auto:
			r.startStop(w, write, stopped)
		case !w.recorded && stopped && r.sameInstant(w.end, write.Value):
			r.addOwn(journal.TypeActivity, w.uid, fields(write.Set()))
		default:
			r.addOwn(journal.TypeActivity, w.uid, fields{fieldEnd: w.end})
		}
	}
}

// startStop journals the stop tock wrote for an activity another device owns because the activity of
// w.cause.next started while it ran as shown (docs/sync.md, rule 1): as journal.StartStopField says,
// its auto_end set to that start as the new record holds it, or its end, the one tock wrote, never
// before its start. This holds whether or not the new activity still runs. When the rule stops the
// activity earlier, at the start of an activity this device had not read when it made the stop, the
// rule's end is journaled instead. Nothing is journaled when the merged state shows the activity
// stopped by an end, or by an auto_end no later than that: another device stopped it meanwhile, and
// the sync writes that end back.
func (r *run) startStop(w withheldEnd, rule journal.Write, ruled bool) {
	record, _ := r.store.get(ref{typ: journal.TypeActivity, uid: w.uid})
	next := r.startedRecord(w)
	write := journal.Write{UID: w.uid, Field: journal.StartStopField(record, next, r.device)}
	if write.Field == journal.FieldAutoEnd {
		write.Value = next.Fields[fieldStart].Value
	} else {
		write.Value = notBeforeStart(w.end, record)
	}
	if ruled && r.earlier(rule.Value, write.Value) {
		write = rule
	}
	if r.stopsEarlier(record, write.Value) {
		r.addOwn(journal.TypeActivity, w.uid, fields(write.Set()))
	}
}

// startedRecord returns the record of the activity whose start stopped the activity of a withheld end:
// the visible activity, other than that one, that starts at w.cause.next at the backend's precision,
// the first by uid. When the merged state holds none, as when it was removed before the sync, it is a
// record that holds only that start, as a journal line would.
func (r *run) startedRecord(w withheldEnd) journal.Record {
	at := w.cause.next.Truncate(r.precision)
	for _, recRef := range r.store.refs(journal.TypeActivity) {
		if recRef.uid == w.uid {
			continue
		}
		record, _ := r.store.get(recRef)
		if a, visible := record.Activity(); visible && a.StartTime.Truncate(r.precision).Equal(at) {
			return record
		}
	}
	return journal.Record{
		Type:   journal.TypeActivity,
		Fields: map[string]journal.Field{fieldStart: {Value: journalForm().instant(at)}},
	}
}

// notBeforeStart returns end, or the start of the activity of record as the record holds it when end
// is before it, as an end a backend that keeps minutes holds can be.
func notBeforeStart(end json.RawMessage, record journal.Record) json.RawMessage {
	activity, visible := record.Activity()
	if t, ok := parseInstant(end); ok && visible && t.Before(activity.StartTime) {
		return record.Fields[fieldStart].Value
	}
	return end
}

// stopsEarlier reports whether an end stops the activity of record before the merged state shows it
// stopped: it runs, or its shown end is an auto_end later than end. Nothing goes over an end.
func (r *run) stopsEarlier(record journal.Record, end json.RawMessage) bool {
	activity, visible := record.Activity()
	switch {
	case !visible:
		return false
	case activity.EndTime == nil:
		return true
	case record.ShownEndField() == journal.FieldEnd:
		return false
	}
	t, ok := parseInstant(end)
	return ok && t.Truncate(r.precision).Before(activity.EndTime.Truncate(r.precision))
}

// earlier reports whether journal value a holds an instant before the one b holds, at the backend's
// precision.
func (r *run) earlier(a, b json.RawMessage) bool {
	ta, okA := parseInstant(a)
	tb, okB := parseInstant(b)
	return okA && okB && ta.Truncate(r.precision).Before(tb.Truncate(r.precision))
}

// causeOf returns why tock wrote a withheld end: the recorded save's mark, else an auto-stop mark of
// the meta for that end, which a process that did not sync afterwards left. recorded is false for an
// end tock did not record, found by comparing the backend.
func (r *run) causeOf(scope compareScope, w withheldEnd) (stopCause, bool) {
	cause, recorded := scope.stops[w.key]
	if cause.auto {
		return cause, true
	}
	if mark, marked := r.markedStop(w); marked {
		return mark, true
	}
	return cause, recorded
}

const (
	// maxDeletions is how many records one run may delete before the brake stops it.
	maxDeletions = 50
	// deletionShare stops a run that deletes more than one record in deletionShare of a type at once.
	deletionShare = 5
)

// checkDeletions is the deletion brake. A run that would journal the deletion of more than one record
// of a type, and of more than maxDeletions of them or more than one in deletionShare, journals nothing
// and returns a brakeError, coreErrors.ErrSyncTooManyDeletions, unless deletions are allowed: a data file moved and
// started again, emptied, or replaced by an older copy makes every record it held look deleted, and
// every device would lose them. Removals recorded through tock in this process are not counted.
func (r *run) checkDeletions(ctx context.Context, finds []found, sets []fields, scope compareScope) error {
	if r.opts.allowDeletions {
		return nil
	}
	counts := make(map[journal.RecordType]int)
	for i, f := range finds {
		if f.vanished && len(sets[i]) > 0 && !scope.recordedRemoval(f) {
			counts[f.ref.typ]++
		}
	}
	total := 0
	var braked []journal.RecordType
	for _, typ := range []journal.RecordType{journal.TypeActivity, journal.TypeReminder} {
		deletions := counts[typ]
		total += deletions
		if deletions < 2 {
			continue
		}
		// The images the run dropped for the records gone are no longer among those held.
		held, err := r.images.loadAll(ctx, typ)
		if err != nil {
			return err
		}
		if deletions > maxDeletions || deletions*deletionShare > len(held)+deletions {
			braked = append(braked, typ)
		}
	}
	if len(braked) == 0 {
		return nil
	}
	brake := &brakeError{deletions: total}
	if !slices.ContainsFunc(braked, func(typ journal.RecordType) bool { return r.paired[typ] > 0 }) {
		brake.match = make(map[journal.RecordType]bool, len(braked))
		for _, typ := range braked {
			brake.match[typ] = true
		}
	}
	return brake
}

// brakeError is coreErrors.ErrSyncTooManyDeletions with the number of deletions the run would journal.
// match names the types the brake stopped when none of their records in the backend paired with an
// image: their data was started over, emptied or created again, so Init matches them instead, backing
// up first what it rewrites, as a notes file edited by hand since the last sync (run.backUp).
type brakeError struct {
	deletions int
	match     map[journal.RecordType]bool
}

func (e *brakeError) Error() string {
	return strconv.Itoa(e.deletions) + ": " + coreErrors.ErrSyncTooManyDeletions.Error()
}

func (e *brakeError) Unwrap() error {
	return coreErrors.ErrSyncTooManyDeletions
}

// othersEnd reports whether a set ends an activity another device owns and keeps owning: one whose
// start this set does not move. A cleared end is a reopen, journaled as it is.
func (r *run) othersEnd(activity ref, set fields) bool {
	if _, ok := parseInstant(set[fieldEnd]); !ok {
		return false
	}
	if _, ok := set[fieldStart]; ok {
		return false
	}
	record, known := r.store.get(activity)
	return known && !r.owned(record)
}

// foundSet returns the changes to journal for what the compare found about one record.
func (r *run) foundSet(f found, stops map[string]json.RawMessage, scope compareScope) fields {
	record, known := r.store.get(f.ref)
	if f.vanished {
		if !known || isDeleted(record) {
			return nil
		}
		return fields{fieldDeleted: jsonBool(true)}
	}
	if !known {
		// A record the merged state does not hold, such as one whose journal went away or lost lines:
		// it is written whole, so other devices get all of its fields. While a segment is still to be
		// read, which may hold it, only once it changes.
		if f.old != nil && !r.complete && len(diff(f.old, f.current, f.written)) == 0 {
			return nil
		}
		return f.written.clone()
	}
	if f.old == nil {
		if isDeleted(record) {
			// A copy of a record deleted elsewhere that is about to leave the backend.
			return nil
		}
		return r.adoptedSet(f, record, stops, scope)
	}

	set := diff(f.old, f.current, f.written)
	if view, visible := r.view(record, stops); visible {
		for path := range set {
			held, ok := f.current[path]
			if !ok {
				held = cleared(path)
			}
			if sameValue(path, held, view) {
				delete(set, path)
			}
		}
	}
	reopen(record.Type, set)
	return set
}

// adoptedSet returns the changes to journal for a record the backend holds without an image: one a
// run wrote before it could save its image (docs/sync.md, Records interrupted runs wrote). The backend
// holds the merged view as that run wrote it, which other devices' changes may have moved on since,
// so the line is no edit: the write-back writes the merged view over it. Only a stop tock recorded for
// it since, a save or an auto-stop mark at its key, is journaled.
func (r *run) adoptedSet(f found, record journal.Record, stops map[string]json.RawMessage, scope compareScope) fields {
	end := f.written[fieldEnd]
	if _, stopped := parseInstant(end); !stopped || !r.recordedAt(scope, f.key) {
		return nil
	}
	if view, visible := r.view(record, stops); visible && sameValue(fieldEnd, f.current[fieldEnd], view) {
		return nil
	}
	return fields{fieldEnd: end}
}

// recordedAt reports whether tock recorded a save of the activity at key since the last sync that went
// through: in this process, or as an auto-stop mark of the meta.
func (r *run) recordedAt(scope compareScope, key string) bool {
	if _, recorded := scope.stops[key]; recorded {
		return true
	}
	return slices.ContainsFunc(r.meta.AutoStops, func(mark ports.SyncAutoStop) bool { return r.keys.key(mark.Start) == key })
}

// reopen makes a set that clears an activity's end clear its auto_end too, in the same change: the
// backend holds the shown end, so an end removed from a line, by hand or with tock edit --clear-end,
// reopens the activity, which would stay stopped at its auto_end otherwise (docs/sync.md, rule 1).
func reopen(typ journal.RecordType, set fields) {
	if end, ok := set[fieldEnd]; ok && typ == journal.TypeActivity && bytes.Equal(end, jsonNull()) {
		maps.Copy(set, journal.Reopen())
	}
}

// view returns a merged record as the backend should hold it, in image form: with the end rule 1
// gives it, if any, else its shown end. visible is false for a hidden record.
func (r *run) view(record journal.Record, stops map[string]json.RawMessage) (fields, bool) {
	switch record.Type {
	case journal.TypeActivity:
		activity, ok := withStop(record, stops).Activity()
		if !ok {
			return nil, false
		}
		return activityFields(r.normalize(activity), imageForm(r.precision)), true
	case journal.TypeReminder:
		reminder, ok := record.Reminder()
		if !ok {
			return nil, false
		}
		return reminderFields(reminder, imageForm(r.precision)), true
	}
	return nil, false
}

// ownStops journals what rule 1 gives this device to write, while every segment was read: the end of
// the activities it owns and the auto_end of the others. Once they are written none of them runs, so
// the rule gives no more. It returns the ends every device shows that are left: before every segment
// is read, all of them.
func (r *run) ownStops() map[string]json.RawMessage {
	rules := r.store.rules(r.device)
	if !r.complete || len(rules.Writes) == 0 {
		return stopEnds(rules.Stops)
	}
	for _, w := range rules.Writes {
		r.addOwn(journal.TypeActivity, w.UID, fields(w.Set()))
	}
	return stopEnds(r.store.rules(r.device).Stops)
}

// markedStop returns the auto-stop mark of the meta for a withheld end: a stop tock wrote on its own,
// because another activity started or at the end of the working day, which a process that did not
// sync afterwards recorded. ok is false when there is none.
func (r *run) markedStop(w withheldEnd) (stopCause, bool) {
	end, ok := parseInstant(w.end)
	if !ok {
		return stopCause{}, false
	}
	for _, mark := range r.meta.AutoStops {
		if r.keys.key(mark.Start) == w.key && mark.End.Truncate(r.precision).Equal(end.Truncate(r.precision)) {
			return stopCause{auto: true, next: mark.Next}, true
		}
	}
	return stopCause{}, false
}

// sameInstant reports whether two journal values hold the same instant at the backend's precision.
func (r *run) sameInstant(a, b json.RawMessage) bool {
	ta, okA := parseInstant(a)
	tb, okB := parseInstant(b)
	return okA && okB && ta.Truncate(r.precision).Equal(tb.Truncate(r.precision))
}
