package timewarrior

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// Find with no bounds reads every month file, however far from now.
func TestFindReadsEveryMonthFile(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(dir)
	ctx := t.Context()
	old := time.Date(1999, 3, 1, 10, 0, 0, 0, time.UTC)
	far := time.Date(2031, 5, 1, 10, 0, 0, 0, time.UTC)
	for _, start := range []time.Time{old, far} {
		require.NoError(
			t,
			repo.Save(ctx, models.Activity{Project: "Edge", Description: "x", StartTime: start, EndTime: new(start.Add(time.Hour))}),
		)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.data"), []byte("not a month\n"), 0o600))

	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.True(t, all[0].StartTime.Equal(old))
	assert.True(t, all[1].StartTime.Equal(far))

	to := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	early, err := repo.Find(ctx, models.ActivityFilter{ToDate: &to})
	require.NoError(t, err)
	require.Len(t, early, 1)
	assert.True(t, early[0].StartTime.Equal(old))
}

// Apply addresses an interval by everything it holds and moves one across months.
func TestApplyAcrossMonths(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(dir)
	ctx := t.Context()
	start := time.Date(2026, 11, 30, 23, 0, 0, 0, time.UTC)
	a := models.Activity{Project: "A", Description: "first", StartTime: start, EndTime: new(start)}
	b := models.Activity{Project: "B", Description: "second", StartTime: start, EndTime: new(start.Add(30 * time.Minute))}
	require.NoError(t, repo.Save(ctx, a))
	month := filepath.Join(dir, "2026-11.data")
	raw, err := os.ReadFile(month)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(month, append(raw, []byte(formatIncLine(toTWInterval(b)))...), 0o600))

	batch, ok := repo.(ports.ActivityBatchWriter)
	require.True(t, ok)
	moved := a
	moved.StartTime = start.Add(2 * time.Hour)
	moved.EndTime = new(moved.StartTime.Add(time.Hour))
	require.NoError(t, batch.Apply(ctx, []ports.ActivityWrite{{Old: &a, New: &moved}}))

	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, "B", all[0].Project, "the other interval of the second stays")
	assert.Equal(t, "A", all[1].Project)
	assert.True(t, all[1].StartTime.Equal(moved.StartTime))
	_, err = os.Stat(filepath.Join(dir, "2026-12.data"))
	require.NoError(t, err)

	require.ErrorIs(t, batch.Apply(ctx, []ports.ActivityWrite{{Old: &a}}), coreErrors.ErrActivityNotFound)
}

// Apply writes nothing when one Old is not there, in whichever month file: a batch that moves an
// activity from November to December and edits a December interval timew changed meanwhile leaves
// both files as they were.
func TestApplyIsAllOrNothingAcrossMonths(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(dir)
	ctx := t.Context()
	nov := time.Date(2026, 11, 30, 10, 0, 0, 0, time.UTC)
	moved := models.Activity{Project: "A", Description: "moved", StartTime: nov, EndTime: new(nov.Add(time.Hour))}
	require.NoError(t, repo.Save(ctx, moved))
	dec := time.Date(2026, 12, 2, 10, 0, 0, 0, time.UTC)
	kept := models.Activity{Project: "C", Description: "kept", StartTime: dec.Add(time.Hour), EndTime: new(dec.Add(2 * time.Hour))}
	require.NoError(t, repo.Save(ctx, kept))
	gone := models.Activity{Project: "B", Description: "edited by timew", StartTime: dec, EndTime: new(dec.Add(time.Hour))}

	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 2)
	target := all[0]
	target.StartTime = time.Date(2026, 12, 1, 10, 0, 0, 0, time.UTC)
	target.EndTime = new(target.StartTime.Add(time.Hour))
	edit := gone
	edit.Description = "phone edit"

	err = repo.(ports.ActivityBatchWriter).Apply(ctx, []ports.ActivityWrite{{Old: &all[0], New: &target}, {Old: &gone, New: &edit}})
	require.ErrorIs(t, err, coreErrors.ErrActivityNotFound)
	after, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, after, 2, "nothing was written")
	assert.Equal(t, "moved", after[0].Description)
	assert.True(t, after[0].StartTime.Equal(nov), "the moved activity is still in November")
	assert.Equal(t, "kept", after[1].Description)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotContains(t, entry.Name(), ".tmp", "no temporary file is left behind")
	}
}

