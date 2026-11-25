package sync

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// The store merges as journal.State does, whatever the order and however records are seeded.
func TestStoreMergesLikeJournalState(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	uids := []string{
		"019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a10",
		"019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a11",
		"019a9b3c-7d2e-7c41-9a55-3e1f0b6d2a12",
	}
	devices := []string{"aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"}
	paths := []string{"start", "end", "project", "description", "tags/a", "deleted"}

	for round := range 50 {
		var changes []journal.Change
		for i := range 40 {
			set := map[string]json.RawMessage{}
			for range 1 + rng.IntN(3) {
				set[paths[rng.IntN(len(paths))]] = json.RawMessage(strconv.Quote("v" + strconv.Itoa(i)))
			}
			changes = append(changes, journal.Change{
				HLC:  journal.HLC{Wall: int64(1000 + rng.IntN(5)), Counter: rng.IntN(2)},
				Dev:  devices[rng.IntN(len(devices))],
				Type: journal.TypeActivity,
				UID:  uids[rng.IntN(len(uids))],
				Set:  set,
			})
		}

		want := journal.NewState()
		for _, change := range changes {
			want.Apply(change)
		}

		// Half the changes merged and saved, the store seeded from them, then the rest applied.
		half := journal.NewState()
		for _, change := range changes[:20] {
			half.Apply(change)
		}
		s := newStore(nil)
		s.complete = true
		for _, record := range half.Records(journal.TypeActivity) {
			s.seed(record)
		}
		for _, change := range changes[20:] {
			s.apply(change)
		}

		got := s.changed(true)
		require.Len(t, got, len(want.Records(journal.TypeActivity)), "round %d", round)
		for i, record := range want.Records(journal.TypeActivity) {
			assert.Equal(t, record.Fields, got[i].Fields, "round %d uid %s", round, record.UID)
		}
	}
}

// Fields compare at the backend's precision and are written as the journal holds them.
func TestActivityFieldsForms(t *testing.T) {
	zone := time.FixedZone("+03", 3*60*60)
	start := time.Date(2026, 11, 18, 10, 0, 37, 500, zone)
	end := start.Add(45 * time.Minute)
	a := models.Activity{
		Project:     "Backend",
		Description: "a<b",
		StartTime:   start,
		EndTime:     &end,
		Notes:       " notes \n",
		Tags:        []string{"b", " a", "b", ""},
	}

	image := activityFields(a, imageForm(time.Minute))
	imageStart, ok := parseInstant(image["start"])
	require.True(t, ok)
	assert.True(t, imageStart.Equal(time.Date(2026, 11, 18, 7, 0, 0, 0, time.UTC)), "the start at the minute")
	assert.Equal(t, `"`+imageStart.In(time.Local).Format(time.RFC3339)+`"`, string(image["start"]), "with the local offset")
	imageEnd, ok := parseInstant(image["end"])
	require.True(t, ok)
	assert.True(t, imageEnd.Equal(time.Date(2026, 11, 18, 7, 45, 0, 0, time.UTC)))
	assert.Equal(t, `"a<b"`, string(image["description"]))
	assert.Equal(t, `"notes"`, string(image["notes"]))
	assert.Equal(t, []string{"description", "end", "notes", "project", "start", "tags/a", "tags/b"}, sortedKeysOf(image))

	written := activityFields(a, journalForm())
	startText := string(written["start"])
	parsed, ok := parseInstant(written["start"])
	require.True(t, ok, startText)
	assert.True(t, parsed.Equal(start.Truncate(time.Second)))
	assert.NotContains(t, startText, ".")

	running := models.Activity{Project: "Backend", Description: "a<b", StartTime: start, Tags: []string{"a"}}
	set := diff(image, activityFields(running, imageForm(time.Minute)), activityFields(running, journalForm()))
	assert.Equal(t, "null", string(set["end"]))
	assert.Equal(t, "false", string(set["tags/b"]))
	assert.NotContains(t, set, "start")
}

func TestScopeOfRecordedWrites(t *testing.T) {
	start := time.Date(2026, 11, 18, 10, 0, 37, 0, time.UTC)
	moved := start.Add(time.Hour)
	scope := scopeOf([]recorded{
		{write: writeOf(1, start), autoStop: true},
		{write: writeOf(1, moved)},
		{write: writeOf(2, start), movedTo: &moved},
		{write: writeOf(3, moved)},
		{write: writeOf(5, time.Time{})},
	}, keying{precision: time.Minute})

	keys := keying{precision: time.Minute}
	startKey, movedKey := keys.key(start), keys.key(moved)
	assert.Equal(t, map[string]bool{startKey: true, movedKey: true}, scope.keys)
	assert.Equal(t, map[string]bool{movedKey: true}, scope.notes)
	assert.Equal(t, map[string]string{startKey: movedKey}, scope.moves)
	assert.Empty(t, scope.removed)
	assert.True(t, scope.autoStop[startKey])
	assert.True(t, scope.reminders)

	// A removal right after a save without the mark of an update is a deletion, not a move.
	scope = scopeOf([]recorded{{write: writeOf(1, moved)}, {write: writeOf(2, start)}}, keys)
	assert.Empty(t, scope.moves)
	assert.Equal(t, map[string]bool{startKey: true}, scope.removed)
}

func writeOf(kind int, start time.Time) ports.SyncWrite {
	return ports.SyncWrite{Kind: ports.SyncWriteKind(kind), Activity: models.Activity{StartTime: start}}
}

func sortedKeysOf(f fields) []string {
	out := make([]string, 0, len(f))
	for key := range f {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}
