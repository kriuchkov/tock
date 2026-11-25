package syncfolder

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
)

func TestEnsureFormatCreatesMissingFile(t *testing.T) {
	f, _ := newTestFolder(t)

	require.NoError(t, f.EnsureFormat(t.Context()))
	assertJSONLine(t, `{"format":"tock-sync","version":1}`, readFile(t, f, formatFile))
	assert.Equal(t, []string{formatFile}, tree(t, f.dir), "no temporary file is left behind")

	require.NoError(t, f.EnsureFormat(t.Context()))
	assert.Equal(t, []string{formatFile}, tree(t, f.dir))
}

func TestEnsureFormatAcceptsAnotherWriter(t *testing.T) {
	f, _ := newTestFolder(t)
	written := "{\n  \"version\": 1.0,\n  \"format\": \"tock-sync\",\n  \"note\": \"made by the app\"\n}\n"
	writeFile(t, f, formatFile, written)

	require.NoError(t, f.EnsureFormat(t.Context()))
	assert.Equal(t, written, readFile(t, f, formatFile), "format.json is never rewritten")
}

func TestEnsureFormatRefusesUnknownFormat(t *testing.T) {
	for name, content := range map[string]string{
		"newer version":  `{"format":"tock-sync","version":2}`,
		"other format":   `{"format":"other-sync","version":1}`,
		"version text":   `{"format":"tock-sync","version":"1"}`,
		"no version":     `{"format":"tock-sync"}`,
		"no format":      `{"version":1}`,
		"fraction":       `{"format":"tock-sync","version":1.5}`,
		"null":           `null`,
		"format is null": `{"format":null,"version":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := newTestFolder(t)
			writeFile(t, f, formatFile, content)

			err := f.EnsureFormat(t.Context())
			require.ErrorIs(t, err, coreErrors.ErrSyncFormatUnsupported)
			assert.Equal(t, content, readFile(t, f, formatFile))
			assert.Equal(t, []string{formatFile}, tree(t, f.dir), "nothing is written")
		})
	}
}

func TestEnsureFormatFailsOnBrokenFile(t *testing.T) {
	f, _ := newTestFolder(t)
	writeFile(t, f, formatFile, `{"format":"tock-sync",`)

	err := f.EnsureFormat(t.Context())
	require.Error(t, err)
	require.NotErrorIs(t, err, coreErrors.ErrSyncFormatUnsupported)
	assert.Equal(t, []string{formatFile}, tree(t, f.dir), "nothing is written")
}

func TestEnsureFormatWaitsForPlaceholder(t *testing.T) {
	f, asked := newTestFolder(t)
	writeFile(t, f, ".format.json.icloud", "")

	err := f.EnsureFormat(t.Context())
	require.ErrorIs(t, err, errNotDownloaded)
	assert.Equal(t, []string{".format.json.icloud"}, tree(t, f.dir), "format.json is not written over a placeholder")
	assert.Equal(t, []string{f.path(formatFile)}, asked.asked())
}

func TestEnsureFormatStopsWhenCancelled(t *testing.T) {
	f, _ := newTestFolder(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, f.EnsureFormat(ctx), context.Canceled)
	assert.NoDirExists(t, f.dir)
}

func TestCreateFileKeepsExistingFile(t *testing.T) {
	f, _ := newTestFolder(t)
	file := writeFile(t, f, "x.json", "first")

	created, err := createFile(file, []byte("second"))
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, "first", readFile(t, f, "x.json"))
	assert.Equal(t, []string{"x.json"}, tree(t, f.dir))
}
