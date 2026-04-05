package watching_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/watching"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

type stubResolver struct {
	listFn  func(context.Context, models.ActivityFilter) ([]models.Activity, error)
	startFn func(context.Context, models.StartActivityRequest) (*models.Activity, error)
	stopFn  func(context.Context, models.StopActivityRequest) (*models.Activity, error)
}

func (s stubResolver) Start(ctx context.Context, req models.StartActivityRequest) (*models.Activity, error) {
	if s.startFn == nil {
		return nil, nil
	}
	return s.startFn(ctx, req)
}

func (s stubResolver) Stop(ctx context.Context, req models.StopActivityRequest) (*models.Activity, error) {
	if s.stopFn == nil {
		return nil, nil
	}
	return s.stopFn(ctx, req)
}

func (s stubResolver) Add(context.Context, models.AddActivityRequest) (*models.Activity, error) {
	return nil, nil
}

func (s stubResolver) List(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
	if s.listFn == nil {
		return nil, nil
	}
	return s.listFn(ctx, filter)
}

func (s stubResolver) GetReport(context.Context, models.ActivityFilter) (*models.Report, error) {
	return nil, nil
}

func (s stubResolver) GetRecent(context.Context, int) ([]models.Activity, error) {
	return nil, nil
}

func (s stubResolver) GetLast(context.Context) (*models.Activity, error) {
	return nil, nil
}

func (s stubResolver) Remove(context.Context, models.Activity) error {
	return nil
}

var _ ports.ActivityResolver = stubResolver{}

func TestFindCurrentActivity(t *testing.T) {
	start := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)

	t.Run("returns first running activity", func(t *testing.T) {
		resolver := stubResolver{
			listFn: func(_ context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
				require.NotNil(t, filter.IsRunning)
				assert.True(t, *filter.IsRunning)
				return []models.Activity{{Project: "tock", StartTime: start, EndTime: &end}}, nil
			},
		}

		activity, err := watching.FindCurrentActivity(context.Background(), resolver)
		require.NoError(t, err)
		require.NotNil(t, activity)
		assert.Equal(t, "tock", activity.Project)
	})

	t.Run("returns no active activity", func(t *testing.T) {
		resolver := stubResolver{listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) { return nil, nil }}
		activity, err := watching.FindCurrentActivity(context.Background(), resolver)
		require.ErrorIs(t, err, coreErrors.ErrNoActiveActivity)
		assert.Nil(t, activity)
	})
}

func TestTogglePause(t *testing.T) {
	at := time.Date(2026, time.March, 14, 18, 0, 0, 0, time.Local)
	activity := models.Activity{Project: "tock", Description: "cleanup", StartTime: at.Add(-time.Hour)}

	t.Run("resumes paused activity", func(t *testing.T) {
		resolver := stubResolver{
			startFn: func(_ context.Context, req models.StartActivityRequest) (*models.Activity, error) {
				assert.Equal(t, activity.Project, req.Project)
				assert.Equal(t, activity.Description, req.Description)
				assert.Equal(t, at, req.StartTime)
				started := models.Activity{Project: req.Project, Description: req.Description, StartTime: req.StartTime}
				return &started, nil
			},
		}

		updated, paused, err := watching.TogglePause(context.Background(), resolver, activity, true, at)
		require.NoError(t, err)
		assert.False(t, paused)
		assert.Equal(t, at, updated.StartTime)
	})

	t.Run("pauses running activity", func(t *testing.T) {
		resolver := stubResolver{
			stopFn: func(_ context.Context, req models.StopActivityRequest) (*models.Activity, error) {
				assert.Equal(t, at, req.EndTime)
				end := req.EndTime
				stopped := activity
				stopped.EndTime = &end
				return &stopped, nil
			},
		}

		updated, paused, err := watching.TogglePause(context.Background(), resolver, activity, false, at)
		require.NoError(t, err)
		assert.True(t, paused)
		require.NotNil(t, updated.EndTime)
		assert.Equal(t, at, *updated.EndTime)
	})

	t.Run("pause keeps old activity on stop error", func(t *testing.T) {
		resolver := stubResolver{stopFn: func(context.Context, models.StopActivityRequest) (*models.Activity, error) {
			return nil, errors.New("boom")
		}}

		updated, paused, err := watching.TogglePause(context.Background(), resolver, activity, false, at)
		require.NoError(t, err)
		assert.True(t, paused)
		assert.Equal(t, activity, updated)
	})
}

func TestStopOnExit(t *testing.T) {
	at := time.Date(2026, time.March, 14, 18, 0, 0, 0, time.Local)
	resolver := stubResolver{
		stopFn: func(_ context.Context, req models.StopActivityRequest) (*models.Activity, error) {
			end := req.EndTime
			return &models.Activity{Project: "tock", Description: "cleanup", StartTime: end.Add(-time.Hour), EndTime: &end}, nil
		},
	}

	activity, err := watching.StopOnExit(context.Background(), resolver, at)
	require.NoError(t, err)
	require.NotNil(t, activity)
	assert.Equal(t, "tock", activity.Project)
}
