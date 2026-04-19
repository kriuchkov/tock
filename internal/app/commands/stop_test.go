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

func TestRunStopCmdUsesOptionsAndWriter(t *testing.T) {
	service := &stubActivityResolver{
		stopFn: func(_ context.Context, req models.StopActivityRequest) (*models.Activity, error) {
			assert.Equal(t, "closing", req.Notes)
			assert.Equal(t, []string{"done"}, req.Tags)
			assert.Equal(t, 18, req.EndTime.Hour())
			assert.Equal(t, 45, req.EndTime.Minute())

			end := req.EndTime
			return &models.Activity{Project: "tock", Description: "cleanup", StartTime: end.Add(-time.Hour), EndTime: &end}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runStopCmd(cmd, &stopOptions{At: "18:45", Notes: "closing", Tags: []string{"done"}})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Stopped activity: tock | cleanup at 18:45")
}

func TestRunStopCmdJSONUsesCommandWriter(t *testing.T) {
	service := &stubActivityResolver{
		stopFn: func(_ context.Context, req models.StopActivityRequest) (*models.Activity, error) {
			end := req.EndTime
			return &models.Activity{Project: "tock", Description: "cleanup", StartTime: end.Add(-time.Hour), EndTime: &end}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runStopCmd(cmd, &stopOptions{JSONOutput: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "\"project\": \"tock\"")
	assert.Contains(t, out.String(), "\"description\": \"cleanup\"")
}

func TestRunStopCmdUsesAutoStoppedActivityFromContext(t *testing.T) {
	service := &stubActivityResolver{
		stopFn: func(_ context.Context, _ models.StopActivityRequest) (*models.Activity, error) {
			return nil, coreErrors.ErrNoActiveActivity
		},
	}

	cmd := newTestCLICommand(service)
	stoppedAt := time.Date(2026, time.April, 21, 17, 30, 0, 0, time.Local)
	cmd.SetContext(withAutoStoppedActivity(cmd.Context(), &models.Activity{
		Project:     "tock",
		Description: "cleanup",
		StartTime:   stoppedAt.Add(-time.Hour),
		EndTime:     &stoppedAt,
	}))

	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runStopCmd(cmd, &stopOptions{})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Stopped activity: tock | cleanup at 17:30")
}
