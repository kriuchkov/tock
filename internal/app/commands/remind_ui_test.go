package commands

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func remindAt(day, hour, minute int) time.Time {
	return time.Date(2026, time.November, day, hour, minute, 0, 0, time.Local)
}

func TestFormatRepeat(t *testing.T) {
	loc := localization.MustNew(localization.LanguageEnglish)
	due := remindAt(16, 17, 30)
	until := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.Local)

	tests := map[string]models.Repeat{
		"":                                {},
		"daily":                           {Freq: models.RepeatDaily},
		"every 3 days":                    {Freq: models.RepeatDaily, Interval: 3},
		"weekdays":                        {Freq: models.RepeatWeekdays},
		"weekly on Mon":                   {Freq: models.RepeatWeekly},
		"every 2 weeks on Mon, Thu":       {Freq: models.RepeatWeekly, Interval: 2, Weekdays: []time.Weekday{time.Thursday, time.Monday}},
		"monthly on 16":                   {Freq: models.RepeatMonthly},
		"monthly on last day":             {Freq: models.RepeatMonthly, MonthDay: models.MonthDayLast},
		"monthly on 2nd Tue":              {Freq: models.RepeatMonthly, Week: 2, Weekday: time.Tuesday},
		"every 6 months on last Fri":      {Freq: models.RepeatMonthly, Interval: 6, Week: models.WeekLast, Weekday: time.Friday},
		"yearly on 16 Nov":                {Freq: models.RepeatYearly},
		"every 2 years on 16 Nov":         {Freq: models.RepeatYearly, Interval: 2},
		"daily until 31 Dec 2026":         {Freq: models.RepeatDaily, Until: &until},
		"weekdays, 5 times":               {Freq: models.RepeatWeekdays, Count: 5},
		"monthly on 16 until 31 Dec 2026": {Freq: models.RepeatMonthly, Until: &until, Count: 3},
	}
	for want, repeat := range tests {
		t.Run(want, func(t *testing.T) {
			reminder := models.Reminder{Due: due, Repeat: repeat}
			assert.Equal(t, want, formatRepeat(loc, reminder)+formatRepeatBounds(loc, repeat))
		})
	}
}

func TestFormatSpan(t *testing.T) {
	assert.Equal(t, "0m", formatSpan(-time.Minute))
	assert.Equal(t, "45m", formatSpan(45*time.Minute+20*time.Second))
	assert.Equal(t, "3h05m", formatSpan(3*time.Hour+5*time.Minute))
	assert.Equal(t, "2d", formatSpan(50*time.Hour))
}

func TestFormatReminderStatus(t *testing.T) {
	loc := localization.MustNew(localization.LanguageEnglish)
	tf := timeutil.NewFormatter("24")
	now := remindAt(16, 14, 25)
	tracked := remindAt(16, 13, 2)
	snoozed := remindAt(16, 14, 40)
	timed := models.Reminder{Due: remindAt(16, 13, 0)}

	occurrence := func(due time.Time, state models.OccurrenceState) models.ReminderOccurrence {
		return models.ReminderOccurrence{Reminder: timed, Due: due, State: state}
	}
	assert.Equal(t, "! overdue 1h25m", formatReminderStatusLine(loc, tf, occurrence(remindAt(16, 13, 0), models.OccurrenceState{}), now, 0))
	assert.Equal(t, "in 3h05m", formatReminderStatusLine(loc, tf, occurrence(remindAt(16, 17, 30), models.OccurrenceState{}), now, 0))
	assert.Empty(t, formatReminderStatusLine(loc, tf, occurrence(remindAt(18, 17, 30), models.OccurrenceState{}), now, 0))
	assert.Equal(t, "z until 14:40",
		formatReminderStatusLine(loc, tf, occurrence(remindAt(16, 13, 0), models.OccurrenceState{SnoozedUntil: &snoozed}), now, 0))
	assert.Equal(
		t,
		"✓ tracked 15m",
		formatReminderStatusLine(
			loc,
			tf,
			occurrence(remindAt(16, 13, 0), models.OccurrenceState{DoneAt: &tracked, ActivityStart: &tracked}),
			now,
			15*time.Minute,
		),
	)

	allDay := models.ReminderOccurrence{Reminder: models.Reminder{AllDay: true}, Due: remindAt(16, 0, 0)}
	assert.Equal(t, "today", formatReminderStatusLine(loc, tf, allDay, now, 0))
}

