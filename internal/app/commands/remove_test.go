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

func TestRunRemoveCmdUsesCommandWriter(t *testing.T) {
	removed := false
	activity := &models.Activity{
		Project:     "tock",
		Description: "cleanup",
		StartTime:   time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local),
	}

	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) {
			return activity, nil
		},
		removeFn: func(_ context.Context, got models.Activity) error {
			removed = true
			assert.Equal(t, activity.Project, got.Project)
			return nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runRemoveCmd(cmd, nil, &removeOptions{SkipConfirm: true})
	require.NoError(t, err)
	assert.True(t, removed)
	assert.Equal(t, "Activity removed.\n", out.String())
}

func TestFindActivityByIndexNumbersByStartDayLikeList(t *testing.T) {
	overnightStart := time.Date(2026, time.March, 13, 23, 0, 0, 0, time.Local)
	overnightEnd := overnightStart.Add(2 * time.Hour)
	morning := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)

	// File-like backends return every entry overlapping the day, including one from the day before.
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{
				{Project: "overnight", StartTime: overnightStart, EndTime: &overnightEnd},
				{Project: "morning", StartTime: morning},
			}, nil
		},
	}

	activity, err := findActivityByIndex(context.Background(), service, "2026-03-14-01")
	require.NoError(t, err)
	assert.Equal(t, "morning", activity.Project)

	_, err = findActivityByIndex(context.Background(), service, "2026-03-14-02")
	require.Error(t, err)
}

func TestConfirmRemovalAbort(t *testing.T) {
	activity := models.Activity{
		Project:     "tock",
		Description: "cleanup",
		StartTime:   time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local),
	}
	var out bytes.Buffer

	confirmed, err := confirmRemoval(&out, bytes.NewBufferString("n\n"), activity)
	require.NoError(t, err)
	assert.False(t, confirmed)
	assert.Contains(t, out.String(), "About to remove:")
	assert.Contains(t, out.String(), "Aborted.")
}
