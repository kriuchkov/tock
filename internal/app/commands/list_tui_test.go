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

func TestRunListCmdOpensTheListTab(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})
	shell := captureShell(t, func() error { return runListCmd(cmd, nil) })

	screen, ok := shell.current().(*listScreen)
	require.True(t, ok)
	require.Equal(t, periodDaily, screen.period)
	assert.NotNil(t, screen.day.service)
	assert.NotNil(t, screen.day.timeFormat)
	assert.NotNil(t, screen.day.loc)
	assert.Equal(t, 1, screen.day.tabs.active)
}

func TestListModelViewLocalizesHeaderAndHelp(t *testing.T) {
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{}, nil
		},
	}
	model := initialListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish), DarkTheme())
	model.selectedDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)
	model.Update(tea.WindowSizeMsg{Width: 140, Height: 20})

	view := model.View()
	assert.Contains(t, view, "Saturday, 04 Apr 2026 ←→")
	assert.Contains(t, view, " day   week   month   year")
	assert.Contains(t, view, "No activities on this day.")
	assert.Contains(t, view, "←→ day   ↑↓ entry   tab view   q quit")
	assert.Equal(t, 20, lipgloss.Height(view), "the footer is on the last line")
}

func TestListModelViewShowsTheColumnsThatFit(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error { return nil })

	model.Update(tea.WindowSizeMsg{Width: 140, Height: 20})
	view := model.View()
	assert.Regexp(t, `KEY\s+START\s+END\s+PROJECT\s+DESCRIPTION\s+TAGS\s+NOTES\s+TIME`, view)
	assert.Regexp(t, `● 2026-04-04-02\s+11:00\s+now\s+ops\s+deploy`, view, "a running activity ends now")
	assert.Contains(t, view, "←→ day   ↑↓ entry   e edit   c continue   x delete   tab view   q quit")

	model.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	view = model.View()
	assert.Regexp(t, `START\s+END\s+PROJECT\s+DESCRIPTION\s+TIME`, view)
	assert.NotContains(t, view, "KEY")
	assert.NotContains(t, view, "NOTES")
}

func TestListModelFillsTheTerminal(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error { return nil })

	model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	assert.Equal(t, 30, lipgloss.Height(model.View()))
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 8})
	assert.Equal(t, 8, lipgloss.Height(model.View()))
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
	model := initialListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish), DarkTheme())
	model.selectedDate = time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)

	model.navigate(1)
	assert.Equal(t, time.Date(2026, time.April, 6, 0, 0, 0, 0, time.Local), model.selectedDate)
}

func TestListModelBuildsStableKeys(t *testing.T) {
	firstStart := time.Date(2026, time.April, 4, 9, 0, 0, 0, time.Local)
	firstEnd := firstStart.Add(time.Hour)
	secondStart := time.Date(2026, time.April, 4, 11, 0, 0, 0, time.Local)
	secondEnd := secondStart.Add(30 * time.Minute)
	service := &stubActivityResolver{
		// Deliberately out of order: the keys must follow the start time, because
		// that is how `tock edit`/`tock remove` resolve a DATE-INDEX.
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{
				{Project: "ops", Description: "second", StartTime: secondStart, EndTime: &secondEnd},
				{Project: "core", Description: "first", StartTime: firstStart, EndTime: &firstEnd},
			}, nil
		},
	}
	model := newListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish), DarkTheme(), firstStart)
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 20})

	view := model.View()
	assert.Regexp(t, `2026-04-04-01\s+09:00\s+10:00\s+core\s+first\s+1h`, view)
	assert.Regexp(t, `2026-04-04-02\s+11:00\s+11:30\s+ops\s+second\s+30m`, view)
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
	model := initialListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish), DarkTheme())
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
	model.Update(tea.KeyMsg{Type: tea.KeyDown})

	model.Update(runeKey('x'))
	require.NotNil(t, model.pendingDelete)
	assert.Contains(t, model.View(), "Delete ops: deploy? [y/N]")
	assert.Empty(t, removed)

	model.Update(runeKey('y'))
	require.Len(t, removed, 1)
	assert.Equal(t, "ops", removed[0].Project)
	assert.Nil(t, model.pendingDelete)
	require.Len(t, model.list.activities, 1)
	assert.Equal(t, "core", model.list.activities[0].Project)
	assert.Equal(t, 0, model.list.cursor)
	assert.Contains(t, model.View(), "x delete")
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
	assert.Len(t, model.list.activities, 2)
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

	assert.Equal(t, 0, model.list.cursor)
}

