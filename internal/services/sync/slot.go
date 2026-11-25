package sync

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

// A backend holds one activity per key: tock.txt one per minute. Two merged activities can start
// within one key, as when the phone starts one and switches to another a few seconds later; rule 1
// then ends the first where the second starts, so at the backend's precision the first one is empty.
// Such an activity gives its key way to one that keeps time there, and waits outside the backend
// until the key is free. Nothing is journaled for it: its image goes with it, so the comparison sees
// no deletion.

// shown returns a merged activity as this device shows it: with the end rule 1 gives it, which this
// device journals only once it read every segment, else its shown end (end, else auto_end).
func (r *run) shown(record journal.Record) (models.Activity, bool) {
	return withStop(record, r.stops).Activity()
}

// withStop returns a merged record with the end rule 1 gives it, when stops holds one, as every device
// shows it (journal.Rules.Show): over a running activity's missing end or a later auto_end, never over
// an end.
func withStop(record journal.Record, stops map[string]json.RawMessage) journal.Record {
	if end, stopped := stops[record.UID]; stopped {
		return journal.Rules{Stops: []journal.Stop{{UID: record.UID, End: end}}}.Show(record)
	}
	return record
}

// emptyAt reports whether an activity ends within the step of precision it starts in, so the backend
// shows it as taking no time. A running activity is never empty.
func emptyAt(a models.Activity, precision time.Duration) bool {
	if a.EndTime == nil {
		return false
	}
	precision = max(precision, time.Second)
	return !a.EndTime.Truncate(precision).After(a.StartTime.Truncate(precision))
}

// evictee is an activity in the backend that gives its key way, with the merged record it shows.
type evictee struct {
	activity models.Activity
	ref      ref
}

// makeRoom frees key for the plan's activity when that one is not empty at the backend's precision
// and every other activity at key shows a merged record that is. It removes those from the backend,
// with their sidecar notes, and drops their images. An activity it cannot tie to a merged record,
// such as one written by hand since the last sync, is never removed.
func (r *run) makeRoom(ctx context.Context, key string, plan writePlan, current models.Activity, hasCurrent bool) (bool, error) {
	if emptyAt(plan.activity, r.precision) {
		return false, nil
	}
	var evict []evictee
	for _, a := range r.index.at(key) {
		if (hasCurrent && identity(a) == identity(current)) || (r.keepsUIDs && a.UID == plan.ref.uid) {
			continue
		}
		holder, ok, err := r.holderOf(ctx, key, a, plan.ref)
		if err != nil || !ok {
			return false, err
		}
		record, _ := r.store.get(holder)
		shown, visible := r.shown(record)
		if !visible || !emptyAt(shown, r.precision) {
			return false, nil
		}
		evict = append(evict, evictee{activity: a, ref: holder})
	}
	for _, e := range evict {
		if err := r.evict(ctx, e); err != nil {
			return false, err
		}
	}
	return true, nil
}

// holderOf returns the merged record an activity at key in the backend shows, loaded into the store:
// the one with its uid when the backend keeps uids, else the one whose image at key has its project
// and description.
func (r *run) holderOf(ctx context.Context, key string, a models.Activity, not ref) (ref, bool, error) {
	holder := ref{typ: journal.TypeActivity}
	switch {
	case r.keepsUIDs:
		if !validUID(a.UID) {
			return ref{}, false, nil
		}
		holder.uid = a.UID
	default:
		imgs, err := r.images.loadKeys(ctx, []string{key})
		if err != nil {
			return ref{}, false, err
		}
		for _, img := range imgs {
			if img.UID != not.uid && sameTitle(img.Fields, a) {
				holder.uid = img.UID
				break
			}
		}
		if holder.uid == "" {
			return ref{}, false, nil
		}
	}
	if err := r.store.load(ctx, []ref{holder}); err != nil {
		return ref{}, false, err
	}
	_, ok := r.store.get(holder)
	return holder, ok, nil
}

// evict takes an activity out of the backend so another one can have its key. Its sidecar notes go
// with it: the key's notes are those of the activity that takes it.
func (r *run) evict(ctx context.Context, e evictee) error {
	if err := r.replaceActivity(ctx, &e.activity, nil); err != nil {
		return errors.Wrapf(err, "make room at %s", r.keys.key(e.activity.StartTime))
	}
	if r.sidecar {
		if err := r.afterLines(ctx, func(ctx context.Context) error {
			return wrap(r.cfg.Notes.Delete(ctx, e.activity.ID(), e.activity.StartTime), "remove the notes of an activity that made room")
		}); err != nil {
			return err
		}
	}
	r.index.remove(e.activity)
	r.images.drop(e.ref)
	r.report.Applied++
	return nil
}
