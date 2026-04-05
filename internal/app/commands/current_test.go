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

func TestRunCurrentCmdJSONUsesCommandWriter(t *testing.T) {
	end := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.Local)
	service := &stubActivityResolver{
		listFn: func(_ context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
			require.NotNil(t, filter.IsRunning)
			assert.True(t, *filter.IsRunning)
			return []models.Activity{{
				Project:     "tock",
				Description: "active",
				StartTime:   time.Date(2026, time.March, 14, 11, 0, 0, 0, time.Local),
				EndTime:     &end,
			}}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runCurrentCmd(cmd, &currentOptions{JSONOutput: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "\"project\": \"tock\"")
	assert.Contains(t, out.String(), "\"description\": \"active\"")
}

func TestRunCurrentCmdEmptyWritesNotice(t *testing.T) {
	service := &stubActivityResolver{
		listFn: func(_ context.Context, _ models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runCurrentCmd(cmd, &currentOptions{})
	require.NoError(t, err)
	assert.Equal(t, "No currently running activities.\n", out.String())
}
