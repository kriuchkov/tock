// Package sync is the bridge between the CLI's backend and the sync folder of docs/sync.md. It keeps
// the backend (file, todotxt, timewarrior or sqlite) as the place you read and edit, and works only
// through ports: the backend's repositories, ports.SyncBackend for its precision and change token,
// ports.SyncFolder for the journals and ports.SyncState for the merged state, the cursors and the
// last-sync image of the backend.
//
// A Run, under ports.SyncState.Exclusive:
//
//  1. checks format.json and loads the meta; before tock sync init it stops with
//     coreErrors.ErrSyncNotInitialized, and while tock runs on another backend or data path than the
//     one sync was set up with, with coreErrors.ErrSyncBackendChanged: only Init switches backends;
//  2. reads every segment from its cursor to its last complete line (journal.ReadSegment), skipping
//     segments whose file is unchanged, observes the clocks, and merges the new changes into the
//     records they touch; a segment that shrank or went away makes it read everything again, and a
//     change in its own journal later than its stored clock makes it take a new device ID;
//  3. applies the rules after the merge: every activity shows its shown end (end, else an auto_end
//     not before its start), and rule 1 stops all running activities but the last, and an activity
//     stopped by its auto_end at an earlier start of a running one; in a run that read every segment
//     this device writes each such stop, as the end of an activity whose winning start it wrote and
//     as the auto_end of anyone else's, and before that only shows it (written into the backend, not
//     journaled);
//  4. writes the recorded changes made through tock, and those found by comparing the backend with
//     the last-sync image over the scope ports.SyncBackend.Changed gives (everything when Full),
//     plus the note files of the last 7 days (all of them when Full) for sidecar notes; an end tock
//     wrote for an activity another device owns because a start stopped it is journaled as rule 1
//     says for a start, the end of the working day not at all, an end found that is the end rule 1
//     gives as its auto_end, any other as its end, and an end cleared as end and auto_end cleared
//     together;
//  5. appends this device's changes to its journal, one write per month, with clocks from its HLC;
//  6. writes into the backend, at its precision and through the repositories that do not record, in one
//     batch where the repository applies batches (lines.go), every record whose merged value differs from
//     what the backend gives back for it (ports.SyncBackend.Normalize), giving a reminder from another device
//     the next free #N (rule 2); where two activities fall on one key, the one empty at the backend's
//     precision gives way (slot.go), and the other one waits for the key, which the run that frees it,
//     a run that reads the waiting one, or at the latest a full run, holding every record, gives it;
//  7. saves the new image, the token, the cursors, the records and the clock in one ports.SyncDelta,
//     compresses its own closed months and writes devices/<id>.json with seen.
//
// Changes made through tock are not taken from the writes themselves: a recorded write only names
// the records to compare, so changes made through tock and outside it are found the same way, at the
// backend's precision. The values found are journaled only where they differ from the merged state,
// so a run interrupted after writing into the backend finds nothing new the next time.
package sync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	stdsync "sync"
	"time"

	"github.com/go-faster/errors"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	// recentNotes is how far back a sync checks sidecar note files, which no change token covers.
	recentNotes = 7 * 24 * time.Hour
	// deviceSeenEvery is how often devices/<id>.json is written again only to move seen on.
	deviceSeenEvery = time.Hour
	// deviceIDBytes makes a device ID of 16 hex characters.
	deviceIDBytes = 8
)

// Config is what the service works with.
type Config struct {
	// Dir is the configured sync folder (sync.dir, ~ expanded).
	Dir string
	// OpenFolder returns the sync folder at a path; Init may set up another one than Dir.
	OpenFolder func(dir string) ports.SyncFolder
	// State is state.db.
	State ports.SyncState
	// Backend describes the backend the repositories belong to.
	Backend ports.SyncBackend
	// Activities, Notes and Reminders are the backend's repositories, not wrapped by the recorder:
	// what the bridge writes into the backend are other devices' changes, not this device's.
	Activities ports.ActivityRepository
	Notes      ports.NotesRepository
	Reminders  ports.ReminderRepository
	// Profile describes this device in devices/<id>.json; the service fills in ID and Seen.
	Profile ports.SyncDevice
	// Now returns the current time; time.Now when nil.
	Now func() time.Time
	// RecordWait is how long Record waits for a sync that holds the state's lock before it writes
	// without recording; 10 seconds when zero.
	RecordWait time.Duration
}

