package syncfolder

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	appendBatches   = 40
	appendLinesEach = 3
	// childFolderEnv tells the test binary run by TestAppendFromTwoProcesses which folder to append to.
	childFolderEnv = "TOCK_SYNCFOLDER_TEST_FOLDER"
	childWriterEnv = "TOCK_SYNCFOLDER_TEST_WRITER"
)

// batch returns the lines a writer appends in one call: lines of different lengths, so a write that
// interleaves with another shows.
func batch(writer string, n int) string {
	var lines strings.Builder
	for i := range appendLinesEach {
		fmt.Fprintf(&lines, "%s %d %d %s\n", writer, n, i, strings.Repeat("x", (n*7+i*13)%300))
	}
	return lines.String()
}

// checkBatches checks that the journal holds every batch of every writer, each one whole and in a
// piece, and those of one writer in order.
func checkBatches(t *testing.T, content string, writers []string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	require.Len(t, lines, len(writers)*appendBatches*appendLinesEach)
	next := make(map[string]int)
	for i := 0; i < len(lines); i += appendLinesEach {
		writer, _, ok := strings.Cut(lines[i], " ")
		require.True(t, ok, lines[i])
		got := strings.Join(lines[i:i+appendLinesEach], "\n") + "\n"
		assert.Equal(t, batch(writer, next[writer]), got, "batch %d of %s", next[writer], writer)
		next[writer]++
	}
	for _, writer := range writers {
		assert.Equal(t, appendBatches, next[writer], writer)
	}
}

// Two folder values, as the CLI and the menu bar icon would hold, append to the same month at once.
func TestAppendFromTwoInstances(t *testing.T) {
	first, _ := newTestFolder(t)
	second := &folder{dir: first.dir, download: first.download}
	writers := []string{"cli-1", "cli-2", "tray-1", "tray-2"}

	var wg sync.WaitGroup
	for i, writer := range writers {
		target := first
		if i%2 == 1 {
			target = second
		}
		wg.Go(func() {
			for n := range appendBatches {
				assert.NoError(t, target.Append(t.Context(), devA, "2026-11", []byte(batch(writer, n))))
			}
		})
	}
	wg.Wait()

	checkBatches(t, readFile(t, first, "journal/"+devA+"/2026-11.jsonl"), writers)
}

// Compressing a month while another one is appended to loses no line of either.
func TestCompressWhileAppending(t *testing.T) {
	first, _ := newTestFolder(t)
	second := &folder{dir: first.dir, download: first.download}
	for n := range appendBatches {
		require.NoError(t, first.Append(t.Context(), devA, "2026-10", []byte(batch("old", n))))
	}

	var wg sync.WaitGroup
	wg.Go(func() {
		for n := range appendBatches {
			assert.NoError(t, first.Append(t.Context(), devA, "2026-11", []byte(batch("new", n))))
		}
	})
	wg.Go(func() {
		assert.NoError(t, second.Compress(t.Context(), devA, "2026-10"))
	})
	wg.Wait()

	checkBatches(t, gunzipped(t, readFile(t, first, "journal/"+devA+"/2026-10.jsonl.gz")), []string{"old"})
	checkBatches(t, readFile(t, first, "journal/"+devA+"/2026-11.jsonl"), []string{"new"})
}

// Two tock processes append to the same month at once: this test runs its own binary twice.
func TestAppendFromTwoProcesses(t *testing.T) {
	if dir := os.Getenv(childFolderEnv); dir != "" {
		appendAsChild(t, dir, os.Getenv(childWriterEnv))
		return
	}
	f, _ := newTestFolder(t)
	writers := []string{"process-1", "process-2"}

	children := make([]*exec.Cmd, len(writers))
	for i, writer := range writers {
		child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAppendFromTwoProcesses$", "-test.count=1")
		child.Env = append(os.Environ(), childFolderEnv+"="+f.dir, childWriterEnv+"="+writer)
		child.Stdout = os.Stderr
		child.Stderr = os.Stderr
		require.NoError(t, child.Start())
		children[i] = child
	}
	for _, child := range children {
		require.NoError(t, child.Wait())
	}

	checkBatches(t, readFile(t, f, "journal/"+devA+"/2026-11.jsonl"), writers)
}

func appendAsChild(t *testing.T, dir, writer string) {
	t.Helper()
	f := New(dir)
	for n := range appendBatches {
		require.NoError(t, f.Append(t.Context(), devA, "2026-11", []byte(batch(writer, n))), strconv.Itoa(n))
	}
}
