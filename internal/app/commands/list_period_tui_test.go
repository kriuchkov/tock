package commands

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestParseListPeriod(t *testing.T) {
	tests := []struct {
		arg    string
		want   listPeriod
		wantOK bool
	}{
		{"", periodDaily, true},
		{periodArgDaily, periodDaily, true},
		{"d", periodDaily, true},
		{periodArgWeekly, periodWeekly, true},
		{"w", periodWeekly, true},
		{periodArgMonthly, periodMonthly, true},
		{"Monthly", periodMonthly, true},
		{"m", periodMonthly, true},
		{periodArgYearly, periodYearly, true},
		{"y", periodYearly, true},
		{"nonsense", periodDaily, false},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			got, ok := parseListPeriod(tt.arg)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestRunListCmdOpensTheMonthlyView(t *testing.T) {
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{}, nil
		},
	}
	cmd := newTestCLICommand(service)
	shell := captureShell(t, func() error { return runListCmd(cmd, []string{periodArgMonthly}) })

	screen, ok := shell.current().(*listScreen)
	require.True(t, ok)
	assert.Equal(t, periodMonthly, screen.period)
	assert.Nil(t, screen.day, "the day view is built only when shown")
	assert.NotNil(t, screen.periods.service)
	assert.NotNil(t, screen.periods.loc)
}

func TestRunListCmdRejectsInvalidPeriod(t *testing.T) {
	cmd := newTestCLICommand(&stubActivityResolver{})
	err := runListCmd(cmd, []string{"nonsense"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonsense")
}

func TestPeriodRange(t *testing.T) {
	anchor := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.Local)

	t.Run(periodArgMonthly, func(t *testing.T) {
		start, end := periodRange(periodMonthly, anchor)
		assert.Equal(t, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.Local), start)
		assert.Equal(t, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.Local), end)
	})

	t.Run("weekly starts monday", func(t *testing.T) {
		// 2026-07-15 is a Wednesday; the week starts Monday 2026-07-13.
		start, end := periodRange(periodWeekly, anchor)
		assert.Equal(t, time.Date(2026, time.July, 13, 0, 0, 0, 0, time.Local), start)
		assert.Equal(t, time.Date(2026, time.July, 20, 0, 0, 0, 0, time.Local), end)
	})

	t.Run(periodArgYearly, func(t *testing.T) {
		start, end := periodRange(periodYearly, anchor)
		assert.Equal(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.Local), start)
		assert.Equal(t, time.Date(2027, time.January, 1, 0, 0, 0, 0, time.Local), end)
	})
}

func TestShiftPeriodNormalizesMonthEnd(t *testing.T) {
	// Anchor on the 31st must not skip February when moving by a month.
	anchor := time.Date(2026, time.January, 31, 0, 0, 0, 0, time.Local)
	next := shiftPeriod(periodMonthly, anchor, 1)
	assert.Equal(t, time.Date(2026, time.February, 1, 0, 0, 0, 0, time.Local), next)

	prevWeek := shiftPeriod(periodWeekly, time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local), -1)
	assert.Equal(t, time.Date(2026, time.July, 6, 0, 0, 0, 0, time.Local), prevWeek)

	nextYear := shiftPeriod(periodYearly, time.Date(2026, time.June, 1, 0, 0, 0, 0, time.Local), 1)
	assert.Equal(t, time.Date(2027, time.January, 1, 0, 0, 0, 0, time.Local), nextYear)
}

func TestListPeriodModelReloadFetchesPeriodWindow(t *testing.T) {
	var gotFilter models.ActivityFilter
	service := &stubActivityResolver{
		getReportFn: func(_ context.Context, filter models.ActivityFilter) (*models.Report, error) {
			gotFilter = filter
			return &models.Report{}, nil
		},
	}
	model := newTestPeriodModel(service, periodMonthly)
	model.anchor = time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local)

	model.reload()

	require.NotNil(t, gotFilter.FromDate)
	require.NotNil(t, gotFilter.ToDate)
	assert.Equal(t, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.Local), *gotFilter.FromDate)
	assert.Equal(t, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.Local), *gotFilter.ToDate)
}

func TestListPeriodModelReloadResetsCursor(t *testing.T) {
	day := time.Date(2026, time.July, 6, 9, 0, 0, 0, time.Local)
	dayEnd := day.Add(time.Hour)
	day2 := day.AddDate(0, 0, 1)
	day2End := day2.Add(time.Hour)

	var activities []models.Activity
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{Activities: activities}, nil
		},
	}
	model := newTestPeriodModel(service, periodMonthly)
	model.anchor = time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local)

	// Empty period: nothing to select.
	model.reload()
	assert.Empty(t, model.rows)
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 0, model.cursor)

	// Rows appear: the first row must be highlighted without pressing down.
	activities = []models.Activity{
		{Project: "core", StartTime: day, EndTime: &dayEnd},
		{Project: "core", StartTime: day2, EndTime: &day2End},
	}
	model.reload()
	assert.Equal(t, 0, model.cursor)

	// Moving within a period and switching to another one starts at the top again.
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, 1, model.cursor)
	model.reload()
	assert.Equal(t, 0, model.cursor)
}

