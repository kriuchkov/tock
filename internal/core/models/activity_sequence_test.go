package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestParseActivityReference(t *testing.T) {
	ref, err := models.ParseActivityReference("2026-03-14-02")
	require.NoError(t, err)
	assert.True(t, ref.HasSequence)
	assert.Equal(t, 2, ref.Sequence)
	assert.Equal(t, time.Date(2026, time.March, 14, 0, 0, 0, 0, time.Local), ref.Date)

	dateOnly, err := models.ParseActivityReference("2026-03-14")
	require.NoError(t, err)
	assert.False(t, dateOnly.HasSequence)
	assert.Zero(t, dateOnly.Sequence)
}

func TestParseActivityKey(t *testing.T) {
	_, _, err := models.ParseActivityKey("2026-03-14")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected YYYY-MM-DD-NN")
}

func TestActivityForSequenceAndIDs(t *testing.T) {
	start1 := time.Date(2026, time.March, 14, 11, 0, 0, 0, time.Local)
	end1 := start1.Add(time.Hour)
	start2 := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	end2 := start2.Add(30 * time.Minute)
	activities := []models.Activity{
		{Project: "late", StartTime: start1, EndTime: &end1},
		{Project: "early", StartTime: start2, EndTime: &end2},
	}

	selected, err := models.ActivityForSequence(activities, 1)
	require.NoError(t, err)
	assert.Equal(t, "early", selected.Project)

	ids := models.ActivitySequenceIDs(activities)
	assert.Equal(t, "2026-03-14-01", ids[start2.UnixNano()])
	assert.Equal(t, "2026-03-14-02", ids[start1.UnixNano()])
}
