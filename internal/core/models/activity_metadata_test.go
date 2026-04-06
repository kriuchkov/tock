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
