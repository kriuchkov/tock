package sync_test

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	stdsync "sync"
	"time"

	"github.com/go-faster/errors"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// memFolder is a sync folder in memory that several simulated devices share.
type memFolder struct {
	mu      stdsync.Mutex
	dir     string
	format  []byte
	plain   map[[2]string][]byte
	gz      map[[2]string][]byte
	devices map[string]ports.SyncDevice
	pending []string
	// broken makes Read of these segments (device/month) fail.
	broken map[string]bool

	appends      int
	compressions int
	deviceWrites int
}

func newMemFolder() *memFolder {
	return &memFolder{
		dir:     "/sync",
		plain:   make(map[[2]string][]byte),
		gz:      make(map[[2]string][]byte),
		devices: make(map[string]ports.SyncDevice),
		broken:  make(map[string]bool),
	}
}

func (f *memFolder) open(string) ports.SyncFolder { return f }

func (f *memFolder) Dir() string { return f.dir }

func (f *memFolder) EnsureFormat(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.format == nil {
		f.format = []byte(`{"format":"tock-sync","version":1}`)
		return nil
	}
	var format struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(f.format, &format); err != nil || format.Format != "tock-sync" || format.Version != 1 {
		return coreErrors.ErrSyncFormatUnsupported
	}
	return nil
}

func (f *memFolder) Segments(context.Context) ([]ports.SyncSegment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ports.SyncSegment
	for key, data := range f.plain {
		out = append(out, ports.SyncSegment{Device: key[0], Month: key[1], Size: int64(len(data))})
	}
	for key, data := range f.gz {
		if _, ok := f.plain[key]; !ok {
			out = append(out, ports.SyncSegment{Device: key[0], Month: key[1], Compressed: true, Size: int64(len(data))})
		}
	}
	slices.SortFunc(out, func(a, b ports.SyncSegment) int {
		return cmp.Or(cmp.Compare(a.Device, b.Device), cmp.Compare(a.Month, b.Month))
	})
	return out, nil
}

func (f *memFolder) Read(_ context.Context, segment ports.SyncSegment, offset int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]string{segment.Device, segment.Month}
	if f.broken[segment.Device+"/"+segment.Month] {
		return nil, errors.New("cannot read")
	}
	var data []byte
	if segment.Compressed {
		zr, err := gzip.NewReader(bytes.NewReader(f.gz[key]))
		if err != nil {
			return nil, err
		}
		if data, err = io.ReadAll(zr); err != nil {
			return nil, err
		}
	} else {
		data = f.plain[key]
	}
	if int64(len(data)) < offset {
		return nil, coreErrors.ErrSyncSegmentShrank
	}
	return bytes.Clone(data[offset:]), nil
}

func (f *memFolder) Append(_ context.Context, device, month string, lines []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]string{device, month}
	f.plain[key] = append(f.plain[key], lines...)
	f.appends++
	return nil
}

func (f *memFolder) Compress(_ context.Context, device, month string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]string{device, month}
	data, ok := f.plain[key]
	if !ok {
		return nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	f.gz[key] = buf.Bytes()
	delete(f.plain, key)
	f.compressions++
	return nil
}

func (f *memFolder) Devices(context.Context) ([]ports.SyncDevice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Collect(maps.Values(f.devices))
	slices.SortFunc(out, func(a, b ports.SyncDevice) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (f *memFolder) WriteDevice(_ context.Context, device ports.SyncDevice) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	device.Seen = device.Seen.UTC()
	f.devices[device.ID] = device
	f.deviceWrites++
	return nil
}

func (f *memFolder) Pending(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.pending), nil
}

// lines returns the changes in a device's journal, every month, in order.
func (f *memFolder) lines(device string) []journal.Change {
	f.mu.Lock()
	keys := make([][2]string, 0)
	for key := range f.plain {
		if key[0] == device {
			keys = append(keys, key)
		}
	}
	for key := range f.gz {
		if key[0] == device {
			if _, ok := f.plain[key]; !ok {
				keys = append(keys, key)
			}
		}
	}
	f.mu.Unlock()
	slices.SortFunc(keys, func(a, b [2]string) int { return cmp.Compare(a[1], b[1]) })

	var out []journal.Change
	for _, key := range keys {
		_, compressed := f.gz[key]
		_, plain := f.plain[key]
		data, _ := f.Read(context.Background(), ports.SyncSegment{Device: key[0], Month: key[1], Compressed: compressed && !plain}, 0)
		out = append(out, journal.ReadSegment(device, data).Changes...)
	}
	return out
}

