package commands

import (
	"context"
	"testing"
	"time"

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
	require.NoError(t, runListCmd(cmd))
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

	model.renderTable([]models.Activity{
		{Project: "core", Description: "first", StartTime: firstStart, EndTime: &firstEnd},
		{Project: "ops", Description: "second", StartTime: secondStart, EndTime: &secondEnd},
	})

	rows := model.table.Rows()
	require.Len(t, rows, 2)
	assert.Equal(t, "2026-04-04-01", rows[0][0])
	assert.Equal(t, "2026-04-04-02", rows[1][0])
	assert.Equal(t, "core", rows[0][2])
	assert.Equal(t, "ops", rows[1][2])
}
