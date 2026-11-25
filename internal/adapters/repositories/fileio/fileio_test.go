package fileio_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

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

// WriteAtomicAll replaces nothing when one of its files cannot be written, and leaves no temporary
// file behind.
func TestWriteAtomicAllWritesAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	second := filepath.Join(dir, "second.txt")
	require.NoError(t, os.WriteFile(first, []byte("old"), 0600))
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("a file, not a directory"), 0600))

	err := fileio.WriteAtomicAll([]fileio.File{
		{Path: first, Data: []byte("new")},
		{Path: filepath.Join(blocker, "third.txt"), Data: []byte("cannot be written")},
	})
	require.Error(t, err)
	got, err := os.ReadFile(first)
	require.NoError(t, err)
	assert.Equal(t, "old", string(got), "a failure before the renames replaces nothing")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "no temporary file is left behind")

	require.NoError(t, fileio.WriteAtomicAll([]fileio.File{{Path: first, Data: []byte("new")}, {Path: second, Data: []byte("2")}}))
	got, err = os.ReadFile(first)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	got, err = os.ReadFile(second)
	require.NoError(t, err)
	assert.Equal(t, "2", string(got))
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

// UpdateContext stops waiting for a lock another holder keeps once its context is done.
func TestUpdateContextStopsWaiting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.txt")
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- fileio.Update(path, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	ran := false
	err := fileio.UpdateContext(ctx, path, func() error {
		ran = true
		return nil
	})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, ran)

	close(release)
	require.NoError(t, <-done)
	require.NoError(t, fileio.UpdateContext(t.Context(), path, func() error {
		ran = true
		return nil
	}))
	assert.True(t, ran)
}
