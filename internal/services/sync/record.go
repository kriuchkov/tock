package sync

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// recorded is one write made through tock in this process, waiting for the next Run.
type recorded struct {
	write ports.SyncWrite
	// stop is why tock saved the activity: on its own (ports.WithAutoStop) or not.
	stop stopCause
	// movedTo is the start an update moved the activity removed to, from the mark of ports.WithMovedTo;
	// nil for any other write.
	movedTo *time.Time
}

// recordings holds the writes made through tock since the last Run that wrote them, and the token
// to save with them.
type recordings struct {
	writes []recorded
	// token is the backend's change token after the last write, valid as the token to save while
	// every write so far started from the saved token or from the token after the write before it.
	token    ports.SyncToken
	hasToken bool
	// outside is set once a write started from another token: something outside tock changed the
	// backend too, and the next Run compares what changed since the saved token.
	outside bool
}

// recordWait is how long Record waits by default for a sync that holds the state's lock before it
// writes without recording; a later sync finds the change by comparing the backend. A stop tock makes
// on its own waits as long as it takes: only Record keeps the mark that tells it from a stop made on
// purpose.
const recordWait = 10 * time.Second

// maxAutoStops is how many auto-stop marks the meta keeps while no sync goes through.
const maxAutoStops = 256

// stopCause is why tock wrote an activity's end: on purpose, or on its own (ports.WithAutoStop)
// because the activity of next started, or at the end of the working day when next is zero.
type stopCause struct {
	auto bool
	next time.Time
}

// workday reports whether the end is that of the working day.
func (c stopCause) workday() bool {
	return c.auto && c.next.IsZero()
}

// Record runs write under the state's lock and remembers the change it made for the next Run, with
// the auto-stop mark of ctx; see ports.SyncChangeSink. It keeps the token taken after write in
// memory with the change, as the token to save, only when the token taken before write equals the
// saved one: otherwise something outside tock changed the backend too, and the next Run must
// compare. Nothing of the change is saved before that Run writes it to the journal, so a process
// that exits first leaves the change to the next compare, never loses it. An auto-stop is also kept
// in the meta (ports.SyncMeta.AutoStops), so a Run in another process, or after this one's failed,
// still knows it. Before tock sync init, and while tock runs on another backend than the one sync was
// set up with, it only runs write.
func (s *Service) Record(ctx context.Context, write func(ctx context.Context) (ports.SyncWrite, error)) error {
	meta, err := s.cfg.State.Meta(ctx)
	if err != nil || meta.Dir == "" || s.otherBackend(meta) {
		// Before init, with a state that cannot be read, or on another backend, the write goes on
		// unrecorded: a later Run on this backend compares it and finds it.
		_, writeErr := write(ctx)
		return writeErr
	}

	next, autoStop := ports.AutoStopNext(ctx)
	lockCtx, cancel := ctx, context.CancelFunc(func() {})
	if !autoStop {
		lockCtx, cancel = context.WithTimeout(ctx, s.cfg.RecordWait)
	}
	defer cancel()

	ran := false
	err = s.cfg.State.Exclusive(lockCtx, func(context.Context) error {
		ran = true
		return s.record(ctx, write, stopCause{auto: autoStop, next: next})
	})
	if !ran && ctx.Err() == nil && lockCtx.Err() != nil {
		_, writeErr := write(ctx)
		return writeErr
	}
	return err
}

