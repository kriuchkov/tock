package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestUniqueProjects(t *testing.T) {
	activities := []models.Activity{
		{Project: "ops"},
		{Project: "core"},
		{Project: "ops"},
		{Project: ""},
	}

	assert.Equal(t, []string{"core", "ops"}, models.UniqueProjects(activities))
}

func TestDescriptionsForProject(t *testing.T) {
	activities := []models.Activity{
		{Project: "core", Description: "refactor"},
		{Project: "core", Description: "cleanup"},
		{Project: "core", Description: "refactor"},
		{Project: "ops", Description: "deploy"},
	}

	assert.Equal(t, []string{"cleanup", "refactor"}, models.DescriptionsForProject(activities, "core"))
}

func TestFindTargetDate(t *testing.T) {
	activities := []models.Activity{
		{StartTime: time.Date(2026, time.March, 10, 10, 0, 0, 0, time.Local)},
		{StartTime: time.Date(2026, time.March, 12, 10, 0, 0, 0, time.Local)},
		{StartTime: time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local)},
	}
	current := time.Date(2026, time.March, 12, 0, 0, 0, 0, time.Local)

	prev := models.FindTargetDate(activities, current, -1)
	next := models.FindTargetDate(activities, current, 1)

	require.NotNil(t, prev)
	require.NotNil(t, next)
	assert.Equal(t, time.Date(2026, time.March, 10, 0, 0, 0, 0, time.Local), *prev)
	assert.Equal(t, time.Date(2026, time.March, 14, 0, 0, 0, 0, time.Local), *next)
	assert.Nil(t, models.FindTargetDate(activities, time.Date(2026, time.March, 10, 0, 0, 0, 0, time.Local), -1))
}

func TestMoveActivityToDayKeepsTimeOfDayAndDuration(t *testing.T) {
	start := time.Date(2026, time.March, 14, 9, 15, 0, 0, time.Local)
	end := start.Add(90 * time.Minute)
	activity := models.Activity{Project: "tock", StartTime: start, EndTime: &end}

	moved := models.MoveActivityToDay(activity, time.Date(2026, time.March, 16, 0, 0, 0, 0, time.Local))

	assert.Equal(t, time.Date(2026, time.March, 16, 9, 15, 0, 0, time.Local), moved.StartTime)
	require.NotNil(t, moved.EndTime)
	assert.Equal(t, 90*time.Minute, moved.EndTime.Sub(moved.StartTime))
}

func TestMoveActivityToDayLeavesARunningActivityOpen(t *testing.T) {
	start := time.Date(2026, time.March, 14, 9, 15, 0, 0, time.Local)
	activity := models.Activity{Project: "tock", StartTime: start}

	moved := models.MoveActivityToDay(activity, time.Date(2026, time.March, 16, 0, 0, 0, 0, time.Local))

	assert.Equal(t, time.Date(2026, time.March, 16, 9, 15, 0, 0, time.Local), moved.StartTime)
	assert.Nil(t, moved.EndTime)
}

func TestRescheduleActivityPreservesDuration(t *testing.T) {
	start := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)
	activity := models.Activity{Project: "tock", StartTime: start, EndTime: &end}

	rescheduled := models.RescheduleActivity(activity, start.Add(-30*time.Minute))

	assert.Equal(t, start.Add(-30*time.Minute), rescheduled.StartTime)
	require.NotNil(t, rescheduled.EndTime)
	assert.Equal(t, time.Hour, rescheduled.EndTime.Sub(rescheduled.StartTime))
}
