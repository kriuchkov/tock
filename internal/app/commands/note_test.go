package commands

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestRunNoteCmdAppendsToLastActivity(t *testing.T) {
	activity := &models.Activity{
		Project:     "tock",
		Description: "cleanup",
		StartTime:   time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local),
	}

	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) {
			return activity, nil
		},
	}

	notesRepo := stubNotesRepository{
		getFn: func(_ context.Context, activityID string, date time.Time) (string, []string, error) {
			assert.Equal(t, activity.ID(), activityID)
			assert.Equal(t, activity.StartTime, date)
			return "initial note", []string{"focus"}, nil
		},
		saveFn: func(_ context.Context, activityID string, date time.Time, notes string, tags []string) error {
			assert.Equal(t, activity.ID(), activityID)
			assert.Equal(t, activity.StartTime, date)
			assert.Equal(t, "initial note\n\nfollow up", notes)
			assert.Equal(t, []string{"focus"}, tags)
			return nil
		},
	}

	cmd := newTestCLICommandWithNotes(service, notesRepo)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runNoteCmd(cmd, []string{"follow up"}, &noteOptions{})
	require.NoError(t, err)
	assert.Equal(t, "Note added.\n", out.String())
}

func TestRunNoteCmdUsesIndexAndWritesJSON(t *testing.T) {
	first := models.Activity{
		Project:     "tock",
		Description: "cleanup",
		StartTime:   time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local),
	}
	second := models.Activity{
		Project:     "tock",
		Description: "review",
		StartTime:   time.Date(2026, time.March, 14, 11, 0, 0, 0, time.Local),
	}

	service := &stubActivityResolver{
		listFn: func(_ context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
			require.NotNil(t, filter.FromDate)
			require.NotNil(t, filter.ToDate)
			assert.Equal(t, time.Date(2026, time.March, 14, 0, 0, 0, 0, time.Local), *filter.FromDate)
			assert.Equal(t, time.Date(2026, time.March, 15, 0, 0, 0, 0, time.Local), *filter.ToDate)
			return []models.Activity{second, first}, nil
		},
	}

	notesRepo := stubNotesRepository{
		getFn: func(context.Context, string, time.Time) (string, []string, error) {
			return "", nil, nil
		},
		saveFn: func(_ context.Context, activityID string, _ time.Time, notes string, tags []string) error {
			assert.Equal(t, first.ID(), activityID)
			assert.Equal(t, "backfilled", notes)
			assert.Empty(t, tags)
			return nil
		},
	}

	cmd := newTestCLICommandWithNotes(service, notesRepo)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runNoteCmd(cmd, []string{"2026-03-14-01", "backfilled"}, &noteOptions{JSONOutput: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "\"description\": \"cleanup\"")
	assert.Contains(t, out.String(), "\"notes\": \"backfilled\"")
	assert.Contains(t, out.String(), "\"project\": \"tock\"")
}

func TestRunNoteCmdRequiresNoteTextAfterKey(t *testing.T) {
	cmd := newTestCLICommandWithNotes(&stubActivityResolver{}, stubNotesRepository{})

	err := runNoteCmd(cmd, []string{"2026-03-14-01"}, &noteOptions{})
	require.Error(t, err)
	assert.EqualError(t, err, "note text is required")
}
