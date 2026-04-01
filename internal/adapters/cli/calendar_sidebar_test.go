package cli

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
)

type stubActivityResolver struct {
	getReport func(ctx context.Context, filter dto.ActivityFilter) (*dto.Report, error)
}

func (s stubActivityResolver) Start(context.Context, dto.StartActivityRequest) (*models.Activity, error) {
	panic("unexpected Start call")
}

func (s stubActivityResolver) Stop(context.Context, dto.StopActivityRequest) (*models.Activity, error) {
	panic("unexpected Stop call")
}

func (s stubActivityResolver) Add(context.Context, dto.AddActivityRequest) (*models.Activity, error) {
	panic("unexpected Add call")
}

func (s stubActivityResolver) List(context.Context, dto.ActivityFilter) ([]models.Activity, error) {
	panic("unexpected List call")
}

func (s stubActivityResolver) GetReport(ctx context.Context, filter dto.ActivityFilter) (*dto.Report, error) {
	return s.getReport(ctx, filter)
}

func (s stubActivityResolver) GetRecent(context.Context, int) ([]models.Activity, error) {
	panic("unexpected GetRecent call")
}

func (s stubActivityResolver) GetLast(context.Context) (*models.Activity, error) {
	panic("unexpected GetLast call")
}

func (s stubActivityResolver) Remove(context.Context, models.Activity) error {
	panic("unexpected Remove call")
}

func TestFetchMonthData_CachesCrossMonthDailyReports(t *testing.T) {
	model := reportModel{
		service:      stubActivityResolver{},
		viewDate:     time.Date(2026, time.March, 1, 12, 0, 0, 0, time.Local),
		currentDate:  time.Date(2026, time.March, 1, 12, 0, 0, 0, time.Local),
		monthReports: make(map[int]*dto.Report),
		dailyReports: make(map[string]*dto.Report),
	}

	februaryWeekStart := time.Date(2026, time.February, 23, 9, 0, 0, 0, time.Local)
	februaryWeekEnd := februaryWeekStart.Add(2 * time.Hour)
	marchDayStart := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.Local)
	marchDayEnd := marchDayStart.Add(3 * time.Hour)

	model.service = stubActivityResolver{getReport: func(_ context.Context, filter dto.ActivityFilter) (*dto.Report, error) {
		require.NotNil(t, filter.FromDate)
		require.NotNil(t, filter.ToDate)

		expectedFrom := time.Date(2026, time.February, 15, 0, 0, 0, 0, time.Local)
		expectedTo := time.Date(2026, time.April, 15, 0, 0, 0, 0, time.Local)
		assert.True(t, filter.FromDate.Equal(expectedFrom))
		assert.True(t, filter.ToDate.Equal(expectedTo))

		return &dto.Report{
			Activities: []models.Activity{
				{Project: "emcd", StartTime: februaryWeekStart, EndTime: &februaryWeekEnd},
				{Project: "emcd", StartTime: marchDayStart, EndTime: &marchDayEnd},
			},
			ByProject: make(map[string]dto.ProjectReport),
		}, nil
	}}

	msg := model.fetchMonthData()
	monthMsg, ok := msg.(monthDataMsg)
	require.True(t, ok)

	require.Contains(t, monthMsg.dailyReports, dateKey(februaryWeekStart))
	assert.Equal(t, 2*time.Hour, monthMsg.dailyReports[dateKey(februaryWeekStart)].TotalDuration)

	require.Contains(t, monthMsg.monthReports, 1)
	assert.Equal(t, 3*time.Hour, monthMsg.monthReports[1].TotalDuration)
	assert.NotContains(t, monthMsg.monthReports, 23)
}

func TestRenderWeeklyActivity_UsesCrossMonthDailyReports(t *testing.T) {
	selectedDate := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.Local)
	theme := DarkTheme()

	model := reportModel{
		currentDate: selectedDate,
		viewDate:    selectedDate,
		monthReports: map[int]*dto.Report{
			1: {TotalDuration: time.Hour, ByProject: make(map[string]dto.ProjectReport)},
		},
		dailyReports: map[string]*dto.Report{
			dateKey(time.Date(2026, time.February, 23, 0, 0, 0, 0, time.Local)): {
				TotalDuration: 2 * time.Hour,
				ByProject:     make(map[string]dto.ProjectReport),
			},
			dateKey(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.Local)): {
				TotalDuration: time.Hour,
				ByProject:     make(map[string]dto.ProjectReport),
			},
		},
		styles: InitStyles(theme),
		theme:  theme,
	}

	rendered := stripANSISequences(model.renderWeeklyActivity())
	assert.Regexp(t, `(?m)^Mo\s+█+$`, rendered)
	assert.Regexp(t, `(?m)^Su\s+█+`, rendered)
}

func TestFetchMonthData_SplitsActivitiesAcrossMidnight(t *testing.T) {
	model := reportModel{
		service:      stubActivityResolver{},
		viewDate:     time.Date(2026, time.April, 1, 12, 0, 0, 0, time.Local),
		currentDate:  time.Date(2026, time.April, 1, 12, 0, 0, 0, time.Local),
		monthReports: make(map[int]*dto.Report),
		dailyReports: make(map[string]*dto.Report),
	}

	overnightStart := time.Date(2026, time.March, 31, 23, 30, 0, 0, time.Local)
	overnightEnd := time.Date(2026, time.April, 1, 0, 56, 0, 0, time.Local)
	aprilStart := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.Local)

	model.service = stubActivityResolver{getReport: func(_ context.Context, filter dto.ActivityFilter) (*dto.Report, error) {
		require.NotNil(t, filter.FromDate)
		require.NotNil(t, filter.ToDate)

		return &dto.Report{
			Activities: []models.Activity{
				{Project: "eve", StartTime: overnightStart, EndTime: &overnightEnd},
			},
			ByProject: make(map[string]dto.ProjectReport),
		}, nil
	}}

	msg := model.fetchMonthData()
	monthMsg, ok := msg.(monthDataMsg)
	require.True(t, ok)

	marchKey := dateKey(overnightStart)
	aprilKey := dateKey(aprilStart)
	require.Contains(t, monthMsg.dailyReports, marchKey)
	require.Contains(t, monthMsg.dailyReports, aprilKey)
	assert.Equal(t, 30*time.Minute, monthMsg.dailyReports[marchKey].TotalDuration)
	assert.Equal(t, 56*time.Minute, monthMsg.dailyReports[aprilKey].TotalDuration)

	require.Contains(t, monthMsg.monthReports, 1)
	aprilReport := monthMsg.monthReports[1]
	assert.Equal(t, 56*time.Minute, aprilReport.TotalDuration)
	require.Contains(t, aprilReport.ByProject, "eve")
	assert.Equal(t, 56*time.Minute, aprilReport.ByProject["eve"].Duration)
	require.Len(t, aprilReport.Activities, 1)
	assert.Equal(t, aprilStart, aprilReport.Activities[0].StartTime)
	if assert.NotNil(t, aprilReport.Activities[0].EndTime) {
		assert.Equal(t, overnightEnd, *aprilReport.Activities[0].EndTime)
	}
}

func stripANSISequences(input string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(input, "")
}
