package notes_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/notes"
	"github.com/kriuchkov/tock/internal/core/ports"
)

func TestRepository_SaveAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	repo := notes.NewRepository(tmpDir)
	ctx := context.Background()

	now := time.Now()
	activityID := "123456"
	noteContent := "This is a test note."
	tags := []string{"work", "important"}

	// Test Save
	err := repo.Save(ctx, activityID, now, noteContent, tags)
	require.NoError(t, err)

	// Verify file exists
	dateDir := now.Format("2006-01-02")
	expectedPath := filepath.Join(tmpDir, dateDir, activityID+".txt")
	assert.FileExists(t, expectedPath)

	// Test Get
	retrievedNotes, retrievedTags, err := repo.Get(ctx, activityID, now)
	require.NoError(t, err)
	assert.Equal(t, noteContent, retrievedNotes)
	assert.Equal(t, tags, retrievedTags)
}

func TestRepository_Get_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	repo := notes.NewRepository(tmpDir)
	ctx := context.Background()

	now := time.Now()
	activityID := "nonexistent"

	retrievedNotes, retrievedTags, err := repo.Get(ctx, activityID, now)
	require.NoError(t, err)
	assert.Empty(t, retrievedNotes)
	assert.Nil(t, retrievedTags)
}

func TestRepository_Save_NoTags(t *testing.T) {
	tmpDir := t.TempDir()
	repo := notes.NewRepository(tmpDir)
	ctx := context.Background()

	now := time.Now()
	activityID := "notags"
	noteContent := "Just a note."

	err := repo.Save(ctx, activityID, now, noteContent, nil)
	require.NoError(t, err)

	retrievedNotes, retrievedTags, err := repo.Get(ctx, activityID, now)
	require.NoError(t, err)
	assert.Equal(t, noteContent, retrievedNotes)
	assert.Empty(t, retrievedTags)
}

func TestRepository_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	repo := notes.NewRepository(tmpDir)
	ctx := context.Background()

	now := time.Now()
	activityID := "090000"

	err := repo.Save(ctx, activityID, now, "note", []string{"test"})
	require.NoError(t, err)

	err = repo.Delete(ctx, activityID, now)
	require.NoError(t, err)

	dateDir := now.Format("2006-01-02")
	assert.NoFileExists(t, filepath.Join(tmpDir, dateDir, activityID+".txt"))

	retrievedNotes, retrievedTags, err := repo.Get(ctx, activityID, now)
	require.NoError(t, err)
	assert.Empty(t, retrievedNotes)
	assert.Nil(t, retrievedTags)
}

func TestRepository_Delete_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	repo := notes.NewRepository(tmpDir)
	ctx := context.Background()

	err := repo.Delete(ctx, "nonexistent", time.Now())
	require.NoError(t, err)
}

func TestRepository_Save_Update(t *testing.T) {
	tmpDir := t.TempDir()
	repo := notes.NewRepository(tmpDir)
	ctx := context.Background()

	now := time.Now()
	activityID := "update_test"

	// Initial save
	err := repo.Save(ctx, activityID, now, "Initial note", []string{"tag1"})
	require.NoError(t, err)

	// Update
	newNote := "Updated note"
	newTags := []string{"tag1", "tag2"}
	err = repo.Save(ctx, activityID, now, newNote, newTags)
	require.NoError(t, err)

	// Verify update
	retrievedNotes, retrievedTags, err := repo.Get(ctx, activityID, now)
	require.NoError(t, err)
	assert.Equal(t, newNote, retrievedNotes)
	assert.Equal(t, newTags, retrievedTags)
}

// Lookup tells an activity without a notes file from one whose notes were cleared, which leaves an
// empty file.
func TestRepository_LookupTellsMissingFromCleared(t *testing.T) {
	repo := notes.NewRepository(t.TempDir())
	lookup, ok := repo.(ports.NotesLookup)
	require.True(t, ok)
	ctx := context.Background()
	day := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)

	text, tags, found, err := lookup.Lookup(ctx, "090000", day)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, text)
	assert.Empty(t, tags)

	require.NoError(t, repo.Save(ctx, "090000", day, "note", []string{"x"}))
	text, tags, found, err = lookup.Lookup(ctx, "090000", day)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "note", text)
	assert.Equal(t, []string{"x"}, tags)

	require.NoError(t, repo.Save(ctx, "090000", day, "", nil))
	text, tags, found, err = lookup.Lookup(ctx, "090000", day)
	require.NoError(t, err)
	assert.True(t, found, "cleared notes leave an empty file")
	assert.Empty(t, text)
	assert.Empty(t, tags)
}
