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

func TestRunStartCmdUsesArgsAndWriter(t *testing.T) {
	service := &stubActivityResolver{
		startFn: func(_ context.Context, req models.StartActivityRequest) (*models.Activity, error) {
			assert.Equal(t, "core", req.Project)
			assert.Equal(t, "refactor", req.Description)
			assert.Equal(t, "note text", req.Notes)
			assert.Equal(t, []string{"tag1", "tag2"}, req.Tags)

			end := req.StartTime.Add(time.Hour)
			return &models.Activity{Project: req.Project, Description: req.Description, StartTime: req.StartTime, EndTime: &end}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runStartCmd(cmd, []string{"core", "refactor", "note text", "tag1, tag2"}, &startOptions{At: "09:30"})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Started activity: core | refactor at 09:30")
}

func TestRunStartCmdJSONUsesCommandWriter(t *testing.T) {
	service := &stubActivityResolver{
		startFn: func(_ context.Context, req models.StartActivityRequest) (*models.Activity, error) {
			return &models.Activity{Project: req.Project, Description: req.Description, StartTime: req.StartTime}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runStartCmd(cmd, nil, &startOptions{Project: "tock", Description: "json", JSONOutput: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "\"project\": \"tock\"")
	assert.Contains(t, out.String(), "\"description\": \"json\"")
}