// tock files an interval under the month of its local start: east of UTC, one in the first hours of a
// month's first day sits in that month's file while its UTC start lies in the month before. A span of
// one second around that UTC start still finds it.
func TestFindReadsTheLocalMonthOfAShortSpan(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	dir := t.TempDir()
	repo := NewRepository(dir)
	ctx := t.Context()
	start := time.Date(2026, 12, 1, 8, 0, 0, 0, tokyo)
	early := models.Activity{Project: "Mac", Description: "early", StartTime: start, EndTime: new(start.Add(time.Hour))}
	require.NoError(t, repo.Save(ctx, early))
	_, err = os.Stat(filepath.Join(dir, "2026-12.data"))
	require.NoError(t, err, "filed under its local month")

	from, to := start.UTC().Add(-time.Second), start.UTC().Add(time.Second)
	found, err := repo.Find(ctx, models.ActivityFilter{FromDate: &from, ToDate: &to})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.True(t, found[0].StartTime.Equal(start))
}

// After a change of the Mac's time zone, an interval filed under its local month in the old zone may
// read as one of another month: Apply finds it in the file it is in, and files the new interval under
// its month in the zone the Mac is in now.
func TestApplyFindsAnIntervalFiledInAnotherZone(t *testing.T) {
	moscow, err := time.LoadLocation("Europe/Moscow")
	require.NoError(t, err)
	london, err := time.LoadLocation("Europe/London")
	require.NoError(t, err)
	dir := t.TempDir()
	repo := NewRepository(dir)
	ctx := t.Context()
	start := time.Date(2026, 12, 1, 1, 30, 0, 0, moscow)
	filed := models.Activity{Project: "Mac", Description: "late", StartTime: start, EndTime: new(start.Add(30 * time.Minute))}
	require.NoError(t, repo.Save(ctx, filed))

	// As read in London: November 30, 22:30.
	old := filed
	old.StartTime, old.EndTime = start.In(london), new(filed.EndTime.In(london))
	renamed := old
	renamed.Description = "renamed"
	require.NoError(t, repo.(ports.ActivityBatchWriter).Apply(ctx, []ports.ActivityWrite{{Old: &old, New: &renamed}}))

	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "renamed", all[0].Description)
	assert.True(t, all[0].StartTime.Equal(start))
	nov, err := os.ReadFile(filepath.Join(dir, "2026-11.data"))
	require.NoError(t, err)
	assert.Contains(t, string(nov), "renamed", "filed under its month in London")
	dec, err := os.ReadFile(filepath.Join(dir, "2026-12.data"))
	require.NoError(t, err)
	assert.Empty(t, string(dec))
}

// The month files a batch adds to are written before those it only removes from.
func TestWriteRankPutsAdditionsFirst(t *testing.T) {
	a := &models.Activity{}
	iv := &twInterval{}
	assert.Less(t, writeRank([]monthWrite{{new: iv}}), writeRank([]monthWrite{{old: a}}))
	assert.Less(t, writeRank([]monthWrite{{old: a, new: iv}}), writeRank([]monthWrite{{new: iv}, {old: a}}))
	assert.Less(t, writeRank([]monthWrite{{new: iv}, {old: a}}), writeRank([]monthWrite{{old: a}}))
}

// Normalize gives what the data files give back for values a line does not keep as they are.
func TestNormalizeRoundTrips(t *testing.T) {
	dir := t.TempDir()
	repo := NewRepository(dir)
	ctx := t.Context()
	start := time.Date(2026, 11, 18, 9, 0, 37, 500, time.UTC)
	a := models.Activity{
		Project:     "Backend",
		Description: `say "hi" to C:\temp` + "\nnext",
		StartTime:   start,
		EndTime:     new(start.Add(time.Hour)),
	}
	require.NoError(t, repo.Save(ctx, a))
	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)

	got := Normalize(a)
	assert.Equal(t, all[0].Description, got.Description)
	assert.Equal(t, all[0].Project, got.Project)
	assert.True(t, all[0].StartTime.Equal(got.StartTime))
}

