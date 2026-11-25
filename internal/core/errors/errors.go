package errors

import "errors"

var (
	ErrActivityNotFound       = errors.New("activity not found")
	ErrNoActiveActivity       = errors.New("no active activity found")
	ErrActivityAlreadyStarted = errors.New("activity already started")
	ErrCancelled              = errors.New("operation cancelled")
	ErrNotesUnavailable       = errors.New("notes repository is not configured")
	ErrInvalidTimeRange       = errors.New("end time cannot be before start time")
	ErrStartTimeConflict      = errors.New("another activity already starts at this time")
	ErrAmbiguousStartTime     = errors.New("more than one activity starts at this time, so they cannot be told apart")
	ErrProjectRequired        = errors.New("project name is required")
	ErrDescriptionRequired    = errors.New("description is required")
	ErrReminderNotFound       = errors.New("reminder not found")
	ErrReminderTitleRequired  = errors.New("reminder title is required")
	ErrReminderDueRequired    = errors.New("reminder date is required")
	ErrNoReminderOccurrence   = errors.New("the reminder has no such occurrence")
	ErrReminderNeverDue       = errors.New("the reminder never comes due")

	// ErrSyncNotInitialized is returned by a sync before tock sync init set the folder up.
	ErrSyncNotInitialized = errors.New("the sync folder is not set up")
	// ErrSyncFormatUnsupported means format.json names a format or version this tock does not know:
	// sync stops and writes nothing until tock is updated.
	ErrSyncFormatUnsupported = errors.New("the sync folder has a format this version of tock does not know")
	// ErrSyncSegmentShrank means a journal segment is shorter than what was read of it: the journals
	// are read again from the start.
	ErrSyncSegmentShrank = errors.New("a journal segment is shorter than what was read of it")
	// ErrSyncBackendChanged means tock runs on another backend or data path than the one sync was set
	// up with, as with --backend or -f for one command: sync leaves both alone until tock sync init
	// sets the other one up.
	ErrSyncBackendChanged = errors.New("tock runs on another backend than the one sync was set up with")
	// ErrSyncBackendMissing means the backend's data file or directory is gone while sync holds images
	// of it (a file moved or renamed, a volume not mounted): compared, every activity would look
	// deleted, so the sync stops and writes nothing.
	ErrSyncBackendMissing = errors.New("the backend's data is missing")
	// ErrSyncTooManyDeletions means a sync found many records gone from the backend at once: it journals
	// nothing until tock sync init --restore writes the backend again from the sync folder, or a sync
	// runs with deletions allowed (tock sync --allow-deletions).
	ErrSyncTooManyDeletions = errors.New("the sync would delete many records on every device")
	// ErrSyncNothingToRestore means tock sync init --restore found no activity or reminder in the sync
	// folder: letting it win would empty the backend, so it writes nothing.
	ErrSyncNothingToRestore = errors.New("the sync folder holds no records to restore the backend from")
	// ErrSyncRestoreNotSynced means tock sync init --restore runs on a backend this Mac has not synced
	// with that folder yet (a first init, another folder or backend): its records may be in no journal,
	// so it writes nothing.
	ErrSyncRestoreNotSynced = errors.New("this Mac has not synced the backend with the sync folder yet")
	// ErrSyncRestorePending means tock sync init --restore found journal files not downloaded yet: they
	// may hold records it would remove from the backend, so it writes nothing.
	ErrSyncRestorePending = errors.New("the sync folder is still downloading")
	// ErrActivityOwnerUnknown means sync could not tell which device owns the running activity, as when
	// state.db cannot be read: the end of the working day leaves the activity running until it can.
	ErrActivityOwnerUnknown = errors.New("the device that owns the running activity is unknown")
)
