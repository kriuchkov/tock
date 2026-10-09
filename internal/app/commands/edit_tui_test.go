package commands

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/go-faster/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func newEditTestModel(service *stubActivityResolver) *editModel {
	model := newEditModel(
		context.Background(),
		service,
		timeutil.NewFormatter("24"),
		localization.MustNew(localization.LanguageEnglish),
	)
	model.loadHistory()
	return model
}

func editListService(activities ...models.Activity) *stubActivityResolver {
	return &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return activities, nil
		},
	}
}

func TestEditModelEnterOpensFormForSelectedRow(t *testing.T) {
	activity := editTestActivity()
	model := newEditTestModel(editListService(activity))

	model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, model.formOpen)
	assert.Equal(t, "review", model.form.fields[editFieldDescription].value)
	assert.Equal(t, "2026-03-14 11:00", model.form.fields[editFieldEnd].value)
}

func TestEditModelFormTypingAndSaveCallsUpdate(t *testing.T) {
	activity := editTestActivity()
	var gotReq models.UpdateActivityRequest

	service := editListService(activity)
	service.updateFn = func(
		_ context.Context, original models.Activity, req models.UpdateActivityRequest,
	) (*models.Activity, error) {
		gotReq = req
		assert.Equal(t, activity.StartTime, original.StartTime)
		updated := req.Apply(original)
		return &updated, nil
	}

	model := newEditTestModel(service)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("retro")})
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	require.NotNil(t, gotReq.Description)
	assert.Equal(t, "retro", *gotReq.Description)
	assert.False(t, model.formOpen)
	assert.Contains(t, model.status, "Saved: tock | retro")
}

func TestEditModelFormKeepsFormOpenOnServiceError(t *testing.T) {
	activity := editTestActivity()
	service := editListService(activity)
	service.updateFn = func(
		_ context.Context, _ models.Activity, _ models.UpdateActivityRequest,
	) (*models.Activity, error) {
		return nil, errors.New("another activity already starts at this time")
	}

	model := newEditTestModel(service)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	assert.True(t, model.formOpen)
	assert.Contains(t, model.formErr, "already starts at this time")
	assert.Contains(t, model.View(), "already starts at this time")
}

func TestEditModelEscapeLeavesFormWithoutSaving(t *testing.T) {
	activity := editTestActivity()
	service := editListService(activity)
	service.updateFn = func(
		_ context.Context, _ models.Activity, _ models.UpdateActivityRequest,
	) (*models.Activity, error) {
		t.Fatal("update must not be called after escape")
		return nil, nil //nolint:nilnil // unreachable, required by the signature
	}

	model := newEditTestModel(service)
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	assert.False(t, model.formOpen)
}

func TestBuildFormRequestClearsEndTimeWhenFieldIsEmpty(t *testing.T) {
	tf := timeutil.NewFormatter("24")
	form := newEditForm(editTestActivity(), tf)
	form.fields[editFieldEnd].value = ""

	req, err := buildFormRequest(tf, form)
	require.NoError(t, err)
	assert.True(t, req.ClearEndTime)
	assert.Nil(t, req.EndTime)
}

func TestBuildFormRequestAcceptsTimeOnlyValues(t *testing.T) {
	tf := timeutil.NewFormatter("24")
	form := newEditForm(editTestActivity(), tf)
	form.fields[editFieldStart].value = "08:15"
	form.fields[editFieldEnd].value = "12:45"

	req, err := buildFormRequest(tf, form)
	require.NoError(t, err)
	require.NotNil(t, req.StartTime)
	require.NotNil(t, req.EndTime)
	assert.Equal(t, time.Date(2026, time.March, 14, 8, 15, 0, 0, time.Local), *req.StartTime)
	assert.Equal(t, time.Date(2026, time.March, 14, 12, 45, 0, 0, time.Local), *req.EndTime)
}

func TestEditModelShowsLocalizedTextForDomainErrors(t *testing.T) {
	activity := editTestActivity()
	var gotReq models.UpdateActivityRequest
	service := editListService(activity)
	service.updateFn = func(
		_ context.Context, _ models.Activity, req models.UpdateActivityRequest,
	) (*models.Activity, error) {
		gotReq = req
		return nil, coreErrors.ErrProjectRequired
	}

	model := newEditTestModel(service)
	model.openForm(activity)
	model.form.fields[editFieldProject].value = "   "
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	require.NotNil(t, gotReq.Project, "the form must forward the blanked project so the service can reject it")
	assert.Empty(t, *gotReq.Project)
	assert.True(t, model.formOpen)
	assert.Equal(t, "project name is required", model.formErr)
}

