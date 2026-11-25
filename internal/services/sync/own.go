package sync

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
)

// withheldEnd is an end found for an activity another device owns, decided once the rule has run.
type withheldEnd struct {
	uid string
	key string
	end json.RawMessage
}

// ownChanges turns what the compare found into this device's changes and applies rule 1 with
// owner-only stops:
//
//   - a value found that the merged state already holds, with the ends rule 1 gives, is no change:
//     it is what this or another device wrote, or what a run interrupted before saving wrote into the
//     backend;
//   - an end found for an activity another device owns is journaled only when the user stopped it on
//     purpose: not when the stop came from starting another activity (a recorded write or an
//     auto-stop mark of the meta tells), and not when it is the end rule 1 gives it anyway;
//   - the ends rule 1 gives the activities this device owns are journaled, while every segment was
//     read; the ends it gives the others are only shown.
func (r *run) ownChanges(ctx context.Context, finds []found, scope compareScope) error {
	refs := make([]ref, 0, len(finds))
	for _, f := range finds {
		refs = append(refs, f.ref)
	}
	if err := r.store.load(ctx, refs); err != nil {
		return err
	}

	before := stopEnds(r.store.rules(r.device).Stops)
	sets := make([]fields, len(finds))
	for i, f := range finds {
		sets[i] = r.foundSet(f, before)
		f.keepMergedNotes(sets[i], scope)
	}
	if err := r.checkDeletions(ctx, finds, sets, scope); err != nil {
		return err
	}

	var withheld []withheldEnd
	for i, f := range finds {
		set := sets[i]
		if f.ref.typ == journal.TypeActivity && r.othersEnd(f.ref, set) {
			withheld = append(withheld, withheldEnd{uid: f.ref.uid, key: f.key, end: set[fieldEnd]})
			delete(set, fieldEnd)
		}
		r.addOwn(f.ref.typ, f.ref.uid, set)
	}

	if len(withheld) > 0 {
		computed := stopEnds(r.store.rules(r.device).Stops)
		for _, w := range withheld {
			if scope.autoStop[w.key] || r.sameInstant(w.end, computed[w.uid]) || r.markedEnd(w) {
				continue
			}
			r.addOwn(journal.TypeActivity, w.uid, fields{fieldEnd: w.end})
		}
	}
	r.stops = r.ownStops()
	return nil
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
// start this set does not move.
func (r *run) othersEnd(activity ref, set fields) bool {
	if _, ok := set[fieldEnd]; !ok {
		return false
	}
	if _, ok := set[fieldStart]; ok {
		return false
	}
	record, known := r.store.get(activity)
	return known && !r.owned(record)
}

// foundSet returns the changes to journal for what the compare found about one record.
func (r *run) foundSet(f found, stops map[string]json.RawMessage) fields {
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
	if isDeleted(record) && f.old == nil {
		// A copy of a record deleted elsewhere that is about to leave the backend.
		return nil
	}

	set := diff(f.old, f.current, f.written)
	view, visible := r.view(record, stops)
	if !visible {
		return set
	}
	for path := range set {
		held, ok := f.current[path]
		if !ok {
			held = cleared(path)
		}
		if sameValue(path, held, view) {
			delete(set, path)
		}
	}
	return set
}

// view returns a merged record as the backend should hold it, in image form: with the end rule 1
// gives a running activity. visible is false for a hidden record.
func (r *run) view(record journal.Record, stops map[string]json.RawMessage) (fields, bool) {
	switch record.Type {
	case journal.TypeActivity:
		activity, ok := record.Activity()
		if !ok {
			return nil, false
		}
		if end, stopped := stops[record.UID]; stopped && activity.EndTime == nil {
			if t, parsed := parseInstant(end); parsed {
				activity.EndTime = &t
			}
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

// ownStops journals the ends rule 1 gives the activities this device owns, until none is left, and
// returns the ends every device shows, the others' included. Before every segment is read it
// journals none.
func (r *run) ownStops() map[string]json.RawMessage {
	for {
		rules := r.store.rules(r.device)
		if !r.complete || len(rules.Writes) == 0 {
			return stopEnds(rules.Stops)
		}
		for _, stop := range rules.Writes {
			r.addOwn(journal.TypeActivity, stop.UID, fields{fieldEnd: stop.End})
		}
	}
}

// markedEnd reports whether a withheld end is that of an auto-stop mark of the meta: a stop tock wrote
// because another activity started, which a process that did not sync afterwards recorded.
func (r *run) markedEnd(w withheldEnd) bool {
	end, ok := parseInstant(w.end)
	if !ok {
		return false
	}
	for _, mark := range r.meta.AutoStops {
		if r.keys.key(mark.Start) == w.key && mark.End.Truncate(r.precision).Equal(end.Truncate(r.precision)) {
			return true
		}
	}
	return false
}

// sameInstant reports whether two journal values hold the same instant at the backend's precision.
func (r *run) sameInstant(a, b json.RawMessage) bool {
	ta, okA := parseInstant(a)
	tb, okB := parseInstant(b)
	return okA && okB && ta.Truncate(r.precision).Equal(tb.Truncate(r.precision))
}
