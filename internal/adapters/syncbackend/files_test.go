package syncbackend_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/syncbackend"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// writeFile writes content at path, modified at a fixed time plus offset so a token never depends
// on the clock.
func writeFile(t *testing.T, path, content string, offset time.Duration) {
	t.Helper()
	modified := time.Date(2026, time.November, 18, 10, 0, 0, 0, time.UTC).Add(offset)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.Chtimes(path, modified, modified))
}

func token(t *testing.T, backend ports.SyncBackend) ports.SyncToken {
	t.Helper()
	tok, err := backend.Token(t.Context())
	require.NoError(t, err)
	return tok
}

func changed(t *testing.T, backend ports.SyncBackend, since, now ports.SyncToken) ports.SyncScope {
	t.Helper()
	scope, err := backend.Changed(t.Context(), since, now)
	require.NoError(t, err)
	return scope
}

func TestFileBackends(t *testing.T) {
	tests := []struct {
		name string
		open func(path, reminders string) ports.SyncBackend
	}{
		{name: syncbackend.NameFile, open: syncbackend.NewFile},
		{name: syncbackend.NameTodoTXT, open: syncbackend.NewTodoTXT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			data := filepath.Join(dir, "tock.txt")
			reminders := filepath.Join(dir, ".tock", "reminders.yaml")
			backend := tt.open(data, reminders)

			empty := token(t, backend)
			assert.Len(t, empty, 2, "a missing file is a part too")
			assert.True(t, empty.Equal(token(t, backend)))
			assert.True(t, changed(t, backend, empty, empty).IsEmpty())

			writeFile(t, data, "line\n", 0)
			withData := token(t, backend)
			assert.Equal(t, ports.SyncScope{AllActivities: true}, changed(t, backend, empty, withData))

			// An edit that keeps the size still moves the modification time.
			writeFile(t, data, "LINE\n", time.Second)
			edited := token(t, backend)
			assert.False(t, edited.Equal(withData))
			assert.Equal(t, ports.SyncScope{AllActivities: true}, changed(t, backend, withData, edited))

			writeFile(t, reminders, "reminders: []\n", 0)
			withReminders := token(t, backend)
			assert.Equal(t, ports.SyncScope{Reminders: true}, changed(t, backend, edited, withReminders))

			require.NoError(t, os.Remove(data))
			assert.Equal(t, ports.SyncScope{AllActivities: true}, changed(t, backend, withReminders, token(t, backend)))

			assert.Equal(t, ports.SyncScope{AllActivities: true, Reminders: true}, changed(t, backend, nil, withReminders),
				"an empty since covers everything")
		})
	}
}

func TestFileBackendWithoutReminders(t *testing.T) {
	dir := t.TempDir()
	backend := syncbackend.NewFile(filepath.Join(dir, "tock.txt"), "")

	tok := token(t, backend)

	assert.Len(t, tok, 1)
}

func TestFileBackendUnknownPart(t *testing.T) {
	backend := syncbackend.NewFile(filepath.Join(t.TempDir(), "tock.txt"), "")
	since := ports.SyncToken{"activities": "none", "elsewhere": "1"}
	now := ports.SyncToken{"activities": "none", "elsewhere": "2"}

	assert.Equal(t, ports.SyncScope{AllActivities: true, Reminders: true}, changed(t, backend, since, now))
}