// writes counts what was written to the folder.
func (f *memFolder) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.appends + f.compressions + f.deviceWrites
}

// memState is state.db in memory.
type memState struct {
	// lock is Exclusive's lock: a slot taken while it runs.
	lock chan struct{}
	mu   stdsync.Mutex

	meta        ports.SyncMeta
	cursors     map[[2]string]ports.SyncCursor
	records     map[[2]string]journal.Record
	images      map[[2]string]ports.SyncImage
	reminderIDs map[string]int

	saves  []ports.SyncDelta
	failAt int
}

func newMemState() *memState {
	return &memState{
		lock:        make(chan struct{}, 1),
		cursors:     make(map[[2]string]ports.SyncCursor),
		records:     make(map[[2]string]journal.Record),
		images:      make(map[[2]string]ports.SyncImage),
		reminderIDs: make(map[string]int),
	}
}

func (s *memState) Exclusive(ctx context.Context, fn func(ctx context.Context) error) error {
	select {
	case s.lock <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.lock }()
	return fn(ctx)
}

func (s *memState) Meta(context.Context) (ports.SyncMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta := s.meta
	meta.Token = maps.Clone(s.meta.Token)
	meta.AutoStops = slices.Clone(s.meta.AutoStops)
	return meta, nil
}

func (s *memState) ActivitiesStartingIn(_ context.Context, span ports.SyncSpan) ([]journal.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []journal.Record
	for key, record := range s.records {
		if key[0] != string(journal.TypeActivity) {
			continue
		}
		var text string
		if json.Unmarshal(record.Fields["start"].Value, &text) != nil {
			continue
		}
		start, err := time.Parse(time.RFC3339, text)
		if err == nil && !start.Before(span.From) && (span.To.IsZero() || start.Before(span.To)) {
			out = append(out, copyRecord(record))
		}
	}
	slices.SortFunc(out, func(a, b journal.Record) int { return strings.Compare(a.UID, b.UID) })
	return out, nil
}

func (s *memState) Cursors(context.Context) ([]ports.SyncCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Collect(maps.Values(s.cursors)), nil
}

func copyRecord(record journal.Record) journal.Record {
	out := record
	out.Fields = maps.Clone(record.Fields)
	return out
}

func (s *memState) Records(_ context.Context, typ journal.RecordType, uids []string) ([]journal.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []journal.Record
	for _, uid := range uids {
		if record, ok := s.records[[2]string{string(typ), uid}]; ok {
			out = append(out, copyRecord(record))
		}
	}
	return out, nil
}

func (s *memState) AllRecords(_ context.Context, typ journal.RecordType) ([]journal.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []journal.Record
	for key, record := range s.records {
		if key[0] == string(typ) {
			out = append(out, copyRecord(record))
		}
	}
	return out, nil
}

// RunningActivities returns, as state.db does, the visible activities with no shown end: those an
// auto_end stops come from AutoEndedActivities.
func (s *memState) RunningActivities(context.Context) ([]journal.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []journal.Record
	for key, record := range s.records {
		if key[0] != string(journal.TypeActivity) {
			continue
		}
		if a, visible := record.Activity(); visible && a.EndTime == nil {
			out = append(out, copyRecord(record))
		}
	}
	return out, nil
}

func (s *memState) AutoEndedActivities(_ context.Context, after time.Time) ([]journal.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []journal.Record
	for key, record := range s.records {
		if key[0] != string(journal.TypeActivity) || record.ShownEndField() != journal.FieldAutoEnd {
			continue
		}
		if a, visible := record.Activity(); visible && a.EndTime.After(after) {
			out = append(out, copyRecord(record))
		}
	}
	return out, nil
}

func (s *memState) Images(_ context.Context, typ journal.RecordType) ([]ports.SyncImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ports.SyncImage
	for key, img := range s.images {
		if key[0] == string(typ) {
			out = append(out, img)
		}
	}
	return out, nil
}

func (s *memState) ImagesByUID(_ context.Context, typ journal.RecordType, uids []string) ([]ports.SyncImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ports.SyncImage
	for _, uid := range uids {
		if img, ok := s.images[[2]string{string(typ), uid}]; ok {
			out = append(out, img)
		}
	}
	return out, nil
}

