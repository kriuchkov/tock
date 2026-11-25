package sync

import (
	"cmp"
	"context"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/go-faster/errors"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// segmentKey names a segment by device and month.
type segmentKey struct {
	device string
	month  string
}

func cursorKey(c ports.SyncCursor) segmentKey {
	return segmentKey{device: c.Device, month: c.Month}
}

func segmentKeyOf(s ports.SyncSegment) segmentKey {
	return segmentKey{device: s.Device, month: s.Month}
}

// segmentPath is a segment as Report.Unreadable names it: journal/<device>/<file>.
func segmentPath(s ports.SyncSegment) string {
	name := s.Month + ".jsonl"
	if s.Compressed {
		name += ".gz"
	}
	return path.Join("journal", s.Device, name)
}

// pendingSegments returns the segments among the files not downloaded yet.
func pendingSegments(pending []string) map[segmentKey]bool {
	out := make(map[segmentKey]bool)
	for _, name := range pending {
		parts := strings.Split(name, "/")
		if len(parts) != 3 || parts[0] != "journal" {
			continue
		}
		month, ok := strings.CutSuffix(strings.TrimSuffix(parts[2], ".gz"), ".jsonl")
		if ok {
			out[segmentKey{device: parts[1], month: month}] = true
		}
	}
	return out
}

// reading is what reading the journals found.
type reading struct {
	// changes are the new changes, in the order of the segments and of their lines.
	changes []journal.Change
	// cursors holds the cursor of every segment, moved past what was read.
	cursors map[segmentKey]ports.SyncCursor
	// updated holds the segments whose cursor moved.
	updated map[segmentKey]bool
	// listed holds the segments the folder listed.
	listed  map[segmentKey]bool
	skipped int
	// unreadable lists the segments that could not be read.
	unreadable []string
	// cloned is set when this device's own journal holds a change later than its saved clock.
	cloned bool
	// rebuilt is set when a segment shrank or went away and everything was read from the start.
	rebuilt bool
}

// journalReader reads the journals of the sync folder from the cursors on.
type journalReader struct {
	folder ports.SyncFolder
	// device is this device's ID and saved its clock: a change of its own journal later than saved
	// shows another device writing under its ID. A zero saved clock checks nothing.
	device string
	saved  journal.HLC
	clock  *journal.Clock
	now    time.Time
	// waiting holds the segments not downloaded yet: one with a cursor did not go away.
	waiting map[segmentKey]bool
}

// read reads every segment from its cursor to its last complete line, skipping those whose file is
// unchanged. When a segment shrank or went away it starts again with no cursors.
func (r *journalReader) read(ctx context.Context, cursors []ports.SyncCursor) (reading, error) {
	known := make(map[segmentKey]ports.SyncCursor, len(cursors))
	for _, c := range cursors {
		known[cursorKey(c)] = c
	}

	result, restart, err := r.readFrom(ctx, known, true)
	if err != nil || !restart {
		return result, err
	}
	result, _, err = r.readFrom(ctx, map[segmentKey]ports.SyncCursor{}, false)
	result.rebuilt = true
	return result, err
}

// readFrom reads the segments past the cursors. When restartable, a segment that shrank or went
// away stops it and asks for a restart; otherwise such a segment is unreadable.
func (r *journalReader) readFrom(
	ctx context.Context,
	known map[segmentKey]ports.SyncCursor,
	restartable bool,
) (reading, bool, error) {
	segments, err := r.folder.Segments(ctx)
	if err != nil {
		return reading{}, false, errors.Wrap(err, "list the journal segments")
	}

	listed := make(map[segmentKey]bool, len(segments))
	for _, segment := range segments {
		listed[segmentKeyOf(segment)] = true
	}
	if restartable {
		for key := range known {
			if !listed[key] && !r.waiting[key] {
				return reading{}, true, nil
			}
		}
	}

	result := reading{cursors: known, updated: make(map[segmentKey]bool), listed: listed}
	for _, segment := range segments {
		restart, readErr := r.readSegment(ctx, segment, &result, restartable)
		if readErr != nil || restart {
			return reading{}, restart, readErr
		}
	}
	return result, false, nil
}

// readSegment reads one segment past its cursor into result.
func (r *journalReader) readSegment(ctx context.Context, segment ports.SyncSegment, result *reading, restartable bool) (bool, error) {
	key := segmentKeyOf(segment)
	cursor, ok := result.cursors[key]
	if ok && cursor.Size == segment.Size && cursor.Compressed == segment.Compressed {
		return false, nil
	}

	data, err := r.folder.Read(ctx, segment, cursor.Offset)
	switch {
	case errors.Is(err, coreErrors.ErrSyncSegmentShrank) && restartable:
		return true, nil
	case err != nil:
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, errors.Wrap(ctxErr, "read the journals")
		}
		result.unreadable = append(result.unreadable, segmentPath(segment))
		return false, nil
	}

	parsed := journal.ReadSegment(segment.Device, data)
	for _, change := range parsed.Changes {
		if segment.Device == r.device && r.saved != (journal.HLC{}) && change.HLC.Compare(r.saved) > 0 {
			result.cloned = true
		}
		r.clock.Observe(change.HLC, r.now)
	}
	result.changes = append(result.changes, parsed.Changes...)
	result.skipped += len(parsed.Skipped)
	result.cursors[key] = ports.SyncCursor{
		Device:     segment.Device,
		Month:      segment.Month,
		Offset:     cursor.Offset + int64(parsed.Read),
		Size:       segment.Size,
		Compressed: segment.Compressed,
	}
	result.updated[key] = true
	return false, nil
}

// sortedCursors returns the cursors named, by device and month.
func sortedCursors(cursors map[segmentKey]ports.SyncCursor, keys map[segmentKey]bool) []ports.SyncCursor {
	out := make([]ports.SyncCursor, 0, len(keys))
	for key := range keys {
		if cursor, ok := cursors[key]; ok {
			out = append(out, cursor)
		}
	}
	sortCursors(out)
	return out
}

func sortCursors(cursors []ports.SyncCursor) {
	slices.SortFunc(cursors, func(a, b ports.SyncCursor) int {
		return cmp.Or(cmp.Compare(a.Device, b.Device), cmp.Compare(a.Month, b.Month))
	})
}
