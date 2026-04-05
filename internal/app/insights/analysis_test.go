package insights_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestAnalyzeActivities(t *testing.T) {
	makeActivity := func(day int, startHour int, minutes int, project string) models.Activity {
		start := time.Date(2026, time.March, day, startHour, 0, 0, 0, time.Local)
		end := start.Add(time.Duration(minutes) * time.Minute)
		return models.Activity{Project: project, StartTime: start, EndTime: &end}
	}

	activities := []models.Activity{
		makeActivity(2, 9, 120, "core"),
		makeActivity(2, 11, 30, "core"),
		makeActivity(2, 14, 10, "ops"),
		makeActivity(3, 21, 90, "research"),
	}

	stats := insights.AnalyzeActivities(activities)

	assert.Equal(t, 250*time.Minute, stats.TotalDuration)
	assert.Equal(t, 210*time.Minute, stats.DeepWorkDuration)
	assert.InDelta(t, 84.0, stats.DeepWorkScore, 0.1)
	assert.Equal(t, 1, stats.ContextSwitches)
	assert.Equal(t, 1.0, stats.AvgSwitchesPerDay)
	assert.Equal(t, "Morning Lark 🐦", stats.Chronotype)
	assert.Equal(t, 9, stats.PeakHour)
	assert.Equal(t, "Monday", stats.MostProductiveDay)
	assert.Equal(t, 62*time.Minute+30*time.Second, stats.AvgSessionDuration)
	assert.Equal(t, 1, stats.FocusDistribution[insights.FocusDistributionFragmented])
	assert.Equal(t, 1, stats.FocusDistribution[insights.FocusDistributionFlow])
	assert.Equal(t, 2, stats.FocusDistribution[insights.FocusDistributionDeep])
}

func TestAnalyzeActivitiesEmpty(t *testing.T) {
	stats := insights.AnalyzeActivities(nil)
	require.NotNil(t, stats.FocusDistribution)
	assert.Zero(t, stats.TotalDuration)
	assert.Zero(t, stats.ContextSwitches)
	assert.Zero(t, stats.DeepWorkScore)
}