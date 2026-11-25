package syncstate

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// scaleRecords is the size of the history docs/sync.md measures with.
const scaleRecords = 100_000

// syntheticHistory makes n stopped activities of six fields each, one an hour, and their images.
func syntheticHistory(n int) ([]journal.Record, []ports.SyncImage) {
	base := time.Date(2015, 1, 1, 9, 0, 0, 0, time.UTC)
	records := make([]journal.Record, n)
	images := make([]ports.SyncImage, n)
	for i := range n {
		start := base.Add(time.Duration(i) * time.Hour)
		startJSON := json.RawMessage(`"` + start.Format(time.RFC3339) + `"`)
		endJSON := json.RawMessage(`"` + start.Add(45*time.Minute).Format(time.RFC3339) + `"`)
		project := json.RawMessage(fmt.Sprintf(`"Project %d"`, i%40))
		description := json.RawMessage(fmt.Sprintf(`"Task number %d"`, i))
		wall := start.UnixMilli()
		dev := devA
		if i%3 == 0 {
			dev = devB
		}
		records[i] = journal.Record{Type: journal.TypeActivity, UID: uid(i), Fields: map[string]journal.Field{
			"start":       {Value: startJSON, HLC: hlc(wall, 0), Dev: dev},
			"end":         {Value: endJSON, HLC: hlc(wall+45*60*1000, 0), Dev: dev},
			"project":     {Value: project, HLC: hlc(wall, 0), Dev: dev},
			"description": {Value: description, HLC: hlc(wall, 0), Dev: dev},
			"notes":       {Value: json.RawMessage(`""`), HLC: hlc(wall, 0), Dev: dev},
			"tags/work":   {Value: json.RawMessage(`true`), HLC: hlc(wall, 1), Dev: dev},
		}}
		images[i] = ports.SyncImage{
			Type: journal.TypeActivity, UID: uid(i), Key: start.Format(time.RFC3339),
			Fields: map[string]json.RawMessage{
				"start": startJSON, "end": endJSON, "project": project, "description": description,
				"notes": json.RawMessage(`""`), "tags/work": json.RawMessage(`true`),
			},
		}
	}
	return records, images
}

// TestScale saves and loads a history of 100,000 activities and logs how long each step takes; run
// it with -v to see the timings. Only the results are checked, not the times.
func TestScale(t *testing.T) {
	if testing.Short() {
		t.Skip("saves 100,000 records")
	}
	ctx := t.Context()
	path := statePath(t)
	st := newState(t, path)
	records, images := syntheticHistory(scaleRecords)
	meta := fullMeta()

	measure := func(name string, fn func()) {
		started := time.Now()
		fn()
		t.Logf("%-40s %8.1f ms", name, float64(time.Since(started).Microseconds())/1000)
	}

	measure("save 100k records + 100k images (reset)", func() {
		require.NoError(t, st.Save(ctx, ports.SyncDelta{Reset: true, Meta: &meta, Records: records, Images: images}))
	})
	info, err := os.Stat(path)
	require.NoError(t, err)
	t.Logf("%-40s %8.1f MB", "state.db size", float64(info.Size())/(1<<20))

	fresh := newState(t, path)
	measure("open + meta (another process)", func() {
		_, err = fresh.Meta(ctx)
		require.NoError(t, err)
	})
	measure("all 100k records", func() {
		got, loadErr := fresh.AllRecords(ctx, journal.TypeActivity)
		require.NoError(t, loadErr)
		require.Len(t, got, scaleRecords)
		assert.Equal(t, records[scaleRecords-1], got[scaleRecords-1])
	})
	measure("all 100k images", func() {
		got, loadErr := fresh.Images(ctx, journal.TypeActivity)
		require.NoError(t, loadErr)
		require.Len(t, got, scaleRecords)
	})
	uids := make([]string, 0, 200)
	keys := make([]string, 0, 200)
	for i := 0; i < scaleRecords; i += scaleRecords / 200 {
		uids = append(uids, records[i].UID)
		keys = append(keys, images[i].Key)
	}
	measure("200 records by uid", func() {
		got, loadErr := fresh.Records(ctx, journal.TypeActivity, uids)
		require.NoError(t, loadErr)
		require.Len(t, got, len(uids))
	})
	measure("200 images by key", func() {
		got, loadErr := fresh.ImagesByKey(ctx, journal.TypeActivity, keys)
		require.NoError(t, loadErr)
		require.Len(t, got, len(keys))
	})
	measure("running activities", func() {
		got, loadErr := fresh.RunningActivities(ctx)
		require.NoError(t, loadErr)
		require.Empty(t, got)
	})

	// A usual sync: 20 changed records, their images, the meta and a cursor.
	changed := make([]journal.Record, 20)
	for i := range changed {
		changed[i] = records[scaleRecords-1-i]
		changed[i].Fields = maps.Clone(changed[i].Fields)
		delete(changed[i].Fields, "end")
	}
	for _, name := range []string{"save a 20-record delta", "save another 20-record delta"} {
		measure(name, func() {
			require.NoError(t, fresh.Save(ctx, ports.SyncDelta{
				Meta: &meta, Records: changed, Images: images[:20],
				Cursors: []ports.SyncCursor{{Device: devB, Month: "2026-11", Offset: 1 << 20, Size: 1 << 20}},
			}))
		})
	}
	measure("running activities after it", func() {
		got, loadErr := fresh.RunningActivities(ctx)
		require.NoError(t, loadErr)
		require.Len(t, got, len(changed))
	})
	wal, err := os.Stat(path + "-wal")
	require.NoError(t, err)
	t.Logf("%-40s %8.1f MB", "WAL size after them", float64(wal.Size())/(1<<20))
}
