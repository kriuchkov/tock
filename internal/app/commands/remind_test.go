package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/reminders"
	"github.com/kriuchkov/tock/internal/app/localization"
	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/services/reminder"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// remindTestNow is a Monday afternoon.
var remindTestNow = time.Date(2026, time.November, 16, 14, 25, 0, 0, time.Local)

type remindHarness struct {
	t          *testing.T
	svc        ports.ReminderResolver
	activities *stubActivityResolver
	rt         *appruntime.Runtime
}

func newRemindHarness(t *testing.T) *remindHarness {
	t.Helper()
	previous := reminderNow
	reminderNow = func() time.Time { return remindTestNow }
	t.Cleanup(func() { reminderNow = previous })

	activities := &stubActivityResolver{}
	svc := reminder.NewService(reminders.NewRepository(filepath.Join(t.TempDir(), "reminders.yaml")), activities)
	cfg := &config.Config{}
	cfg.Reminders.Snooze = 15 * time.Minute
	return &remindHarness{
		t:          t,
		svc:        svc,
		activities: activities,
		rt: &appruntime.Runtime{
			ActivityService: activities,
			ReminderService: svc,
			Config:          cfg,
			TimeFormatter:   timeutil.NewFormatter("24"),
			Localizer:       localization.MustNew(localization.LanguageEnglish),
		},
	}
}

// run executes `tock remind ARGS` and returns its stdout.
func (h *remindHarness) run(stdin string, args ...string) (string, error) {
	h.t.Helper()
	cmd := NewRemindCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	err := cmd.ExecuteContext(h.rt.WithContext(context.Background()))
	return out.String(), err
}

func (h *remindHarness) mustRun(args ...string) string {
	h.t.Helper()
	out, err := h.run("", args...)
	require.NoError(h.t, err, out)
	return out
}

func TestRemindAddPrintsTheSchedule(t *testing.T) {
	h := newRemindHarness(t)

	out := h.mustRun(
		"add",
		"Team",
		"sync",
		"-p",
		"Work",
		"--on",
		"mon",
		"--at",
		"17:30",
		"--every",
		"weekly:mon,thu",
		"--until",
		"2026-12-31",
	)
	assert.Equal(t, "Added reminder 1: Work | Team sync — Mon 16 Nov 17:30, repeats weekly on Mon, Thu until 31 Dec 2026\n", out)

	out = h.mustRun("add", "Pay rent", "--on", "2026-11-18", "--every", "monthly:last-wed")
	assert.Equal(t, "Added reminder 2: Pay rent — Wed 25 Nov (all day), repeats monthly on last Wed\n", out)

	out = h.mustRun("add", "Water", "--every", "3d", "--count", "4", "--json")
	var added models.Reminder
	require.NoError(t, json.Unmarshal([]byte(out), &added))
	assert.Equal(t, 3, added.ID)
	assert.True(t, added.AllDay)
}

func TestRemindAddRejectsBadInput(t *testing.T) {
	h := newRemindHarness(t)

	_, err := h.run("", "add", "Once", "--until", "2026-12-01")
	require.EqualError(t, err, "--until and --count need a repeat (--every)")

	_, err = h.run("", "add", "Bad", "--every", "hourly")
	require.ErrorContains(t, err, `invalid repeat "hourly"`)

	_, err = h.run("", "add", "Bad", "--notify", "soon")
	require.ErrorContains(t, err, "invalid --notify")

	_, err = h.run("", "add", "--json")
	require.EqualError(t, err, "reminder title is required")
}

func TestRemindListShowsOverdueAndUpcoming(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Send invoice", "-p", "Admin", "--on", "2026-10-16", "--at", "09:00", "--every", "monthly")
	h.mustRun("add", "Call bank", "--on", "tomorrow", "--at", "11:00")
	h.mustRun("add", "Next month", "--on", "2026-12-20")

	out := h.mustRun("list")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 3, out)
	assert.Contains(t, lines[1], "Send invoice")
	assert.Contains(t, lines[1], "! overdue 5h25m")
	assert.Contains(t, lines[2], "Call bank")
	assert.Contains(t, lines[2], "in 20h35m")

	var overdue []reminderOccurrenceJSON
	require.NoError(t, json.Unmarshal([]byte(h.mustRun("list", "--overdue", "--json")), &overdue))
	require.Len(t, overdue, 1)
	assert.Equal(t, "overdue", overdue[0].Status)
	assert.Equal(t, "monthly", overdue[0].Every)

	out = h.mustRun("list", "--all")
	assert.Contains(t, out, "Next month")

	_, err := h.run("", "list", "--today", "--all")
	require.Error(t, err)
}

