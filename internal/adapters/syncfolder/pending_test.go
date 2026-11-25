package syncfolder

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPendingListsPlaceholders(t *testing.T) {
	f, asked := newTestFolder(t)
	writeFile(t, f, ".format.json.icloud", "")
	writeFile(t, f, "devices/."+devB+".json.icloud", "")
	writeFile(t, f, "devices/.MacBook.json.icloud", "")
	writeFile(t, f, "devices/"+devA+".json", "{}")
	writeFile(t, f, "devices/."+devA+".json.icloud", "")
	writeFile(t, f, "journal/"+devB+"/.2026-11.jsonl.icloud", "")
	writeFile(t, f, "journal/"+devB+"/.2026-10.jsonl.gz.icloud", "")
	writeFile(t, f, "journal/"+devB+"/.notes.txt.icloud", "")
	writeFile(t, f, "journal/"+devA+"/2026-11.jsonl", change(devA, 1))
	writeFile(t, f, "journal/"+devA+"/.2026-11.jsonl.icloud", "")
	writeFile(t, f, "journal/not-a-device/.2026-11.jsonl.icloud", "")

	pending, err := f.Pending(t.Context())
	require.NoError(t, err)
	want := []string{
		"devices/" + devB + ".json",
		"format.json",
		"journal/" + devB + "/2026-10.jsonl.gz",
		"journal/" + devB + "/2026-11.jsonl",
	}
	assert.Equal(t, want, pending)
	paths := make([]string, len(want))
	for i, rel := range want {
		paths[i] = f.path(rel)
	}
	assert.Equal(t, paths, asked.asked(), "iCloud is asked for each")

	segments, err := f.Segments(t.Context())
	require.NoError(t, err)
	require.Len(t, segments, 1, "listings leave placeholders out")
	assert.Equal(t, devA, segments[0].Device)
}

func TestPendingOfMissingFolder(t *testing.T) {
	f, asked := newTestFolder(t)

	pending, err := f.Pending(t.Context())
	require.NoError(t, err)
	assert.Empty(t, pending)
	assert.Empty(t, asked.asked())
}
