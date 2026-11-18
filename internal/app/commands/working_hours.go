package commands

import (
	"context"
	"time"

	"github.com/go-faster/errors"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/core/models"
)

var currentWorkingHoursTime = time.Now

type autoStoppedActivityKey struct{}

func withAutoStoppedActivity(ctx context.Context, activity *models.Activity) context.Context {
	if activity == nil {
		return ctx
	}
	return context.WithValue(ctx, autoStoppedActivityKey{}, activity)
}

func autoStoppedActivityFromContext(ctx context.Context) (*models.Activity, bool) {
	activity, ok := ctx.Value(autoStoppedActivityKey{}).(*models.Activity)
	return activity, ok && activity != nil
}

// reconcileWorkingHours stops an activity left running past the end of the working day.
func reconcileWorkingHours(ctx context.Context, now time.Time) (context.Context, error) {
	rt, ok := appruntime.FromContext(ctx)
	if !ok || rt.ActivityService == nil {
		return ctx, nil
	}

	hours, enabled, err := rt.WorkingHours()
	if err != nil || !enabled {
		return ctx, err
	}

	stopped, err := rt.ActivityService.StopAfterWorkingHours(ctx, hours, now)
	if err != nil {
		return ctx, errors.Wrap(err, "auto-stop activity")
	}
	return withAutoStoppedActivity(ctx, stopped), nil
}
