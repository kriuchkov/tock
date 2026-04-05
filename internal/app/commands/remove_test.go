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
