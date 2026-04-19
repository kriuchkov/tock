package commands

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestRootPersistentPreRunLoadsRuntimeDependencies(t *testing.T) {
	loader := loadRuntime
	t.Cleanup(func() {
		loadRuntime = loader
	})

	svc := &stubActivityResolver{}
	cfg := &config.Config{}
	tf := timeutil.NewFormatter("24")
	loc := localization.MustNew(localization.LanguageEnglish)
	var gotReq appruntime.Request

	loadRuntime = func(_ context.Context, req appruntime.Request) (*appruntime.Runtime, error) {
		gotReq = req
		return &appruntime.Runtime{
			ActivityService: svc,
			Config:          cfg,
			TimeFormatter:   tf,
			Localizer:       loc,
		}, nil
	}

	root := NewRootCmd()
	root.SetContext(context.Background())
	require.NoError(t, root.PersistentPreRunE(root, nil))

	assert.Equal(t, appruntime.Request{}, gotReq)
	rt, ok := appruntime.FromContext(root.Context())
	require.True(t, ok)
	assert.Same(t, svc, rt.ActivityService)
	assert.Same(t, cfg, rt.Config)
	assert.Same(t, tf, rt.TimeFormatter)
	assert.Same(t, loc, rt.Localizer)
}

func TestRootPersistentPreRunSkipsVersion(t *testing.T) {
	loader := loadRuntime
	t.Cleanup(func() {
		loadRuntime = loader
	})

	called := false
	loadRuntime = func(context.Context, appruntime.Request) (*appruntime.Runtime, error) {
		called = true
		return nil, errors.New("unexpected runtime load")
	}

	root := NewRootCmd()
	cmd := &cobra.Command{Use: "version"}
	cmd.SetContext(context.Background())

	require.NoError(t, root.PersistentPreRunE(cmd, nil))
	assert.False(t, called)
	_, ok := appruntime.FromContext(cmd.Context())
	assert.False(t, ok)
}

func TestRootPersistentPreRunAutoStopsActivityOutsideWorkingHours(t *testing.T) {
	loader := loadRuntime
	clock := currentWorkingHoursTime
	t.Cleanup(func() {
		loadRuntime = loader
		currentWorkingHoursTime = clock
	})

	running := models.Activity{
		Project:     "tock",
		Description: "review",
		StartTime:   time.Date(2026, time.April, 21, 16, 0, 0, 0, time.Local),
	}

	svc := &stubActivityResolver{
		listFn: func(_ context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
			require.NotNil(t, filter.IsRunning)
			assert.True(t, *filter.IsRunning)
			return []models.Activity{running}, nil
		},
		stopFn: func(_ context.Context, req models.StopActivityRequest) (*models.Activity, error) {
			end := req.EndTime
			assert.True(t, end.Equal(time.Date(2026, time.April, 21, 17, 30, 0, 0, time.Local)))
			running.EndTime = &end
			return &running, nil
		},
	}

	loadRuntime = func(_ context.Context, req appruntime.Request) (*appruntime.Runtime, error) {
		assert.Equal(t, appruntime.Request{}, req)
		return &appruntime.Runtime{
			ActivityService: svc,
			Config: &config.Config{
				WorkingHours: config.WorkingHoursConfig{
					Enabled: true,
					StopAt:  "17:30",
				},
			},
			TimeFormatter: timeutil.NewFormatter("24"),
			Localizer:     localization.MustNew(localization.LanguageEnglish),
		}, nil
	}
	currentWorkingHoursTime = func() time.Time {
		return time.Date(2026, time.April, 21, 18, 0, 0, 0, time.Local)
	}

	root := NewRootCmd()
	root.SetContext(context.Background())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	require.NoError(t, root.PersistentPreRunE(root, nil))

	autoStopped, ok := autoStoppedActivityFromContext(root.Context())
	require.True(t, ok)
	require.NotNil(t, autoStopped.EndTime)
	assert.True(t, autoStopped.EndTime.Equal(time.Date(2026, time.April, 21, 17, 30, 0, 0, time.Local)))
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "Automatically stopped activity: tock | review at 17:30")
}
