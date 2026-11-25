package sync

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-faster/errors"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// run is one sync: what it read, what it found and what it writes.
type run struct {
	s       *Service
	cfg     Config
	opts    runOptions
	meta    ports.SyncMeta
	folder  ports.SyncFolder
	backend ports.SyncBackend
	// precision is the backend's time precision; keys and comparisons use it.
	precision time.Duration
	// keys makes the activities' keys; wall is set for a wall-clock backend.
	keys    keying
	wall    bool
	sidecar bool
	// lineTags is set for a backend whose lines hold the activities' tags; see
	// ports.SyncBackend.LineTags.
	lineTags  bool
	keepsUIDs bool
	now       time.Time
	taken     recordings

	device string
	clock  *journal.Clock
	store  *store
	report ports.SyncReport

	reading reading
	// complete is set when every segment was read and none waits for a download: only then does this
	// device write the stops of rule 1, which a change not read yet could contradict.
	complete bool
	// token is the backend's change token when the run began.
	token ports.SyncToken
	// lastToken is the token after the run's last write into the backend, token before the first;
	// wrote is set once it wrote. outside is set when the token moved between two of its writes, or
	// could not be read: something outside tock changed the backend while the run went on.
	lastToken ports.SyncToken
	wrote     bool
	outside   bool
	// lines queues the activity writes into the backend; see lines.go.
	lines lines
	// held indexes the merged activities the run holds by key, for adopt, and heldWritten by the key a
	// wall-clock backend gives them in the zone they were journaled in; heldKeys are the keys whose
	// activities it loaded from state.db.
	held        map[string][]string
	heldWritten map[string][]string
	heldKeys    map[string]bool

	images *images
	index  *backendIndex
	// stops are the ends rule 1 gives the running activities in the merged state at the end.
	stops map[string]json.RawMessage
	// own are this device's changes, in clock order.
	own []journal.Change
	// force names the records written into the backend even when their image matches, to store a uid.
	force map[ref]bool
	// extra holds the activities the write-back removes from the backend without journaling: the copies
	// a move across month files left at the old start (findLeftBehind), and on a restore those no
	// merged record holds. extraReminders are the numbers of the reminders a restore removes so.
	extra          []models.Activity
	extraReminders []int
	// paired counts, by type, the backend's records the compare paired with an image: none, of a type
	// the backend held records of, means its data was started over or created again.
	paired map[journal.RecordType]int
}

// matches reports whether the run matches the backend's records of a type to the merged ones instead
// of comparing them with their images.
func (r *run) matches(typ journal.RecordType) bool {
	return r.opts.match || r.opts.matchTypes[typ]
}

func newRun(s *Service, meta ports.SyncMeta, folder ports.SyncFolder, opts runOptions, taken recordings) *run {
	backend := s.cfg.Backend
	now := s.cfg.Now()
	keys := keying{precision: max(backend.Precision(), time.Second), wall: backend.WallClock()}
	return &run{
		s:         s,
		cfg:       s.cfg,
		opts:      opts,
		meta:      meta,
		folder:    folder,
		backend:   backend,
		precision: keys.precision,
		keys:      keys,
		wall:      keys.wall,
		sidecar:   backend.SidecarNotes() && s.cfg.Notes != nil,
		lineTags:  backend.LineTags(),
		keepsUIDs: backend.KeepsUIDs(),
		now:       now,
		taken:     taken,
		device:    meta.Device,
		clock:     journal.NewClock(meta.Clock),
		store:     newStore(s.cfg.State),
		images:    newImages(s.cfg.State),
		index:     newBackendIndex(keys),
		force:     make(map[ref]bool),
		paired:    make(map[journal.RecordType]int),
		lines:     newLines(s.cfg.Activities),
	}
}