func TestReminderNotifierSendsEachNotificationOnce(t *testing.T) {
	loc := localization.MustNew(localization.LanguageEnglish)
	notifier := newReminderNotifier(loc, timeutil.NewFormatter("24"))
	reminder := models.Reminder{
		ID: 1, Title: "Team sync", Project: "Work", Due: remindAt(16, 17, 30), Notify: 10 * time.Minute,
		Repeat: models.Repeat{Freq: models.RepeatWeekly},
	}
	occurrences := reminder.Occurrences(remindAt(16, 0, 0), remindAt(17, 0, 0), remindAt(16, 17, 0))

	assert.Empty(t, notifier.due(occurrences, remindAt(16, 17, 19)))

	lead := notifier.due(occurrences, remindAt(16, 17, 20))
	require.Len(t, lead, 1)
	assert.Equal(t, reminderNotification{Title: "Team sync", Subtitle: "in 10m", Body: "17:30 · Work · repeats weekly on Mon"}, lead[0])
	assert.Empty(t, notifier.due(occurrences, remindAt(16, 17, 21)), "sent once")

	due := notifier.due(occurrences, remindAt(16, 17, 30))
	require.Len(t, due, 1)
	assert.Equal(t, "Due now", due[0].Subtitle)
}

func TestReminderNotifierSkipsStaleAndDoneOccurrences(t *testing.T) {
	loc := localization.MustNew(localization.LanguageEnglish)
	notifier := newReminderNotifier(loc, timeutil.NewFormatter("24"))
	doneAt := remindAt(16, 9, 0)

	stale := models.ReminderOccurrence{Reminder: models.Reminder{ID: 1, Title: "Old"}, Due: remindAt(16, 8, 30)}
	done := models.ReminderOccurrence{
		Reminder: models.Reminder{ID: 2, Title: "Done"},
		Due:      remindAt(16, 13, 55),
		State:    models.OccurrenceState{DoneAt: &doneAt},
	}
	allDay := models.ReminderOccurrence{Reminder: models.Reminder{ID: 3, Title: "Renew", AllDay: true}, Due: remindAt(16, 0, 0)}

	got := notifier.due([]models.ReminderOccurrence{stale, done, allDay}, remindAt(16, 9, 5))
	require.Len(t, got, 1, "only the all-day one, at 09:00; 08:30 is past the grace period")
	assert.Equal(t, "Renew", got[0].Title)
	assert.Equal(t, "today", got[0].Subtitle)
	assert.Empty(t, notifier.due([]models.ReminderOccurrence{done}, remindAt(16, 14, 0)))
}

func TestReminderNotifierSnoozes(t *testing.T) {
	loc := localization.MustNew(localization.LanguageEnglish)
	notifier := newReminderNotifier(loc, timeutil.NewFormatter("24"))
	due := remindAt(16, 17, 0)
	until := remindAt(16, 16, 40)
	occurrences := []models.ReminderOccurrence{{
		Reminder: models.Reminder{ID: 1, Title: "Call", Due: due, Notify: 30 * time.Minute},
		Due:      due,
		State:    models.OccurrenceState{SnoozedUntil: &until},
	}}

	require.Len(t, notifier.due(occurrences, remindAt(16, 16, 30)), 1, "the lead banner")
	snoozed := notifier.due(occurrences, remindAt(16, 16, 40))
	require.Len(t, snoozed, 1, "the snooze adds a banner before the due time")
	assert.Equal(t, "in 20m", snoozed[0].Subtitle)
	dueNow := notifier.due(occurrences, remindAt(16, 17, 0))
	require.Len(t, dueNow, 1, "and keeps the one at the due time")
	assert.Equal(t, "Due now", dueNow[0].Subtitle)

	missedUntil := remindAt(16, 18, 0)
	missed := models.ReminderOccurrence{
		Reminder: models.Reminder{ID: 2, Title: "Old"},
		Due:      remindAt(14, 9, 0),
		Missed:   true,
		State:    models.OccurrenceState{SnoozedUntil: &missedUntil},
	}
	require.Len(t, notifier.due([]models.ReminderOccurrence{missed}, remindAt(16, 18, 1)), 1, "a snoozed missed one notifies")

	notifier.due(nil, remindAt(16, 18, 30))
	assert.Empty(t, notifier.sent, "moments past the grace period are forgotten")
}

func TestFormatNotifyValueRoundTrips(t *testing.T) {
	for _, d := range []time.Duration{10 * time.Second, 90 * time.Second, 10 * time.Minute, 90 * time.Minute, 2 * time.Hour} {
		parsed, err := parseNotify(formatNotifyValue(d))
		require.NoError(t, err, d)
		assert.Equal(t, d, parsed)
	}
	assert.Equal(t, "1h30m", formatNotifyValue(90*time.Minute))
}

