package commands

import (
	"context"
	"errors"

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
	removeFn    func(context.Context, models.Activity) error
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
