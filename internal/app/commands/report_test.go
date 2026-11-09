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
		dateFilterFlags: dateFilterFlags{Date: "2026-03-14"},
		Project:         "tock",
		Description:     "cleanup",
		TotalOnly:       true,
	})
	require.NoError(t, err)
	assert.Equal(t, "1h 30m\n", out.String())
}

func TestRunReportCmdNumbersOvernightEntryByItsStartDay(t *testing.T) {
	realStart := time.Date(2026, time.November, 6, 22, 0, 0, 0, time.Local)
	clippedStart := time.Date(2026, time.November, 7, 0, 0, 0, 0, time.Local)
	overnightEnd := time.Date(2026, time.November, 7, 1, 0, 0, 0, time.Local)
	standupStart := time.Date(2026, time.November, 7, 9, 0, 0, 0, time.Local)
	standupEnd := time.Date(2026, time.November, 7, 10, 0, 0, 0, time.Local)
	overnight := models.Activity{Project: "Night", StartTime: clippedStart, EndTime: &overnightEnd}
	standup := models.Activity{Project: "Day", StartTime: standupStart, EndTime: &standupEnd}

	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{
				Activities: []models.Activity{overnight, standup},
				ByProject: map[string]models.ProjectReport{
					"Night": {ProjectName: "Night", Activities: []models.Activity{overnight}},
					"Day":   {ProjectName: "Day", Activities: []models.Activity{standup}},
				},
				ClippedStarts: map[int64]time.Time{clippedStart.UnixNano(): realStart},
			}, nil
		},
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{{Project: "Night", StartTime: realStart, EndTime: &overnightEnd}}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	require.NoError(t, runReportCmd(cmd, &reportOptions{}))
	assert.Contains(t, out.String(), "2026-11-06-01")
	assert.Contains(t, out.String(), "2026-11-07-01")
	assert.NotContains(t, out.String(), "2026-11-07-02")
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
			return &models.Report{
				TotalDuration: 26*time.Hour + 30*time.Minute + 30*time.Second,
				ByProject: map[string]models.ProjectReport{
					"tock":    {ProjectName: "tock", Duration: 90*time.Minute + 30*time.Second},
					"billing": {ProjectName: "billing", Duration: 25 * time.Hour},
				},
			}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runReportCmd(cmd, &reportOptions{JSONOutput: true, Summary: true})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"projects": [
			{"project": "billing", "duration": "25:00:00"},
			{"project": "tock", "duration": "01:30:30"}
		],
		"total": "26:30:30"
	}`, out.String())
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
		dateFilterFlags: dateFilterFlags{From: "2026-04-01", To: "2026-04-15"},
		TotalOnly:       true,
	})
	require.NoError(t, err)
	assert.Equal(t, "8h 0m\n", out.String())
}
