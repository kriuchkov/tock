package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// formatVersion is the version of the journal lines this device writes.
const formatVersion = 1

// journalLine is one line of a journal as this device writes it.
type journalLine struct {
	V    int                        `json:"v"`
	HLC  string                     `json:"hlc"`
	Dev  string                     `json:"dev"`
	Type journal.RecordType         `json:"type"`
	UID  string                     `json:"uid"`
	Set  map[string]json.RawMessage `json:"set"`
}

// encodeLine writes a change as a journal line ending in "\n", without escaping HTML characters.
func encodeLine(change journal.Change) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	line := journalLine{V: formatVersion, HLC: change.HLC.String(), Dev: change.Dev, Type: change.Type, UID: change.UID, Set: change.Set}
	if err := encoder.Encode(line); err != nil {
		return nil, errors.Wrapf(err, "encode a change of %s %s", change.Type, change.UID)
	}
	return buf.Bytes(), nil
}

// monthOf is the segment a change goes to: the UTC month of its clock's wall time.
func monthOf(hlc journal.HLC) string {
	return time.UnixMilli(hlc.Wall).UTC().Format("2006-01")
}

// monthLines is what one append writes to one segment.
type monthLines struct {
	month string
	lines []byte
	count int
}

// appendOwn appends this device's changes to its journal, one write per month. The clock is saved
// first, so the own journal never holds a clock later than the saved one, which would look like
// another device writing under this device's ID.
func (r *run) appendOwn(ctx context.Context) error {
	r.report.Written = len(r.own)
	if len(r.own) == 0 {
		return nil
	}

	var groups []monthLines
	for _, change := range r.own {
		line, err := encodeLine(change)
		if err != nil {
			return err
		}
		if _, err = journal.ParseChange(bytes.TrimSuffix(line, []byte("\n"))); err != nil {
			return errors.Wrapf(err, "write a change of %s %s", change.Type, change.UID)
		}
		month := monthOf(change.HLC)
		if len(groups) == 0 || groups[len(groups)-1].month != month {
			groups = append(groups, monthLines{month: month})
		}
		last := &groups[len(groups)-1]
		last.lines = append(last.lines, line...)
		last.count++
	}

	saved := r.meta
	saved.Device = r.device
	saved.Clock = r.clock.Last()
	if err := r.cfg.State.Save(ctx, ports.SyncDelta{Meta: &saved}); err != nil {
		return errors.Wrap(err, "save the clock")
	}
	for _, group := range groups {
		if err := r.folder.Append(ctx, r.device, group.month, group.lines); err != nil {
			return errors.Wrapf(err, "append to the journal of %s", group.month)
		}
	}
	return r.moveOwnCursors(ctx, groups)
}

// moveOwnCursors moves the cursors of the own segments past the lines just appended when nothing
// else was unread in them, so the next run does not read them back.
func (r *run) moveOwnCursors(ctx context.Context, groups []monthLines) error {
	segments, err := r.folder.Segments(ctx)
	if err != nil {
		return errors.Wrap(err, "list the journal segments")
	}
	sizes := make(map[segmentKey]ports.SyncSegment)
	for _, segment := range segments {
		sizes[segmentKeyOf(segment)] = segment
	}
	for _, group := range groups {
		key := segmentKey{device: r.device, month: group.month}
		before, had := r.reading.cursors[key]
		caughtUp := !before.Compressed && before.Offset == before.Size && (had || !r.reading.listed[key])
		segment, ok := sizes[key]
		if !caughtUp || !ok || segment.Compressed || segment.Size != before.Size+int64(len(group.lines)) {
			continue
		}
		r.reading.cursors[key] = ports.SyncCursor{
			Device: r.device,
			Month:  group.month,
			Offset: before.Offset + int64(len(group.lines)),
			Size:   segment.Size,
		}
		r.reading.updated[key] = true
	}
	return nil
}

// compressClosedMonths compresses the own segments of the months before the latest one that were
// read to their end. A failure leaves the plain file for a later run.
func (r *run) compressClosedMonths(ctx context.Context) {
	segments, err := r.folder.Segments(ctx)
	if err != nil {
		return
	}
	var own []ports.SyncSegment
	latest := ""
	for _, segment := range segments {
		if segment.Device == r.device {
			own = append(own, segment)
			latest = max(latest, segment.Month)
		}
	}

	compressed := make(map[string]bool)
	for _, segment := range own {
		key := segmentKeyOf(segment)
		cursor, ok := r.reading.cursors[key]
		if segment.Month >= latest || segment.Compressed || !ok || cursor.Compressed ||
			cursor.Size != segment.Size || cursor.Offset != segment.Size {
			continue
		}
		if r.folder.Compress(ctx, r.device, segment.Month) == nil {
			compressed[segment.Month] = true
		}
	}
	if len(compressed) == 0 {
		return
	}
	if segments, err = r.folder.Segments(ctx); err != nil {
		return
	}
	for _, segment := range segments {
		key := segmentKeyOf(segment)
		if segment.Device != r.device || !segment.Compressed || !compressed[segment.Month] {
			continue
		}
		cursor := r.reading.cursors[key]
		cursor.Compressed, cursor.Size = true, segment.Size
		r.reading.cursors[key] = cursor
		r.reading.updated[key] = true
	}
}

// writeDevice writes devices/<id>.json when it is missing or says something else, or to move seen
// on once an hour, so a run with nothing new writes nothing to the folder.
func (r *run) writeDevice(ctx context.Context) error {
	devices, err := r.folder.Devices(ctx)
	if err != nil {
		return errors.Wrap(err, "read the devices of the sync folder")
	}
	profile := r.cfg.Profile
	profile.ID = r.device
	profile.Seen = r.now.UTC().Truncate(time.Second)
	for _, device := range devices {
		if device.ID == r.device && device.Name == profile.Name && device.Platform == profile.Platform &&
			device.App == profile.App && r.now.Sub(device.Seen) < deviceSeenEvery {
			return nil
		}
	}
	if err = r.folder.WriteDevice(ctx, profile); err != nil {
		return errors.Wrap(err, "write this device's file")
	}
	return nil
}