func TestTimewarriorMonths(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	reminders := filepath.Join(dir, ".tock", "reminders.yaml")
	backend := syncbackend.NewTimewarrior(data, reminders)

	assert.Equal(t, ports.SyncToken{"reminders": "none"}, token(t, backend), "a data directory not made yet holds no months")

	writeFile(t, filepath.Join(data, "2026-10.data"), "inc 20261001T090000Z - 20261001T100000Z # Ops\n", 0)
	writeFile(t, filepath.Join(data, "2026-11.data"), "inc 20261118T090000Z # Backend\n", 0)
	// Not months: timewarrior's own files, tock's lock file and a directory.
	writeFile(t, filepath.Join(data, "undo.data"), "x\n", 0)
	writeFile(t, filepath.Join(data, "tags.data"), "{}\n", 0)
	writeFile(t, filepath.Join(data, ".2026-11.data.lock"), "", 0)
	require.NoError(t, os.MkdirAll(filepath.Join(data, "2026-09.data"), 0o750))
	before := token(t, backend)
	assert.ElementsMatch(t, []string{"activities/2026-10", "activities/2026-11", "reminders"}, keys(before))

	writeFile(t, filepath.Join(data, "2026-11.data"), "inc 20261118T090000Z - 20261118T100000Z # Backend\n", time.Second)
	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{
		span("2026-10-31T00:00:00Z", "2026-12-02T00:00:00Z"),
	}}, changed(t, backend, before, token(t, backend)), "a changed month is compared with a day around it")

	writeFile(t, filepath.Join(data, "2026-12.data"), "inc 20261201T090000Z # Backend\n", 0)
	require.NoError(t, os.Remove(filepath.Join(data, "2026-10.data")))
	writeFile(t, reminders, "reminders: []\n", 0)
	assert.Equal(t, ports.SyncScope{
		Reminders: true,
		Activities: []ports.SyncSpan{
			span("2026-09-30T00:00:00Z", "2027-01-02T00:00:00Z"),
		},
	}, changed(t, backend, before, token(t, backend)), "added, changed and removed months next to each other make one span")
}

func TestTimewarriorSeparateMonths(t *testing.T) {
	backend := syncbackend.NewTimewarrior(t.TempDir(), "")
	since := ports.SyncToken{"activities/2026-03": "10:1", "activities/2026-11": "10:1"}
	now := ports.SyncToken{"activities/2026-03": "12:2", "activities/2026-11": "12:2"}

	assert.Equal(t, ports.SyncScope{Activities: []ports.SyncSpan{
		span("2026-02-28T00:00:00Z", "2026-04-02T00:00:00Z"),
		span("2026-10-31T00:00:00Z", "2026-12-02T00:00:00Z"),
	}}, changed(t, backend, since, now))

	bad := ports.SyncToken{"activities/2026-13": "1:1"}
	assert.Equal(t, ports.SyncScope{AllActivities: true, Reminders: true}, changed(t, backend, since, bad),
		"a month part that does not parse covers everything")
}

func keys(tok ports.SyncToken) []string {
	result := make([]string, 0, len(tok))
	for key := range tok {
		result = append(result, key)
	}
	return result
}

func span(from, to string) ports.SyncSpan {
	start, err := time.Parse(time.RFC3339, from)
	if err != nil {
		panic(err)
	}
	end, err := time.Parse(time.RFC3339, to)
	if err != nil {
		panic(err)
	}
	return ports.SyncSpan{From: start, To: end}
}

// Exists looks for the data file, the timewarrior data directory and the sqlite database.
func TestExistsLooksForTheData(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "tock.txt")
	twData := filepath.Join(dir, "timewarrior")
	reminders := filepath.Join(dir, ".tock", "reminders.yaml")
	for _, backend := range []ports.SyncBackend{
		syncbackend.NewFile(data, reminders),
		syncbackend.NewTodoTXT(data, reminders),
	} {
		assert.False(t, exists(t, backend), backend.Identity().Name)
		writeFile(t, data, "line\n", 0)
		assert.True(t, exists(t, backend), backend.Identity().Name)
		require.NoError(t, os.Remove(data))
	}

	timewarrior := syncbackend.NewTimewarrior(twData, reminders)
	assert.False(t, exists(t, timewarrior), "a data directory that is not there")
	require.NoError(t, os.MkdirAll(twData, 0o750))
	assert.True(t, exists(t, timewarrior), "an empty data directory is there")

	activities, _, path := openSQLite(t)
	assert.True(t, exists(t, syncbackend.NewSQLite(activities.DB, path)))
}

func exists(t *testing.T, backend ports.SyncBackend) bool {
	t.Helper()
	ok, err := backend.Exists(t.Context())
	require.NoError(t, err)
	return ok
}
