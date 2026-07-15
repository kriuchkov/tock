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

func TestRunNoteCmdDelegatesToServiceForLastActivity(t *testing.T) {
	activity := &models.Activity{
		Project:     "tock",
		Description: "cleanup",
		StartTime:   time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local),
	}

	var gotActivity models.Activity
	var gotNote string
	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) {
			return activity, nil
		},
		addNoteFn: func(_ context.Context, a models.Activity, note string) (*models.Activity, error) {
			gotActivity = a
			gotNote = note
			updated := a
			updated.Notes = "follow up"
			return &updated, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runNoteCmd(cmd, []string{"follow up"}, &noteOptions{})
	require.NoError(t, err)

	assert.Equal(t, *activity, gotActivity)
	assert.Equal(t, "follow up", gotNote)
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

	var gotActivity models.Activity
	service := &stubActivityResolver{
		listFn: func(_ context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
			require.NotNil(t, filter.FromDate)
			require.NotNil(t, filter.ToDate)
			assert.Equal(t, time.Date(2026, time.March, 14, 0, 0, 0, 0, time.Local), *filter.FromDate)
			assert.Equal(t, time.Date(2026, time.March, 15, 0, 0, 0, 0, time.Local), *filter.ToDate)
			return []models.Activity{second, first}, nil
		},
		addNoteFn: func(_ context.Context, a models.Activity, note string) (*models.Activity, error) {
			gotActivity = a
			updated := a
			updated.Notes = note
			return &updated, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runNoteCmd(cmd, []string{"2026-03-14-01", "backfilled"}, &noteOptions{JSONOutput: true})
	require.NoError(t, err)

	assert.Equal(t, "cleanup", gotActivity.Description)
	assert.Contains(t, out.String(), "\"description\": \"cleanup\"")
	assert.Contains(t, out.String(), "\"notes\": \"backfilled\"")
	assert.Contains(t, out.String(), "\"project\": \"tock\"")
}

func TestRunNoteCmdRequiresNoteTextAfterKey(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})

	err := runNoteCmd(cmd, []string{"2026-03-14-01"}, &noteOptions{})
	require.Error(t, err)
}
