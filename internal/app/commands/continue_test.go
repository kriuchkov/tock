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

func TestRunContinueCmdUsesRecentActivityAndOverrides(t *testing.T) {
	service := &stubActivityResolver{
		getRecentFn: func(_ context.Context, limit int) ([]models.Activity, error) {
			assert.Equal(t, 2, limit)
			return []models.Activity{
				{Project: "core", Description: "first"},
				{Project: "ops", Description: "second"},
			}, nil
		},
		startFn: func(_ context.Context, req models.StartActivityRequest) (*models.Activity, error) {
			assert.Equal(t, "renamed", req.Description)
			assert.Equal(t, "ops", req.Project)
			assert.Equal(t, []string{"done"}, req.Tags)
			return &models.Activity{Project: req.Project, Description: req.Description, StartTime: req.StartTime}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runContinueCmd(cmd, []string{"1"}, &continueOptions{Description: "renamed", At: "13:15", Tags: []string{"done"}})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Started activity: ops | renamed at 13:15")
}

func TestRunContinueCmdJSONUsesCommandWriter(t *testing.T) {
	service := &stubActivityResolver{
		getRecentFn: func(_ context.Context, limit int) ([]models.Activity, error) {
			return []models.Activity{{Project: "core", Description: "first"}}, nil
		},
		startFn: func(_ context.Context, req models.StartActivityRequest) (*models.Activity, error) {
			return &models.Activity{Project: req.Project, Description: req.Description, StartTime: time.Now()}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runContinueCmd(cmd, nil, &continueOptions{JSONOutput: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "\"project\": \"core\"")
	assert.Contains(t, out.String(), "\"description\": \"first\"")
}
