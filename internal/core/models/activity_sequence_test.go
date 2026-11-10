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

func TestActivitiesStartingOnSkipsEntriesFromThePreviousDay(t *testing.T) {
	overnightStart := time.Date(2026, time.March, 13, 23, 0, 0, 0, time.Local)
	overnightEnd := overnightStart.Add(2 * time.Hour)
	morning := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	activities := []models.Activity{
		{Project: "overnight", StartTime: overnightStart, EndTime: &overnightEnd},
		{Project: "morning", StartTime: morning},
	}

	started := models.ActivitiesStartingOn(activities, time.Date(2026, time.March, 14, 0, 0, 0, 0, time.Local))
	require.Len(t, started, 1)
	assert.Equal(t, "morning", started[0].Project)
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

func TestDayKeys(t *testing.T) {
	at := func(day, hour, minute int) time.Time {
		return time.Date(2026, time.November, day, hour, minute, 0, 0, time.Local)
	}
	entry := func(description string, start, end time.Time) models.Activity {
		return models.Activity{Project: "p", Description: description, StartTime: start, EndTime: &end}
	}

	overnight := entry("overnight", at(6, 22, 0), at(7, 1, 0))
	blip := entry("blip", at(7, 8, 0), at(7, 8, 0))
	alpha := entry("alpha", at(7, 9, 0), at(7, 10, 0))
	beta := entry("beta", at(7, 9, 0), at(7, 9, 30))
	review := entry("review", at(7, 11, 0), at(7, 12, 0))
	missing := entry("missing", at(7, 13, 0), at(7, 14, 0))
	all := []models.Activity{review, alpha, beta, blip, entry("early", at(6, 9, 0), at(6, 10, 0)), overnight}

	keys := models.DayKeys(all, []models.Activity{overnight, review, beta, alpha, missing})
	assert.Equal(t, []string{"2026-11-06-02", "2026-11-07-04", "2026-11-07-03", "2026-11-07-02", ""}, keys)
}

func TestDayKeysGivesIdenticalEntriesDistinctKeys(t *testing.T) {
	start := time.Date(2026, time.November, 7, 9, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)
	dup := models.Activity{Project: "p", Description: "dup", StartTime: start, EndTime: &end}

	keys := models.DayKeys([]models.Activity{dup, dup}, []models.Activity{dup, dup})
	assert.Equal(t, []string{"2026-11-07-01", "2026-11-07-02"}, keys)
}
