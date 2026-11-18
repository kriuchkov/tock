//go:build darwin && cgo

package commands

import (
	"context"
	"os/exec"
	"time"

	"fyne.io/systray"
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/localization"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	trayReminderTodaySlots    = 8
	trayReminderTomorrowSlots = 5
	// trayReminderDays is how many days ahead the tray loads, enough for notify lead times up to a week.
	trayReminderDays = 8
)

// trayReminderAction is what a reminder's submenu item does.
type trayReminderAction int

const (
	trayActionTrack trayReminderAction = iota
	trayActionDone
	trayActionSnooze
	trayActionSnoozeHour
	trayActionSnoozeTomorrow
	trayActionCount
)

type trayReminderClick struct {
	slot   int
	action trayReminderAction
}

// trayReminderSlot is a fixed menu row reused across refreshes, with its action submenu.
type trayReminderSlot struct {
	item       *systray.MenuItem
	occurrence *models.ReminderOccurrence
}

// trayReminders is the reminder part of the menu bar item: the "Next" line, the reminders
// submenu, the overdue badge and the notifications.
type trayReminders struct {
	svc      ports.ReminderResolver
	loc      *localization.Localizer
	tf       *timeutil.Formatter
	snooze   time.Duration
	notifier *reminderNotifier // nil when notifications are off

	occurrences []models.ReminderOccurrence // yesterday to trayReminderDays ahead
	overdue     []models.ReminderOccurrence

	mNext     *systray.MenuItem
	mMenu     *systray.MenuItem
	mTomorrow *systray.MenuItem
	slots     []trayReminderSlot // today's first, then tomorrow's
	click     chan trayReminderClick

	nextTitle string
	menuTitle string
	badge     bool
}

// newTrayReminders returns nil when reminders are unavailable, which leaves them out of the menu.
func newTrayReminders(cmd *cobra.Command) *trayReminders {
	rt := getRuntime(cmd)
	if rt.ReminderService == nil {
		return nil
	}
	r := &trayReminders{svc: rt.ReminderService, loc: rt.Localizer, tf: rt.TimeFormatter, snooze: defaultSnooze(cmd)}
	if rt.Config.Reminders.Notifications {
		r.notifier = newReminderNotifier(rt.Localizer, rt.TimeFormatter)
	}
	return r
}

// addMenu adds the "Next" line and the reminders submenu at the current menu position.
func (r *trayReminders) addMenu() {
	r.mNext = systray.AddMenuItem("", "")
	r.mNext.Disable()
	r.mNext.Hide()

	r.mMenu = systray.AddMenuItem(r.loc.Text("tray.reminders.title"), "")
	today := r.mMenu.AddSubMenuItem(r.loc.Text("calendar.upcoming.today"), "")
	today.Disable()

	r.click = make(chan trayReminderClick)
	r.slots = make([]trayReminderSlot, 0, trayReminderTodaySlots+trayReminderTomorrowSlots)
	for range trayReminderTodaySlots {
		r.addSlot()
	}
	r.mTomorrow = r.mMenu.AddSubMenuItem(r.loc.Text("calendar.upcoming.tomorrow"), "")
	r.mTomorrow.Disable()
	for range trayReminderTomorrowSlots {
		r.addSlot()
	}
}

func (r *trayReminders) addSlot() {
	index := len(r.slots)
	item := r.mMenu.AddSubMenuItem("", "")
	labels := [trayActionCount]string{
		r.loc.Text("tray.reminders.track"),
		r.loc.Text("tray.reminders.done"),
		r.loc.Format("tray.reminders.snooze", formatNotifyValue(r.snooze)),
		r.loc.Text("tray.reminders.snooze_hour"),
		r.loc.Text("tray.reminders.snooze_tomorrow"),
	}
	for action, label := range labels {
		go r.forwardClicks(item.AddSubMenuItem(label, ""), index, trayReminderAction(action))
	}
	item.Hide()
	r.slots = append(r.slots, trayReminderSlot{item: item})
}

func (r *trayReminders) forwardClicks(item *systray.MenuItem, slot int, action trayReminderAction) {
	for range item.ClickedCh {
		r.click <- trayReminderClick{slot: slot, action: action}
	}
}

// load reads the reminders from the store and rebuilds the submenu.
func (r *trayReminders) load(ctx context.Context, now time.Time) {
	today, _ := timeutil.LocalDayBounds(now)
	occurrences, err := r.svc.Occurrences(ctx, today.AddDate(0, 0, -1), today.AddDate(0, 0, trayReminderDays), now)
	if err != nil {
		return
	}
	overdue, err := r.svc.Overdue(ctx, now)
	if err != nil {
		return
	}
	r.occurrences, r.overdue = occurrences, overdue
	r.renderSlots(now)
}

func (r *trayReminders) renderSlots(now time.Time) {
	var todayRows, tomorrowRows []models.ReminderOccurrence
	tomorrow := now.AddDate(0, 0, 1)
	for _, occurrence := range r.occurrences {
		switch {
		case sameDay(occurrence.Due, now):
			todayRows = append(todayRows, occurrence)
		case sameDay(occurrence.Due, tomorrow):
			tomorrowRows = append(tomorrowRows, occurrence)
		}
	}
	todayRows = models.MergeOccurrences(r.overdue, todayRows)

	r.fillSlots(r.slots[:trayReminderTodaySlots], todayRows, now)
	r.fillSlots(r.slots[trayReminderTodaySlots:], tomorrowRows, now)
	if len(tomorrowRows) == 0 {
		r.mTomorrow.Hide()
	} else {
		r.mTomorrow.Show()
	}
}

