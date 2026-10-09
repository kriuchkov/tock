package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
)

func editTestActivity() models.Activity {
	start := time.Date(2026, time.March, 14, 9, 0, 0, 0, time.Local)
	end := start.Add(2 * time.Hour)
	return models.Activity{Project: "tock", Description: "review", StartTime: start, EndTime: &end}
}

// captureEditRequest records the request `tock edit` builds and returns the
// activity it would produce.
func captureEditRequest(activity models.Activity, got *models.UpdateActivityRequest) func(
	context.Context, models.Activity, models.UpdateActivityRequest,
) (*models.Activity, error) {
	return func(_ context.Context, _ models.Activity, req models.UpdateActivityRequest) (*models.Activity, error) {
		*got = req
		updated := req.Apply(activity)
		return &updated, nil
	}
}

// stubEditProgram replaces the Bubble Tea runner for the duration of a test.
func stubEditProgram(t *testing.T, run func(*editModel) error) {
	t.Helper()
	runner := runEditProgram
	t.Cleanup(func() { runEditProgram = runner })
	runEditProgram = run
}

func newEditTestCmd(service *stubActivityResolver, out *bytes.Buffer, args ...string) *cobra.Command {
	cmd := NewEditCmd()
	cmd.SetContext(newTestCLICommand(service).Context())
	cmd.SetArgs(args)
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SilenceUsage = true
	return cmd
}

func TestRunEditCmdFixesEndTimeOfLastActivity(t *testing.T) {
	activity := editTestActivity()
	var gotReq models.UpdateActivityRequest

	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) { return &activity, nil },
		updateFn: func(
			_ context.Context, original models.Activity, req models.UpdateActivityRequest,
		) (*models.Activity, error) {
			gotReq = req
			assert.Equal(t, activity.StartTime, original.StartTime)
			updated := req.Apply(original)
			return &updated, nil
		},
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out, "-e", "17:30").Execute())

	require.NotNil(t, gotReq.EndTime)
	assert.Equal(t, time.Date(2026, time.March, 14, 17, 30, 0, 0, time.Local), *gotReq.EndTime)
	assert.Contains(t, out.String(), "Activity updated: tock | review")
}

func TestRunEditCmdMovesActivityToAnotherDay(t *testing.T) {
	activity := editTestActivity()
	var gotReq models.UpdateActivityRequest

	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{activity}, nil
		},
		updateFn: captureEditRequest(activity, &gotReq),
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out, "2026-03-14-01", "--day", "2026-03-15").Execute())

	require.NotNil(t, gotReq.StartTime)
	require.NotNil(t, gotReq.EndTime)
	assert.Equal(t, time.Date(2026, time.March, 15, 9, 0, 0, 0, time.Local), *gotReq.StartTime)
	assert.Equal(t, time.Date(2026, time.March, 15, 11, 0, 0, 0, time.Local), *gotReq.EndTime)
}

func TestRunEditCmdSetsDurationFromStart(t *testing.T) {
	activity := editTestActivity()
	var gotReq models.UpdateActivityRequest

	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) { return &activity, nil },
		updateFn:  captureEditRequest(activity, &gotReq),
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out, "--duration", "90m").Execute())

	require.NotNil(t, gotReq.EndTime)
	assert.Equal(t, time.Date(2026, time.March, 14, 10, 30, 0, 0, time.Local), *gotReq.EndTime)
}

func TestRunEditCmdClearEndMakesActivityRunning(t *testing.T) {
	activity := editTestActivity()

	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) { return &activity, nil },
		updateFn: func(
			_ context.Context, _ models.Activity, req models.UpdateActivityRequest,
		) (*models.Activity, error) {
			assert.True(t, req.ClearEndTime)
			updated := req.Apply(activity)
			return &updated, nil
		},
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out, "--clear-end").Execute())
	assert.Contains(t, out.String(), "running")
}

func TestRunEditCmdRejectsClearEndWithEndTime(t *testing.T) {
	activity := editTestActivity()
	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) { return &activity, nil },
		updateFn: func(
			_ context.Context, _ models.Activity, _ models.UpdateActivityRequest,
		) (*models.Activity, error) {
			t.Fatal("update must not be called for conflicting flags")
			return nil, nil //nolint:nilnil // unreachable, required by the signature
		},
	}

	var out bytes.Buffer
	err := newEditTestCmd(service, &out, "--clear-end", "-e", "17:00").Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--clear-end")
}

func TestRunEditCmdJSONOutput(t *testing.T) {
	activity := editTestActivity()
	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) { return &activity, nil },
		updateFn: func(
			_ context.Context, _ models.Activity, req models.UpdateActivityRequest,
		) (*models.Activity, error) {
			updated := req.Apply(activity)
			return &updated, nil
		},
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out, "-d", "retro", "--json").Execute())

	var decoded models.Activity
	require.NoError(t, json.Unmarshal(out.Bytes(), &decoded))
	assert.Equal(t, "retro", decoded.Description)
}

