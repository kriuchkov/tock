package ports

import (
	"context"
	"encoding/json"
	"maps"
	"time"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

// The ports of sync (docs/sync.md). SyncResolver is what the delivery layer calls. The bridge
// service reads and writes the sync folder through SyncFolder, keeps its state in SyncState, learns
// what changed in the backend from SyncBackend, and takes the changes made through tock from the
// recorder decorators as a SyncChangeSink.

// SyncResolver is the sync API used by the delivery layer: tock sync, the hooks after commands
// that change data, the menu bar icon and the MCP server.
type SyncResolver interface {
	// Run syncs once: reads new lines from the journals, merges them, applies the rules after the
	// merge, writes this device's changes (recorded through tock, found by comparing the backend, and
	// the stops of its own activities) to its journal, and writes other devices' changes into the
	// backend. It returns coreErrors.ErrSyncNotInitialized before tock sync init.
	Run(ctx context.Context, opts SyncOptions) (SyncReport, error)
	// Init sets a folder up as this Mac's sync folder and runs the first sync, which journals the
	// backend's records the folder does not hold yet. Like Run, it returns
	// coreErrors.ErrSyncTooManyDeletions when the backend has lost many records it held, unless it holds
	// none of them any more; opts.Restore writes the backend again from the folder instead.
	Init(ctx context.Context, opts SyncInitOptions) (SyncReport, error)
	// Status describes the sync folder, this device and the last sync, reading no journal.
	Status(ctx context.Context) (SyncStatus, error)
}

// SyncOptions tunes one Run.
type SyncOptions struct {
	// Full compares the whole backend with the last-sync image whatever its change token says, and
	// checks every note file instead of those of the last days.
	Full bool
	// AllowDeletions lets a Run journal deletions in bulk. Without it, a Run that finds many records gone
	// from the backend at once (more than a few, and more than a fifth or more than 50 of them) journals
	// nothing and returns coreErrors.ErrSyncTooManyDeletions: the data file may have been moved or
	// replaced, and every device would lose them.
	AllowDeletions bool
}

// SyncInitOptions tunes Init.
type SyncInitOptions struct {
	// Dir is the sync folder; empty means the configured one.
	Dir string
	// Restore writes the backend again from the records of the sync folder: the folder wins. It backs
	// up the backend's files it rewrites first (SyncReport.Backups) and journals nothing it finds in
	// the backend, so the changes made there since the last sync stay only in those copies.
	Restore bool
}

// SyncReport is what one Run or Init did.
type SyncReport struct {
	// Device is the ID this device writes under.
	Device string
	// ReplacedDevice is the ID this device wrote under before it found another device writing under
	// it (a cloned device); empty otherwise.
	ReplacedDevice string
	// Read counts the changes read from the journals.
	Read int
	// Skipped counts the invalid lines met.
	Skipped int
	// Written counts the changes this device appended to its journal.
	Written int
	// Applied counts the records written into the backend from other devices' changes.
	Applied int
	// Compared is set when the backend was compared with the last-sync image.
	Compared bool
	// Rebuilt is set when the journals were read again from the start.
	Rebuilt bool
	// Pending counts the files in the folder not downloaded yet: their changes come later.
	Pending int
	// Unreadable lists the segments that could not be read, as journal/<device>/<file>; their
	// changes wait for a later run.
	Unreadable []string
	// Backups lists the copies an Init with Restore made of the backend's files before it wrote them
	// again.
	Backups []string
}

// SyncStatus is what tock sync status shows.
type SyncStatus struct {
	// Dir is the sync folder.
	Dir string
	// Initialized is set once tock sync init set the folder up for this Mac.
	Initialized bool
	// Device is this device's ID; empty before init.
	Device string
	// Backend is the backend the state was built for.
	Backend SyncBackendIdentity
	// LastSync is when the last run finished; zero before the first.
	LastSync time.Time
	// Devices are the devices of devices/*.json, this one included.
	Devices []SyncDevice
	// Pending counts the files in the folder not downloaded yet.
	Pending int
	// Skipped counts the invalid lines skipped so far.
	Skipped int
}

// SyncDevice is devices/<id>.json: who a device is, for the sync status.
type SyncDevice struct {
	// ID is 16 lowercase hex characters.
	ID string
	// Name is what the user calls the device, such as "MacBook".
	Name string
	// Platform is "macos" or "ios".
	Platform string
	// App is the app and its version, such as "tock 2.4.0".
	App string
	// Seen is when the device last synced; zero when unknown.
	Seen time.Time
}

// SyncSegment is one segment file of a device's journal: journal/<device>/<month>.jsonl, or
// <month>.jsonl.gz once the month is closed.
type SyncSegment struct {
	Device string
	// Month is the UTC month of the segment's changes, YYYY-MM.
	Month string
	// Compressed is set for the .jsonl.gz of a closed month.
	Compressed bool
	// Size is the size of the file on disk, the compressed size for a .gz.
	Size int64
}

// SyncCursor is how far a segment has been applied.
type SyncCursor struct {
	Device string
	Month  string
	// Offset counts the bytes of the uncompressed content applied: up to the end of the last
	// complete line read. It carries over from the plain file to the .gz of the same month.
	Offset int64
	// Size and Compressed describe the file read last; a segment whose file is unchanged is skipped.
	Size       int64
	Compressed bool
}

// SyncFolder is the sync folder of docs/sync.md:
//
//	format.json                          {"format": "tock-sync", "version": 1}
//	devices/<device>.json                who the device is
//	journal/<device>/<YYYY-MM>.jsonl     the device's changes this month, append-only
//	journal/<device>/<YYYY-MM>.jsonl.gz  a closed month, compressed
//
// A device writes only devices/<own id>.json, the files under journal/<own id>/ and a missing
// format.json. A file iCloud has not downloaded yet (a .<name>.icloud placeholder) counts as missing:
// listings leave it out, Pending reports it, and a write never replaces it.
type SyncFolder interface {
	// Dir returns the folder's path.
	Dir() string
	// EnsureFormat reads format.json, writing {"format":"tock-sync","version":1} first when it is
	// missing and creating the folder. For another format or version it returns
	// coreErrors.ErrSyncFormatUnsupported and writes nothing; while format.json is not downloaded yet
	// it returns an error and writes nothing.
	EnsureFormat(ctx context.Context) error
	// Segments lists the segments of every device's journal, valid device IDs only, by device and
	// month: one per month, the plain file when both files of a month exist.
	Segments(ctx context.Context) ([]SyncSegment, error)
	// Read returns the uncompressed content of a segment from offset to its end; empty at the end.
	// It returns coreErrors.ErrSyncSegmentShrank when the content is shorter than offset.
	Read(ctx context.Context, segment SyncSegment, offset int64) ([]byte, error)
	// Append appends lines to journal/<device>/<month>.jsonl with one write call, creating the folder
	// and the file when missing, and syncs the file to disk. lines are whole lines: empty, or ending
	// in "\n".
	Append(ctx context.Context, device, month string, lines []byte) error
	// Compress closes a month of a device's journal: writes its content gzipped to a temporary file,
	// checks that it reads back, renames it to <month>.jsonl.gz and removes <month>.jsonl. It does
	// nothing when there is no plain file.
	Compress(ctx context.Context, device, month string) error
	// Devices returns every devices/<id>.json that parses and names its own ID, by ID.
	Devices(ctx context.Context) ([]SyncDevice, error)
	// WriteDevice writes devices/<device.ID>.json atomically, with "v": 1 and seen in UTC.
	WriteDevice(ctx context.Context, device SyncDevice) error
	// Pending returns the files that exist but are not downloaded yet, as paths in the folder
	// (journal/<device>/<file>, devices/<id>.json, format.json), sorted, and asks iCloud for them.
	Pending(ctx context.Context) ([]string, error)
}

// SyncBackendIdentity names the backend a sync state was built for. After a switch the bridge
// matches the new backend's records to the merged state instead of journaling them again.
type SyncBackendIdentity struct {
	// Name is file, todotxt, timewarrior or sqlite.
	Name string
	// Path is the backend's data file or directory.
	Path string
}

// SyncToken tells whether the backend changed: one token per part of it, such as a file's size and
// modification time, a timewarrior month or the sqlite change log.
type SyncToken map[string]string

// Equal reports whether both tokens hold the same parts with the same values.
func (t SyncToken) Equal(other SyncToken) bool {
	return maps.Equal(t, other)
}

// SyncSpan is the times from From up to To, To not included.
type SyncSpan struct {
	From time.Time
	To   time.Time
}

// SyncScope is the part of the backend a compare reads.
type SyncScope struct {
	// AllActivities compares every activity.
	AllActivities bool
	// Activities are spans of start times to compare when AllActivities is not set.
	Activities []SyncSpan
	// Reminders compares the reminders.
	Reminders bool
}

// IsEmpty reports whether the scope holds nothing to compare.
func (s SyncScope) IsEmpty() bool {
	return !s.AllActivities && len(s.Activities) == 0 && !s.Reminders
}

// SyncBackend is what the bridge knows about the backend beyond its repositories: its time
// precision, where uids live and its change token (docs/sync.md, Mac: the CLI's backends).
type SyncBackend interface {
	// Identity names the backend and its data path.
	Identity() SyncBackendIdentity
	// Precision is the step the backend keeps activity times at: a minute for the file backend, a
	// second for the others. Comparisons with the backend use it.
	Precision() time.Duration
	// WallClock reports whether the backend keeps activity times as local wall times with no offset,
	// as tock.txt does. Its keys and images then hold wall times, so a change of the Mac's time zone, or
	// the repeated hour when daylight saving time ends, never looks like an edit of the activities.
	WallClock() bool
	// Normalize returns an activity as the backend gives it back once saved: its times at the backend's
	// precision as it reads them in local time, and its project and description as its format keeps
	// them. Notes, tags and the uid pass through unchanged. Comparing with what the backend can hold
	// keeps a value it cannot hold from being journaled back over the merged one.
	Normalize(activity models.Activity) models.Activity
	// KeepsUIDs reports whether the backend stores activity uids itself (sqlite). Otherwise
	// SyncState maps them by start time.
	KeepsUIDs() bool
	// SidecarNotes reports whether notes and tags live in .tock/notes/, which no change token covers:
	// a sync then checks the note files of recent activities itself, and all of them on a full run.
	SidecarNotes() bool
	// LineTags reports whether an activity as the backend keeps it holds its tags itself (todotxt,
	// timewarrior and sqlite): one without tags there and without a notes file has none. A tock.txt line
	// holds no tags, so for the file backend a missing notes file says nothing about them.
	LineTags() bool
	// Exists reports whether the backend's activity data is there: the data file, the timewarrior data
	// directory, the sqlite database. A sync never compares a backend whose data is gone while it holds
	// images of it (a file moved away, a volume not mounted): every activity would look deleted.
	Exists(ctx context.Context) (bool, error)
	// Token returns the backend's change token now.
	Token(ctx context.Context) (SyncToken, error)
	// Changed returns the part of the backend that may have changed between two tokens. An empty
	// since covers everything.
	Changed(ctx context.Context, since, now SyncToken) (SyncScope, error)
	// Backup copies the files that hold the part of the backend scope covers, before a restore writes
	// them again, next to each with suffix added to its name, and returns the paths of the copies:
	// the data file or the timewarrior month files, the notes and reminders sidecars, the sqlite
	// database. A file that does not exist has no copy, and one that would replace a file is an error.
	Backup(ctx context.Context, scope SyncScope, suffix string) ([]string, error)
}

// SyncRecordRef names one record.
type SyncRecordRef struct {
	Type journal.RecordType
	UID  string
}

// SyncImage is a record as the backend held it at the last sync, as journal fields at the
// backend's precision. Comparing the backend with the images finds the changes made outside tock.
type SyncImage struct {
	Type journal.RecordType
	UID  string
	// Key finds the record in the backend: an activity's start at the backend's precision in RFC 3339
	// UTC, a reminder's local #N in decimal. For backends without uid storage, the activity keys are
	// the uid map.
	Key string
	// Fields maps field paths to values as the journal writes them; deleted is never among them.
	Fields map[string]json.RawMessage
}

// SyncMeta is the small state kept next to the merged state.
type SyncMeta struct {
	// Dir is the sync folder the state was built from; empty before init.
	Dir string
	// Device is this device's ID; empty before one is made.
	Device string
	// Clock is the latest clock this device wrote or read; zero before the first.
	Clock journal.HLC
	// Backend is the backend the images were taken from.
	Backend SyncBackendIdentity
	// MatchBackend makes the next sync match the backend's records to the merged ones, as after a
	// backend switch, instead of comparing them with images: a rebuild of state.db keeps the backend but
	// drops the images and the merged state.
	MatchBackend bool
	// Token is the backend's change token saved after tock's own last write.
	Token SyncToken
	// LastSync is when the last run finished; zero before the first.
	LastSync time.Time
	// Skipped counts the invalid lines skipped so far.
	Skipped int
	// AutoStops are the stops tock wrote into the backend because another activity started
	// (WithAutoStop) since the last sync that went through. They are kept here, not only in the
	// process that wrote them, so a sync that fails or never runs after the command does not lose
	// them: a later sync would take the end it finds for a stop made on purpose.
	AutoStops []SyncAutoStop
}

// SyncAutoStop is an activity as tock saved it when it stopped it because another one started.
type SyncAutoStop struct {
	Start       time.Time
	End         time.Time
	Project     string
	Description string
}

// SyncDelta is what one sync changed in the state, saved in one transaction so the cursors, the
// merged records, the images and the clock never get out of step.
type SyncDelta struct {
	// Reset drops every merged record and cursor before the rest is saved: the journals are read
	// again from the start. Images, reminder numbers and the meta stay.
	Reset bool
	// Meta replaces the saved meta when set.
	Meta *SyncMeta
	// Records replaces these merged records, with all their fields.
	Records []journal.Record
	// Cursors replaces the cursors of these segments.
	Cursors []SyncCursor
	// Images replaces the images of these records.
	Images []SyncImage
	// DropImages removes the images of records the backend no longer holds.
	DropImages []SyncRecordRef
	// ReminderIDs adds or replaces local #N numbers by reminder uid.
	ReminderIDs map[string]int
}

// SyncState is the local sync state outside the sync folder: state.db in ~/.tock/sync/. It holds
// the merged state of the journals (every record, deleted ones included, with every field's value,
// clock and device), the cursors, the last-sync image of the backend, the local reminder numbers,
// and this device's ID and clock.
type SyncState interface {
	// Exclusive runs fn while holding a lock shared by every tock process on this Mac, so only one
	// sync or recorded write runs at a time. It waits for the lock until ctx is done, then returns
	// ctx's error without running fn. fn must not call Exclusive.
	Exclusive(ctx context.Context, fn func(ctx context.Context) error) error
	// Meta returns the saved meta; the zero SyncMeta before the first save.
	Meta(ctx context.Context) (SyncMeta, error)
	// Cursors returns the cursor of every segment applied so far.
	Cursors(ctx context.Context) ([]SyncCursor, error)
	// Records returns the merged records of one type with the given uids, hidden ones included;
	// uids it does not hold are left out.
	Records(ctx context.Context, typ journal.RecordType, uids []string) ([]journal.Record, error)
	// AllRecords returns every merged record of one type, hidden ones included.
	AllRecords(ctx context.Context, typ journal.RecordType) ([]journal.Record, error)
	// RunningActivities returns the activity records that may be running: deleted not true, end
	// missing or null. The caller reads them with journal.Record.Activity to be sure.
	RunningActivities(ctx context.Context) ([]journal.Record, error)
	// ActivitiesStartingIn returns the activity records whose merged start lies in span, hidden ones
	// included.
	ActivitiesStartingIn(ctx context.Context, span SyncSpan) ([]journal.Record, error)
	// Images returns every image of one type.
	Images(ctx context.Context, typ journal.RecordType) ([]SyncImage, error)
	// ImagesByUID returns the images of one type with the given uids.
	ImagesByUID(ctx context.Context, typ journal.RecordType, uids []string) ([]SyncImage, error)
	// ImagesByKey returns the images of one type with the given keys; a key can match several.
	ImagesByKey(ctx context.Context, typ journal.RecordType, keys []string) ([]SyncImage, error)
	// ReminderIDs returns this device's #N of every reminder it numbered, deleted ones included.
	ReminderIDs(ctx context.Context) (map[string]int, error)
	// Save applies a delta in one transaction.
	Save(ctx context.Context, delta SyncDelta) error
}

// SyncWriteKind is what a write made through tock did.
type SyncWriteKind int

const (
	// SyncActivitySaved is an ActivityRepository.Save.
	SyncActivitySaved SyncWriteKind = iota + 1
	// SyncActivityRemoved is an ActivityRepository.Remove.
	SyncActivityRemoved
	// SyncNotesSaved is a NotesRepository.Save.
	SyncNotesSaved
	// SyncNotesDeleted is a NotesRepository.Delete.
	SyncNotesDeleted
	// SyncReminderSaved is a ReminderRepository.Save.
	SyncReminderSaved
	// SyncReminderRemoved is a ReminderRepository.Remove.
	SyncReminderRemoved
)

// SyncWrite is one save or removal made through tock, as the recorder saw it.
type SyncWrite struct {
	Kind SyncWriteKind
	// Activity is the activity saved or removed. Its UID is empty when the caller did not know it:
	// the sink finds it in the backend or in SyncState. For notes, StartTime is the date the notes
	// were saved under (the activity's start) and Notes and Tags are what was saved, empty when
	// deleted.
	Activity models.Activity
	// ActivityID is the notes key the NotesRepository got, for notes writes.
	ActivityID string
	// Reminder is the reminder as saved, with its ID and UID, or as it was before its removal.
	Reminder models.Reminder
}

// SyncChangeSink takes the changes made through tock while sync is on. The recorder decorators of
// the repositories call Record for every save and removal.
type SyncChangeSink interface {
	// Record runs write, which makes one save or removal in the backend and returns what it did,
	// and remembers it, so the next Run of the same service writes it to the journal. It returns
	// write's error and remembers nothing then. Record takes the backend's change token before and
	// after write, so tock's own write does not make the next sync compare the backend, while a change
	// made outside tock in between still does. A stop made under WithAutoStop is written only by the
	// activity's owner. Before tock sync init, while tock runs on another backend than the one sync
	// is set up for, and while a sync holds the state for long, Record only runs write: a later sync
	// finds the change by comparing the backend.
	Record(ctx context.Context, write func(ctx context.Context) (SyncWrite, error)) error
}

// ActivityOwners tells, while sync is on, which activities of the backend another device owns: the
// device whose change holds the activity's start (docs/sync.md, Rules after the merge). Such an
// activity is stopped here only when the user asks; the end of this Mac's working day leaves it to
// its owner, since sync would neither write nor show that end.
type ActivityOwners interface {
	// OwnedElsewhere reports whether another device owns an activity as the backend holds it. An
	// activity sync does not know yet, such as one started here since the last sync, is this device's.
	OwnedElsewhere(ctx context.Context, activity models.Activity) (bool, error)
}

// ActivityWrite is one write of ActivityBatchWriter.Apply.
type ActivityWrite struct {
	// Old is an activity as the repository returned it; nil adds New.
	Old *models.Activity
	// New replaces Old; nil removes Old.
	New *models.Activity
}

// ActivityBatchWriter applies many activity writes in one pass, for the sync bridge, which may write
// thousands at once. Unlike ActivityRepository.Save and Remove, which address an activity by its start
// alone, a write addresses exactly the activity Old describes (start, end, project and description),
// so two activities that start within one step of the backend's precision stay apart. Apply checks
// every Old first and writes nothing when one is not there: the backend changed since it was read.
type ActivityBatchWriter interface {
	Apply(ctx context.Context, writes []ActivityWrite) error
}

// autoStopKey marks a context of WithAutoStop.
type autoStopKey struct{}

// WithAutoStop marks ctx for the saves that stop running activities because another one starts.
// Sync tells such a stop from one the user makes on purpose: the end it gives is shown on every
// device but written to the journal only by the activity's owner, the device whose change holds
// the winning value of its start (docs/sync.md, Rules after the merge).
func WithAutoStop(ctx context.Context) context.Context {
	return context.WithValue(ctx, autoStopKey{}, true)
}

// IsAutoStop reports whether ctx carries the mark of WithAutoStop.
func IsAutoStop(ctx context.Context) bool {
	marked, _ := ctx.Value(autoStopKey{}).(bool)
	return marked
}

// movedToKey marks a context of WithMovedTo.
type movedToKey struct{}

// WithMovedTo marks ctx for the removal that ends an update which moved an activity's start: the
// activity was saved at start just before, so sync takes the save and the removal for one record that
// moved. Any other removal is a deletion, even one that follows a save in the same process.
func WithMovedTo(ctx context.Context, start time.Time) context.Context {
	return context.WithValue(ctx, movedToKey{}, start)
}

// MovedTo returns the start of the mark of WithMovedTo, and whether ctx carries one.
func MovedTo(ctx context.Context) (time.Time, bool) {
	start, marked := ctx.Value(movedToKey{}).(time.Time)
	return start, marked
}
