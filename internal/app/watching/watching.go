package watching

import (
	"context"
	"time"

	"github.com/go-faster/errors"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

func FindCurrentActivity(ctx context.Context, resolver ports.ActivityResolver) (*models.Activity, error) {
	isRunning := true
	activities, err := resolver.List(ctx, models.ActivityFilter{IsRunning: &isRunning})
	if err != nil {
		return nil, errors.Wrap(err, "list activities")
	}
	if len(activities) == 0 {
		return nil, coreErrors.ErrNoActiveActivity
	}
	return &activities[0], nil
}

func TogglePause(
	ctx context.Context,
	resolver ports.ActivityResolver,
	activity models.Activity,
	paused bool,
	at time.Time,
) (models.Activity, bool, error) {
	if paused {
		started, err := resolver.Start(ctx, models.StartActivityRequest{
			Project:     activity.Project,
			Description: activity.Description,
			StartTime:   at,
		})
		if err != nil {
			return activity, paused, errors.Wrap(err, "resume activity")
		}
		if started == nil {
			return activity, paused, nil
		}
		return *started, false, nil
	}

	stopped, err := resolver.Stop(ctx, models.StopActivityRequest{EndTime: at})
	if err != nil {
		return activity, true, errors.Wrap(err, "pause activity")
	}
	if stopped == nil {
		return activity, true, nil
	}
	return *stopped, true, nil
}

func StopOnExit(ctx context.Context, resolver ports.ActivityResolver, at time.Time) (*models.Activity, error) {
	stopped, err := resolver.Stop(ctx, models.StopActivityRequest{EndTime: at})
	if err != nil {
		return nil, errors.Wrap(err, "stop activity")
	}
	return stopped, nil
}
