package todotxt_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/todotxt"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// Apply addresses each line by everything it holds and writes the file once.
func TestApplyAddressesOneLine(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "todo.txt")
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	a := models.Activity{Project: "A", Description: "first", StartTime: start, EndTime: new(start)}
	b := models.Activity{Project: "B", Description: "second", StartTime: start, EndTime: new(start.Add(40 * time.Minute))}
	require.NoError(t, os.WriteFile(path, []byte(todotxt.FormatActivity(a)+"\n"+todotxt.FormatActivity(b)+"\n"), 0o600))

	repo := todotxt.NewRepository(path)
	batch, ok := repo.(ports.ActivityBatchWriter)
	require.True(t, ok)
	edited := b
	edited.Description = "second edited"
	require.NoError(t, batch.Apply(ctx, []ports.ActivityWrite{{Old: &a}, {Old: &b, New: &edited}}))

	all, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, "second edited", all[0].Description)
	require.ErrorIs(t, batch.Apply(ctx, []ports.ActivityWrite{{Old: &b}}), coreErrors.ErrActivityNotFound)
}

func TestNormalizeRoundTrips(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 37, 500, time.UTC)
	a := models.Activity{Project: "Back end", Description: "a +b @c\nnext", StartTime: start, Tags: []string{"x"}, Notes: "kept", UID: "u"}
	parsed, err := todotxt.ParseActivity(todotxt.FormatActivity(a))
	require.NoError(t, err)
	got := todotxt.Normalize(a)
	assert.Equal(t, parsed.Description, got.Description)
	assert.Equal(t, parsed.Project, got.Project)
	assert.True(t, got.StartTime.Equal(start.Truncate(time.Second)))
	assert.Equal(t, "kept", got.Notes)
	assert.Equal(t, "u", got.UID)
}