// Service syncs the backend with the sync folder. It implements ports.SyncResolver for the delivery
// layer and ports.SyncChangeSink for the recorder decorators: writes recorded in a process reach the
// journal at that process's next Run.
type Service struct {
	cfg Config

	mu       stdsync.Mutex
	recorded recordings
}

var (
	_ ports.SyncResolver   = (*Service)(nil)
	_ ports.SyncChangeSink = (*Service)(nil)
)

// NewService returns the sync service. It touches nothing on disk until a method is called.
func NewService(cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.RecordWait <= 0 {
		cfg.RecordWait = recordWait
	}
	return &Service{cfg: cfg}
}

// Run syncs once; see the package documentation for the steps.
func (s *Service) Run(ctx context.Context, opts ports.SyncOptions) (ports.SyncReport, error) {
	var report ports.SyncReport
	err := s.cfg.State.Exclusive(ctx, func(ctx context.Context) error {
		meta, err := s.cfg.State.Meta(ctx)
		if err != nil {
			return errors.Wrap(err, "read the sync state")
		}
		if meta.Dir == "" {
			return coreErrors.ErrSyncNotInitialized
		}
		if s.otherBackend(meta) {
			return errors.Wrapf(coreErrors.ErrSyncBackendChanged, "%s at %s", meta.Backend.Name, meta.Backend.Path)
		}
		report, err = s.sync(ctx, meta, runOptions{dir: meta.Dir, full: opts.Full, allowDeletions: opts.AllowDeletions})
		return err
	})
	return report, err
}

// otherBackend reports whether tock runs on another backend or data path than the one the meta names,
// as with --backend or -f for one command. A meta that names none, as after state.db was rebuilt,
// takes the backend tock runs on.
func (s *Service) otherBackend(meta ports.SyncMeta) bool {
	return meta.Backend.Name != "" && meta.Backend != s.cfg.Backend.Identity()
}

// Init sets initOpts.Dir up as this Mac's sync folder: checks or writes format.json, makes the device ID
// when there is none, records the folder and the backend in the meta, and runs the first sync. That
// sync matches the backend's records to the merged records by start, project and description (as after
// a backend switch), journals the ones the folder does not hold, and writes the others' records into
// the backend. An empty Dir means Config.Dir.
//
// Init again on the same folder and backend is a full Run. Init on another folder drops the merged
// state of the old one and starts over from the new one. Init on another backend or data path is the
// only way to switch sync to it: the run matches it as on a new folder. So does Init on a backend whose
// data is gone, which a Run refuses to compare. When the deletion brake stops the run for a type of
// records none of which in the backend pairs with an image, Init matches that type's records and
// compares the other type's with their images as usual: its data was started over, emptied or created
// again, so what remains of it is copied next to it before it is written (run.backUp), as a notes file
// edited by hand since the last sync. Otherwise the brake stops Init as it stops Run, writing
// nothing: the backend may hold edits the merged state does not (an older copy, a hand edit), which no
// run can tell from stale values. initOpts.Restore then writes the backend again from the merged state:
// it matches it as on a new folder, journals nothing it finds there, removes the records no merged
// record holds, and backs up the files it rewrites first. It refuses, writing nothing, a backend this
// Mac has not synced with the folder yet (coreErrors.ErrSyncRestoreNotSynced: its records may be in no
// journal), a folder still downloading journals (coreErrors.ErrSyncRestorePending), and a folder that
// holds no activity or reminder of any device, this one's included (coreErrors.ErrSyncNothingToRestore):
// the backend would be emptied.
func (s *Service) Init(ctx context.Context, initOpts ports.SyncInitOptions) (ports.SyncReport, error) {
	dir := initOpts.Dir
	if dir == "" {
		dir = s.cfg.Dir
	}
	if dir == "" {
		return ports.SyncReport{}, errors.New("no sync folder is configured")
	}

	var report ports.SyncReport
	err := s.cfg.State.Exclusive(ctx, func(ctx context.Context) error {
		meta, err := s.cfg.State.Meta(ctx)
		if err != nil {
			return errors.Wrap(err, "read the sync state")
		}
		if initOpts.Restore {
			if err = s.checkRestore(ctx, meta, dir); err != nil {
				return err
			}
		}
		opts := runOptions{dir: dir, full: true, restore: initOpts.Restore, match: initOpts.Restore}
		if meta.Dir != dir {
			opts.reset, opts.match = true, true
		}
		if !opts.match {
			exists, existsErr := s.cfg.Backend.Exists(ctx)
			if existsErr != nil {
				return errors.Wrap(existsErr, "look for the backend's data")
			}
			opts.match = !exists
		}
		report, err = s.sync(ctx, meta, opts)
		var brake *brakeError
		if errors.As(err, &brake) && len(brake.match) > 0 && !opts.match {
			// The brake stops a run before it writes anything.
			opts.matchTypes = brake.match
			report, err = s.sync(ctx, meta, opts)
		}
		return err
	})
	return report, err
}