func TestListPeriodModelScrollsToTheSelectedRow(t *testing.T) {
	var activities []models.Activity
	for day := 1; day <= 20; day++ {
		start := time.Date(2026, time.July, day, 9, 0, 0, 0, time.Local)
		end := start.Add(time.Duration(day) * time.Minute)
		activities = append(activities, models.Activity{Project: fmt.Sprintf("p%02d", day), StartTime: start, EndTime: &end})
	}
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{Activities: activities}, nil
		},
	}
	model := newTestPeriodModel(service, periodMonthly)
	model.anchor = time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local)
	model.reload()

	model.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	assert.Equal(t, 12, lipgloss.Height(model.View()))
	assert.NotContains(t, model.View(), "p20")

	for range 19 {
		model.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	view := model.View()
	assert.Equal(t, 12, lipgloss.Height(view))
	assert.Contains(t, view, "p20")
	assert.NotContains(t, view, "p01")
	assert.Contains(t, view, "DATE")
}

func TestListPeriodModelBuildsBucketsAndRows(t *testing.T) {
	day1 := time.Date(2026, time.July, 6, 9, 0, 0, 0, time.Local)
	day1CoreEnd := day1.Add(3 * time.Hour)
	day1Ops := time.Date(2026, time.July, 6, 13, 0, 0, 0, time.Local)
	day1OpsEnd := day1Ops.Add(time.Hour)
	day2 := time.Date(2026, time.July, 8, 9, 0, 0, 0, time.Local)
	day2End := day2.Add(2 * time.Hour)

	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{Activities: []models.Activity{
				{Project: "core", StartTime: day1, EndTime: &day1CoreEnd},
				{Project: "ops", StartTime: day1Ops, EndTime: &day1OpsEnd},
				{Project: "core", StartTime: day2, EndTime: &day2End},
			}}, nil
		},
	}
	model := newTestPeriodModel(service, periodMonthly)
	model.anchor = time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local)
	model.reload()

	require.Len(t, model.buckets, 2)
	assert.Equal(t, 6*time.Hour, model.total)

	// Day 1: two projects (core before ops) + a subtotal row = 3 rows.
	// Day 2: one project, no subtotal = 1 row.
	assert.Equal(t, []periodRow{
		{label: "Mo 06 Jul", project: "core", duration: 3 * time.Hour},
		{project: "ops", duration: time.Hour}, // continuation row has no date label
		{duration: 4 * time.Hour, subtotal: true},
		{label: "We 08 Jul", project: "core", duration: 2 * time.Hour},
	}, model.rows)
}

func TestListPeriodModelYearlyBucketsByMonth(t *testing.T) {
	jan := time.Date(2026, time.January, 10, 9, 0, 0, 0, time.Local)
	janEnd := jan.Add(2 * time.Hour)
	mar := time.Date(2026, time.March, 5, 9, 0, 0, 0, time.Local)
	marEnd := mar.Add(time.Hour)

	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{Activities: []models.Activity{
				{Project: "core", StartTime: jan, EndTime: &janEnd},
				{Project: "core", StartTime: mar, EndTime: &marEnd},
			}}, nil
		},
	}
	model := newTestPeriodModel(service, periodYearly)
	model.anchor = time.Date(2026, time.July, 1, 0, 0, 0, 0, time.Local)
	model.reload()

	require.Len(t, model.buckets, 2)
	require.Len(t, model.rows, 2)
	assert.Equal(t, "January", model.rows[0].label)
	assert.Equal(t, "March", model.rows[1].label)
}

func TestListPeriodModelViewLocalizesHeaderTotalAndHelp(t *testing.T) {
	start := time.Date(2026, time.July, 6, 9, 0, 0, 0, time.Local)
	end := start.Add(2 * time.Hour)
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{Activities: []models.Activity{
				{Project: "core", StartTime: start, EndTime: &end},
			}}, nil
		},
	}
	model := newTestPeriodModel(service, periodMonthly)
	model.anchor = time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local)
	model.reload()

	model.Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	view := model.View()
	assert.Contains(t, view, "July 2026 ←→")
	assert.Contains(t, view, "total 2h")
	assert.Contains(t, view, "←→ month   ↑↓ row   tab view   q quit")
	assert.Regexp(t, `Mo 06 Jul\s+core\s+▇+\s+2h`, view)
}

func TestListPeriodModelViewEmptyState(t *testing.T) {
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{}, nil
		},
	}
	model := newTestPeriodModel(service, periodMonthly)
	model.reload()

	assert.Contains(t, model.View(), "No activities in this period")
}

func TestListPeriodModelNavigationShiftsAnchorAndRefetches(t *testing.T) {
	calls := 0
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			calls++
			return &models.Report{}, nil
		},
	}
	model := newTestPeriodModel(service, periodMonthly)
	model.anchor = time.Date(2026, time.July, 15, 0, 0, 0, 0, time.Local)
	callsAfterInit := calls

	model.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.Local), model.anchor)

	model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.Local), model.anchor)

	assert.Equal(t, callsAfterInit+2, calls, "each navigation refetches the report")
}

// newTestPeriodModel builds a period model without running the Bubble Tea program,
// using an English localizer and default (empty) config.
func newTestPeriodModel(service *stubActivityResolver, period listPeriod) listPeriodModel {
	return initialListPeriodModel(service, &config.Config{}, localization.MustNew(localization.LanguageEnglish), period)
}
