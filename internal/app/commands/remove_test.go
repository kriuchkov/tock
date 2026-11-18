package commands

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
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

func TestFindActivityByIndexUsesServiceLookup(t *testing.T) {
	service := &stubActivityResolver{
		getByKeyFn: func(_ context.Context, key string) (*models.Activity, error) {
			if key == "2026-03-14-01" {
				return &models.Activity{Project: "morning"}, nil
			}
			return nil, coreErrors.ErrActivityNotFound
		},
	}

	activity, err := findActivityByIndex(context.Background(), service, "2026-03-14-01")
	require.NoError(t, err)
	assert.Equal(t, "morning", activity.Project)

	_, err = findActivityByIndex(context.Background(), service, "2026-03-14-02")
	require.ErrorIs(t, err, coreErrors.ErrActivityNotFound)
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
