package syncfolder

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

func TestSegmentsListsOneFilePerMonth(t *testing.T) {
	f, _ := newTestFolder(t)
	closed := gzipped(t, change(devA, 1))
	writeFile(t, f, "journal/"+devA+"/2026-10.jsonl.gz", closed)
	writeFile(t, f, "journal/"+devA+"/2026-11.jsonl.gz", gzipped(t, change(devA, 2)))
	writeFile(t, f, "journal/"+devA+"/2026-11.jsonl", change(devA, 2))
	writeFile(t, f, "journal/"+devA+"/.2026-12.jsonl.icloud", "")
	writeFile(t, f, "journal/"+devA+"/notes.txt", "x")
	writeFile(t, f, "journal/"+devA+"/2026-13.jsonl", "x")
	writeFile(t, f, "journal/"+devA+"/.writer.lock", "")
	writeFile(t, f, "journal/"+devA+"/.2026-11.jsonl.gz.123.tmp", "x")
	require.NoError(t, os.MkdirAll(f.path("journal/"+devA+"/2027-01.jsonl"), 0o750))
	writeFile(t, f, "journal/"+devB+"/2026-11.jsonl", change(devB, 3)+change(devB, 4))
	writeFile(t, f, "journal/NOT-A-DEVICE/2026-11.jsonl", "x")
	writeFile(t, f, "journal/ABCDEF0123456789/2026-11.jsonl", "x")
	writeFile(t, f, "journal/"+devA+"0/2026-11.jsonl", "x")

	segments, err := f.Segments(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ports.SyncSegment{
		{Device: devB, Month: "2026-11", Size: int64(len(change(devB, 3) + change(devB, 4)))},
		{Device: devA, Month: "2026-10", Compressed: true, Size: int64(len(closed))},
		{Device: devA, Month: "2026-11", Size: int64(len(change(devA, 2)))},
	}, segments)
}

func TestSegmentsOfMissingFolder(t *testing.T) {
	f, _ := newTestFolder(t)

	segments, err := f.Segments(t.Context())
	require.NoError(t, err)
	assert.Empty(t, segments)
	assert.NoDirExists(t, f.dir)
}

func TestReadFromOffset(t *testing.T) {
	f, _ := newTestFolder(t)
	content := change(devA, 1) + change(devA, 2)
	writeFile(t, f, "journal/"+devA+"/2026-11.jsonl", content)
	segment := ports.SyncSegment{Device: devA, Month: "2026-11", Size: int64(len(content))}

	data, err := f.Read(t.Context(), segment, 0)
	require.NoError(t, err)
	assert.Equal(t, content, string(data))

	offset := int64(len(change(devA, 1)))
	data, err = f.Read(t.Context(), segment, offset)
	require.NoError(t, err)
	assert.Equal(t, change(devA, 2), string(data))

	data, err = f.Read(t.Context(), segment, int64(len(content)))
	require.NoError(t, err)
	assert.Empty(t, data, "nothing past the end")
}

// A writer appends a line in pieces: the reader stops at the last complete line and carries on from
// there once the line is whole.
func TestReadLeavesCutLineForLater(t *testing.T) {
	f, _ := newTestFolder(t)
	first, second := change(devB, 1), change(devB, 2)
	cut := len(second) / 2
	file := writeFile(t, f, "journal/"+devB+"/2026-11.jsonl", first+second[:cut])

	segments, err := f.Segments(t.Context())
	require.NoError(t, err)
	require.Len(t, segments, 1)
	data, err := f.Read(t.Context(), segments[0], 0)
	require.NoError(t, err)
	read := journal.ReadSegment(devB, data)
	require.Len(t, read.Changes, 1)
	assert.Empty(t, read.Skipped)
	cursor := int64(read.Read)
	assert.Equal(t, int64(len(first)), cursor, "the cut line waits")

	writer, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0)
	require.NoError(t, err)
	_, err = writer.WriteString(second[cut:])
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	segments, err = f.Segments(t.Context())
	require.NoError(t, err)
	data, err = f.Read(t.Context(), segments[0], cursor)
	require.NoError(t, err)
	assert.Equal(t, second, string(data))
	read = journal.ReadSegment(devB, data)
	require.Len(t, read.Changes, 1)
	assert.Equal(t, "019a9b3c-7d2e-7c41-9a55-000000000002", read.Changes[0].UID)
}

