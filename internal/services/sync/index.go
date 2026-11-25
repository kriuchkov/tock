package sync

import (
	"context"
	"strconv"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
)

// findAllAbove is how many single keys are looked up one by one before the whole backend is read.
const findAllAbove = 16

// backendIndex holds the activities a run read from the backend, by key, and which keys it read, so
// the writes into the backend address the activity exactly as the backend holds it.
type backendIndex struct {
	keys  keying
	byKey map[string][]models.Activity
	seen  map[string]bool
	// complete is set once every activity was read.
	complete bool
	// spans are the start spans read: keys from from up to to, to zero for no end.
	spans []keySpan
}

type keySpan struct {
	from time.Time
	to   time.Time
}

func (s keySpan) holds(t time.Time) bool {
	return !t.Before(s.from) && (s.to.IsZero() || t.Before(s.to))
}

func newBackendIndex(keys keying) *backendIndex {
	return &backendIndex{keys: keys, byKey: make(map[string][]models.Activity), seen: make(map[string]bool)}
}

// identity tells two activities read from the backend apart.
func identity(a models.Activity) string {
	return strconv.FormatInt(a.StartTime.UnixNano(), 10) + "|" + a.UID + "|" + a.Project + "|" + a.Description
}

// add adds an activity read from the backend, once.
func (x *backendIndex) add(a models.Activity) {
	id := identity(a)
	if x.seen[id] {
		return
	}
	x.seen[id] = true
	key := x.keys.key(a.StartTime)
	x.byKey[key] = append(x.byKey[key], a)
}

// remove takes an activity out after it was removed or replaced in the backend.
func (x *backendIndex) remove(a models.Activity) {
	id := identity(a)
	if !x.seen[id] {
		return
	}
	delete(x.seen, id)
	key := x.keys.key(a.StartTime)
	kept := x.byKey[key][:0]
	for _, other := range x.byKey[key] {
		if identity(other) != id {
			kept = append(kept, other)
		}
	}
	x.byKey[key] = kept
}

// covered reports whether the activities with this key were read.
func (x *backendIndex) covered(key string) bool {
	if x.complete {
		return true
	}
	t, ok := x.keys.parse(key)
	if !ok {
		return false
	}
	for _, span := range x.spans {
		if span.holds(t) {
			return true
		}
	}
	return false
}

// at returns the activities read with this key.
func (x *backendIndex) at(key string) []models.Activity {
	return x.byKey[key]
}

// findAll reads every activity of the backend into the index.
func (r *run) findAll(ctx context.Context) ([]models.Activity, error) {
	activities, err := r.cfg.Activities.Find(ctx, models.ActivityFilter{})
	if err != nil {
		return nil, errors.Wrap(err, "read the backend's activities")
	}
	for _, a := range activities {
		r.index.add(a)
	}
	r.index.complete = true
	return activities, nil
}

// findSpan reads the activities whose key falls in span into the index and returns them.
func (r *run) findSpan(ctx context.Context, span keySpan) ([]models.Activity, error) {
	// An activity is found by the span it overlaps, so the filter starts a second early to catch one
	// that ends where it starts, and ends a step late to catch one whose key rounds down into span. A
	// wall-clock backend's keys may lie an hour off their activities' instants, so it reads an hour more
	// on either side.
	from := span.from.Add(-time.Second - r.keys.slack())
	filter := models.ActivityFilter{FromDate: &from}
	if !span.to.IsZero() {
		to := span.to.Add(r.precision + r.keys.slack())
		filter.ToDate = &to
	}
	activities, err := r.cfg.Activities.Find(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "read the backend's activities")
	}
	var out []models.Activity
	for _, a := range activities {
		if t, ok := r.keys.parse(r.keys.key(a.StartTime)); ok && span.holds(t) {
			r.index.add(a)
			out = append(out, a)
		}
	}
	r.index.spans = append(r.index.spans, span)
	return out, nil
}

// keySpanOf is the span of one key: its start up to the next step.
func (r *run) keySpanOf(key string) (keySpan, bool) {
	t, ok := r.keys.parse(key)
	if !ok {
		return keySpan{}, false
	}
	return keySpan{from: t, to: t.Add(r.precision)}, true
}

// ensureKeys reads the activities with the given keys that the index does not cover yet: one by
// one when they are few, else the whole backend.
func (r *run) ensureKeys(ctx context.Context, keys map[string]bool) error {
	var missing []string
	for key := range keys {
		if !r.index.covered(key) {
			missing = append(missing, key)
		}
	}
	if len(missing) > findAllAbove {
		_, err := r.findAll(ctx)
		return err
	}
	for _, key := range missing {
		span, ok := r.keySpanOf(key)
		if !ok {
			continue
		}
		if _, err := r.findSpan(ctx, span); err != nil {
			return err
		}
	}
	return nil
}
