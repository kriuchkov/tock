package commands

import (
	"bytes"
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestRunWatchCmdWritesEmptyNotice(t *testing.T) {
	runner := runWatchProgram
	timeNow := currentActivityTime
	t.Cleanup(func() {
		runWatchProgram = runner
		currentActivityTime = timeNow
	})

	runWatchProgram = func(_ watchModel) error {
		t.Fatalf("program should not run when no activity exists")
		return nil
	}

	service := &stubActivityResolver{
		listFn: func(_ context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
			require.NotNil(t, filter.IsRunning)
			assert.True(t, *filter.IsRunning)
			return []models.Activity{}, nil
		},
	}
	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runWatchCmd(cmd, &watchOptions{})
	require.NoError(t, err)
	assert.Equal(t, "No currently running activities.\n", out.String())
}

func TestRunWatchCmdStopOnExitUsesWriter(t *testing.T) {
	runner := runWatchProgram
	timeNow := currentActivityTime
	t.Cleanup(func() {
		runWatchProgram = runner
		currentActivityTime = timeNow
	})

	runWatchProgram = func(_ watchModel) error {
		return nil
	}
	stopTime := time.Date(2026, time.March, 14, 18, 0, 0, 0, time.Local)
	currentActivityTime = func() time.Time { return stopTime }

	service := &stubActivityResolver{
		listFn: func(_ context.Context, _ models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{{Project: "tock", Description: "watching", StartTime: stopTime.Add(-time.Hour)}}, nil
		},
		stopFn: func(_ context.Context, req models.StopActivityRequest) (*models.Activity, error) {
			assert.Equal(t, stopTime, req.EndTime)
			end := req.EndTime
			return &models.Activity{Project: "tock", Description: "watching", StartTime: stopTime.Add(-time.Hour), EndTime: &end}, nil
		},
	}

	cmd := newTestCLICommand(service)
	var out bytes.Buffer
	cmd.SetOut(&out)

	err := runWatchCmd(cmd, &watchOptions{StopOnExit: true})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Stopped activity: tock | watching")
}

func TestWatchModelPauseTogglesState(t *testing.T) {
	timeNow := currentActivityTime
	t.Cleanup(func() {
		currentActivityTime = timeNow
	})

	pauseTime := time.Date(2026, time.March, 14, 18, 0, 0, 0, time.Local)
	currentActivityTime = func() time.Time { return pauseTime }

	service := &stubActivityResolver{
		stopFn: func(_ context.Context, req models.StopActivityRequest) (*models.Activity, error) {
			assert.Equal(t, pauseTime, req.EndTime)
			end := req.EndTime
			return &models.Activity{Project: "tock", Description: "watching", StartTime: pauseTime.Add(-time.Hour), EndTime: &end}, nil
		},
	}
	model := initialWatchModel(
		models.Activity{Project: "tock", Description: "watching", StartTime: pauseTime.Add(-time.Hour)},
		service,
		DarkTheme(),
		localization.MustNew(localization.LanguageEnglish),
	)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	watchState := updated.(watchModel)
	assert.True(t, watchState.paused)
	assert.NotNil(t, watchState.activity.EndTime)
	assert.Equal(t, pauseTime, *watchState.activity.EndTime)

	resumeTime := pauseTime.Add(15 * time.Minute)
	currentActivityTime = func() time.Time { return resumeTime }
	service.startFn = func(_ context.Context, req models.StartActivityRequest) (*models.Activity, error) {
		assert.Equal(t, resumeTime, req.StartTime)
		return &models.Activity{Project: req.Project, Description: req.Description, StartTime: req.StartTime}, nil
	}

	updated, _ = watchState.Update(tea.KeyMsg{Type: tea.KeySpace})
	watchState = updated.(watchModel)
	assert.False(t, watchState.paused)
	assert.Equal(t, resumeTime, watchState.activity.StartTime)
	assert.Nil(t, watchState.activity.EndTime)
}

func TestWatchModelViewLocalizedPausedState(t *testing.T) {
	end := time.Date(2026, time.March, 14, 10, 0, 0, 0, time.Local)
	model := initialWatchModel(
		models.Activity{Project: "core", Description: "refactor", StartTime: end.Add(-time.Hour), EndTime: &end},
		&stubActivityResolver{},
		DarkTheme(),
		localization.MustNew(localization.LanguageEnglish),
	)
	model.paused = true

	view := model.View()
	assert.Contains(t, view, "PAUSED")
	assert.Contains(t, view, "refactor")
	assert.Contains(t, view, "core")
	assert.Contains(t, view, "quit")
	assert.Contains(t, view, "pause/resume")
}
