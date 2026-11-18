package commands

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/localization"
	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func stubMethodNotConfigured() error {
	return errors.New("stub activity resolver method not configured")
}

type stubActivityResolver struct {
	startFn     func(context.Context, models.StartActivityRequest) (*models.Activity, error)
	stopFn      func(context.Context, models.StopActivityRequest) (*models.Activity, error)
	addFn       func(context.Context, models.AddActivityRequest) (*models.Activity, error)
	listFn      func(context.Context, models.ActivityFilter) ([]models.Activity, error)
	getReportFn func(context.Context, models.ActivityFilter) (*models.Report, error)
	getRecentFn func(context.Context, int) ([]models.Activity, error)
	getLastFn   func(context.Context) (*models.Activity, error)
	updateFn    func(context.Context, models.Activity, models.UpdateActivityRequest) (*models.Activity, error)
	addNoteFn   func(context.Context, models.Activity, string) (*models.Activity, error)
	addTagsFn   func(context.Context, models.Activity, []string) (*models.Activity, error)
	removeFn    func(context.Context, models.Activity) error
	continueFn  func(context.Context, models.ContinueActivityRequest) (*models.Activity, error)
	stopAfterFn func(context.Context, models.WorkingHours, time.Time) (*models.Activity, error)
	getByKeyFn  func(context.Context, string) (*models.Activity, error)
}

// GetByKey falls back to numbering what listFn returns for the key's day, like the service does.
func (s stubActivityResolver) GetByKey(ctx context.Context, key string) (*models.Activity, error) {
	if s.getByKeyFn != nil {
		return s.getByKeyFn(ctx, key)
	}
	if s.listFn == nil {
		return nil, stubMethodNotConfigured()
	}

	date, seq, err := models.ParseActivityKey(key)
	if err != nil {
		return nil, err
	}
	to := date.AddDate(0, 0, 1)
	activities, err := s.listFn(ctx, models.ActivityFilter{FromDate: &date, ToDate: &to})
	if err != nil {
		return nil, err
	}
	activity, err := models.ActivityForSequence(models.ActivitiesStartingOn(activities, date), seq)
	if err != nil {
		return nil, err
	}
	return &activity, nil
}

func (s stubActivityResolver) Continue(ctx context.Context, req models.ContinueActivityRequest) (*models.Activity, error) {
	if s.continueFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.continueFn(ctx, req)
}

func (s stubActivityResolver) StopAfterWorkingHours(
	ctx context.Context,
	hours models.WorkingHours,
	now time.Time,
) (*models.Activity, error) {
	if s.stopAfterFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.stopAfterFn(ctx, hours, now)
}

func (s stubActivityResolver) Start(ctx context.Context, req models.StartActivityRequest) (*models.Activity, error) {
	if s.startFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.startFn(ctx, req)
}

func (s stubActivityResolver) Stop(ctx context.Context, req models.StopActivityRequest) (*models.Activity, error) {
	if s.stopFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.stopFn(ctx, req)
}

func (s stubActivityResolver) Add(ctx context.Context, req models.AddActivityRequest) (*models.Activity, error) {
	if s.addFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.addFn(ctx, req)
}

func (s stubActivityResolver) List(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
	if s.listFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.listFn(ctx, filter)
}

func (s stubActivityResolver) GetReport(ctx context.Context, filter models.ActivityFilter) (*models.Report, error) {
	if s.getReportFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.getReportFn(ctx, filter)
}

func (s stubActivityResolver) GetRecent(ctx context.Context, limit int) ([]models.Activity, error) {
	if s.getRecentFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.getRecentFn(ctx, limit)
}

func (s stubActivityResolver) GetLast(ctx context.Context) (*models.Activity, error) {
	if s.getLastFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.getLastFn(ctx)
}

func (s stubActivityResolver) Update(
	ctx context.Context,
	original models.Activity,
	req models.UpdateActivityRequest,
) (*models.Activity, error) {
	if s.updateFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.updateFn(ctx, original, req)
}

func (s stubActivityResolver) AddNote(
	ctx context.Context,
	activity models.Activity,
	note string,
) (*models.Activity, error) {
	if s.addNoteFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.addNoteFn(ctx, activity, note)
}

func (s stubActivityResolver) AddTags(
	ctx context.Context,
	activity models.Activity,
	tags []string,
) (*models.Activity, error) {
	if s.addTagsFn == nil {
		return nil, stubMethodNotConfigured()
	}
	return s.addTagsFn(ctx, activity, tags)
}

func (s stubActivityResolver) Remove(ctx context.Context, activity models.Activity) error {
	if s.removeFn == nil {
		return nil
	}
	return s.removeFn(ctx, activity)
}

func newTestCLICommand(service *stubActivityResolver) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.SetContext(context.Background())

	ctx := (&appruntime.Runtime{
		ActivityService: service,
		Config:          &config.Config{},
		TimeFormatter:   timeutil.NewFormatter("24"),
		Localizer:       localization.MustNew(localization.LanguageEnglish),
	}).WithContext(cmd.Context())
	cmd.SetContext(ctx)

	return cmd
}
