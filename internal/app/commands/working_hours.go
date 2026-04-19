package commands

import (
	"context"
	"strings"
	"time"

	"github.com/go-faster/errors"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/config"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
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

func reconcileWorkingHours(ctx context.Context, now time.Time) (context.Context, error) {
	rt, ok := appruntime.FromContext(ctx)
	if !ok || rt == nil || rt.Config == nil || rt.ActivityService == nil {
		return ctx, nil
	}

	workingHours := rt.Config.WorkingHours
	if !workingHours.Enabled || strings.TrimSpace(workingHours.StopAt) == "" {
		return ctx, nil
	}

	isRunning := true
	activities, err := rt.ActivityService.List(ctx, models.ActivityFilter{IsRunning: &isRunning})
	if err != nil {
		return ctx, errors.Wrap(err, "list running activities")
	}
	if len(activities) == 0 {
		return ctx, nil
	}

	latest := latestRunningActivity(activities)
	stopTime, shouldStop, err := resolveWorkingHoursAutoStopTime(now, latest.StartTime, workingHours, rt.TimeFormatter)
	if err != nil {
		return ctx, err
	}
	if !shouldStop {
		return ctx, nil
	}

	stopped, err := rt.ActivityService.Stop(ctx, models.StopActivityRequest{EndTime: stopTime})
	if err != nil {
		if errors.Is(err, coreErrors.ErrNoActiveActivity) {
			return ctx, nil
		}
		return ctx, errors.Wrap(err, "auto-stop activity")
	}
	if stopped == nil {
		return ctx, nil
	}

	return withAutoStoppedActivity(ctx, stopped), nil
}

func resolveWorkingHoursAutoStopTime(
	now time.Time,
	start time.Time,
	workingHours config.WorkingHoursConfig,
	formatter *timeutil.Formatter,
) (time.Time, bool, error) {
	if !workingHours.Enabled || strings.TrimSpace(workingHours.StopAt) == "" {
		return time.Time{}, false, nil
	}

	if formatter == nil {
		formatter = timeutil.NewFormatter("24")
	}

	parsedStopTime, err := formatter.ParseTime(workingHours.StopAt)
	if err != nil {
		return time.Time{}, false, errors.Wrap(err, "parse working hours stop time")
	}

	allowedWeekdays, err := parseWorkingHoursWeekdays(workingHours.Weekdays)
	if err != nil {
		return time.Time{}, false, err
	}

	startLocal := start.In(time.Local)
	nowLocal := now.In(time.Local)
	if nowLocal.Before(startLocal) {
		return time.Time{}, false, nil
	}

	firstDay := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, time.Local)
	for day := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, time.Local); !day.Before(firstDay); day = day.AddDate(0, 0, -1) {
		if !allowedWeekdays[day.Weekday()] {
			continue
		}

		cutoff := time.Date(
			day.Year(),
			day.Month(),
			day.Day(),
			parsedStopTime.Hour(),
			parsedStopTime.Minute(),
			0,
			0,
			time.Local,
		)

		if cutoff.After(nowLocal) || cutoff.Before(startLocal) {
			continue
		}

		return cutoff, true, nil
	}

	return time.Time{}, false, nil
}

func parseWorkingHoursWeekdays(raw string) (map[time.Weekday]bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "mon,tue,wed,thu,fri"
	}

	weekdays := make(map[time.Weekday]bool, 7)
	for token := range strings.SplitSeq(trimmed, ",") {
		switch strings.ToLower(strings.TrimSpace(token)) {
		case "mon", "monday":
			weekdays[time.Monday] = true
		case "tue", "tues", "tuesday":
			weekdays[time.Tuesday] = true
		case "wed", "wednesday":
			weekdays[time.Wednesday] = true
		case "thu", "thur", "thurs", "thursday":
			weekdays[time.Thursday] = true
		case "fri", "friday":
			weekdays[time.Friday] = true
		case "sat", "saturday":
			weekdays[time.Saturday] = true
		case "sun", "sunday":
			weekdays[time.Sunday] = true
		case "all", "*":
			return map[time.Weekday]bool{
				time.Sunday:    true,
				time.Monday:    true,
				time.Tuesday:   true,
				time.Wednesday: true,
				time.Thursday:  true,
				time.Friday:    true,
				time.Saturday:  true,
			}, nil
		case "":
			continue
		default:
			return nil, errors.Errorf("invalid working hours weekday: %q", token)
		}
	}

	if len(weekdays) == 0 {
		return nil, errors.New("working hours weekdays cannot be empty")
	}

	return weekdays, nil
}

func latestRunningActivity(activities []models.Activity) models.Activity {
	latest := activities[0]
	for i := 1; i < len(activities); i++ {
		if activities[i].StartTime.After(latest.StartTime) {
			latest = activities[i]
		}
	}
	return latest
}
