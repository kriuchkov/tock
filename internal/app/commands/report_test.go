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

func TestRunReportCmdJSONSummaryGroupsByProjectSortedAndFormatted(t *testing.T) {
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{ByProject: map[string]models.ProjectReport{
				"tock":    {ProjectName: "tock", Duration: 90*time.Minute + 30*time.Second},
				"billing": {ProjectName: "billing", Duration: 25 * time.Hour},
			}}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runReportCmd(cmd, &reportOptions{JSONOutput: true, Summary: true})
	require.NoError(t, err)
	assert.JSONEq(t, `[
		{"project": "billing", "duration": "25:00:00"},
		{"project": "tock", "duration": "01:30:30"}
	]`, out.String())
}

func TestDurationString(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "zero", d: 0, want: "00:00:00"},
		{name: "under an hour", d: 45 * time.Minute, want: "00:45:00"},
		{name: "over a day", d: 25*time.Hour + 5*time.Minute, want: "25:05:00"},
		{name: "rounds sub-second remainder", d: 90*time.Second + 700*time.Millisecond, want: "00:01:31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, durationString(tt.d))
		})
	}
}

func TestRunReportCmdBuildsInclusiveDateRange(t *testing.T) {
	service := &stubActivityResolver{
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			require.NotNil(t, filter.FromDate)
			require.NotNil(t, filter.ToDate)
			assert.Equal(t, time.Date(2026, time.April, 1, 0, 0, 0, 0, time.Local), *filter.FromDate)
			assert.Equal(t, time.Date(2026, time.April, 16, 0, 0, 0, 0, time.Local), *filter.ToDate)

			return &models.Report{TotalDuration: 8 * time.Hour}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runReportCmd(cmd, &reportOptions{
		From:      "2026-04-01",
		To:        "2026-04-15",
		TotalOnly: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "8h 0m\n", out.String())
}
