package file_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/file"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

func minute(m int) time.Time {
	return time.Date(2026, 11, 18, 9, m, 0, 0, time.Local)
}

// Apply addresses each line by everything it holds, so of two lines in one minute only the one meant
// changes, and the file is read and written once.
func TestApplyAddressesOneLineOfAMinute(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "tock.txt")
	require.NoError(t, os.WriteFile(path, []byte(
		"2026-11-18 09:00 - 2026-11-18 09:00 | A | first\n2026-11-18 09:00 - 2026-11-18 09:40 | B | second\n\n"+
			"2026-11-18 10:00 - 2026-11-18 10:30 | C | third\n"), 0o600))
	repo := file.NewRepository(path)
	batch, ok := repo.(ports.ActivityBatchWriter)
	require.True(t, ok)

	a := models.Activity{Project: "A", Description: "first", StartTime: minute(0), EndTime: new(minute(0))}
	b := models.Activity{Project: "B", Description: "second", StartTime: minute(0), EndTime: new(minute(40))}
	c := models.Activity{Project: "C", Description: "third", StartTime: minute(60), EndTime: new(minute(90))}
	edited := b
	edited.Description = "second edited"
	added := models.Activity{Project: "D", Description: "new", StartTime: minute(0)}
	require.NoError(t, batch.Apply(ctx, []ports.ActivityWrite{
		{Old: &a, New: nil},
		{Old: &b, New: &edited},
		{Old: &c},
		{New: &added},
	}))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-18 09:00 - 2026-11-18 09:40 | B | second edited\n\n2026-11-18 09:00 | D | new\n", string(raw))

	// A write whose activity is no longer there writes nothing.
	err = batch.Apply(ctx, []ports.ActivityWrite{{New: &a}, {Old: &b}})
	require.ErrorIs(t, err, coreErrors.ErrActivityNotFound)
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-18 09:00 - 2026-11-18 09:40 | B | second edited\n\n2026-11-18 09:00 | D | new\n", string(raw))
}

// Normalize gives what the file gives back: a minute, local time, one line.
func TestNormalize(t *testing.T) {
	zone := time.FixedZone("+05", 5*60*60)
	start := time.Date(2026, 11, 18, 9, 0, 37, 0, zone)
	got := file.Normalize(models.Activity{
		Project: "Backend", Description: "first\nsecond", StartTime: start, EndTime: new(start.Add(time.Hour)),
		Notes: "kept", Tags: []string{"kept"}, UID: "uid",
	})
	assert.Equal(t, "first second", got.Description)
	assert.True(t, got.StartTime.Equal(start.Truncate(time.Minute)))
	assert.Equal(t, time.Local, got.StartTime.Location())
	assert.Equal(t, "kept", got.Notes)
	assert.Equal(t, []string{"kept"}, got.Tags)
	assert.Equal(t, "uid", got.UID)

	piped := file.Normalize(models.Activity{Project: "Backend", Description: "Review PR | auth", StartTime: start})
	parsed, err := file.ParseActivity(
		file.FormatActivity(models.Activity{Project: "Backend", Description: "Review PR | auth", StartTime: start}),
	)
	require.NoError(t, err)
	assert.Equal(t, parsed.Description, piped.Description)
}