func TestListModelEditsTheSelectedActivity(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error { return nil })
	var req models.UpdateActivityRequest
	service := model.service.(*stubActivityResolver)
	service.updateFn = func(_ context.Context, original models.Activity, r models.UpdateActivityRequest) (*models.Activity, error) {
		req = r
		updated := original
		updated.Description = *r.Description
		return &updated, nil
	}

	model.Update(runeKey('e'))
	require.NotNil(t, model.edit)
	assert.True(t, model.modal(), "digits go to the form")
	view := model.View()
	assert.Contains(t, view, "2026-04-04-01")
	assert.Contains(t, view, "core · Sat 4 Apr")
	assert.Contains(t, view, "tab next field   enter save   ctrl+u clear field   esc back")

	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model.Update(runeKey('2'))
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.Nil(t, model.edit)
	require.NotNil(t, req.Description)
	assert.Equal(t, "2", *req.Description)
	assert.Contains(t, model.View(), "✓ Saved: core | 2")
}

func TestListModelEditKeepsTheFormOnError(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error { return nil })

	model.Update(runeKey('e'))
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, model.edit)
	assert.Contains(t, model.View(), "✗ ")

	model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, model.edit)
}

func TestListModelContinuesTheSelectedActivity(t *testing.T) {
	model := newDeleteTestListModel(t, func(context.Context, models.Activity) error { return nil })
	var started models.StartActivityRequest
	service := model.service.(*stubActivityResolver)
	service.startFn = func(_ context.Context, req models.StartActivityRequest) (*models.Activity, error) {
		started = req
		return &models.Activity{Project: req.Project, Description: req.Description, StartTime: req.StartTime}, nil
	}

	model.Update(runeKey('c'))
	assert.Equal(t, "core", started.Project)
	assert.Equal(t, "planning", started.Description)
	assert.Contains(t, model.View(), "✓ Started: core | planning")
}

func TestListModelShowsTwelveHourTimesWhole(t *testing.T) {
	start := time.Date(2026, time.April, 4, 8, 48, 0, 0, time.Local)
	end := start.Add(90 * time.Minute)
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{{Project: "core", Description: "Review", StartTime: start, EndTime: &end}}, nil
		},
	}
	model := newListModel(service, timeutil.NewFormatter("12"), localization.MustNew(localization.LanguageEnglish), DarkTheme(), start)
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 12})

	view := model.View()
	assert.Contains(t, view, "08:48 AM")
	assert.Contains(t, view, "10:18 AM")
}

func TestListModelPagesThroughTheDay(t *testing.T) {
	day := time.Date(2026, time.April, 4, 0, 0, 0, 0, time.Local)
	var activities []models.Activity
	for i := range 30 {
		activities = append(activities, models.Activity{Project: "core", StartTime: day.Add(time.Duration(i) * 10 * time.Minute)})
	}
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) { return activities, nil },
	}
	model := newListModel(service, timeutil.NewFormatter("24"), localization.MustNew(localization.LanguageEnglish), DarkTheme(), day)
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	model.View()

	model.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, model.list.page, model.list.cursor, "pgdown moves a screen")
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	assert.Equal(t, 29, model.list.cursor)
	model.Update(tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, model.list.cursor)
}
