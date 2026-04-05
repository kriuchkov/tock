package insights_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestBuildMonthDataSplitsCrossDayActivity(t *testing.T) {
	start := time.Date(2026, time.March, 31, 23, 0, 0, 0, time.Local)
	end := start.Add(2 * time.Hour)

	data := insights.BuildMonthData([]models.Activity{{
		Project:     "tock",
		Description: "late work",
		StartTime:   start,
		EndTime:     &end,
	}}, 2026, time.April, end)

	require.Contains(t, data.MonthReports, 1)
	assert.Equal(t, time.Hour, data.MonthReports[1].TotalDuration)
	require.Contains(t, data.DailyReports, "2026-03-31")
	require.Contains(t, data.DailyReports, "2026-04-01")
	assert.Equal(t, time.Hour, data.DailyReports["2026-03-31"].TotalDuration)
	assert.Equal(t, time.Hour, data.DailyReports["2026-04-01"].TotalDuration)
}

func TestComputeProductivityStats(t *testing.T) {
	monthReports := map[int]*models.Report{
		1: {TotalDuration: 2 * time.Hour},
		2: {TotalDuration: 90 * time.Minute},
		4: {TotalDuration: 3 * time.Hour},
	}

	stats := insights.ComputeProductivityStats(monthReports, 5)

	assert.Equal(t, 3, stats.ActiveDays)
	assert.Equal(t, 390*time.Minute, stats.TotalDuration)
	assert.Equal(t, 130*time.Minute, stats.AvgDuration)
	assert.Equal(t, 3*time.Hour, stats.MaxDailyDuration)
	assert.Equal(t, 2, stats.LongestStreak)
}

func TestAggregateProjectDurations(t *testing.T) {
	monthReports := map[int]*models.Report{
		1: {ByProject: map[string]models.ProjectReport{
			"core": {Duration: 2 * time.Hour},
			"ops":  {Duration: time.Hour},
		}},
		2: {ByProject: map[string]models.ProjectReport{
			"core": {Duration: 30 * time.Minute},
		}},
	}

	projects := insights.AggregateProjectDurations(monthReports)
	require.Len(t, projects, 2)
	assert.Equal(t, "core", projects[0].Name)
	assert.Equal(t, 150*time.Minute, projects[0].Duration)
	assert.Equal(t, "ops", projects[1].Name)
}

func TestBuildWeeklyActivityData(t *testing.T) {
	dailyReports := map[string]*models.Report{
		"2026-03-09": {TotalDuration: time.Hour},
		"2026-03-10": {TotalDuration: 2 * time.Hour},
		"2026-03-12": {TotalDuration: 90 * time.Minute},
		"2026-03-03": {TotalDuration: 30 * time.Minute},
		"2026-03-05": {TotalDuration: 3 * time.Hour},
	}

	data := insights.BuildWeeklyActivityData(dailyReports, time.Date(2026, time.March, 12, 12, 0, 0, 0, time.Local))

	assert.Equal(t, time.Date(2026, time.March, 9, 0, 0, 0, 0, time.Local), data.StartOfWeek)
	assert.Equal(t, time.Hour, data.CurrentWeekDurations[0])
	assert.Equal(t, 2*time.Hour, data.CurrentWeekDurations[1])
	assert.Equal(t, 90*time.Minute, data.CurrentWeekDurations[3])
	assert.Equal(t, 30*time.Minute, data.PreviousWeekDurations[1])
	assert.Equal(t, 3*time.Hour, data.PreviousWeekDurations[3])
	assert.Equal(t, 270*time.Minute, data.CurrentWeekTotal)
	assert.Equal(t, 3*time.Hour, data.MaxDuration)
}