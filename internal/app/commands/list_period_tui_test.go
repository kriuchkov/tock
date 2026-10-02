package commands

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

func TestRunListCmdDispatchesDailyToListProgram(t *testing.T) {
	restore := runListProgram
	t.Cleanup(func() { runListProgram = restore })
	restorePeriod := runListPeriodProgram
	t.Cleanup(func() { runListPeriodProgram = restorePeriod })

	dailyCalled := false
	runListProgram = func(listModel) error { dailyCalled = true; return nil }
	periodCalled := false
	runListPeriodProgram = func(listPeriodModel) error { periodCalled = true; return nil }

	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{}, nil
		},
	}
	cmd := newTestCLICommand(service)

	require.NoError(t, runListCmd(cmd, nil))
	assert.True(t, dailyCalled)
	assert.False(t, periodCalled)
}

func TestRunListCmdDispatchesMonthlyToPeriodProgram(t *testing.T) {
	restore := runListPeriodProgram
	t.Cleanup(func() { runListPeriodProgram = restore })

	called := false
	runListPeriodProgram = func(model listPeriodModel) error {
		called = true
		assert.Equal(t, periodMonthly, model.period)
		assert.NotNil(t, model.service)
		assert.NotNil(t, model.loc)
		return nil
	}

	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{}, nil
		},
	}
	cmd := newTestCLICommand(service)

	require.NoError(t, runListCmd(cmd, []string{periodArgMonthly}))
	assert.True(t, called)
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
	assert.Equal(t, -1, model.table.Cursor())

	// Rows appear: the first row must be highlighted without pressing down.
	activities = []models.Activity{
		{Project: "core", StartTime: day, EndTime: &dayEnd},
		{Project: "core", StartTime: day2, EndTime: &day2End},
	}
	model.reload()
	assert.Equal(t, 0, model.table.Cursor())

	// Moving within a period and switching to another one starts at the top again.
	model.table.MoveDown(1)
	require.Equal(t, 1, model.table.Cursor())
	model.reload()
	assert.Equal(t, 0, model.table.Cursor())
}

func TestListPeriodModelWindowSizeResizesTable(t *testing.T) {
	service := &stubActivityResolver{
		getReportFn: func(context.Context, models.ActivityFilter) (*models.Report, error) {
			return &models.Report{}, nil
		},
	}
	model := newTestPeriodModel(service, periodMonthly)

	model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// 30 terminal lines minus the chrome around the table (header, blanks, total, help) minus the
	// table's own header rows gives the visible data rows.
	assert.Equal(t, 30-periodViewChromeLines-2, model.table.Height())

	model.Update(tea.WindowSizeMsg{Width: 100, Height: 5})
	assert.Equal(t, minPeriodTableHeight-2, model.table.Height())
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
	rows := model.table.Rows()
	require.Len(t, rows, 4)

	assert.Equal(t, "Mo 06 Jul", rows[0][0])
	assert.Equal(t, "core", rows[0][1])
	assert.Equal(t, "3h", rows[0][2])
	assert.Empty(t, rows[1][0]) // continuation row has no date label
	assert.Equal(t, "ops", rows[1][1])
	assert.Equal(t, "1h", rows[1][2])
	assert.Equal(t, "4h", rows[2][2]) // subtotal row

	assert.Equal(t, "We 08 Jul", rows[3][0])
	assert.Equal(t, "core", rows[3][1])
	assert.Equal(t, "2h", rows[3][2])
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
	rows := model.table.Rows()
	require.Len(t, rows, 2)
	assert.Equal(t, "January", rows[0][0])
	assert.Equal(t, "March", rows[1][0])
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

	view := model.View()
	assert.Contains(t, view, "July 2026")
	assert.Contains(t, view, "Total: 2h")
	assert.Contains(t, view, "left/right to change period")
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
