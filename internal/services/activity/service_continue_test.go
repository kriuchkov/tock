package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
	"github.com/kriuchkov/tock/internal/services/activity"
)

func runningFilter(running bool) any {
	return mock.MatchedBy(func(f models.ActivityFilter) bool {
		return (f.IsRunning != nil) == running
	})
}

func endedAt(start time.Time, d time.Duration) *time.Time {
	end := start.Add(d)
	return &end
}

func TestService_Continue_StartsRecentActivityWithOverrides(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	svc := activity.NewService(repo, nil)

	day := time.Date(2026, 11, 18, 9, 0, 0, 0, time.Local)
	history := []models.Activity{
		{Project: "Work", Description: "Email", StartTime: day, EndTime: endedAt(day, time.Hour)},
		{Project: "Tock", Description: "Review", StartTime: day.Add(2 * time.Hour), EndTime: endedAt(day.Add(2*time.Hour), time.Hour)},
		{Project: "Work", Description: "Email", StartTime: day.Add(4 * time.Hour), EndTime: endedAt(day.Add(4*time.Hour), time.Hour)},
	}
	repo.EXPECT().Find(mock.Anything, runningFilter(false)).Return(history, nil)
	repo.EXPECT().Find(mock.Anything, runningFilter(true)).Return(nil, nil)

	start := day.Add(6 * time.Hour)
	repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
		return a.Project == "Tock" && a.Description == "Sync design" && a.StartTime.Equal(start) && a.EndTime == nil
	})).Return(nil)

	got, err := svc.Continue(context.Background(), models.ContinueActivityRequest{
		Index:       1,
		Description: "Sync design",
		StartTime:   start,
	})
	require.NoError(t, err)
	assert.Equal(t, "Tock", got.Project)
	assert.Equal(t, "Sync design", got.Description)
}

func TestService_Continue_RejectsMissingIndex(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	svc := activity.NewService(repo, nil)

	repo.EXPECT().Find(mock.Anything, runningFilter(false)).Return([]models.Activity{
		{Project: "Work", Description: "Email", StartTime: time.Now()},
	}, nil)

	_, err := svc.Continue(context.Background(), models.ContinueActivityRequest{Index: 3})
	require.ErrorContains(t, err, "activity number 3 not found")

	_, err = svc.Continue(context.Background(), models.ContinueActivityRequest{Index: -1})
	require.Error(t, err)
}