func TestBuildFormRequestRoundTripsTagsAndMultilineNotes(t *testing.T) {
	tf := timeutil.NewFormatter("24")
	activity := editTestActivity()
	activity.Notes = "first\nsecond"
	activity.Tags = []string{"review"}

	form := newEditForm(activity, tf)
	assert.Equal(t, `first\nsecond`, form.fields[editFieldNotes].value)

	req, err := buildFormRequest(tf, form)
	require.NoError(t, err)
	assert.Nil(t, req.Notes, "unchanged notes must not be rewritten")
	assert.Nil(t, req.Tags, "unchanged tags must not be rewritten")

	form.fields[editFieldNotes].value = `first\nthird`
	form.fields[editFieldTags].value = "review, urgent"

	req, err = buildFormRequest(tf, form)
	require.NoError(t, err)
	require.NotNil(t, req.Notes)
	require.NotNil(t, req.Tags)
	assert.Equal(t, "first\nthird", *req.Notes)
	assert.Equal(t, []string{"review", "urgent"}, *req.Tags)
}

func TestEditModelViewsAreLocalized(t *testing.T) {
	activity := editTestActivity()
	model := newEditTestModel(editListService(activity))

	listView := model.View()
	assert.Contains(t, listView, "<< Saturday, 14 Mar 2026 >> select an entry to edit")
	assert.Contains(t, listView, "enter: edit")

	model.openForm(activity)
	formView := model.View()
	assert.Contains(t, formView, "Edit activity (2026-03-14)")
	assert.Contains(t, formView, "Project")
	assert.Contains(t, formView, "Notes")
	assert.Contains(t, formView, "enter: save")
}

func TestEditModelNavigateJumpsToNextDayWithActivities(t *testing.T) {
	first := editTestActivity()
	second := first
	second.StartTime = first.StartTime.AddDate(0, 0, 2)
	second.EndTime = nil
	second.Description = "later"

	model := newEditTestModel(editListService(first, second))
	model.selectedDate = first.StartTime
	model.renderTable()
	require.Equal(t, "review", model.activities[0].Description)

	model.Update(tea.KeyMsg{Type: tea.KeyRight})

	assert.Equal(t, second.StartTime.Day(), model.selectedDate.Day())
	require.Len(t, model.activities, 1)
	assert.Equal(t, "later", model.activities[0].Description)

	model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, first.StartTime.Day(), model.selectedDate.Day())
}

func TestNewEditModelOpensOnLatestDayWithActivities(t *testing.T) {
	activity := editTestActivity()
	model := newEditTestModel(editListService(activity))

	assert.Equal(t, activity.StartTime.Format(time.DateOnly), model.selectedDate.Format(time.DateOnly))
	require.Len(t, model.activities, 1)
}

func TestEditModelListShowsActivityRowsAfterResize(t *testing.T) {
	activity := editTestActivity()
	model := newEditTestModel(editListService(activity))
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	view := model.View()
	assert.Contains(t, view, "2026-03-14-01")
	assert.Contains(t, view, "review")
	assert.Contains(t, view, "09:00 - 11:00")
}

func TestBuildFormRequestKeepsLiteralBackslashesInNotes(t *testing.T) {
	tf := timeutil.NewFormatter("24")
	activity := editTestActivity()
	activity.Notes = `C:\temp and a` + "\n" + "second line"

	form := newEditForm(activity, tf)
	req, err := buildFormRequest(tf, form)
	require.NoError(t, err)
	assert.Nil(t, req.Notes, "re-encoding the same notes must not look like a change")
}

func TestEditModelNavigationReusesTheLoadedHistory(t *testing.T) {
	first := editTestActivity()
	second := first
	second.StartTime = first.StartTime.AddDate(0, 0, 1)
	second.EndTime = nil

	listCalls := 0
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			listCalls++
			return []models.Activity{first, second}, nil
		},
	}

	model := newEditTestModel(service)
	before := listCalls
	model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model.Update(tea.KeyMsg{Type: tea.KeyRight})

	assert.Equal(t, before, listCalls, "navigating days must not re-scan the whole history")
}

func TestEditModelKeepsDistinctKeysForActivitiesInTheSameMinute(t *testing.T) {
	first := editTestActivity()
	second := first
	second.Description = "same minute"

	model := newEditTestModel(editListService(first, second))

	rows := model.table.Rows()
	require.Len(t, rows, 2)
	assert.Equal(t, "2026-03-14-01", rows[0][0])
	assert.Equal(t, "2026-03-14-02", rows[1][0])
}

func TestBuildFormRequestDeduplicatesTagsLikeTheTagCommand(t *testing.T) {
	tf := timeutil.NewFormatter("24")
	form := newEditForm(editTestActivity(), tf)
	form.fields[editFieldTags].value = "review, urgent, review"

	req, err := buildFormRequest(tf, form)
	require.NoError(t, err)
	require.NotNil(t, req.Tags)
	assert.Equal(t, []string{"review", "urgent"}, *req.Tags)
}