// tock stop in Dubai, of an activity started at 23:30 on November 30 in Moscow: Save finds it in the
// November file and moves it to December's, its month in Dubai; it does not add a second, still running.
func TestSaveFindsAnIntervalFiledInAnotherZone(t *testing.T) {
	moscow, err := time.LoadLocation("Europe/Moscow")
	require.NoError(t, err)
	dubai, err := time.LoadLocation("Asia/Dubai")
	require.NoError(t, err)
	dir := t.TempDir()
	repo := NewRepository(dir)
	ctx := t.Context()
	start := time.Date(2026, 11, 30, 23, 30, 0, 0, moscow)
	require.NoError(t, repo.Save(ctx, models.Activity{Project: "P", Description: "d", StartTime: start}))

	stopped := models.Activity{Project: "P", Description: "d", StartTime: start.In(dubai), EndTime: new(start.Add(time.Hour).In(dubai))}
	require.NoError(t, repo.Save(ctx, stopped))

	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.NotNil(t, all[0].EndTime)
	assert.True(t, all[0].EndTime.Equal(*stopped.EndTime))
	nov, err := os.ReadFile(filepath.Join(dir, "2026-11.data"))
	require.NoError(t, err)
	assert.Empty(t, string(nov))
	dec, err := os.ReadFile(filepath.Join(dir, "2026-12.data"))
	require.NoError(t, err)
	assert.Contains(t, string(dec), "P")
}

// Remove in Dubai finds an interval filed under its month in Moscow.
func TestRemoveFindsAnIntervalFiledInAnotherZone(t *testing.T) {
	moscow, err := time.LoadLocation("Europe/Moscow")
	require.NoError(t, err)
	dubai, err := time.LoadLocation("Asia/Dubai")
	require.NoError(t, err)
	repo := NewRepository(t.TempDir())
	ctx := t.Context()
	start := time.Date(2026, 11, 30, 23, 30, 0, 0, moscow)
	filed := models.Activity{Project: "P", Description: "d", StartTime: start, EndTime: new(start.Add(time.Hour))}
	require.NoError(t, repo.Save(ctx, filed))

	read := filed
	read.StartTime, read.EndTime = start.In(dubai), new(filed.EndTime.In(dubai))
	require.NoError(t, repo.Remove(ctx, read))
	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	assert.Empty(t, all)
}

// A stop in London moved an interval filed in Tokyo from December's file to November's, and a crash
// between the two renames left it in both. The next Save leaves one, in November's file; Remove then
// removes it from every file.
func TestSaveAndRemoveClearAnIntervalLeftInTwoMonthFiles(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	require.NoError(t, err)
	london, err := time.LoadLocation("Europe/London")
	require.NoError(t, err)
	dir := t.TempDir()
	repo := NewRepository(dir)
	ctx := t.Context()
	start := time.Date(2026, 12, 1, 8, 0, 0, 0, tokyo)
	require.NoError(t, repo.Save(ctx, models.Activity{Project: "P", Description: "d", StartTime: start}))
	dec := filepath.Join(dir, "2026-12.data")
	running, err := os.ReadFile(dec)
	require.NoError(t, err)

	stopped := models.Activity{Project: "P", Description: "d", StartTime: start.In(london), EndTime: new(start.Add(time.Hour).In(london))}
	require.NoError(t, repo.Save(ctx, stopped))
	require.NoError(t, os.WriteFile(dec, running, 0o600))
	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 2, "left in both files")

	edited := stopped
	edited.Description = "edited"
	require.NoError(t, repo.Save(ctx, edited))
	all, err = repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "edited", all[0].Description)
	data, err := os.ReadFile(dec)
	require.NoError(t, err)
	assert.Empty(t, string(data))

	require.NoError(t, os.WriteFile(dec, running, 0o600))
	require.NoError(t, repo.Remove(ctx, edited))
	all, err = repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	assert.Empty(t, all)
}
