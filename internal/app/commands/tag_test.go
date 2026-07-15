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

func TestRunTagCmdDelegatesToServiceForLastActivity(t *testing.T) {
	activity := &models.Activity{
		Project:     "tock",
		Description: "cleanup",
		StartTime:   time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local),
	}

	var gotActivity models.Activity
	var gotTags []string
	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) {
			return activity, nil
		},
		addTagsFn: func(_ context.Context, a models.Activity, tags []string) (*models.Activity, error) {
			gotActivity = a
			gotTags = tags
			updated := a
			updated.Tags = tags
			return &updated, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	// Args are parsed/deduplicated by the command before hitting the service.
	err := runTagCmd(cmd, []string{"review, urgent", "focus"}, &tagOptions{})
	require.NoError(t, err)

	assert.Equal(t, *activity, gotActivity)
	assert.Equal(t, []string{"review", "urgent", "focus"}, gotTags)
	assert.Equal(t, "Tags added.\n", out.String())
}

func TestRunTagCmdUsesIndexAndWritesJSON(t *testing.T) {
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
		addTagsFn: func(_ context.Context, a models.Activity, tags []string) (*models.Activity, error) {
			gotActivity = a
			updated := a
			updated.Notes = "existing note"
			updated.Tags = append([]string{"desk"}, tags...)
			return &updated, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runTagCmd(cmd, []string{"2026-03-14-01", "planning", "ship"}, &tagOptions{JSONOutput: true})
	require.NoError(t, err)

	assert.Equal(t, "cleanup", gotActivity.Description)
	assert.Contains(t, out.String(), "\"description\": \"cleanup\"")
	assert.Contains(t, out.String(), "\"notes\": \"existing note\"")
	assert.Contains(t, out.String(), "\"tags\": [")
	assert.Contains(t, out.String(), "\"planning\"")
	assert.Contains(t, out.String(), "\"ship\"")
	assert.Contains(t, out.String(), "\"desk\"")
}

func TestRunTagCmdRequiresTagAfterKey(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})

	err := runTagCmd(cmd, []string{"2026-03-14-01"}, &tagOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tag is required")
}
