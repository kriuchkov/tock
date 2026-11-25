package sync

import (
	"context"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// lines queues a run's activity writes into the backend for one ports.ActivityBatchWriter.Apply, and
// the sidecar note writes that follow them, so a run that writes thousands of activities reads and
// writes the backend's file once, and each write replaces or removes exactly the activity it read,
// never another one at the same key. A backend whose repository applies no batches gets each write at
// once, as a save and a removal by start.
type lines struct {
	batch  ports.ActivityBatchWriter
	writes []ports.ActivityWrite
	after  []func(ctx context.Context) error
}

func newLines(activities ports.ActivityRepository) lines {
	batch, _ := activities.(ports.ActivityBatchWriter)
	return lines{batch: batch}
}

// batched reports whether the writes go into one batch.
func (l *lines) batched() bool {
	return l.batch != nil
}

// replaceActivity replaces old, an activity as the backend holds it, with saved: nil old adds saved,
// nil saved removes old.
func (r *run) replaceActivity(ctx context.Context, old, saved *models.Activity) error {
	if r.lines.batched() {
		r.lines.writes = append(r.lines.writes, ports.ActivityWrite{Old: copyActivity(old), New: copyActivity(saved)})
		return nil
	}
	return r.backendWrite(ctx, func() error {
		if saved != nil {
			if err := r.cfg.Activities.Save(ctx, *saved); err != nil {
				return errors.Wrapf(err, "write the activity at %s", r.keys.key(saved.StartTime))
			}
		}
		if old != nil && (saved == nil || !old.StartTime.Equal(saved.StartTime)) {
			if err := r.cfg.Activities.Remove(ctx, *old); err != nil {
				return errors.Wrapf(err, "remove the activity at %s", r.keys.key(old.StartTime))
			}
		}
		return nil
	})
}

// afterLines runs a note write once the activity writes queued before it are in the backend.
func (r *run) afterLines(ctx context.Context, write func(ctx context.Context) error) error {
	if r.lines.batched() {
		r.lines.after = append(r.lines.after, write)
		return nil
	}
	return r.backendWrite(ctx, func() error { return write(ctx) })
}

// flushLines applies the queued activity writes, then the note writes that follow them.
func (r *run) flushLines(ctx context.Context) error {
	if writes := r.lines.writes; len(writes) > 0 {
		r.lines.writes = nil
		if err := r.backendWrite(ctx, func() error { return r.lines.batch.Apply(ctx, writes) }); err != nil {
			return errors.Wrap(err, "write the activities into the backend")
		}
	}
	after := r.lines.after
	r.lines.after = nil
	for _, write := range after {
		if err := r.backendWrite(ctx, func() error { return write(ctx) }); err != nil {
			return err
		}
	}
	return nil
}

// wrap wraps err with msg; nil stays nil.
func wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return errors.Wrap(err, msg)
}

func copyActivity(a *models.Activity) *models.Activity {
	if a == nil {
		return nil
	}
	c := *a
	if a.EndTime != nil {
		end := *a.EndTime
		c.EndTime = &end
	}
	return &c
}
