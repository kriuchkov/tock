package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/dto"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
	"github.com/kriuchkov/tock/internal/services/activity"
)

func TestService_Stop(t *testing.T) {
	t.Run("stop running activity", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo)
		ctx := context.Background()

		now := time.Now()
		runningAct := models.Activity{
			Project:     "test",
			Description: "running",
			StartTime:   now.Add(-1 * time.Hour),
			EndTime:     nil,
		}

		// Expect Find with IsRunning=true
		repo.EXPECT().Find(ctx, mock.MatchedBy(func(f dto.ActivityFilter) bool {
			return f.IsRunning != nil && *f.IsRunning
		})).Return([]models.Activity{runningAct}, nil)

		// Expect Save with EndTime set
		repo.EXPECT().Save(ctx, mock.MatchedBy(func(a models.Activity) bool {
			return a.Project == runningAct.Project && a.EndTime != nil
		})).Return(nil)

		req := dto.StopActivityRequest{EndTime: now}
		stopped, err := svc.Stop(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, stopped.EndTime)
	})

	t.Run("stop with multiple running activities (should pick latest)", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo)
		ctx := context.Background()

		now := time.Now()
		// Older running activity (maybe zombie)
		act1 := models.Activity{
			Project:   "old",
			StartTime: now.Add(-5 * time.Hour),
			EndTime:   nil,
		}
		// Newer running activity
		act2 := models.Activity{
			Project:   "new",
			StartTime: now.Add(-1 * time.Hour),
			EndTime:   nil,
		}

		repo.EXPECT().Find(ctx, mock.MatchedBy(func(f dto.ActivityFilter) bool {
			return f.IsRunning != nil && *f.IsRunning
		})).Return([]models.Activity{act1, act2}, nil)

		// Expect Save to be called for act2 (the latest one)
		repo.EXPECT().Save(ctx, mock.MatchedBy(func(a models.Activity) bool {
			return a.Project == "new" && a.EndTime != nil
		})).Return(nil)

		req := dto.StopActivityRequest{EndTime: now}
		stopped, err := svc.Stop(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "new", stopped.Project)
	})

	t.Run("no running activity", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo)
		ctx := context.Background()

		repo.EXPECT().Find(ctx, mock.MatchedBy(func(f dto.ActivityFilter) bool {
			return f.IsRunning != nil && *f.IsRunning
		})).Return([]models.Activity{}, nil)

		req := dto.StopActivityRequest{EndTime: time.Now()}
		_, err := svc.Stop(ctx, req)
		assert.ErrorIs(t, err, coreErrors.ErrNoActiveActivity)
	})
}

func TestService_Start(t *testing.T) {
	t.Run("start stops currently running", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo)
		ctx := context.Background()

		runningAct := models.Activity{
			Project:   "prev",
			StartTime: time.Now().Add(-1 * time.Hour),
		}

		// 1. Find running
		repo.EXPECT().Find(ctx, mock.MatchedBy(func(f dto.ActivityFilter) bool {
			return f.IsRunning != nil && *f.IsRunning
		})).Return([]models.Activity{runningAct}, nil)

		// 2. Save (stop) running
		repo.EXPECT().Save(ctx, mock.MatchedBy(func(a models.Activity) bool {
			return a.Project == "prev" && a.EndTime != nil
		})).Return(nil)

		// 3. Save new
		repo.EXPECT().Save(ctx, mock.MatchedBy(func(a models.Activity) bool {
			return a.Project == "new" && a.EndTime == nil
		})).Return(nil)

		req := dto.StartActivityRequest{Project: "new", Description: "task"}
		started, err := svc.Start(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "new", started.Project)
	})

	t.Run("start with specific time stops running activity at that time", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo)
		ctx := context.Background()

		now := time.Now()
		newStartTime := now.Add(-10 * time.Minute)

		runningAct := models.Activity{
			Project:   "prev",
			StartTime: now.Add(-1 * time.Hour),
		}

		// 1. Find running
		repo.EXPECT().Find(ctx, mock.MatchedBy(func(f dto.ActivityFilter) bool {
			return f.IsRunning != nil && *f.IsRunning
		})).Return([]models.Activity{runningAct}, nil)

		// 2. Save (stop) running - Expect EndTime to be newStartTime
		repo.EXPECT().Save(ctx, mock.MatchedBy(func(a models.Activity) bool {
			if a.Project != "prev" {
				return false
			}
			if a.EndTime == nil {
				return false
			}
			return a.EndTime.Equal(newStartTime)
		})).Return(nil)

		// 3. Save new
		repo.EXPECT().Save(ctx, mock.MatchedBy(func(a models.Activity) bool {
			return a.Project == "new" && a.StartTime.Equal(newStartTime)
		})).Return(nil)

		req := dto.StartActivityRequest{
			Project:     "new",
			Description: "task",
			StartTime:   newStartTime,
		}
		started, err := svc.Start(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "new", started.Project)
		assert.Equal(t, newStartTime, started.StartTime)
	})

	t.Run("start time before running start time falls back to now", func(t *testing.T) {
		repo := portsmocks.NewMockActivityRepository(t)
		svc := activity.NewService(repo)
		ctx := context.Background()

		now := time.Now()
		// Start time is VERY old, before the running activity started
		newStartTime := now.Add(-2 * time.Hour)

		runningAct := models.Activity{
			Project:   "prev",
			StartTime: now.Add(-1 * time.Hour), // Started 1 hour ago
		}

		// 1. Find running
		repo.EXPECT().Find(ctx, mock.MatchedBy(func(f dto.ActivityFilter) bool {
			return f.IsRunning != nil && *f.IsRunning
		})).Return([]models.Activity{runningAct}, nil)

		// 2. Save (stop) running - Expect EndTime to correspond to Now (approx) or at least NOT be newStartTime
		// The implementation uses time.Now() when overlap is detected.
		// Since time.Now() moves, we can't test equality exactly, but we can check it's after runningAct.StartTime
		repo.EXPECT().Save(ctx, mock.MatchedBy(func(a models.Activity) bool {
			if a.Project != "prev" {
				return false
			}
			if a.EndTime == nil {
				return false
			}
			// Should be roughly now, definitely not 2 hours ago
			return a.EndTime.After(runningAct.StartTime)
		})).Return(nil)

		// 3. Save new
		repo.EXPECT().Save(ctx, mock.MatchedBy(func(a models.Activity) bool {
			return a.Project == "new" && a.StartTime.Equal(newStartTime)
		})).Return(nil)

		req := dto.StartActivityRequest{
			Project:     "new",
			Description: "task",
			StartTime:   newStartTime,
		}
		started, err := svc.Start(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "new", started.Project)
	})
}