// checkRestore refuses tock sync init --restore, before anything is written, unless this Mac synced
// the backend with dir already, so that its records are in the journal the restore takes them from,
// and dir holds a journal: the run would write format.json into a new folder. The run checks for
// records again once it read them.
func (s *Service) checkRestore(ctx context.Context, meta ports.SyncMeta, dir string) error {
	if meta.Dir != dir || meta.Backend != s.cfg.Backend.Identity() {
		return coreErrors.ErrSyncRestoreNotSynced
	}
	synced := false
	for _, typ := range []journal.RecordType{journal.TypeActivity, journal.TypeReminder} {
		images, err := s.cfg.State.Images(ctx, typ)
		if err != nil {
			return errors.Wrap(err, "read the images")
		}
		synced = synced || len(images) > 0
	}
	if !synced {
		return coreErrors.ErrSyncRestoreNotSynced
	}
	segments, err := s.cfg.OpenFolder(dir).Segments(ctx)
	if err != nil {
		return errors.Wrap(err, "list the journal segments")
	}
	if len(segments) == 0 {
		return coreErrors.ErrSyncNothingToRestore
	}
	return nil
}

// Status reads the meta, the devices of devices/*.json and the pending downloads; no journal.
func (s *Service) Status(ctx context.Context) (ports.SyncStatus, error) {
	meta, err := s.cfg.State.Meta(ctx)
	if err != nil {
		return ports.SyncStatus{}, errors.Wrap(err, "read the sync state")
	}
	status := ports.SyncStatus{
		Dir:         s.cfg.Dir,
		Initialized: meta.Dir != "",
		Device:      meta.Device,
		Backend:     meta.Backend,
		LastSync:    meta.LastSync,
		Skipped:     meta.Skipped,
	}
	if !status.Initialized {
		return status, nil
	}

	status.Dir = meta.Dir
	folder := s.cfg.OpenFolder(meta.Dir)
	if status.Devices, err = folder.Devices(ctx); err != nil {
		return status, errors.Wrap(err, "read the devices of the sync folder")
	}
	pending, err := folder.Pending(ctx)
	if err != nil {
		return status, errors.Wrap(err, "list the files not downloaded yet")
	}
	status.Pending = len(pending)
	return status, nil
}

// runOptions tunes one sync.
type runOptions struct {
	// dir is the sync folder.
	dir string
	// full compares the whole backend and every note file.
	full bool
	// reset drops the merged state and the cursors: the folder is read from the start.
	reset bool
	// match matches the backend's records to the merged ones instead of to the images, as after a
	// backend switch. matchTypes does so for the records of some types only, and compares the others
	// with their images as usual: Init after the deletion brake (see brakeError).
	match      bool
	matchTypes map[journal.RecordType]bool
	// allowDeletions journals deletions however many the compare finds; see checkDeletions.
	allowDeletions bool
	// restore, with match, writes the backend again from the merged state: what the match leaves over is
	// removed from the backend instead of journaled, and the files the write-back rewrites are backed up
	// first. See Service.Init.
	restore bool
}

// sync runs the steps of the package documentation under the lock.
func (s *Service) sync(ctx context.Context, meta ports.SyncMeta, opts runOptions) (ports.SyncReport, error) {
	if meta.Backend != s.cfg.Backend.Identity() || meta.MatchBackend {
		opts.match, opts.full = true, true
	}
	if meta.Device == "" {
		device, err := newDeviceID()
		if err != nil {
			return ports.SyncReport{}, err
		}
		meta.Device = device
	}

	folder := s.cfg.OpenFolder(opts.dir)
	if err := folder.EnsureFormat(ctx); err != nil {
		return ports.SyncReport{}, errors.Wrap(err, "check the sync folder")
	}

	taken := s.takeRecorded()
	r := newRun(s, meta, folder, opts, taken)
	if err := r.sync(ctx); err != nil {
		return r.report, err
	}
	s.dropRecorded(taken)
	return r.report, nil
}

// newDeviceID makes a random device ID: 16 lowercase hex characters.
func newDeviceID() (string, error) {
	buf := make([]byte, deviceIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.Wrap(err, "make a device ID")
	}
	return hex.EncodeToString(buf), nil
}