func TestService_StopAfterWorkingHours(t *testing.T) {
	weekdays, err := models.ParseWorkingWeekdays("")
	require.NoError(t, err)
	hours := models.WorkingHours{StopHour: 17, StopMinute: 30, Weekdays: weekdays}

	start := time.Date(2026, time.April, 21, 15, 0, 0, 0, time.Local)
	now := time.Date(2026, time.April, 21, 18, 0, 0, 0, time.Local)
	cutoff := time.Date(2026, time.April, 21, 17, 30, 0, 0, time.Local)

	t.Run("stops at the end of the day", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo, nil)

		repo.EXPECT().Find(mock.Anything, runningFilter(true)).Return([]models.Activity{
			{Project: "Work", Description: "Late", StartTime: start},
		}, nil)
		repo.EXPECT().Save(mock.MatchedBy(ports.IsAutoStop), mock.MatchedBy(func(a models.Activity) bool {
			return a.EndTime != nil && a.EndTime.Equal(cutoff)
		})).Return(nil)

		stopped, stopErr := svc.StopAfterWorkingHours(context.Background(), hours, now)
		require.NoError(t, stopErr)
		require.NotNil(t, stopped)
		assert.True(t, stopped.EndTime.Equal(cutoff))
	})

	t.Run("leaves an activity within hours alone", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo, nil)

		repo.EXPECT().Find(mock.Anything, runningFilter(true)).Return([]models.Activity{
			{Project: "Work", Description: "Early", StartTime: start},
		}, nil)

		stopped, stopErr := svc.StopAfterWorkingHours(context.Background(), hours, start.Add(time.Hour))
		require.NoError(t, stopErr)
		assert.Nil(t, stopped)
	})

	t.Run("nothing running", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo, nil)

		repo.EXPECT().Find(mock.Anything, runningFilter(true)).Return(nil, nil)

		stopped, stopErr := svc.StopAfterWorkingHours(context.Background(), hours, now)
		require.NoError(t, stopErr)
		assert.Nil(t, stopped)
	})

	t.Run("leaves another device's activity to its owner", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		owners := ownersFunc(func(a models.Activity) (bool, error) { return a.Project == "Phone", nil })
		svc := activity.NewService(repo, nil, activity.WithOwners(owners))

		repo.EXPECT().Find(mock.Anything, runningFilter(true)).Return([]models.Activity{
			{Project: "Phone", Description: "Late", StartTime: start},
		}, nil)

		stopped, stopErr := svc.StopAfterWorkingHours(context.Background(), hours, now)
		require.NoError(t, stopErr)
		assert.Nil(t, stopped)
	})

	t.Run("stops an activity this device owns", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		owners := ownersFunc(func(a models.Activity) (bool, error) { return a.Project == "Phone", nil })
		svc := activity.NewService(repo, nil, activity.WithOwners(owners))

		repo.EXPECT().Find(mock.Anything, runningFilter(true)).Return([]models.Activity{
			{Project: "Work", Description: "Late", StartTime: start},
		}, nil)
		repo.EXPECT().Save(mock.MatchedBy(ports.IsAutoStop), mock.Anything).Return(nil)

		stopped, stopErr := svc.StopAfterWorkingHours(context.Background(), hours, now)
		require.NoError(t, stopErr)
		require.NotNil(t, stopped)
	})

	t.Run("fails when the owner is unknown", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		owners := ownersFunc(func(models.Activity) (bool, error) { return false, assert.AnError })
		svc := activity.NewService(repo, nil, activity.WithOwners(owners))

		repo.EXPECT().Find(mock.Anything, runningFilter(true)).Return([]models.Activity{
			{Project: "Work", Description: "Late", StartTime: start},
		}, nil)

		_, stopErr := svc.StopAfterWorkingHours(context.Background(), hours, now)
		require.ErrorIs(t, stopErr, assert.AnError)
	})

	t.Run("leaves the activity running when the owner is unknown", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		owners := ownersFunc(func(models.Activity) (bool, error) { return false, assert.AnError })
		svc := activity.NewService(repo, nil, activity.WithOwners(owners))

		repo.EXPECT().Find(mock.Anything, runningFilter(true)).Return([]models.Activity{
			{Project: "Work", Description: "Late", StartTime: start},
		}, nil)

		stopped, stopErr := svc.StopAfterWorkingHours(context.Background(), hours, now)
		require.ErrorIs(t, stopErr, coreErrors.ErrActivityOwnerUnknown)
		assert.Nil(t, stopped)
	})
}

// ownersFunc is a ports.ActivityOwners that asks a function.
type ownersFunc func(models.Activity) (bool, error)

func (f ownersFunc) OwnedElsewhere(_ context.Context, a models.Activity) (bool, error) {
	return f(a)
}

func TestService_GetByKey_NumbersByStartDay(t *testing.T) {
	repo := portsmocks.NewMockActivityRepository(t)
	svc := activity.NewService(repo, nil)

	overnightStart := time.Date(2026, time.March, 13, 23, 0, 0, 0, time.Local)
	morning := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	// File-like backends return every entry overlapping the day, including one from the day before.
	repo.EXPECT().Find(mock.Anything, mock.Anything).Return([]models.Activity{
		{Project: "overnight", StartTime: overnightStart, EndTime: endedAt(overnightStart, 2*time.Hour)},
		{Project: "morning", StartTime: morning},
	}, nil)

	got, err := svc.GetByKey(context.Background(), "2026-03-14-01")
	require.NoError(t, err)
	assert.Equal(t, "morning", got.Project)

	_, err = svc.GetByKey(context.Background(), "2026-03-14-02")
	require.ErrorIs(t, err, coreErrors.ErrActivityNotFound)

	_, err = svc.GetByKey(context.Background(), "yesterday")
	require.Error(t, err)
}
