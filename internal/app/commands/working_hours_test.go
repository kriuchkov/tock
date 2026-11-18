package commands

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestReconcileWorkingHoursStopsThroughService(t *testing.T) {
	now := time.Date(2026, time.April, 21, 18, 0, 0, 0, time.Local)
	stoppedAt := time.Date(2026, time.April, 21, 17, 30, 0, 0, time.Local)

	var gotHours models.WorkingHours
	service := stubActivityResolver{
		stopAfterFn: func(_ context.Context, hours models.WorkingHours, at time.Time) (*models.Activity, error) {
			gotHours = hours
			assert.True(t, at.Equal(now))
			return &models.Activity{Project: "Work", EndTime: &stoppedAt}, nil
		},
	}
	rt := &appruntime.Runtime{
		ActivityService: service,
		Config: &config.Config{WorkingHours: config.WorkingHoursConfig{
			Enabled: true, StopAt: "17:30", Weekdays: "mon,fri",
		}},
		TimeFormatter: timeutil.NewFormatter("24"),
	}

	ctx, err := reconcileWorkingHours(rt.WithContext(context.Background()), now)
	require.NoError(t, err)

	assert.Equal(t, 17, gotHours.StopHour)
	assert.Equal(t, 30, gotHours.StopMinute)
	assert.Len(t, gotHours.Weekdays, 2)
	assert.True(t, gotHours.Weekdays[time.Monday])
	assert.True(t, gotHours.Weekdays[time.Friday])

	stopped, ok := autoStoppedActivityFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "Work", stopped.Project)
}

func TestReconcileWorkingHoursDisabledSkipsService(t *testing.T) {
	rt := &appruntime.Runtime{
		ActivityService: stubActivityResolver{},
		Config:          &config.Config{WorkingHours: config.WorkingHoursConfig{StopAt: "17:30"}},
	}

	ctx, err := reconcileWorkingHours(rt.WithContext(context.Background()), time.Now())
	require.NoError(t, err)
	_, ok := autoStoppedActivityFromContext(ctx)
	assert.False(t, ok)
}

func TestReconcileWorkingHoursRejectsBadWeekdays(t *testing.T) {
	rt := &appruntime.Runtime{
		ActivityService: stubActivityResolver{},
		Config: &config.Config{WorkingHours: config.WorkingHoursConfig{
			Enabled: true, StopAt: "17:30", Weekdays: "someday",
		}},
	}

	_, err := reconcileWorkingHours(rt.WithContext(context.Background()), time.Now())
	require.ErrorContains(t, err, "someday")
}
