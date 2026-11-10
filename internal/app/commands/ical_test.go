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

func TestRunICalCmdRequiresPathForBulkExport(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})
	err := runICalCmd(cmd, []string{"2026-03-14"}, "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output directory (--path) is required")
}

func TestHandleFullExportWritesEmptyNotice(t *testing.T) {
	service := &stubActivityResolver{
		listFn: func(_ context.Context, _ models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{}, nil
		},
	}
	cmd := newTestCLICommand(service)
	var out bytes.Buffer

	err := handleFullExport(cmd, &out, "", false)
	require.NoError(t, err)
	assert.Equal(t, "No activities found.\n", out.String())
}

func TestRunICalCmdSingleExportWritesSelectedActivity(t *testing.T) {
	startFirst := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	endFirst := startFirst.Add(time.Hour)
	startSecond := time.Date(2026, time.March, 14, 11, 0, 0, 0, time.Local)
	endSecond := startSecond.Add(30 * time.Minute)

	service := &stubActivityResolver{
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			require.NotNil(t, filter.FromDate)
			require.NotNil(t, filter.ToDate)
			assert.Equal(t, time.Date(2026, time.March, 14, 0, 0, 0, 0, time.Local), *filter.FromDate)
			assert.Equal(t, time.Date(2026, time.March, 15, 0, 0, 0, 0, time.Local), *filter.ToDate)
			return &models.Report{
				Activities: []models.Activity{
					{Project: "core", Description: "first", StartTime: startFirst, EndTime: &endFirst},
					{Project: "ops", Description: "second", StartTime: startSecond, EndTime: &endSecond},
				},
				Keys: []string{"2026-03-14-01", "2026-03-14-02"},
			}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runICalCmd(cmd, []string{"2026-03-14-02"}, "", false)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "BEGIN:VCALENDAR")
	assert.Contains(t, out.String(), "SUMMARY:ops: second")
	assert.NotContains(t, out.String(), "SUMMARY:core: first")
}

func TestRunICalCmdSingleExportSkipsEntriesFromThePreviousDay(t *testing.T) {
	// The report clips the overnight entry to 00:00, but its key stays on the day it started.
	clippedStart := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.Local)
	overnightEnd := clippedStart.Add(time.Hour)
	morningStart := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	morningEnd := morningStart.Add(time.Hour)

	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{
				Activities: []models.Activity{
					{Project: "core", Description: "overnight", StartTime: clippedStart, EndTime: &overnightEnd},
					{Project: "ops", Description: "morning", StartTime: morningStart, EndTime: &morningEnd},
				},
				Keys: []string{"2026-03-13-01", "2026-03-14-01"},
			}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runICalCmd(cmd, []string{"2026-03-14-01"}, "", false)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "SUMMARY:ops: morning")
	assert.NotContains(t, out.String(), "SUMMARY:core: overnight")
}
