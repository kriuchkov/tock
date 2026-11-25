package syncfolder

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"

	"github.com/go-faster/errors"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// maxSegmentSize bounds the uncompressed content of a .gz read whole; a month is about 150 KB.
const maxSegmentSize = 1 << 30

// Segments lists the segment files of every journal/<device>/ named by a valid device ID: one per
// month, the plain file when both exist, by device and month. Placeholders and other names are
// left out; a missing journal folder has none.
func (f *folder) Segments(ctx context.Context) ([]ports.SyncSegment, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	entries, err := readDir(f.path(journalDir))
	if err != nil {
		return nil, err
	}
	var segments []ports.SyncSegment
	for _, entry := range entries {
		if !entry.IsDir() || !isDeviceID(entry.Name()) {
			continue
		}
		found, listErr := f.deviceSegments(entry.Name())
		if listErr != nil {
			return nil, listErr
		}
		segments = append(segments, found...)
	}
	slices.SortFunc(segments, func(a, b ports.SyncSegment) int {
		return cmp.Or(cmp.Compare(a.Device, b.Device), cmp.Compare(a.Month, b.Month))
	})
	return segments, nil
}

// deviceSegments lists the segment files of one device's journal, one per month.
func (f *folder) deviceSegments(device string) ([]ports.SyncSegment, error) {
	entries, err := readDir(f.journalPath(device))
	if err != nil {
		return nil, err
	}
	byMonth := make(map[string]ports.SyncSegment)
	for _, entry := range entries {
		month, compressed, ok := parseSegmentName(entry.Name())
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			// Gone since the listing: a compression or a rename by iCloud.
			continue
		}
		if listed, seen := byMonth[month]; !seen || listed.Compressed {
			byMonth[month] = ports.SyncSegment{Device: device, Month: month, Compressed: compressed, Size: info.Size()}
		}
	}
	return slices.Collect(maps.Values(byMonth)), nil
}

// Read returns the uncompressed content of a segment from offset to its end. A .gz is read whole.
// When the file listed is gone but the other file of its month is there (the month was compressed,
// or reopened, since the listing), that one is read: both hold the same changes. A segment with
// neither file reads as empty, so it shrank when offset is past 0, unless its placeholder shows
// it is not downloaded yet.
func (f *folder) Read(ctx context.Context, segment ports.SyncSegment, offset int64) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := checkSegment(segment.Device, segment.Month); err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, errors.Errorf("invalid offset %d", offset)
	}
	rel := segmentRel(segment.Device, segment.Month, segment.Compressed)

	content, err := readSegmentFile(f.path(rel), segment.Compressed, offset)
	if errors.Is(err, fs.ErrNotExist) {
		other := segmentRel(segment.Device, segment.Month, !segment.Compressed)
		content, err = readSegmentFile(f.path(other), !segment.Compressed, offset)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return f.readMissing(ctx, segment, offset)
	}
	if err != nil {
		return nil, errors.Wrapf(err, "read %s", rel)
	}
	return content, nil
}

// readMissing is Read of a segment whose files are both gone.
func (f *folder) readMissing(ctx context.Context, segment ports.SyncSegment, offset int64) ([]byte, error) {
	rel := segmentRel(segment.Device, segment.Month, segment.Compressed)
	var waiting []string
	for _, compressed := range []bool{false, true} {
		if path := f.segmentPath(segment.Device, segment.Month, compressed); isPlaceholder(path) {
			waiting = append(waiting, path)
		}
	}
	if len(waiting) > 0 {
		f.download(ctx, waiting)
		return nil, errors.Wrapf(errNotDownloaded, "read %s", rel)
	}
	if offset > 0 {
		return nil, errors.Wrapf(coreErrors.ErrSyncSegmentShrank, "%s is gone, %d bytes were read", rel, offset)
	}
	return []byte{}, nil
}

// readSegmentFile returns the uncompressed content of a segment file from offset.
func readSegmentFile(path string, compressed bool, offset int64) ([]byte, error) {
	if compressed {
		return readCompressed(path, offset)
	}
	return readPlain(path, offset)
}

func readPlain(path string, offset int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, errors.Wrap(err, "stat")
	}
	if info.Size() < offset {
		return nil, errors.Wrapf(coreErrors.ErrSyncSegmentShrank, "%d bytes, %d were read", info.Size(), offset)
	}
	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		return nil, errors.Wrap(err, "seek")
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, errors.Wrap(err, "read")
	}
	return content, nil
}

func readCompressed(path string, offset int64) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content, err := gunzip(raw)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) < offset {
		return nil, errors.Wrapf(coreErrors.ErrSyncSegmentShrank, "%d bytes uncompressed, %d were read", len(content), offset)
	}
	return content[offset:], nil
}

// gunzip returns the content of every gzip member of raw, joined.
func gunzip(raw []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.Wrap(err, "open gzip")
	}
	defer reader.Close()

	content, err := io.ReadAll(io.LimitReader(reader, maxSegmentSize+1))
	if err != nil {
		return nil, errors.Wrap(err, "decompress")
	}
	if len(content) > maxSegmentSize {
		return nil, errors.Errorf("more than %d bytes uncompressed", maxSegmentSize)
	}
	return content, nil
}

// gzipBytes compresses content as one gzip member.
func gzipBytes(content []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, errors.Wrap(err, "create gzip writer")
	}
	if _, err = writer.Write(content); err != nil {
		return nil, errors.Wrap(err, "compress")
	}
	if err = writer.Close(); err != nil {
		return nil, errors.Wrap(err, "compress")
	}
	return buf.Bytes(), nil
}
