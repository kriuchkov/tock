package fileio_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/fileio"
)

func TestWriteAtomicCreatesAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data.txt")

	require.NoError(t, fileio.WriteAtomic(path, []byte("one\n")))
	require.NoError(t, fileio.WriteAtomic(path, []byte("two\n")))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "two\n", string(got))

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left behind")
}

func TestWriteAtomicKeepsSymlinkAndMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0600))
	require.NoError(t, os.Symlink(target, link))

	require.NoError(t, fileio.WriteAtomic(link, []byte("new")))

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link stays a link")

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))

	info, err = os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestUpdateSerializesReadModifyWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "counter.txt")
	require.NoError(t, fileio.WriteAtomic(path, []byte("0")))

	const workers = 20
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			err := fileio.Update(path, func() error {
				raw, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
				if err != nil {
					return err
				}
				return fileio.WriteAtomic(path, []byte(strconv.Itoa(n+1)))
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(workers), string(got))
	assert.FileExists(t, filepath.Join(filepath.Dir(path), ".counter.txt.lock"), "the lock file is hidden")
}