func TestReadReportsShrunkSegment(t *testing.T) {
	f, _ := newTestFolder(t)
	content := change(devA, 1)
	writeFile(t, f, "journal/"+devA+"/2026-11.jsonl", content)
	writeFile(t, f, "journal/"+devA+"/2026-10.jsonl.gz", gzipped(t, content))
	past := int64(len(content) + 1)

	_, err := f.Read(t.Context(), ports.SyncSegment{Device: devA, Month: "2026-11"}, past)
	require.ErrorIs(t, err, coreErrors.ErrSyncSegmentShrank)

	_, err = f.Read(t.Context(), ports.SyncSegment{Device: devA, Month: "2026-10", Compressed: true}, past)
	require.ErrorIs(t, err, coreErrors.ErrSyncSegmentShrank, "a .gz counts uncompressed bytes")

	_, err = f.Read(t.Context(), ports.SyncSegment{Device: devA, Month: "2026-09"}, 1)
	require.ErrorIs(t, err, coreErrors.ErrSyncSegmentShrank, "a segment that went away shrank")

	data, err := f.Read(t.Context(), ports.SyncSegment{Device: devA, Month: "2026-09"}, 0)
	require.NoError(t, err)
	assert.Empty(t, data, "a segment that went away with nothing read is empty")
}

func TestReadCompressedSegment(t *testing.T) {
	f, _ := newTestFolder(t)
	content := change(devA, 1) + change(devA, 2)
	packed := gzipped(t, change(devA, 1)) + gzipped(t, change(devA, 2))
	writeFile(t, f, "journal/"+devA+"/2026-10.jsonl.gz", packed)
	segment := ports.SyncSegment{Device: devA, Month: "2026-10", Compressed: true, Size: int64(len(packed))}

	data, err := f.Read(t.Context(), segment, 0)
	require.NoError(t, err)
	assert.Equal(t, content, string(data), "every gzip member is read")

	data, err = f.Read(t.Context(), segment, int64(len(change(devA, 1))))
	require.NoError(t, err)
	assert.Equal(t, change(devA, 2), string(data), "the cursor of the plain file carries over")
}

func TestReadFailsOnBrokenGzip(t *testing.T) {
	f, _ := newTestFolder(t)
	packed := gzipped(t, change(devA, 1))
	writeFile(t, f, "journal/"+devA+"/2026-10.jsonl.gz", packed[:len(packed)-4])

	_, err := f.Read(t.Context(), ports.SyncSegment{Device: devA, Month: "2026-10", Compressed: true}, 0)
	require.Error(t, err)
	require.NotErrorIs(t, err, coreErrors.ErrSyncSegmentShrank)
}

func TestReadFollowsTheMonthToItsOtherFile(t *testing.T) {
	f, _ := newTestFolder(t)
	content := change(devA, 1) + change(devA, 2)
	writeFile(t, f, "journal/"+devA+"/2026-10.jsonl", content)
	segments, err := f.Segments(t.Context())
	require.NoError(t, err)
	require.Len(t, segments, 1)

	require.NoError(t, f.Compress(t.Context(), devA, "2026-10"))

	offset := int64(len(change(devA, 1)))
	data, err := f.Read(t.Context(), segments[0], offset)
	require.NoError(t, err)
	assert.Equal(t, change(devA, 2), string(data), "the plain file listed was compressed since")
}

func TestReadWaitsForPlaceholder(t *testing.T) {
	f, asked := newTestFolder(t)
	writeFile(t, f, "journal/"+devB+"/.2026-11.jsonl.icloud", "")

	_, err := f.Read(t.Context(), ports.SyncSegment{Device: devB, Month: "2026-11"}, 10)
	require.ErrorIs(t, err, errNotDownloaded)
	require.NotErrorIs(t, err, coreErrors.ErrSyncSegmentShrank, "a file not downloaded did not shrink")
	assert.Equal(t, []string{f.path("journal/" + devB + "/2026-11.jsonl")}, asked.asked())
}

func TestReadRejectsBadSegment(t *testing.T) {
	f, _ := newTestFolder(t)
	for _, segment := range []ports.SyncSegment{
		{Device: "../../etc", Month: "2026-11"},
		{Device: devA, Month: "../2026"},
	} {
		_, err := f.Read(t.Context(), segment, 0)
		require.Error(t, err)
	}
	_, err := f.Read(t.Context(), ports.SyncSegment{Device: devA, Month: "2026-11"}, -1)
	require.Error(t, err)
}