func TestTrackedDurationMatchesTheStartByMinute(t *testing.T) {
	started := remindAt(16, 9, 30).Add(37 * time.Second)
	end := remindAt(16, 9, 45)
	occurrence := models.ReminderOccurrence{State: models.OccurrenceState{ActivityStart: &started}}
	activity := models.Activity{StartTime: remindAt(16, 9, 30), EndTime: &end}

	assert.Equal(t, 15*time.Minute, trackedDuration(occurrence, []models.Activity{activity}))
}

func TestReminderFormDraft(t *testing.T) {
	tf := timeutil.NewFormatter("24")
	now := remindAt(16, 14, 25)
	form := newReminderFormFromRequest(tf, models.AddReminderRequest{Due: remindAt(16, 0, 0), AllDay: true})

	typeText := func(text string) {
		for _, r := range text {
			form.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	typeText("Team sync")
	form.update(tea.KeyMsg{Type: tea.KeyTab})
	typeText("Work")
	form.update(tea.KeyMsg{Type: tea.KeyTab}) // date
	form.update(tea.KeyMsg{Type: tea.KeyTab}) // time
	typeText("17:30")
	form.update(tea.KeyMsg{Type: tea.KeyTab}) // repeat
	for range 3 {
		form.update(tea.KeyMsg{Type: tea.KeyRight})
	}
	require.Equal(t, models.RepeatWeekly, form.freq())
	form.update(tea.KeyMsg{Type: tea.KeyTab}) // on: Mon..Sun
	form.update(tea.KeyMsg{Type: tea.KeySpace})
	for range 3 {
		form.update(tea.KeyMsg{Type: tea.KeyRight})
	}
	form.update(tea.KeyMsg{Type: tea.KeySpace})

	req, err := form.draft(tf, now)
	require.NoError(t, err)
	assert.Equal(t, "Team sync", req.Title)
	assert.Equal(t, "Work", req.Project)
	assert.Equal(t, remindAt(16, 17, 30), req.Due)
	assert.False(t, req.AllDay)
	assert.Equal(t, "weekly:mon,thu", req.Repeat.Spec())

	assert.Equal(t, formSave, form.update(tea.KeyMsg{Type: tea.KeyEnter}))
	assert.Equal(t, formCancel, form.update(tea.KeyMsg{Type: tea.KeyEsc}))
}

func TestReminderFormKeepsWhatItDoesNotEdit(t *testing.T) {
	tf := timeutil.NewFormatter("24")
	until := remindAt(30, 0, 0)
	reminder := models.Reminder{
		ID:     3,
		Title:  "Pay rent",
		Due:    remindAt(25, 0, 0),
		AllDay: true,
		Notify: 90 * time.Minute,
		Repeat: models.Repeat{
			Freq:     models.RepeatMonthly,
			Interval: 2,
			Week:     models.WeekLast,
			Weekday:  time.Wednesday,
			Until:    &until,
			Count:    4,
		},
	}
	form := newReminderFormFromReminder(tf, reminder)
	assert.Equal(t, monthlyOnLastWeekday, form.monthly, "the 25th is both the 4th and the last Wednesday")
	assert.Equal(t, "1h30m", form.values[reminderFieldNotify])

	req, err := form.draft(tf, remindAt(16, 14, 0))
	require.NoError(t, err)
	assert.Equal(t, "2m:last-wed", req.Repeat.Spec())
	assert.Equal(t, 4, req.Repeat.Count)
	require.NotNil(t, req.Repeat.Until)
	assert.True(t, until.Equal(*req.Repeat.Until))
	assert.True(t, req.AllDay)
}

func TestReminderFormKeepsAClampedMonthDay(t *testing.T) {
	tf := timeutil.NewFormatter("24")
	reminder := models.Reminder{
		ID: 4, Title: "Rent", Due: remindAt(30, 0, 0), AllDay: true,
		Repeat: models.Repeat{Freq: models.RepeatMonthly, MonthDay: 31},
	}
	form := newReminderFormFromReminder(tf, reminder)

	req, err := form.draft(tf, remindAt(16, 14, 0))
	require.NoError(t, err)
	assert.Equal(t, "monthly:31", req.Repeat.Spec(), "the 30th shown is the 31st clamped")

	form.values[reminderFieldDate] = "2026-12-15"
	req, err = form.draft(tf, remindAt(16, 14, 0))
	require.NoError(t, err)
	assert.Equal(t, "monthly", req.Repeat.Spec(), "a new date sets a new day")
}

func TestRemindModelFitsTheWindowAndKeepsTheSelection(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Water plants", "--at", "09:00", "--note", "the big ones")
	h.mustRun("add", "Send invoice", "-p", "Admin", "--at", "13:00")
	h.mustRun("add", "Team sync", "-p", "Work", "--at", "16:00")

	model := newRemindModel(context.Background(), h.svc, h.activities,
		newReminderFormEnv(h.rt.Localizer, h.rt.TimeFormatter, DarkTheme()), 10*time.Minute)
	model.Update(tea.WindowSizeMsg{Width: 150, Height: 30})
	model.reload()
	require.Len(t, model.rows, 3)

	model.cursor = 0
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	selected, ok := model.selected()
	require.True(t, ok)
	assert.Equal(t, "Water plants", selected.Reminder.Title, "the cursor follows the row it acted on")
	assert.NotEqual(t, 0, model.cursor, "done moved it below the overdue one")

	view := model.View()
	assert.Equal(t, 30, lipgloss.Height(view), "the footer is on the last line")
	assert.Contains(t, view, "◆ 1 overdue ─ 1 of 3 done")
	assert.Contains(t, view, "OVERDUE · 1")
	assert.Contains(t, view, "TODAY · MON 16 NOV · 2")
	assert.Regexp(t, `◆ 13:00\s+Send invoice\s+Admin\s+overdue 1h25m`, view)
	assert.Contains(t, view, "focus  Water plants #1 · Mon 16 Nov 09:00", "the ID that `tock remind` commands take")
	assert.Contains(t, view, "✓ Done: Water plants")
}

func TestRemindModelActions(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Send invoice", "-p", "Admin", "--at", "13:00")
	h.mustRun("add", "Team sync", "-p", "Work", "--at", "17:30")

	model := newRemindModel(context.Background(), h.svc, h.activities,
		newReminderFormEnv(h.rt.Localizer, h.rt.TimeFormatter, DarkTheme()), 10*time.Minute)
	model.reload()
	require.Len(t, model.rows, 2)
	assert.Equal(t, 1, model.overdue)

	model.cursor = 0
	model.Update(tea.KeyMsg{Type: tea.KeySpace})
	assert.Equal(t, "Done: Send invoice", model.status)
	assert.Equal(t, 0, model.overdue)

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	require.NotNil(t, model.pendingDelete)
	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	assert.Equal(t, "Removed: Send invoice", model.status)
	require.Len(t, model.rows, 1)

	model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	require.NotNil(t, model.form)
	for _, r := range "Dentist" {
		model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, model.form)
	assert.Equal(t, "Saved: Dentist", model.status)
	require.Len(t, model.rows, 2)

	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, remindViewWeek, model.view)
	assert.Contains(t, model.View(), "16 – 22 Nov")
}

func TestCalendarShowsAndActsOnReminders(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Send invoice", "-p", "Admin", "--at", "13:00", "--every", "monthly")
	h.mustRun("add", "Standup", "-p", "Work", "--at", "09:30", "--every", "weekdays")
	h.mustRun("add", "Dentist", "--on", "2026-11-20", "--at", "15:00")

	h.activities.getReportFn = func(context.Context, models.ActivityFilter) (*models.Report, error) {
		return &models.Report{ByProject: map[string]models.ProjectReport{}}, nil
	}
	model := initialCalendarModel(h.activities, &config.Config{}, h.rt.TimeFormatter, h.rt.Localizer, nil)
	model.currentDate, model.viewDate = remindTestNow, remindTestNow
	model.withReminders(h.svc, 10*time.Minute)
	model.Update(tea.WindowSizeMsg{Width: 160, Height: 46})
	model.Update(model.fetchMonthData())

	_, marked := model.reminderMark(remindAt(20, 0, 0), remindTestNow)
	assert.True(t, marked, "a one-off marks its day")
	_, marked = model.reminderMark(remindAt(17, 0, 0), remindTestNow)
	assert.False(t, marked, "weekday repeats do not mark every day")

	view := model.View()
	assert.Contains(t, view, "REMINDERS · 0/2")
	assert.Contains(t, view, "UPCOMING · 5", "four Standups and the Dentist; the selected day is left out")
	assert.Contains(t, view, glyphOverdue+" 2 overdue", "the ruler counts the overdue ones")

	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	assert.Nil(t, cmd, "space does nothing until a reminder is selected")

	model.Update(tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, daySelection{section: sectionReminders}, model.sel)
	model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Contains(t, model.View(), "space done   s start   z snooze 10m")

	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	require.NotNil(t, cmd)
	model.Update(cmd())

	assert.Equal(t, "Done: Standup", model.rem.status, "rows run by due time, so Standup comes first")
	assert.Contains(t, model.View(), "REMINDERS · 1/2")
	assert.Contains(t, model.View(), "✓ Done: Standup")
}

func TestCalendarUpcoming(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Renew domain", "--every", "yearly")
	h.mustRun("add", "Water plants", "--every", "3d")
	h.mustRun("add", "Team sync", "--at", "16:40", "--every", "weekly")
	h.mustRun("add", "Late call", "--at", "23:30")
	h.mustRun("add", "Dentist", "--on", "tomorrow", "--at", "15:00")

	h.activities.getReportFn = func(context.Context, models.ActivityFilter) (*models.Report, error) {
		return &models.Report{ByProject: map[string]models.ProjectReport{}}, nil
	}
	model := initialCalendarModel(h.activities, &config.Config{}, h.rt.TimeFormatter, h.rt.Localizer, nil)
	// Friday is selected, so today's reminders are in Upcoming rather than in the day.
	model.currentDate, model.viewDate = remindAt(13, 0, 0), remindAt(13, 0, 0)
	model.withReminders(h.svc, 10*time.Minute)
	model.Update(tea.WindowSizeMsg{Width: 160, Height: 46})
	model.Update(model.fetchMonthData())

	lines := model.upcomingLines(42, 20)
	assert.Equal(t, []string{
		"UPCOMING · 7",
		"today   all day   Renew domain ↻",
		"        all day   Water plants ↻",
		"        16:40     Team sync ↻     in 2h15m",
		"        23:30     Late call       in 9h05m",
		"tmrw    Dentist",
		"thu 19  Water plants",
		"sun 22  Water plants",
	}, lines, "today a line each with the time left, later days a line each")
	assert.Equal(t, lines[:3], model.upcomingLines(42, 3), "cut to the room there is")

	narrow := model.upcomingLines(calendarMonthColW-1, 20)
	assert.Equal(t, "today  all day  Renew doma… ↻", narrow[1], "narrow, the columns lose a space each")
	assert.Equal(t, "       16:40    Team sync ↻", narrow[3], "no countdown when the title would get too little room")
	assert.Equal(t, "thu 19 Water plants", narrow[6])

	model.config.Calendar.UpcomingDays = 2
	model.Update(model.fetchMonthData())
	lines = model.upcomingLines(42, 20)
	assert.Equal(t, "UPCOMING · 5", lines[0], "calendar.upcoming_days limits the days")
	assert.Equal(t, "tmrw    Dentist", lines[len(lines)-1])
}

func TestRemindFocusShowsTheRepeatEnd(t *testing.T) {
	h := newRemindHarness(t)
	h.mustRun("add", "Standup", "--at", "18:00", "--every", "daily", "--until", "2027-06-30")

	model := newRemindModel(context.Background(), h.svc, h.activities,
		newReminderFormEnv(h.rt.Localizer, h.rt.TimeFormatter, DarkTheme()), 10*time.Minute)
	model.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	model.reload()

	view := model.View()
	assert.Contains(t, view, "Standup #1 · Mon 16 Nov 18:00 · ↻ daily until 30 Jun 2027")
	assert.Contains(t, view, "then Tue 17 Nov 18:00")
	assert.True(t, model.next.set, "the next occurrence is remembered for the next redraw")
}

func TestRemindRefreshRunsInTheBackground(t *testing.T) {
	h := newRemindHarness(t)
	model := newRemindModel(context.Background(), h.svc, h.activities,
		newReminderFormEnv(h.rt.Localizer, h.rt.TimeFormatter, DarkTheme()), 10*time.Minute)
	model.reload()
	h.mustRun("add", "Dentist", "--at", "15:00")

	cmd := model.refresh()
	require.NotNil(t, cmd)
	assert.Empty(t, model.rows, "nothing is read until the command runs")
	msg := cmd()

	model.Update(tea.KeyMsg{Type: tea.KeyTab}) // to the week view
	model.Update(msg)
	assert.Equal(t, remindViewWeek, model.view)
	require.Len(t, model.rows, 1, "the week view read the new reminder itself")

	model.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	model.Update(model.refresh()())
	require.Len(t, model.rows, 1)
	assert.Equal(t, "Dentist", model.rows[0].Reminder.Title)
}
