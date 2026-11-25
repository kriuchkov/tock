// Package syncfolder reads and writes the sync folder of docs/sync.md on the file system: on macOS
// the iOS app's iCloud Drive container, which iCloud syncs on its own.
//
// Writes follow the folder's rules: a journal line is appended whole, with one write call, and the
// file is synced to disk; devices/<id>.json, format.json and a compressed month are written to a
// temporary sibling and moved into place, format.json only where nothing is yet. A month that gets a
// change after it was compressed is reopened with the content of its .gz, so no change of it is
// lost. devices/<id>.json is rewritten at most once an hour while only its seen changes.
//
// An iCloud placeholder (.<name>.icloud) counts as a missing file: listings leave it out, Pending
// reports it and asks iCloud for it, and a write never replaces it, since that would drop the lines
// it stands for on every device. A file iCloud keeps without its contents on disk (a dataless file)
// is listed with its size and downloaded when read.
//
// The CLI, the menu bar icon and the MCP server can write the same journal at once, so appends and
// compressions of a device's journal hold a flock (fileio.Update) on journal/<device>/.writer.lock,
// a hidden file in the device's own journal folder.
package syncfolder

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/adapters/repositories/fileio"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	formatFile  = "format.json"
	devicesDir  = "devices"
	journalDir  = "journal"
	plainExt    = ".jsonl"
	gzipExt     = ".jsonl.gz"
	deviceExt   = ".json"
	hiddenStart = "."
	// placeholderExt ends the name of an iCloud placeholder, .<name>.icloud.
	placeholderExt = ".icloud"
	// writerLock is the path fileio.Update locks to write a device's journal: its lock file is
	// journal/<device>/.writer.lock.
	writerLock = ".writer"

	deviceIDLength = 16
	// monthLength is the length of YYYY-MM.
	monthLength = 7
	monthDash   = 4
	maxMonth    = 12

	dirMode  = 0o750
	fileMode = 0o644
)

// errNotDownloaded means a file of the folder is still an iCloud placeholder: it is asked for, and
// its changes come later.
var errNotDownloaded = errors.New("not downloaded yet")

// folder is a sync folder at dir.
type folder struct {
	dir string
	// download asks iCloud for files that are not downloaded yet, by absolute path.
	download func(ctx context.Context, paths []string)
}

// New returns the sync folder at dir. It touches nothing on disk until a method is called.
func New(dir string) ports.SyncFolder {
	return &folder{dir: dir, download: requestDownloads}
}

func (f *folder) Dir() string {
	return f.dir
}

// path returns the absolute path of rel, a slash-separated path in the folder.
func (f *folder) path(rel string) string {
	return filepath.Join(f.dir, filepath.FromSlash(rel))
}

// journalPath returns journal/<device>.
func (f *folder) journalPath(device string) string {
	return f.path(path.Join(journalDir, device))
}

// segmentPath returns journal/<device>/<month>.jsonl, or .jsonl.gz when compressed.
func (f *folder) segmentPath(device, month string, compressed bool) string {
	return f.path(segmentRel(device, month, compressed))
}

// lockJournal runs fn holding the lock on writes to a device's journal, creating its folder.
func (f *folder) lockJournal(device string, fn func() error) error {
	dir := f.journalPath(device)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return errors.Wrap(err, "create journal folder")
	}
	return fileio.Update(filepath.Join(dir, writerLock), fn)
}

// requestDownload asks iCloud for one file.
func (f *folder) requestDownload(ctx context.Context, file string) {
	f.download(ctx, []string{file})
}

// segmentRel returns journal/<device>/<month>.jsonl, or .jsonl.gz when compressed.
func segmentRel(device, month string, compressed bool) string {
	return path.Join(journalDir, device, segmentName(month, compressed))
}

func segmentName(month string, compressed bool) string {
	if compressed {
		return month + gzipExt
	}
	return month + plainExt
}

// parseSegmentName reads <month>.jsonl or <month>.jsonl.gz.
func parseSegmentName(name string) (string, bool, bool) {
	if month, ok := strings.CutSuffix(name, gzipExt); ok && isMonth(month) {
		return month, true, true
	}
	if month, ok := strings.CutSuffix(name, plainExt); ok && isMonth(month) {
		return month, false, true
	}
	return "", false, false
}

// checkSegment rejects a device ID or month that cannot name a segment.
func checkSegment(device, month string) error {
	if !isDeviceID(device) {
		return errors.Errorf("invalid device ID %q", device)
	}
	if !isMonth(month) {
		return errors.Errorf("invalid month %q", month)
	}
	return nil
}

// isDeviceID reports whether s is 16 lowercase hex characters.
func isDeviceID(s string) bool {
	if len(s) != deviceIDLength {
		return false
	}
	for i := range len(s) {
		if !isDigit(s[i]) && (s[i] < 'a' || s[i] > 'f') {
			return false
		}
	}
	return true
}

// isMonth reports whether s is a month written as YYYY-MM.
func isMonth(s string) bool {
	if len(s) != monthLength || s[monthDash] != '-' {
		return false
	}
	for i := range len(s) {
		if i != monthDash && !isDigit(s[i]) {
			return false
		}
	}
	month := int(s[monthDash+1]-'0')*10 + int(s[monthDash+2]-'0')
	return month >= 1 && month <= maxMonth
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// placeholderPath returns the iCloud placeholder of a file: .<name>.icloud beside it.
func placeholderPath(file string) string {
	return filepath.Join(filepath.Dir(file), hiddenStart+filepath.Base(file)+placeholderExt)
}

// placeholderTarget returns <name> of a placeholder named .<name>.icloud.
func placeholderTarget(name string) (string, bool) {
	inner, ok := strings.CutPrefix(name, hiddenStart)
	if !ok {
		return "", false
	}
	target, ok := strings.CutSuffix(inner, placeholderExt)
	return target, ok && target != ""
}

// exists reports whether something is at file.
func exists(file string) bool {
	_, err := os.Lstat(file)
	return err == nil
}

// isPlaceholder reports whether a file is not downloaded yet: it is missing and its placeholder is
// there.
func isPlaceholder(file string) bool {
	return !exists(file) && exists(placeholderPath(file))
}

// readDir lists a folder; a missing folder has no entries.
func readDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "list %s", dir)
	}
	return entries, nil
}

// checkContext returns ctx's error, wrapped, once it is done.
func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "sync folder")
	}
	return nil
}
