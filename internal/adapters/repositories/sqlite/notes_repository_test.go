package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestNotesRepository_SaveAndGet(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test_notes.db")

	repo, err := NewSQLiteActivityRepository(ctx, dbPath)
	require.NoError(t, err)
	defer repo.DB.Close()

	notesRepo := NewNotesRepository(repo.DB)
	now := time.Now().Truncate(time.Second)

	tests := []struct {
		name       string
		activity   models.Activity
		activityID string
		notes      string
		tags       []string
	}{
		{
			name: "basic notes and tags",
			activity: models.Activity{
				Description: "Basic",
				Project:     "P1",
				StartTime:   now,
			},
			activityID: "id1",
			notes:      "These are my notes",
			tags:       []string{"tag1", "tag2"},
		},
		{
			name: "different time, different notes",
			activity: models.Activity{
				Description: "Different Time",
				Project:     "P2",
				StartTime:   now.Add(-1 * time.Hour),
			},
			activityID: "any-id",
			notes:      "Note 2",
			tags:       []string{"tag3"},
		},
		{
			name: "empty notes and tags",
			activity: models.Activity{
				Description: "Empty",
				Project:     "P3",
				StartTime:   now.Add(-2 * time.Hour),
			},
			activityID: "id3",
			notes:      "",
			tags:       []string{},
		},
	}

	for _, tc := range tests {
		err = repo.Save(ctx, tc.activity)
		require.NoError(t, err)

		err = notesRepo.Save(ctx, tc.activityID, tc.activity.StartTime, tc.notes, tc.tags)
		require.NoError(t, err)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var retrievedNotes string
			var retrievedTags []string
			retrievedNotes, retrievedTags, err = notesRepo.Get(ctx, "ignored-id-on-get", tc.activity.StartTime)
			require.NoError(t, err)
			assert.Equal(t, tc.notes, retrievedNotes)

			if len(tc.tags) == 0 {
				assert.Empty(t, retrievedTags)
			} else {
				assert.Equal(t, tc.tags, retrievedTags)
			}
		})
	}
}
