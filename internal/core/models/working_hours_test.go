package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
)

func weekdayHours(t *testing.T) models.WorkingHours {
	t.Helper()
	weekdays, err := models.ParseWorkingWeekdays("")
	require.NoError(t, err)
	return models.WorkingHours{StopHour: 17, StopMinute: 30, Weekdays: weekdays}
}

func TestWorkingHoursStopTimeSameDay(t *testing.T) {
	now := time.Date(2026, time.April, 21, 18, 0, 0, 0, time.Local)
	start := time.Date(2026, time.April, 21, 15, 0, 0, 0, time.Local)

	got, ok := weekdayHours(t).StopTime(start, now)
	require.True(t, ok)
	assert.True(t, got.Equal(time.Date(2026, time.April, 21, 17, 30, 0, 0, time.Local)))
}

func TestWorkingHoursStopTimeSkipsWeekend(t *testing.T) {
	now := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.Local)
	start := time.Date(2026, time.April, 17, 16, 0, 0, 0, time.Local)

	got, ok := weekdayHours(t).StopTime(start, now)
	require.True(t, ok)
	assert.True(t, got.Equal(time.Date(2026, time.April, 17, 17, 30, 0, 0, time.Local)))
}

func TestWorkingHoursStopTimeIgnoresActivitiesStartedAfterCutoff(t *testing.T) {
	now := time.Date(2026, time.April, 22, 9, 0, 0, 0, time.Local)
	start := time.Date(2026, time.April, 21, 18, 0, 0, 0, time.Local)

	_, ok := weekdayHours(t).StopTime(start, now)
	assert.False(t, ok)
}

func TestParseWorkingWeekdays(t *testing.T) {
	got, err := models.ParseWorkingWeekdays("Tues, thurs ,sun")
	require.NoError(t, err)
	assert.Len(t, got, 3)
	for _, wd := range []time.Weekday{time.Tuesday, time.Thursday, time.Sunday} {
		assert.True(t, got[wd], wd.String())
	}

	got, err = models.ParseWorkingWeekdays("all")
	require.NoError(t, err)
	assert.Len(t, got, 7)

	_, err = models.ParseWorkingWeekdays("mon,someday")
	require.Error(t, err)

	_, err = models.ParseWorkingWeekdays(" , ")
	require.Error(t, err)
}
