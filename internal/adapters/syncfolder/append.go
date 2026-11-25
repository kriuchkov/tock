package syncfolder

import (
	"bytes"
	"context"
	"io/fs"
	"os"

	"github.com/go-faster/errors"
)

// Append appends lines to journal/<device>/<month>.jsonl with one write call and syncs the file to
// disk, holding the journal's lock. A file whose last line was cut off (a write interrupted by a
// crash) first gets the "\n" it lacks, so that line stays a line of its own. A month whose plain
// file is not downloaded yet is not written: a new file would replace its lines everywhere. A
// month already compressed is reopened: its plain file is made from the content of the .gz and the
// new lines, so readers, which take the plain file, keep every change of the month.
func (f *folder) Append(ctx context.Context, device, month string, lines []byte) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := checkSegment(device, month); err != nil {
		return err
	}
	if len(lines) == 0 {
		return nil
	}
	if lines[len(lines)-1] != '\n' {
		return errors.New("lines to append must end in a newline")
	}
	return f.lockJournal(device, func() error {
		if err := f.appendLocked(ctx, device, month, lines); err != nil {
			return errors.Wrapf(err, "append to %s", segmentRel(device, month, false))
		}
		return nil
	})
}

func (f *folder) appendLocked(ctx context.Context, device, month string, lines []byte) error {
	plain := f.segmentPath(device, month, false)
	if exists(plain) {
		return appendLines(plain, lines)
	}
	if exists(placeholderPath(plain)) {
		f.requestDownload(ctx, plain)
		return errNotDownloaded
	}

	compressed := f.segmentPath(device, month, true)
	if isPlaceholder(compressed) {
		f.requestDownload(ctx, compressed)
		return errors.Wrap(errNotDownloaded, "the compressed month")
	}
	content, err := readCompressed(compressed, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return appendLines(plain, lines)
	case err != nil:
		return errors.Wrap(err, "reopen the compressed month")
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	created, err := createFile(plain, append(content, lines...))
	if err != nil {
		return err
	}
	if !created {
		return errors.New("the month's file appeared while it was reopened")
	}
	return nil
}

// appendLines appends lines to the file at path with one write call, creating it when missing,
// and syncs it to disk. A file that does not end in "\n" gets one before the lines.
func appendLines(path string, lines []byte) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE, fileMode)
	if err != nil {
		return errors.Wrap(err, "open")
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return errors.Wrap(err, "stat")
	}
	data := lines
	if size := info.Size(); size > 0 {
		last := make([]byte, 1)
		if _, err = file.ReadAt(last, size-1); err != nil {
			return errors.Wrap(err, "read the last byte")
		}
		if last[0] != '\n' {
			data = append([]byte{'\n'}, lines...)
		}
	}
	if _, err = file.Write(data); err != nil {
		return errors.Wrap(err, "write")
	}
	if err = file.Sync(); err != nil {
		return errors.Wrap(err, "sync")
	}
	if err = file.Close(); err != nil {
		return errors.Wrap(err, "close")
	}
	return nil
}

// Compress closes a month of a device's journal, holding the journal's lock: writes the plain
// file gzipped to a temporary file, checks that it reads back, renames it to <month>.jsonl.gz and
// removes <month>.jsonl. It does nothing when there is no plain file, and fails without writing
// while a .gz of the month is not downloaded yet.
func (f *folder) Compress(ctx context.Context, device, month string) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := checkSegment(device, month); err != nil {
		return err
	}
	if !exists(f.segmentPath(device, month, false)) {
		return nil
	}
	return f.lockJournal(device, func() error {
		if err := f.compressLocked(ctx, device, month); err != nil {
			return errors.Wrapf(err, "compress %s", segmentRel(device, month, false))
		}
		return nil
	})
}

func (f *folder) compressLocked(ctx context.Context, device, month string) error {
	plain := f.segmentPath(device, month, false)
	compressed := f.segmentPath(device, month, true)
	content, err := os.ReadFile(plain)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "read")
	}
	if isPlaceholder(compressed) {
		f.requestDownload(ctx, compressed)
		return errors.Wrap(errNotDownloaded, "the compressed month")
	}

	packed, err := gzipBytes(content)
	if err != nil {
		return err
	}
	file, err := createTemp(compressed)
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err = writeAndClose(file, packed); err != nil {
		return errors.Wrap(err, "write temporary file")
	}

	// A .gz other devices cannot read would lose the month for them: check it before it replaces
	// the plain file.
	if err = checkReadsBack(tmp, content); err != nil {
		return err
	}
	if err = os.Rename(tmp, compressed); err != nil {
		return errors.Wrap(err, "rename")
	}
	if err = os.Remove(plain); err != nil {
		return errors.Wrap(err, "remove the plain file")
	}
	return nil
}

// checkReadsBack checks that the gzip file at path holds content.
func checkReadsBack(path string, content []byte) error {
	back, err := readCompressed(path, 0)
	if err != nil {
		return errors.Wrap(err, "read the compressed file back")
	}
	if !bytes.Equal(back, content) {
		return errors.New("the compressed file does not read back")
	}
	return nil
}
