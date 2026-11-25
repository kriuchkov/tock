package activity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
	"github.com/kriuchkov/tock/internal/services/activity"
)

func isRunningFilter(f models.ActivityFilter) bool {
	return f.IsRunning != nil && *f.IsRunning
}

func notAutoStop(ctx context.Context) bool {
	return !ports.IsAutoStop(ctx)
}

// Starting an activity stops the running ones under the auto-stop mark, so sync writes those ends
// only for activities this device owns. The new activity itself is saved without the mark.
func TestService_Start_MarksTheStopsItMakes(t *testing.T) {
	now := time.Now()
	start := now.Add(-10 * time.Minute)
	running := []models.Activity{
		{Project: "phone", Description: "started elsewhere", StartTime: now.Add(-2 * time.Hour)},
		{Project: "mac", Description: "started here", StartTime: now.Add(-1 * time.Hour)},
	}

	repo := portsmocks.NewMockActivityRepository(t)
	repo.EXPECT().Find(mock.Anything, mock.MatchedBy(isRunningFilter)).Return(running, nil)

	marked := mock.MatchedBy(ports.IsAutoStop)
	unmarked := mock.MatchedBy(notAutoStop)
	for _, act := range running {
		repo.EXPECT().Save(marked, mock.MatchedBy(func(a models.Activity) bool {
			return a.Project == act.Project && a.EndTime != nil && a.EndTime.Equal(start)
		})).Return(nil).Once()
	}
	repo.EXPECT().Save(unmarked, mock.MatchedBy(func(a models.Activity) bool {
		return a.Project == "new" && a.EndTime == nil
	})).Return(nil).Once()

	svc := activity.NewService(repo, nil)
	got, err := svc.Start(context.Background(), models.StartActivityRequest{Project: "new", Description: "task", StartTime: start})
	require.NoError(t, err)
	assert.Equal(t, "new", got.Project)
}

// Continue starts through Start, so the activity it stops is marked the same way.
func TestService_Continue_MarksTheStopItMakes(t *testing.T) {
	now := time.Now()
	last := models.Activity{Project: "backend", Description: "api", StartTime: now.Add(-1 * time.Hour)}

	repo := portsmocks.NewMockActivityRepository(t)
	repo.EXPECT().Find(mock.Anything, models.ActivityFilter{}).Return([]models.Activity{last}, nil)
	repo.EXPECT().Find(mock.Anything, mock.MatchedBy(isRunningFilter)).Return([]models.Activity{last}, nil)
	repo.EXPECT().Save(mock.MatchedBy(ports.IsAutoStop), mock.MatchedBy(func(a models.Activity) bool {
		return a.StartTime.Equal(last.StartTime) && a.EndTime != nil
	})).Return(nil).Once()
	repo.EXPECT().Save(mock.MatchedBy(notAutoStop), mock.MatchedBy(func(a models.Activity) bool {
		return a.Project == "backend" && a.EndTime == nil
	})).Return(nil).Once()

	svc := activity.NewService(repo, nil)
	_, err := svc.Continue(context.Background(), models.ContinueActivityRequest{StartTime: now})
	require.NoError(t, err)
}

// A stop the user asks for is written by whichever device makes it, so it carries no mark.
func TestService_Stop_IsNotAnAutoStop(t *testing.T) {
	now := time.Now()
	running := models.Activity{Project: "phone", Description: "started elsewhere", StartTime: now.Add(-1 * time.Hour)}

	repo := portsmocks.NewMockActivityRepository(t)
	repo.EXPECT().Find(mock.Anything, mock.MatchedBy(isRunningFilter)).Return([]models.Activity{running}, nil)
	repo.EXPECT().Save(mock.MatchedBy(notAutoStop), mock.MatchedBy(func(a models.Activity) bool {
		return a.EndTime != nil && a.EndTime.Equal(now)
	})).Return(nil).Once()

	svc := activity.NewService(repo, nil)
	_, err := svc.Stop(context.Background(), models.StopActivityRequest{EndTime: now})
	require.NoError(t, err)
}

// An update that moves an activity's start removes it at the old start under the move mark, so sync
// takes the save and the removal for one record that moved, not for a deletion.
func TestService_Update_MarksTheRemovalOfAMove(t *testing.T) {
	start := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)
	newStart := start.Add(30 * time.Minute)
	original := models.Activity{Project: "tock", Description: "work", StartTime: start, EndTime: &end}

	repo := portsmocks.NewMockActivityRepository(t)
	repo.EXPECT().Find(mock.Anything, mock.Anything).Return([]models.Activity{original}, nil).Twice()
	repo.EXPECT().Save(mock.MatchedBy(func(ctx context.Context) bool {
		_, marked := ports.MovedTo(ctx)
		return !marked
	}), mock.Anything).Return(nil).Once()
	repo.EXPECT().Remove(mock.MatchedBy(func(ctx context.Context) bool {
		to, marked := ports.MovedTo(ctx)
		return marked && to.Equal(newStart)
	}), original).Return(nil).Once()

	svc := activity.NewService(repo, nil)
	_, err := svc.Update(context.Background(), original, models.UpdateActivityRequest{StartTime: &newStart})
	require.NoError(t, err)
}