// record runs write and remembers it, under the state's lock.
func (s *Service) record(ctx context.Context, write func(ctx context.Context) (ports.SyncWrite, error), stop stopCause) error {
	saved, metaErr := s.cfg.State.Meta(ctx)
	before, beforeErr := s.cfg.Backend.Token(ctx)
	done, writeErr := write(ctx)
	if writeErr != nil {
		return writeErr
	}
	after, afterErr := s.cfg.Backend.Token(ctx)
	if stop.auto && metaErr == nil && done.Kind == ports.SyncActivitySaved && done.Activity.EndTime != nil {
		// A mark that cannot be saved still lives in this process until its next Run.
		_ = s.keepAutoStop(ctx, saved, done.Activity, stop.next)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	rec := &s.recorded
	made := recorded{write: done, stop: stop}
	if to, moved := ports.MovedTo(ctx); moved && done.Kind == ports.SyncActivityRemoved {
		made.movedTo = &to
	}
	rec.writes = append(rec.writes, made)

	base := saved.Token
	if rec.hasToken {
		base = rec.token
	}
	if metaErr != nil || beforeErr != nil || afterErr != nil || rec.outside || !before.Equal(base) {
		rec.outside = true
		rec.token, rec.hasToken = nil, false
		return nil
	}
	rec.token, rec.hasToken = after, true
	return nil
}

// keepAutoStop adds the mark of an activity tock stopped on its own to the meta: because the activity
// of next started, or at the end of the working day when next is zero.
func (s *Service) keepAutoStop(ctx context.Context, meta ports.SyncMeta, stopped models.Activity, next time.Time) error {
	meta.AutoStops = append(slices.Clone(meta.AutoStops), ports.SyncAutoStop{
		Start:       stopped.StartTime,
		End:         *stopped.EndTime,
		Project:     stopped.Project,
		Description: stopped.Description,
		Next:        next,
	})
	if len(meta.AutoStops) > maxAutoStops {
		meta.AutoStops = meta.AutoStops[len(meta.AutoStops)-maxAutoStops:]
	}
	return s.cfg.State.Save(ctx, ports.SyncDelta{Meta: &meta})
}

// takeRecorded returns the writes recorded so far; dropRecorded forgets them once a Run wrote them.
func (s *Service) takeRecorded() recordings {
	s.mu.Lock()
	defer s.mu.Unlock()
	taken := s.recorded
	taken.writes = append([]recorded(nil), s.recorded.writes...)
	return taken
}

// dropRecorded forgets the writes a Run took, keeping any recorded after.
func (s *Service) dropRecorded(taken recordings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorded.writes = s.recorded.writes[min(len(taken.writes), len(s.recorded.writes)):]
	if len(s.recorded.writes) == 0 {
		s.recorded = recordings{}
	}
}

// recordedScope is what the recorded writes ask a Run to compare.
type recordedScope struct {
	// keys are the activity keys written.
	keys map[string]bool
	// notes are the activity keys whose notes were written.
	notes map[string]bool
	// stops holds why the last recorded save of each activity key wrote its end (stopCause); a key
	// without a recorded save is not there.
	stops map[string]stopCause
	// moves maps the key an activity was removed from to the key an update that moved its start saved
	// it at just before (ports.WithMovedTo).
	moves map[string]string
	// removed holds the keys of the activities removed through tock, moves left out, and
	// removedReminders the #N of the reminders removed: deletions the user asked for, which the
	// deletion brake does not count.
	removed          map[string]bool
	removedReminders map[string]bool
	// reminders is set when a reminder was written.
	reminders bool
}

// scopeOf returns what the writes ask to compare, with the activities' keys.
func scopeOf(writes []recorded, keys keying) recordedScope {
	scope := recordedScope{
		keys:             make(map[string]bool),
		notes:            make(map[string]bool),
		stops:            make(map[string]stopCause),
		moves:            make(map[string]string),
		removed:          make(map[string]bool),
		removedReminders: make(map[string]bool),
	}
	for _, w := range writes {
		key := keys.key(w.write.Activity.StartTime)
		switch w.write.Kind {
		case ports.SyncActivitySaved:
			scope.keys[key] = true
			scope.stops[key] = w.stop
		case ports.SyncActivityRemoved:
			scope.keys[key] = true
			if w.movedTo != nil && keys.key(*w.movedTo) != key {
				scope.moves[key] = keys.key(*w.movedTo)
			} else {
				scope.removed[key] = true
			}
		case ports.SyncNotesSaved, ports.SyncNotesDeleted:
			scope.keys[key] = true
			scope.notes[key] = true
		case ports.SyncReminderRemoved:
			scope.reminders = true
			scope.removedReminders[strconv.Itoa(w.write.Reminder.ID)] = true
		case ports.SyncReminderSaved:
			scope.reminders = true
		}
	}
	return scope
}