// sync runs the steps after the format check.
func (r *run) sync(ctx context.Context) error {
	if err := r.readJournals(ctx); err != nil {
		return err
	}
	if r.opts.restore && r.report.Pending > 0 {
		// A journal not downloaded yet may hold records the restore would remove from the backend.
		return coreErrors.ErrSyncRestorePending
	}
	if r.opts.restore && !r.store.holdsRecords() {
		// A folder of tombstones only would empty the backend too.
		return coreErrors.ErrSyncNothingToRestore
	}
	found, scope, err := r.compare(ctx)
	if err != nil {
		return err
	}
	if err = r.ownChanges(ctx, found, scope); err != nil {
		return err
	}
	if err = r.backUp(ctx); err != nil {
		return err
	}
	if err = r.appendOwn(ctx); err != nil {
		return err
	}
	if err = r.writeBack(ctx); err != nil {
		return err
	}
	r.compressClosedMonths(ctx)
	if err = r.save(ctx); err != nil {
		return err
	}
	return r.writeDevice(ctx)
}

// readJournals reads the new lines of the journals and merges them into the store.
func (r *run) readJournals(ctx context.Context) error {
	pending, err := r.folder.Pending(ctx)
	if err != nil {
		return errors.Wrap(err, "list the files not downloaded yet")
	}
	r.report.Pending = len(pending)
	waiting := pendingSegments(pending)

	var cursors []ports.SyncCursor
	if !r.opts.reset {
		if cursors, err = r.cfg.State.Cursors(ctx); err != nil {
			return errors.Wrap(err, "read the cursors")
		}
	}
	reader := &journalReader{folder: r.folder, device: r.device, saved: r.meta.Clock, clock: r.clock, now: r.now, waiting: waiting}
	if r.reading, err = reader.read(ctx, cursors); err != nil {
		return err
	}
	r.opts.reset = r.opts.reset || r.reading.rebuilt
	r.complete = len(waiting) == 0 && len(r.reading.unreadable) == 0
	r.report.Read = len(r.reading.changes)
	r.report.Skipped = r.reading.skipped
	r.report.Unreadable = r.reading.unreadable
	r.report.Rebuilt = r.reading.rebuilt

	if r.reading.cloned {
		device, idErr := newDeviceID()
		if idErr != nil {
			return idErr
		}
		r.report.ReplacedDevice, r.device = r.device, device
	}
	r.report.Device = r.device
	return r.merge(ctx)
}

// merge loads what the new changes touch and applies them. A rebuild journals again what it lost of
// this device's changes; see restoreOwn.
func (r *run) merge(ctx context.Context) error {
	var previous []journal.Record
	switch {
	case r.opts.reset:
		if r.reading.rebuilt && !r.reading.cloned {
			var err error
			if previous, err = r.previousRecords(ctx); err != nil {
				return err
			}
		}
		r.store.complete = true
	case r.opts.match || r.opts.full:
		// A full run holds every record, so one that waited for its key in the backend gets written once
		// the key is free.
		if err := r.store.loadAll(ctx); err != nil {
			return err
		}
	default:
		refs := make([]ref, 0, len(r.reading.changes))
		for _, change := range r.reading.changes {
			refs = append(refs, ref{typ: change.Type, uid: change.UID})
		}
		if err := r.store.load(ctx, refs); err != nil {
			return err
		}
		if err := r.store.loadRunning(ctx); err != nil {
			return err
		}
	}
	for _, change := range r.reading.changes {
		r.store.apply(change)
	}
	r.restoreOwn(previous)
	return r.store.loadAutoEnded(ctx)
}

// previousRecords returns the merged records state.db holds before a rebuild replaces them.
func (r *run) previousRecords(ctx context.Context) ([]journal.Record, error) {
	var out []journal.Record
	for _, typ := range []journal.RecordType{journal.TypeActivity, journal.TypeReminder} {
		records, err := r.cfg.State.AllRecords(ctx, typ)
		if err != nil {
			return nil, errors.Wrapf(err, "load the %s records before the rebuild", typ)
		}
		for _, record := range records {
			record.Type = typ
			out = append(out, record)
		}
	}
	return out, nil
}

