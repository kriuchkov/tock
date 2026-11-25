package syncfolder

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	devA = "7f3a9c2e5b1d4a60"
	devB = "0123456789abcdef"
)

// downloads records the files a folder asks iCloud for.
type downloads struct {
	mu    sync.Mutex
	paths []string
}

func (d *downloads) record(_ context.Context, paths []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.paths = append(d.paths, paths...)
}

func (d *downloads) asked() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.paths...)
}

// newTestFolder returns a folder at a path in a temp dir that does not exist yet, and what it asks
// iCloud for.
func newTestFolder(t *testing.T) (*folder, *downloads) {
	t.Helper()
	asked := &downloads{}
	return &folder{dir: filepath.Join(t.TempDir(), "sync"), download: asked.record}, asked
}

// change returns a valid journal line of dev, number n, ending in "\n".
func change(dev string, n int) string {
	return fmt.Sprintf(
		`{"v":1,"hlc":"%016d.000000","dev":"%s","type":"activity","uid":"019a9b3c-7d2e-7c41-9a55-%012x","set":{"project":"P%d"}}`+"\n",
		1795025074932+n, dev, n, n,
	)
}

// writeFile writes a file of the folder, creating its parents.
func writeFile(t *testing.T, f *folder, rel, content string) string {
	t.Helper()
	file := f.path(rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o750))
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

func readFile(t *testing.T, f *folder, rel string) string {
	t.Helper()
	data, err := os.ReadFile(f.path(rel))
	require.NoError(t, err)
	return string(data)
}

func gzipped(t *testing.T, content string) string {
	t.Helper()
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.String()
}

func gunzipped(t *testing.T, data string) string {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(content)
}

// assertJSONLine checks that got is the JSON object want, with its keys in the same order, and
// "\n": the bytes tock-ios writes for the same file.
func assertJSONLine(t *testing.T, want, got string) {
	t.Helper()
	assert.JSONEq(t, want, got)
	assert.Equal(t, want+"\n", got)
}

// tree lists every file under dir as slash-separated paths, sorted.
func tree(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(file string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(dir, file)
		files = append(files, filepath.ToSlash(rel))
		return relErr
	})
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	sort.Strings(files)
	return files
}

func TestNewTouchesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sync")
	f := New(dir)
	assert.Equal(t, dir, f.Dir())
	assert.NoDirExists(t, dir)
}

func TestNamesOfTheFolder(t *testing.T) {
	for _, id := range []string{devA, devB} {
		assert.True(t, isDeviceID(id), id)
	}
	for _, id := range []string{"", "7F3A9C2E5B1D4A60", "7f3a9c2e5b1d4a6", "7f3a9c2e5b1d4a600", "7f3a9c2e5b1d4a6g", "../7f3a9c2e5b1d4"} {
		assert.False(t, isDeviceID(id), id)
	}
	for _, month := range []string{"2026-01", "2026-12", "0000-01"} {
		assert.True(t, isMonth(month), month)
	}
	for _, month := range []string{"", "2026-00", "2026-13", "2026-1", "2026_11", "20261-1", "2026-1a", "../2026"} {
		assert.False(t, isMonth(month), month)
	}

	month, compressed, ok := parseSegmentName("2026-11.jsonl.gz")
	assert.True(t, ok)
	assert.True(t, compressed)
	assert.Equal(t, "2026-11", month)
	for _, name := range []string{".2026-11.jsonl.icloud", "2026-11.json", "2026-11.jsonl.tmp", "2026-13.jsonl", "x2026-11.jsonl"} {
		_, _, ok = parseSegmentName(name)
		assert.False(t, ok, name)
	}

	target, ok := placeholderTarget(".2026-11.jsonl.icloud")
	assert.True(t, ok)
	assert.Equal(t, "2026-11.jsonl", target)
	for _, name := range []string{"2026-11.jsonl.icloud", ".icloud", "..icloud", ".writer.lock"} {
		_, ok = placeholderTarget(name)
		assert.False(t, ok, name)
	}
}
