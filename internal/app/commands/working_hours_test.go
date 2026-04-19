package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestResolveWorkingHoursAutoStopTimeSameDay(t *testing.T) {
	now := time.Date(2026, time.April, 21, 18, 0, 0, 0, time.Local)
	start := time.Date(2026, time.April, 21, 15, 0, 0, 0, time.Local)

	got, ok, err := resolveWorkingHoursAutoStopTime(now, start, config.WorkingHoursConfig{
		Enabled:  true,
		StopAt:   "17:30",
		Weekdays: "mon,tue,wed,thu,fri",
	}, timeutil.NewFormatter("24"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, got.Equal(time.Date(2026, time.April, 21, 17, 30, 0, 0, time.Local)))
}

func TestResolveWorkingHoursAutoStopTimeSkipsWeekend(t *testing.T) {
	now := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.Local)
	start := time.Date(2026, time.April, 17, 16, 0, 0, 0, time.Local)

	got, ok, err := resolveWorkingHoursAutoStopTime(now, start, config.WorkingHoursConfig{
		Enabled: true,
		StopAt:  "17:30",
	}, timeutil.NewFormatter("24"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, got.Equal(time.Date(2026, time.April, 17, 17, 30, 0, 0, time.Local)))
}

func TestResolveWorkingHoursAutoStopTimeDoesNotStopActivitiesStartedAfterCutoff(t *testing.T) {
	now := time.Date(2026, time.April, 22, 9, 0, 0, 0, time.Local)
	start := time.Date(2026, time.April, 21, 18, 0, 0, 0, time.Local)

	_, ok, err := resolveWorkingHoursAutoStopTime(now, start, config.WorkingHoursConfig{
		Enabled: true,
		StopAt:  "17:30",
	}, timeutil.NewFormatter("24"))
	require.NoError(t, err)
	assert.False(t, ok)
}