// restoreOwn journals again, with a new clock, the changes of this device a rebuild lost: every field
// of a record the rebuilt state holds whose value before the rebuild was this device's, and which the
// rebuilt state holds older or not at all. A segment shrinks when an older copy of it wins (an iCloud
// conflict, a restored backup): its records survive, but this device's later changes, such as a stop,
// are gone, and every device, this one's backend included, would get the older values back. A record
// the rebuilt state does not hold at all is written whole by the compare. The changes other devices
// lost are theirs to restore.
func (r *run) restoreOwn(previous []journal.Record) {
	for _, old := range previous {
		rebuilt, ok := r.store.get(ref{typ: old.Type, uid: old.UID})
		if !ok {
			continue
		}
		set := fields{}
		for path, field := range old.Fields {
			if field.Dev != r.device {
				continue
			}
			if now, held := rebuilt.Fields[path]; !held || compareChanges(now.HLC, now.Dev, field.HLC, field.Dev) < 0 {
				set[path] = field.Value
			}
		}
		r.addOwn(old.Type, old.UID, set)
	}
}

// owned reports whether this device owns a record: whether its change holds the merged start.
func (r *run) owned(record journal.Record) bool {
	return record.Owner() == r.device
}

// addOwn stamps a change of this device with the next clock, merges it and keeps it for the journal.
func (r *run) addOwn(typ journal.RecordType, uid string, set fields) {
	if len(set) == 0 {
		return
	}
	change := journal.Change{HLC: r.clock.Next(r.now), Dev: r.device, Type: typ, UID: uid, Set: set}
	r.store.apply(change)
	r.own = append(r.own, change)
}

// backendWrite runs one write into the backend between two readings of its token: the one before
// must be the token after the run's write before, or the one the run began with, else something
// outside tock changed the backend in between, and the token after must not hide that change.
func (r *run) backendWrite(ctx context.Context, write func() error) error {
	if !r.wrote {
		r.wrote, r.lastToken = true, r.token
	}
	if before, err := r.backend.Token(ctx); err != nil || !before.Equal(r.lastToken) {
		r.outside = true
	}
	writeErr := write()
	after, err := r.backend.Token(ctx)
	if err != nil {
		r.outside = true
	}
	r.lastToken = after
	return writeErr
}

// tokenToSave is the token the next run compares with: the one after this run's writes when nothing
// else changed the backend since the run began, else the one it began with.
func (r *run) tokenToSave(ctx context.Context) ports.SyncToken {
	if !r.wrote || r.outside || r.lastToken == nil {
		return r.token
	}
	if now, err := r.backend.Token(ctx); err != nil || !now.Equal(r.lastToken) {
		return r.token
	}
	return r.lastToken
}

// save stores what the run changed in one transaction.
func (r *run) save(ctx context.Context) error {
	meta := r.meta
	meta.Dir = r.opts.dir
	meta.Device = r.device
	meta.Clock = r.clock.Last()
	meta.Backend, meta.MatchBackend = r.backend.Identity(), false
	meta.Token = r.tokenToSave(ctx)
	meta.LastSync = r.now
	// The run compared every auto-stop mark it read; Record adds none while it holds the lock.
	meta.AutoStops = nil
	if r.opts.reset {
		meta.Skipped = 0
	}
	meta.Skipped += r.reading.skipped

	cursorKeys := r.reading.updated
	if r.opts.reset {
		cursorKeys = make(map[segmentKey]bool, len(r.reading.cursors))
		for key := range r.reading.cursors {
			cursorKeys[key] = true
		}
	}

	delta := ports.SyncDelta{
		Reset:       r.opts.reset,
		Meta:        &meta,
		Records:     r.store.changed(r.opts.reset),
		Cursors:     sortedCursors(r.reading.cursors, cursorKeys),
		Images:      r.images.changed(),
		DropImages:  r.images.droppedRefs(),
		ReminderIDs: r.images.changedReminderIDs(),
	}
	if err := r.cfg.State.Save(ctx, delta); err != nil {
		return errors.Wrap(err, "save the sync state")
	}
	return nil
}