func (r *trayReminders) fillSlots(slots []trayReminderSlot, rows []models.ReminderOccurrence, now time.Time) {
	for i := range slots {
		slot := &slots[i]
		if i >= len(rows) {
			slot.occurrence = nil
			slot.item.Hide()
			continue
		}
		occurrence := rows[i]
		slot.occurrence = &occurrence
		slot.item.SetTitle(r.rowTitle(occurrence, now))
		if occurrence.Status(now) == models.ReminderDone {
			slot.item.Disable()
		} else {
			slot.item.Enable()
		}
		slot.item.Show()
	}
}

// rowTitle is "! 13:00  Send invoice — overdue 1h25m" or "17:30  Team sync ↻".
func (r *trayReminders) rowTitle(occurrence models.ReminderOccurrence, now time.Time) string {
	reminder := occurrence.Reminder
	clock := formatReminderClock(r.loc, r.tf, occurrence.Due, reminder.AllDay)
	if !sameDay(occurrence.Due, now) {
		clock = formatReminderDue(r.loc, r.tf, occurrence.Due, reminder.AllDay, now)
	}

	title := clock + "  " + reminder.Title
	if reminder.IsRepeating() {
		title += " ↻"
	}
	status := occurrence.Status(now)
	if status == models.ReminderPending {
		return title
	}
	return reminderStatusGlyph(status) + " " + title + " — " + formatReminderStatus(r.loc, r.tf, occurrence, now, 0)
}

// tick updates what changes with the clock and sends the notifications that came due.
func (r *trayReminders) tick(ctx context.Context, now time.Time) {
	r.updateNext(now)

	overdue := 0
	for _, occurrence := range models.MergeOccurrences(r.overdue, r.occurrences) {
		if occurrence.Status(now) == models.ReminderOverdue {
			overdue++
		}
	}
	menuTitle := r.loc.Text("tray.reminders.title")
	if overdue > 0 {
		menuTitle = r.loc.Format("tray.reminders.title_overdue", overdue)
	}
	if menuTitle != r.menuTitle {
		r.menuTitle = menuTitle
		r.mMenu.SetTitle(menuTitle)
	}
	if badge := overdue > 0; badge != r.badge {
		r.badge = badge
		if icon := trayIcon(badge); len(icon) > 0 {
			systray.SetTemplateIcon(icon, icon)
		}
	}

	if r.notifier != nil {
		// Overdue covers occurrences older than the loaded window, e.g. one snoozed for two days.
		for _, notification := range r.notifier.due(models.MergeOccurrences(r.overdue, r.occurrences), now) {
			postNotification(ctx, notification)
		}
	}
}

// updateNext shows the next reminder to come: "Next: Team sync at 17:30 in 3h05m".
func (r *trayReminders) updateNext(now time.Time) {
	title := ""
	for _, occurrence := range r.occurrences {
		if occurrence.Status(now) != models.ReminderPending || !occurrence.DueBy().After(now) {
			continue
		}
		reminder := occurrence.Reminder
		if sameDay(occurrence.Due, now) && !reminder.AllDay {
			title = r.loc.Format("tray.menu.next_today", reminder.Title,
				occurrence.Due.Format(r.tf.GetDisplayFormat()), formatSpan(occurrence.Due.Sub(now)))
		} else {
			title = r.loc.Format("tray.menu.next", reminder.Title, formatReminderWhen(r.loc, r.tf, occurrence.Due, reminder.AllDay, now))
		}
		break
	}

	if title == r.nextTitle {
		return
	}
	r.nextTitle = title
	if title == "" {
		r.mNext.Hide()
		return
	}
	r.mNext.SetTitle(title)
	r.mNext.Show()
}

// handle runs a submenu action; a failure is reported as a notification.
func (r *trayReminders) handle(ctx context.Context, click trayReminderClick) {
	if click.slot < 0 || click.slot >= len(r.slots) || r.slots[click.slot].occurrence == nil {
		return
	}
	occurrence := *r.slots[click.slot].occurrence
	now := time.Now()

	var err error
	switch click.action {
	case trayActionTrack:
		_, err = r.svc.Track(ctx, occurrence.Ref(), models.TrackReminderRequest{StartTime: now})
	case trayActionDone:
		_, err = r.svc.SetDone(ctx, occurrence.Ref(), true, now)
	case trayActionSnooze:
		_, err = r.svc.Snooze(ctx, occurrence.Ref(), now.Add(r.snooze))
	case trayActionSnoozeHour:
		_, err = r.svc.Snooze(ctx, occurrence.Ref(), now.Add(time.Hour))
	case trayActionSnoozeTomorrow:
		tomorrow := now.AddDate(0, 0, 1)
		morning := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), models.AllDayNotifyHour, 0, 0, 0, tomorrow.Location())
		_, err = r.svc.Snooze(ctx, occurrence.Ref(), morning)
	case trayActionCount:
	}
	if err != nil {
		postNotification(ctx, reminderNotification{Title: occurrence.Reminder.Title, Body: r.errorText(occurrence, err)})
	}
}

func (r *trayReminders) errorText(occurrence models.ReminderOccurrence, err error) string {
	if errors.Is(err, coreErrors.ErrProjectRequired) {
		return r.loc.Format("tray.reminders.no_project", occurrence.Reminder.ID)
	}
	return reminderErrorText(r.loc, err)
}

// postNotification shows a macOS banner. The texts go in as script arguments, so they need no quoting.
func postNotification(ctx context.Context, n reminderNotification) {
	//nolint:gosec // a fixed program and script; the reminder texts are passed as arguments, never run
	cmd := exec.CommandContext(ctx, "osascript",
		"-e", "on run argv",
		"-e", "display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv)",
		"-e", "end run",
		n.Title, n.Subtitle, n.Body)
	go func() { _ = cmd.Run() }()
}
