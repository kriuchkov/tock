package commands

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/config"
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