func (s *memState) ImagesByKey(_ context.Context, typ journal.RecordType, keys []string) ([]ports.SyncImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ports.SyncImage
	for key, img := range s.images {
		if key[0] == string(typ) && slices.Contains(keys, img.Key) {
			out = append(out, img)
		}
	}
	return out, nil
}

func (s *memState) ReminderIDs(context.Context) (map[string]int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.reminderIDs), nil
}

func (s *memState) Save(_ context.Context, delta ports.SyncDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failAt > 0 && len(s.saves)+1 == s.failAt {
		s.saves = append(s.saves, ports.SyncDelta{})
		return errors.New("disk full")
	}
	s.saves = append(s.saves, delta)
	if delta.Reset {
		s.records = make(map[[2]string]journal.Record)
		s.cursors = make(map[[2]string]ports.SyncCursor)
	}
	if delta.Meta != nil {
		s.meta = *delta.Meta
		s.meta.Token = maps.Clone(delta.Meta.Token)
		s.meta.AutoStops = slices.Clone(delta.Meta.AutoStops)
	}
	for _, record := range delta.Records {
		s.records[[2]string{string(record.Type), record.UID}] = copyRecord(record)
	}
	for _, cursor := range delta.Cursors {
		s.cursors[[2]string{cursor.Device, cursor.Month}] = cursor
	}
	for _, img := range delta.Images {
		s.images[[2]string{string(img.Type), img.UID}] = img
	}
	for _, drop := range delta.DropImages {
		delete(s.images, [2]string{string(drop.Type), drop.UID})
	}
	maps.Copy(s.reminderIDs, delta.ReminderIDs)
	return nil
}

// lastSave returns the last delta saved.
func (s *memState) lastSave() ports.SyncDelta {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.saves) == 0 {
		return ports.SyncDelta{}
	}
	return s.saves[len(s.saves)-1]
}

// fileBackend describes a backend kept in files: its token hashes the files, and Changed covers
// everything whenever the token moved.
type fileBackend struct {
	identity  ports.SyncBackendIdentity
	precision time.Duration
	keepsUIDs bool
	sidecar   bool
	// lineTags is set for a backend whose lines hold tags: all but the file backend.
	lineTags bool
	// wall is set for a backend of local wall times; normalize is its round trip, none for sqlite.
	wall      bool
	normalize func(models.Activity) models.Activity
	// files are hashed for the token; dirs too, every file under them.
	files []string
	token func() string
	// backup is the backup of the real backend over the same files.
	backup ports.SyncBackend

	changedCalls int
}

func (b *fileBackend) Identity() ports.SyncBackendIdentity { return b.identity }
func (b *fileBackend) Precision() time.Duration            { return b.precision }
func (b *fileBackend) KeepsUIDs() bool                     { return b.keepsUIDs }
func (b *fileBackend) SidecarNotes() bool                  { return b.sidecar }
func (b *fileBackend) LineTags() bool                      { return b.lineTags }
func (b *fileBackend) WallClock() bool                     { return b.wall }

// Backup copies the files the way the real backend does.
func (b *fileBackend) Backup(ctx context.Context, scope ports.SyncScope, suffix string) ([]string, error) {
	return b.backup.Backup(ctx, scope, suffix)
}

// Exists reports whether the data file or directory is there; the sqlite backend's database always is.
func (b *fileBackend) Exists(context.Context) (bool, error) {
	if b.token != nil {
		return true, nil
	}
	_, err := os.Stat(b.files[0])
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

func (b *fileBackend) Normalize(a models.Activity) models.Activity {
	if b.normalize == nil {
		return a
	}
	return b.normalize(a)
}

func (b *fileBackend) Token(context.Context) (ports.SyncToken, error) {
	if b.token != nil {
		return ports.SyncToken{"data": b.token()}, nil
	}
	hash := sha256.New()
	for _, path := range b.files {
		_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") && strings.HasSuffix(d.Name(), ".lock") {
				return nil //nolint:nilerr // a missing file hashes as empty
			}
			data, _ := os.ReadFile(p)
			hash.Write([]byte(p))
			hash.Write(data)
			return nil
		})
	}
	return ports.SyncToken{"data": hex.EncodeToString(hash.Sum(nil))}, nil
}

func (b *fileBackend) Changed(_ context.Context, since, now ports.SyncToken) (ports.SyncScope, error) {
	b.changedCalls++
	if since.Equal(now) {
		return ports.SyncScope{}, nil
	}
	return ports.SyncScope{AllActivities: true, Reminders: true}, nil
}
