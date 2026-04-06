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

func TestRunReportCmdBuildsFilterAndWritesToCommandOutput(t *testing.T) {
	service := &stubActivityResolver{
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			require.NotNil(t, filter.FromDate)
			require.NotNil(t, filter.ToDate)
			require.NotNil(t, filter.Project)
			require.NotNil(t, filter.Description)

			expectedStart := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.Local)
			assert.Equal(t, expectedStart, *filter.FromDate)
			assert.Equal(t, expectedStart.AddDate(0, 0, 1), *filter.ToDate)
			assert.Equal(t, "tock", *filter.Project)
			assert.Equal(t, "cleanup", *filter.Description)

			return &models.Report{TotalDuration: 90 * time.Minute}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runReportCmd(cmd, &reportOptions{
		Date:        "2026-03-14",
		Project:     "tock",
		Description: "cleanup",
		TotalOnly:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, "1h 30m\n", out.String())
}

func TestRunReportCmdJSONUsesCommandWriter(t *testing.T) {
	end := time.Date(2026, time.March, 14, 11, 0, 0, 0, time.Local)
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{Activities: []models.Activity{{
				Project:     "tock",
				Description: "refactor",
				StartTime:   time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local),
				EndTime:     &end,
			}}}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runReportCmd(cmd, &reportOptions{JSONOutput: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "\"project\": \"tock\"")
	assert.Contains(t, out.String(), "\"description\": \"refactor\"")
}
