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

func TestRunAddCmdUsesDayForTimeOnlyInputs(t *testing.T) {
	day := "2026-04-21"
	start := time.Date(2026, time.April, 21, 9, 30, 0, 0, time.Local)
	end := time.Date(2026, time.April, 21, 10, 45, 0, 0, time.Local)

	service := &stubActivityResolver{
		addFn: func(_ context.Context, req models.AddActivityRequest) (*models.Activity, error) {
			assert.Equal(t, "tock", req.Project)
			assert.Equal(t, "backfill", req.Description)
			assert.Equal(t, start, req.StartTime)
			assert.Equal(t, end, req.EndTime)

			endCopy := req.EndTime
			return &models.Activity{
				Project:     req.Project,
				Description: req.Description,
				StartTime:   req.StartTime,
				EndTime:     &endCopy,
			}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runAdd(cmd, &addOptions{
		Project:     "tock",
		Description: "backfill",
		DayStr:      day,
		StartStr:    "09:30",
		EndStr:      "10:45",
	})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Added activity: tock | backfill (09:30 - 10:45)")
}

func TestRunAddCmdRejectsInvalidDay(t *testing.T) {
	service := &stubActivityResolver{
		addFn: func(_ context.Context, _ models.AddActivityRequest) (*models.Activity, error) {
			t.Fatal("add should not be called when day is invalid")
			return nil, nil
		},
	}

	cmd := newTestCLICommand(service)

	err := runAdd(cmd, &addOptions{
		Project:     "tock",
		Description: "backfill",
		DayStr:      "2026-99-99",
		StartStr:    "09:30",
		EndStr:      "10:45",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse day")
}