func TestRemindDoneAndUndo(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Standup", "-p", "Work", "--at", "09:30", "--every", "weekdays")

	out := h.mustRun("done", "1")
	assert.Equal(t, "Done: Standup (Mon 16 Nov 09:30). Next: Tue 17 Nov 09:30.\n", out)

	out = h.mustRun("done", "1", "--undo")
	assert.Equal(t, "Not done any more: Standup (Mon 16 Nov 09:30)\n", out)

	_, err := h.run("", "done", "1", "--date", "2026-11-21")
	require.EqualError(t, err, "reminder 1 has no occurrence on that day")

	_, err = h.run("", "done", "7")
	require.EqualError(t, err, "reminder 7 not found")

	_, err = h.run("", "done", "x")
	require.EqualError(t, err, `invalid reminder ID "x"`)
}

func TestRemindSnooze(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Standup", "--at", "09:30", "--every", "weekdays")

	assert.Equal(t, "Snoozed: Standup until 14:40\n", h.mustRun("snooze", "1"), "the configured default")
	assert.Equal(t, "Snoozed: Standup until 15:25\n", h.mustRun("snooze", "1", "1h"))
	assert.Equal(t, "Snoozed: Standup until 17:45\n", h.mustRun("snooze", "1", "17:45"))
	assert.Equal(t, "Snoozed: Standup until Tue 17 Nov 08:00\n", h.mustRun("snooze", "1", "08:00"), "a time already gone is tomorrow's")

	_, err := h.run("", "snooze", "1", "-5m")
	require.Error(t, err)
}

func TestRemindStartTracksTheOccurrence(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Team sync", "-p", "Work", "--at", "13:00", "--every", "weekly")

	var started models.StartActivityRequest
	h.activities.startFn = func(_ context.Context, req models.StartActivityRequest) (*models.Activity, error) {
		started = req
		return &models.Activity{Project: req.Project, Description: req.Description, StartTime: req.StartTime}, nil
	}

	out := h.mustRun("start", "1", "-t", "14:00")
	assert.Equal(t, "Work", started.Project)
	assert.Equal(t, "Team sync", started.Description)
	assert.Equal(t, 14, started.StartTime.Hour())
	assert.Contains(t, out, "Started activity: Work | Team sync at 14:00")
	assert.Contains(t, out, "Reminder 1 is done for Mon 16 Nov 13:00. Next: Mon 23 Nov 13:00.")

	occurrence, err := h.svc.Current(context.Background(), 1, remindTestNow)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-23", occurrence.Key())
}

func TestRemindEditWithFlags(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Call bank", "-p", "Admin", "--on", "tomorrow", "--at", "11:00")

	assert.Equal(t, "Updated reminder 1: Admin | Call bank — Tue 17 Nov 12:15\n", h.mustRun("edit", "1", "--at", "12:15"))
	assert.Equal(
		t,
		"Updated reminder 1: Admin | Call bank — Tue 17 Nov 12:15, repeats weekly on Tue\n",
		h.mustRun("edit", "1", "--every", "weekly"),
	)
	assert.Equal(t, "Updated reminder 1: Admin | Call bank — Tue 17 Nov (all day), repeats weekly on Tue until 1 Dec 2026\n",
		h.mustRun("edit", "1", "--all-day", "--until", "2026-12-01"))
	assert.Equal(t, "Updated reminder 1: Admin | Call bank — Tue 17 Nov (all day), repeats weekly on Tue\n",
		h.mustRun("edit", "1", "--until", "none"))
	assert.Equal(t, "Updated reminder 1: Admin | Call bank — Tue 17 Nov (all day)\n", h.mustRun("edit", "1", "--every", "none"))

	_, err := h.run("", "edit", "1", "--json")
	require.EqualError(t, err, "--json needs at least one field to change; the interactive form has no JSON output")
}

func TestRemindRemove(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Standup", "--at", "09:30", "--every", "weekdays")
	h.mustRun("add", "Dentist", "--on", "tomorrow", "--at", "15:00")

	assert.Equal(t, "Removed Standup on Tue 17 Nov 09:30.\n", h.mustRun("rm", "1", "--date", "2026-11-17"))

	out, err := h.run("n\n", "rm", "2")
	require.NoError(t, err)
	assert.Contains(t, out, "Remove reminder 2: Dentist — Tue 17 Nov 15:00? [y/N]: ")
	_, err = h.svc.Get(context.Background(), 2)
	require.NoError(t, err, "declined")

	out, err = h.run("y\n", "rm", "2")
	require.NoError(t, err)
	assert.Contains(t, out, "Reminder removed.")
	_, err = h.svc.Get(context.Background(), 2)
	require.Error(t, err)
}

func TestPrintOverdueRemindersHint(t *testing.T) {
	h := newRemindHarness(t)
	cmd := NewRemindCmd()
	var errOut bytes.Buffer
	cmd.SetErr(&errOut)
	cmd.SetContext(h.rt.WithContext(context.Background()))

	printOverdueReminders(cmd)
	assert.Empty(t, errOut.String())

	h.mustRun("add", "Send invoice", "--at", "13:00")
	printOverdueReminders(cmd)
	assert.Equal(t, "! 1 reminder overdue: Send invoice (13:00) — run 'tock remind' to review\n", errOut.String())
}
