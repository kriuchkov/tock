package syncfolder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

func TestAppendCreatesAndGrowsSegment(t *testing.T) {
	f, _ := newTestFolder(t)
	rel := "journal/" + devA + "/2026-11.jsonl"

	require.NoError(t, f.Append(t.Context(), devA, "2026-11", []byte(change(devA, 1))))
	require.NoError(t, f.Append(t.Context(), devA, "2026-11", []byte(change(devA, 2)+change(devA, 3))))

	assert.Equal(t, change(devA, 1)+change(devA, 2)+change(devA, 3), readFile(t, f, rel))
	assert.Equal(t, []string{"journal/" + devA + "/.writer.lock", rel}, tree(t, f.dir))
}

func TestAppendRejectsWhatIsNotWholeLines(t *testing.T) {
	f, _ := newTestFolder(t)

	require.Error(t, f.Append(t.Context(), devA, "2026-11", []byte(`{"v":1}`)))
	require.Error(t, f.Append(t.Context(), "not-a-device", "2026-11", []byte(change(devA, 1))))
	require.Error(t, f.Append(t.Context(), devA, "2026-13", []byte(change(devA, 1))))
	require.NoError(t, f.Append(t.Context(), devA, "2026-11", nil), "no lines append nothing")
	assert.NoDirExists(t, f.dir, "nothing is written")
}

func TestAppendEndsCutLineFirst(t *testing.T) {
	f, _ := newTestFolder(t)
	rel := "journal/" + devA + "/2026-11.jsonl"
	cut := change(devA, 2)[:20]
	writeFile(t, f, rel, change(devA, 1)+cut)

	require.NoError(t, f.Append(t.Context(), devA, "2026-11", []byte(change(devA, 3))))

	content := readFile(t, f, rel)
	assert.Equal(t, change(devA, 1)+cut+"\n"+change(devA, 3), content)
	read := readAll(t, f, devA, "2026-11")
	assert.Len(t, read.Changes, 2, "the new line is read whole")
	assert.Len(t, read.Skipped, 1, "the cut line is skipped")
}

func TestAppendWaitsForPlaceholder(t *testing.T) {
	f, asked := newTestFolder(t)
	writeFile(t, f, "journal/"+devA+"/.2026-11.jsonl.icloud", "")
	writeFile(t, f, "journal/"+devA+"/.2026-10.jsonl.gz.icloud", "")

	err := f.Append(t.Context(), devA, "2026-11", []byte(change(devA, 1)))
	require.ErrorIs(t, err, errNotDownloaded)
	err = f.Append(t.Context(), devA, "2026-10", []byte(change(devA, 2)))
	require.ErrorIs(t, err, errNotDownloaded, "a closed month not downloaded is not replaced either")

	assert.NoFileExists(t, f.path("journal/"+devA+"/2026-11.jsonl"))
	assert.NoFileExists(t, f.path("journal/"+devA+"/2026-10.jsonl"))
	assert.Equal(t, []string{
		f.path("journal/" + devA + "/2026-11.jsonl"),
		f.path("journal/" + devA + "/2026-10.jsonl.gz"),
	}, asked.asked())
}

func TestCompressClosesMonth(t *testing.T) {
	f, _ := newTestFolder(t)
	content := change(devA, 1) + change(devA, 2)
	require.NoError(t, f.Append(t.Context(), devA, "2026-10", []byte(content)))
	require.NoError(t, f.Append(t.Context(), devA, "2026-11", []byte(change(devA, 3))))

	require.NoError(t, f.Compress(t.Context(), devA, "2026-10"))

	dir := "journal/" + devA + "/"
	assert.Equal(t, []string{dir + ".writer.lock", dir + "2026-10.jsonl.gz", dir + "2026-11.jsonl"}, tree(t, f.dir))
	packed := readFile(t, f, dir+"2026-10.jsonl.gz")
	assert.Equal(t, content, gunzipped(t, packed))

	segments, err := f.Segments(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ports.SyncSegment{
		{Device: devA, Month: "2026-10", Compressed: true, Size: int64(len(packed))},
		{Device: devA, Month: "2026-11", Size: int64(len(change(devA, 3)))},
	}, segments)

	require.NoError(t, f.Compress(t.Context(), devA, "2026-10"), "a closed month has nothing to compress")
	assert.Equal(t, packed, readFile(t, f, dir+"2026-10.jsonl.gz"))
}

func TestCompressWithoutPlainFile(t *testing.T) {
	f, _ := newTestFolder(t)

	require.NoError(t, f.Compress(t.Context(), devA, "2026-10"))
	assert.NoDirExists(t, f.dir, "nothing is written")
	require.Error(t, f.Compress(t.Context(), "../x", "2026-10"))
}

func TestCompressWaitsForPlaceholder(t *testing.T) {
	f, asked := newTestFolder(t)
	dir := "journal/" + devA + "/"
	writeFile(t, f, dir+"2026-10.jsonl", change(devA, 1))
	writeFile(t, f, dir+".2026-10.jsonl.gz.icloud", "")

	err := f.Compress(t.Context(), devA, "2026-10")
	require.ErrorIs(t, err, errNotDownloaded)
	assert.Equal(t, change(devA, 1), readFile(t, f, dir+"2026-10.jsonl"), "the plain file stays")
	assert.NoFileExists(t, f.path(dir+"2026-10.jsonl.gz"))
	assert.Equal(t, []string{f.path(dir + "2026-10.jsonl.gz")}, asked.asked())
}

// A change in a month already compressed (which a device whose clock never goes back does not
// make) reopens the month with every change it had.
func TestAppendReopensCompressedMonth(t *testing.T) {
	f, _ := newTestFolder(t)
	require.NoError(t, f.Append(t.Context(), devA, "2026-10", []byte(change(devA, 1))))
	require.NoError(t, f.Compress(t.Context(), devA, "2026-10"))

	require.NoError(t, f.Append(t.Context(), devA, "2026-10", []byte(change(devA, 2))))

	segments, err := f.Segments(t.Context())
	require.NoError(t, err)
	require.Len(t, segments, 1)
	assert.False(t, segments[0].Compressed, "the plain file is read")
	assert.Equal(t, change(devA, 1)+change(devA, 2), readFile(t, f, "journal/"+devA+"/2026-10.jsonl"))

	data, err := f.Read(t.Context(), segments[0], int64(len(change(devA, 1))))
	require.NoError(t, err)
	assert.Equal(t, change(devA, 2), string(data), "a cursor into the .gz carries over")

	require.NoError(t, f.Compress(t.Context(), devA, "2026-10"))
	assert.Equal(t, change(devA, 1)+change(devA, 2), gunzipped(t, readFile(t, f, "journal/"+devA+"/2026-10.jsonl.gz")))
}

// readAll reads a plain segment from its start.
func readAll(t *testing.T, f *folder, device, month string) journal.Segment {
	t.Helper()
	data, err := f.Read(t.Context(), ports.SyncSegment{Device: device, Month: month}, 0)
	require.NoError(t, err)
	return journal.ReadSegment(device, data)
}
