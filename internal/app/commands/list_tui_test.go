package commands

import (
	"context"
	"slices"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestRunListCmdInvokesProgram(t *testing.T) {
	runner := runListProgram
	t.Cleanup(func() { runListProgram = runner })

	called := false
	runListProgram = func(model listModel) error {
		called = true
		assert.NotNil(t, model.service)
		assert.NotNil(t, model.timeFormat)
		assert.NotNil(t, model.loc)
		return nil
	}

	cmd := newTestCLICommand(&stubActivityResolver{})
	require.NoError(t, runListCmd(cmd, nil))
	assert.True(t, called)
}

func TestListModelViewLocalizesHeaderAndHelp(t *testing.T) {
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{}, nil
		},
	}
	model := initialListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish))
	model.selectedDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)

	view := model.View()
	assert.Contains(t, view, "<< Saturday, 04 Apr 2026 >>")
	assert.Contains(t, view, "Press 'q' to quit")
	assert.Contains(t, view, "Key")
	assert.Contains(t, view, "Time")
	assert.Contains(t, view, "Project")
	assert.Contains(t, view, "Description")
	assert.Contains(t, view, "Duration")
	assert.Contains(t, view, "Tags")
	assert.Contains(t, view, "Notes")
}

func TestListModelWindowSizeResizesTable(t *testing.T) {
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{}, nil
		},
	}
	model := initialListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish))

	model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// 30 terminal lines minus the chrome around the table (header, blanks, help) minus the table's
	// own header rows gives the visible data rows.
	assert.Equal(t, 30-dailyViewChromeLines-2, model.table.Height())
	assert.Equal(t, 30, lipgloss.Height(model.View()))

	model.Update(tea.WindowSizeMsg{Width: 100, Height: 3})
	assert.Equal(t, minTableHeight-2, model.table.Height())
}

func TestListModelNavigateUsesNextAvailableDate(t *testing.T) {
	service := &stubActivityResolver{
		listFn: func(_ context.Context, _ models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{
				{Project: "core", StartTime: time.Date(2026, time.April, 4, 9, 0, 0, 0, time.Local)},
				{Project: "ops", StartTime: time.Date(2026, time.April, 6, 9, 0, 0, 0, time.Local)},
			}, nil
		},
	}
	model := initialListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish))
	model.selectedDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)

	model.navigate(1)
	assert.Equal(t, time.Date(2026, time.April, 6, 0, 0, 0, 0, time.Local), model.selectedDate)
}

func TestListModelRenderTableBuildsStableKeys(t *testing.T) {
	service := &stubActivityResolver{}
	model := initialListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish))
	model.selectedDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)

	firstStart := time.Date(2026, time.April, 4, 9, 0, 0, 0, time.Local)
	firstEnd := firstStart.Add(time.Hour)
	secondStart := time.Date(2026, time.April, 4, 11, 0, 0, 0, time.Local)
	secondEnd := secondStart.Add(30 * time.Minute)

	// Deliberately out of order: the keys must follow the start time, because
	// that is how `tock edit`/`tock remove` resolve a DATE-INDEX.
	model.renderTable([]models.Activity{
		{Project: "ops", Description: "second", StartTime: secondStart, EndTime: &secondEnd},
		{Project: "core", Description: "first", StartTime: firstStart, EndTime: &firstEnd},
	})

	rows := model.table.Rows()
	require.Len(t, rows, 2)
	assert.Equal(t, "2026-04-04-01", rows[0][0])
	assert.Equal(t, "2026-04-04-02", rows[1][0])
	assert.Equal(t, "core", rows[0][2])
	assert.Equal(t, "ops", rows[1][2])
}

func newDeleteTestListModel(t *testing.T, removeFn func(context.Context, models.Activity) error) listModel {
	t.Helper()
	day := time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)
	activities := []models.Activity{
		{Project: "core", Description: "planning", StartTime: day.Add(9 * time.Hour)},
		{Project: "ops", Description: "deploy", StartTime: day.Add(11 * time.Hour)},
	}
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return slices.Clone(activities), nil
		},
		removeFn: func(ctx context.Context, activity models.Activity) error {
			if err := removeFn(ctx, activity); err != nil {
				return err
			}
			activities = slices.DeleteFunc(activities, func(a models.Activity) bool {
				return a.StartTime.Equal(activity.StartTime)
			})
			return nil
		},
	}
	model := initialListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish))
	model.selectedDate = day
	model.updateActivities()
	return model
}

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestListModelDeleteRemovesSelectedActivityOnConfirm(t *testing.T) {
	var removed []models.Activity
	model := newDeleteTestListModel(t, func(_ context.Context, activity models.Activity) error {
		removed = append(removed, activity)
		return nil
	})
	model.table.MoveDown(1)

	model.Update(runeKey('x'))
	require.NotNil(t, model.pendingDelete)
	assert.Contains(t, model.View(), "Delete ops: deploy? [y/N]")
	assert.Empty(t, removed)

	model.Update(runeKey('y'))
	require.Len(t, removed, 1)
	assert.Equal(t, "ops", removed[0].Project)
	assert.Nil(t, model.pendingDelete)
	require.Len(t, model.activities, 1)
	assert.Equal(t, "core", model.activities[0].Project)
	assert.Equal(t, 0, model.table.Cursor())
	assert.Contains(t, model.View(), "'x' to delete")
}

func TestListModelDeleteCancelsOnOtherKey(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error {
		t.Fatal("Remove must not be called when the delete is cancelled")
		return nil
	})

	model.Update(runeKey('x'))
	require.NotNil(t, model.pendingDelete)

	model.Update(runeKey('n'))
	assert.Nil(t, model.pendingDelete)
	assert.Len(t, model.activities, 2)
}

func TestListModelDeleteIgnoresEmptyDay(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error {
		t.Fatal("Remove must not be called on an empty day")
		return nil
	})
	model.selectedDate = time.Date(2026, time.April, 5, 0, 0, 0, 0, time.Local)
	model.updateActivities()

	model.Update(runeKey('x'))
	assert.Nil(t, model.pendingDelete)
}

func TestListModelDeleteReportsRemoveError(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error {
		return errors.New("disk full")
	})

	model.Update(runeKey('x'))
	model.Update(runeKey('y'))
	require.Error(t, model.err)
	assert.Contains(t, model.View(), "disk full")
}

func TestListModelSelectsFirstRowAfterEmptyDay(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error { return nil })

	model.selectedDate = time.Date(2026, time.April, 5, 0, 0, 0, 0, time.Local)
	model.updateActivities()
	model.selectedDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)
	model.updateActivities()

	assert.Equal(t, 0, model.table.Cursor())
}