func TestRunEditCmdReplacesTagsAndNotes(t *testing.T) {
	activity := editTestActivity()
	var gotReq models.UpdateActivityRequest

	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) { return &activity, nil },
		updateFn:  captureEditRequest(activity, &gotReq),
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out, "--tag", "review,urgent", "--note", "corrected").Execute())

	require.NotNil(t, gotReq.Tags)
	require.NotNil(t, gotReq.Notes)
	assert.Equal(t, []string{"review", "urgent"}, *gotReq.Tags)
	assert.Equal(t, "corrected", *gotReq.Notes)
}

func TestRunEditCmdWithoutFlagsOpensPicker(t *testing.T) {
	runner := runEditProgram
	t.Cleanup(func() { runEditProgram = runner })

	var opened *editModel
	runEditProgram = func(model *editModel) error {
		opened = model
		return nil
	}

	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{editTestActivity()}, nil
		},
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out).Execute())

	require.NotNil(t, opened)
	assert.False(t, opened.formOpen)
	assert.False(t, opened.closeAfterSave)
}

func TestRunEditCmdWithIndexOnlyOpensForm(t *testing.T) {
	runner := runEditProgram
	t.Cleanup(func() { runEditProgram = runner })

	var opened *editModel
	runEditProgram = func(model *editModel) error {
		opened = model
		return nil
	}

	activity := editTestActivity()
	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{activity}, nil
		},
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out, "2026-03-14-01").Execute())

	require.NotNil(t, opened)
	assert.True(t, opened.formOpen)
	assert.True(t, opened.closeAfterSave)
	assert.Equal(t, "tock", opened.form.fields[editFieldProject].value)
	assert.Equal(t, "2026-03-14 09:00", opened.form.fields[editFieldStart].value)
}

func TestRunEditCmdPrintsEditorResultAfterExit(t *testing.T) {
	stubEditProgram(t, func(model *editModel) error {
		model.status = "Saved: tock | retro"
		return nil
	})

	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{editTestActivity()}, nil
		},
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out).Execute())
	assert.Contains(t, out.String(), "Saved: tock | retro")
}

func TestRunEditCmdCombinesDayWithDuration(t *testing.T) {
	activity := editTestActivity()
	var gotReq models.UpdateActivityRequest

	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) { return &activity, nil },
		updateFn:  captureEditRequest(activity, &gotReq),
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(service, &out, "--day", "2026-03-15", "--duration", "45m").Execute())

	require.NotNil(t, gotReq.StartTime)
	require.NotNil(t, gotReq.EndTime)
	assert.Equal(t, time.Date(2026, time.March, 15, 9, 0, 0, 0, time.Local), *gotReq.StartTime)
	assert.Equal(t, time.Date(2026, time.March, 15, 9, 45, 0, 0, time.Local), *gotReq.EndTime)
}

func TestRunEditCmdRejectsBlankProject(t *testing.T) {
	activity := editTestActivity()
	service := &stubActivityResolver{
		getLastFn: func(context.Context) (*models.Activity, error) { return &activity, nil },
		updateFn: func(
			_ context.Context, _ models.Activity, _ models.UpdateActivityRequest,
		) (*models.Activity, error) {
			return nil, coreErrors.ErrProjectRequired
		},
	}

	var out bytes.Buffer
	err := newEditTestCmd(service, &out, "-p", "").Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project name is required")
}

func TestRunEditCmdShiftsEndWhenDayAndStartAreCombined(t *testing.T) {
	activity := editTestActivity()
	var gotReq models.UpdateActivityRequest

	service := &stubActivityResolver{
		listFn: func(context.Context, models.ActivityFilter) ([]models.Activity, error) {
			return []models.Activity{activity}, nil
		},
		updateFn: captureEditRequest(activity, &gotReq),
	}

	var out bytes.Buffer
	require.NoError(t, newEditTestCmd(
		service, &out, "2026-03-14-01", "--day", "2026-03-15", "--start", "08:00",
	).Execute())

	applied := gotReq.Apply(activity)
	assert.Equal(t, time.Date(2026, time.March, 15, 8, 0, 0, 0, time.Local), applied.StartTime)
	require.NotNil(t, applied.EndTime)
	assert.Equal(t, time.Date(2026, time.March, 15, 10, 0, 0, 0, time.Local), *applied.EndTime,
		"the end time must follow the start to the new day instead of staying behind")
}

func TestRunEditCmdRejectsJSONWithoutFieldFlags(t *testing.T) {
	stubEditProgram(t, func(*editModel) error {
		t.Fatal("the editor must not open when JSON output was requested")
		return nil
	})

	var out bytes.Buffer
	err := newEditTestCmd(&stubActivityResolver{}, &out, "--json").Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--json")
}
