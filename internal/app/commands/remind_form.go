package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/localization"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	reminderFieldTitle = iota
	reminderFieldProject
	reminderFieldDate
	reminderFieldTime
	reminderFieldRepeat
	reminderFieldOn
	reminderFieldUntil
	reminderFieldNotify
	reminderFieldTags
	reminderFieldNotes
	reminderFieldCount
)

const (
	reminderFormLabelWidth  = 12
	reminderFormPreviewDays = 366 * 2
	reminderFormNextCount   = 5
	reminderFormLeftWidth   = 64
	reminderFormPreviewW    = 28
	reminderFormGap         = 3
	// reminderFormFrameW is the border and padding around the form.
	reminderFormFrameW = 4
	// The form keeps reminderFormMargin columns free on each side from reminderFormMarginMin.
	reminderFormMargin    = 4
	reminderFormMarginMin = 80
	maxWeekOfMonthIndex   = 4
)

// monthlyMode is how a monthly reminder picks its day.
type monthlyMode int

const (
	monthlyOnDay monthlyMode = iota
	monthlyOnWeekday
	monthlyOnLastWeekday
	monthlyOnLastDay
	monthlyModeCount
)

// reminderRepeatChoices are the repeat rules the form cycles through.
var reminderRepeatChoices = []models.RepeatFreq{
	models.RepeatNone, models.RepeatDaily, models.RepeatWeekdays,
	models.RepeatWeekly, models.RepeatMonthly, models.RepeatYearly,
}

type formOutcome int

const (
	formContinue formOutcome = iota
	formSave
	formCancel
)

// reminderFormEnv carries what the form needs to parse and render.
type reminderFormEnv struct {
	loc   *localization.Localizer
	tf    *timeutil.Formatter
	theme Theme
}

func newReminderFormEnv(loc *localization.Localizer, tf *timeutil.Formatter, theme Theme) reminderFormEnv {
	return reminderFormEnv{loc: loc, tf: tf, theme: theme}
}

// reminderForm edits one reminder. It is a component: the host feeds it keys and saves it.
type reminderForm struct {
	id int
	// original keeps what the form does not edit: interval, count and occurrence states.
	original  models.Reminder
	values    [reminderFieldCount]string
	repeat    int
	weekdays  [daysInWeek]bool // Monday first
	dayCursor int
	monthly   monthlyMode
	cursor    int
	err       string
}

const daysInWeek = 7

func newReminderFormFromRequest(tf *timeutil.Formatter, req models.AddReminderRequest) *reminderForm {
	return newReminderForm(tf, models.Reminder{
		Title:   req.Title,
		Project: req.Project,
		Due:     req.Due,
		AllDay:  req.AllDay,
		Repeat:  req.Repeat,
		Notify:  req.Notify,
		Tags:    req.Tags,
		Notes:   req.Notes,
	})
}

func newReminderFormFromReminder(tf *timeutil.Formatter, reminder models.Reminder) *reminderForm {
	return newReminderForm(tf, reminder)
}

func newReminderForm(tf *timeutil.Formatter, reminder models.Reminder) *reminderForm {
	f := &reminderForm{id: reminder.ID, original: reminder}
	due := reminder.Due.In(time.Local)

	f.values[reminderFieldTitle] = reminder.Title
	f.values[reminderFieldProject] = reminder.Project
	f.values[reminderFieldDate] = due.Format(time.DateOnly)
	if !reminder.AllDay {
		f.values[reminderFieldTime] = due.Format(tf.GetDisplayFormat())
	}
	if reminder.Repeat.Until != nil {
		f.values[reminderFieldUntil] = reminder.Repeat.Until.Format(time.DateOnly)
	}
	f.values[reminderFieldNotify] = formatNotifyValue(reminder.Notify)
	f.values[reminderFieldTags] = strings.Join(reminder.Tags, ", ")
	f.values[reminderFieldNotes] = editNotesEncoder.Replace(reminder.Notes)

	for i, freq := range reminderRepeatChoices {
		if freq == reminder.Repeat.Freq {
			f.repeat = i
		}
	}
	if reminder.Repeat.Freq == models.RepeatWeekly {
		for _, wd := range reminder.WeeklyDays() {
			f.weekdays[mondayFirstIndex(wd)] = true
		}
	}
	switch {
	case reminder.Repeat.Week == models.WeekLast:
		f.monthly = monthlyOnLastWeekday
	case reminder.Repeat.Week != 0:
		f.monthly = monthlyOnWeekday
	case reminder.Repeat.MonthDay == models.MonthDayLast:
		f.monthly = monthlyOnLastDay
	}
	return f
}

func mondayFirstIndex(wd time.Weekday) int {
	return (int(wd) + daysInWeek - 1) % daysInWeek
}

// formatNotifyValue shows a lead time the way it is typed: "10m", "1h", "1h30m".
func formatNotifyValue(d time.Duration) string {
	switch {
	case d <= 0:
		return ""
	case d%time.Hour == 0:
		return strconv.Itoa(int(d/time.Hour)) + "h"
	case d%time.Minute == 0 && d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	case d%time.Minute == 0:
		return strings.TrimSuffix(d.String(), "0s")
	}
	return d.String()
}

func (f *reminderForm) freq() models.RepeatFreq {
	return reminderRepeatChoices[f.repeat]
}

// visible reports whether a field applies to the chosen repeat rule.
func (f *reminderForm) visible(field int) bool {
	switch field {
	case reminderFieldOn:
		return f.freq() == models.RepeatWeekly || f.freq() == models.RepeatMonthly
	case reminderFieldUntil:
		return f.freq() != models.RepeatNone
	}
	return true
}

func isReminderTextField(field int) bool {
	return field != reminderFieldRepeat && field != reminderFieldOn
}

// update applies a key and reports whether the host should save or close the form.
func (f *reminderForm) update(msg tea.KeyMsg) formOutcome {
	//nolint:exhaustive // bubbletea key types are an open set; unlisted keys are ignored on purpose
	switch msg.Type {
	case tea.KeyEsc:
		return formCancel
	case tea.KeyEnter:
		return formSave
	case tea.KeyTab, tea.KeyDown:
		f.move(1)
	case tea.KeyShiftTab, tea.KeyUp:
		f.move(-1)
	case tea.KeyLeft:
		f.cycle(-1)
	case tea.KeyRight:
		f.cycle(1)
	case tea.KeySpace:
		if f.cursor == reminderFieldOn && f.freq() == models.RepeatWeekly {
			f.weekdays[f.dayCursor] = !f.weekdays[f.dayCursor]
		} else if isReminderTextField(f.cursor) {
			f.values[f.cursor] += " "
		}
	case tea.KeyBackspace, tea.KeyDelete:
		if runes := []rune(f.values[f.cursor]); isReminderTextField(f.cursor) && len(runes) > 0 {
			f.values[f.cursor] = string(runes[:len(runes)-1])
		}
	case tea.KeyCtrlU:
		if isReminderTextField(f.cursor) {
			f.values[f.cursor] = ""
		}
	case tea.KeyRunes:
		if isReminderTextField(f.cursor) {
			f.values[f.cursor] += string(msg.Runes)
		}
	}
	return formContinue
}

func (f *reminderForm) move(dir int) {
	for range reminderFieldCount {
		f.cursor = (f.cursor + dir + reminderFieldCount) % reminderFieldCount
		if f.visible(f.cursor) {
			return
		}
	}
}

func (f *reminderForm) cycle(dir int) {
	switch f.cursor {
	case reminderFieldRepeat:
		f.repeat = (f.repeat + dir + len(reminderRepeatChoices)) % len(reminderRepeatChoices)
	case reminderFieldOn:
		if f.freq() == models.RepeatWeekly {
			f.dayCursor = (f.dayCursor + dir + daysInWeek) % daysInWeek
		} else {
			f.monthly = (f.monthly + monthlyMode(dir) + monthlyModeCount) % monthlyModeCount
		}
	}
}

// draft parses the form into a reminder request.
func (f *reminderForm) draft(tf *timeutil.Formatter, now time.Time) (models.AddReminderRequest, error) {
	day, err := timeutil.ParseDay(f.values[reminderFieldDate], now)
	if err != nil {
		return models.AddReminderRequest{}, err
	}

	due, allDay := day, true
	if clockValue := strings.TrimSpace(f.values[reminderFieldTime]); clockValue != "" {
		clock, parseErr := tf.ParseTime(clockValue)
		if parseErr != nil {
			return models.AddReminderRequest{}, parseErr
		}
		due = time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, time.Local)
		allDay = false
	}

	repeat, err := f.repeatRule(day, now)
	if err != nil {
		return models.AddReminderRequest{}, err
	}
	notify, err := parseNotify(f.values[reminderFieldNotify])
	if err != nil {
		return models.AddReminderRequest{}, err
	}

	return models.AddReminderRequest{
		Title:   strings.TrimSpace(f.values[reminderFieldTitle]),
		Project: strings.TrimSpace(f.values[reminderFieldProject]),
		Due:     due,
		AllDay:  allDay,
		Repeat:  repeat,
		Notify:  notify,
		Tags:    parseTagValues([]string{f.values[reminderFieldTags]}),
		Notes:   editNotesDecoder.Replace(f.values[reminderFieldNotes]),
	}, nil
}

func (f *reminderForm) repeatRule(day, now time.Time) (models.Repeat, error) {
	repeat := models.Repeat{Freq: f.freq()}
	if repeat.Freq == models.RepeatNone {
		return repeat, nil
	}
	if f.original.Repeat.Freq == repeat.Freq {
		repeat.Interval, repeat.Count = f.original.Repeat.Interval, f.original.Repeat.Count
	}

	switch repeat.Freq {
	case models.RepeatWeekly:
		for i, on := range f.weekdays {
			if on {
				repeat.Weekdays = append(repeat.Weekdays, time.Weekday((i+1)%daysInWeek))
			}
		}
	case models.RepeatMonthly:
		switch f.monthly {
		case monthlyOnWeekday:
			repeat.Week, repeat.Weekday = weekOfMonth(day), day.Weekday()
		case monthlyOnLastWeekday:
			repeat.Week, repeat.Weekday = models.WeekLast, day.Weekday()
		case monthlyOnLastDay:
			repeat.MonthDay = models.MonthDayLast
		case monthlyOnDay, monthlyModeCount:
			repeat.MonthDay = f.clampedMonthDay(day)
		}
	case models.RepeatNone, models.RepeatDaily, models.RepeatWeekdays, models.RepeatYearly:
	}

	if until := strings.TrimSpace(f.values[reminderFieldUntil]); until != "" {
		untilDay, err := timeutil.ParseDay(until, now)
		if err != nil {
			return models.Repeat{}, err
		}
		repeat.Until = &untilDay
	}
	return repeat, nil
}

// clampedMonthDay keeps a monthly day the shown date was clamped from, e.g. 31 shown as the 30th,
// as long as the date is not changed; otherwise the rule follows the date.
func (f *reminderForm) clampedMonthDay(day time.Time) int {
	original := f.original.Repeat
	if original.Freq != models.RepeatMonthly || original.MonthDay <= 0 || !sameDay(day, f.original.Due) {
		return 0
	}
	return original.MonthDay
}

// weekOfMonth is which weekday of its month day is: 1..4, or last for the fifth.
func weekOfMonth(day time.Time) int {
	week := (day.Day()-1)/daysInWeek + 1
	if week > maxWeekOfMonthIndex {
		return models.WeekLast
	}
	return week
}

// preview is the reminder as currently filled in, when it parses.
func (f *reminderForm) preview(tf *timeutil.Formatter, now time.Time) (models.Reminder, bool) {
	req, err := f.draft(tf, now)
	if err != nil || req.Repeat.Validate(req.Due) != nil {
		return models.Reminder{}, false
	}
	return models.Reminder{
		ID:     f.id,
		Title:  req.Title,
		Due:    req.Due,
		AllDay: req.AllDay,
		Repeat: req.Repeat,
		Notify: req.Notify,
		States: f.original.States,
	}, true
}

// saveReminderForm adds the reminder, or replaces every field of the one being edited.
func saveReminderForm(
	ctx context.Context,
	svc ports.ReminderResolver,
	f *reminderForm,
	tf *timeutil.Formatter,
	now time.Time,
) (*models.Reminder, error) {
	req, err := f.draft(tf, now)
	if err != nil {
		return nil, err
	}
	if f.id == 0 {
		return svc.Add(ctx, req)
	}
	return svc.Update(ctx, f.id, models.UpdateReminderRequest{
		Title:   &req.Title,
		Project: &req.Project,
		Due:     &req.Due,
		AllDay:  &req.AllDay,
		Repeat:  &req.Repeat,
		Notify:  &req.Notify,
		Tags:    &req.Tags,
		Notes:   &req.Notes,
	})
}

// reminderErrorText renders an error the form can provoke as localized text.
func reminderErrorText(loc *localization.Localizer, err error) string {
	switch {
	case errors.Is(err, coreErrors.ErrReminderTitleRequired):
		return loc.Text("remind.error.title_required")
	case errors.Is(err, coreErrors.ErrProjectRequired):
		return loc.Text("remind.error.project_required")
	case errors.Is(err, coreErrors.ErrReminderNeverDue):
		return loc.Text("remind.error.never_due")
	}
	return err.Error()
}

// view draws the form as a window framed in the accent color, width columns wide with its
// margins: the fields with what tock makes of them, the occurrences they give, and the month
// with those marked beside the fields when it fits.
func (f *reminderForm) view(env reminderFormEnv, now time.Time, width int) string {
	ui := newChromeStyles(env.theme)
	margin := 0
	if width >= reminderFormMarginMin {
		margin = reminderFormMargin
	}
	boxW := width - 2*margin
	if width <= 0 {
		boxW = reminderFormLeftWidth + reminderFormFrameW
	}
	inner := max(boxW-reminderFormFrameW, 1)

	leftW := min(inner, reminderFormLeftWidth)
	content := f.renderFields(env, ui, now, leftW)
	if inner >= reminderFormLeftWidth+reminderFormGap+reminderFormPreviewW {
		content = lipgloss.JoinHorizontal(lipgloss.Top,
			fitBlock(content, leftW, lipgloss.Height(content)), strings.Repeat(" ", reminderFormGap), f.renderPreview(env, ui, now))
	}

	key := func(k, action string) footerKey { return footerKey{key: k, action: env.loc.Text(action)} }
	keys := []footerKey{
		key(keyTab, "tui.key.next_field"), key("←→", "tui.key.option"), key(keySpace, "tui.key.toggle_day"),
		key("enter", "tui.key.save"), key("ctrl+u", "tui.key.clear"), key("esc", "tui.key.cancel"),
	}
	footer := strings.TrimPrefix(ui.renderFooter(keys, ui.renderStatus(f.err, true), inner), " ")
	hint := ui.muted.Render(cutText(env.loc.Text("remind.form.hint"), inner))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(env.theme.Highlight).
		Padding(0, 1).
		Width(max(boxW-2, 1)).
		Render(content + "\n\n" + hint + "\n" + footer)
	if margin == 0 {
		return box
	}
	return lipgloss.NewStyle().MarginLeft(margin).Render(box)
}

func (f *reminderForm) renderFields(env reminderFormEnv, ui chromeStyles, now time.Time, width int) string {
	loc := env.loc
	header := loc.Text("remind.form.new")
	if f.id != 0 {
		header = loc.Format("remind.form.edit", f.id)
	}
	lines := []string{ui.accentBold.Render(header), ""}

	labels := [reminderFieldCount]string{
		"remind.form.title", "remind.form.project", "remind.form.date", "remind.form.time", "remind.form.repeat",
		"remind.form.on", "remind.form.until", "remind.form.notify", "remind.form.tags", "remind.form.notes",
	}
	for field := range reminderFieldCount {
		if f.visible(field) {
			lines = append(lines, ui.renderFormField(f.field(env, ui, field, labels[field], now), width))
		}
	}

	lines = append(lines, "")
	next := ui.muted.Render(padText(loc.Text("remind.form.next"), reminderFormLabelWidth, false))
	if preview, ok := f.preview(env.tf, now); ok {
		lines = append(lines, "   "+next+ui.text.Render(formatReminderSummary(loc, env.tf, preview, now)))
	} else {
		lines = append(lines, "   "+next+ui.muted.Render(loc.Text("remind.form.invalid")))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

// field is a line of the form: the value as typed, followed by what tock reads in a date when
// that is not plain, "→ Mon 16 Nov"; the option fields are drawn as their choices.
func (f *reminderForm) field(env reminderFormEnv, ui chromeStyles, field int, label string, now time.Time) formField {
	active := field == f.cursor
	line := formField{label: env.loc.Text(label), labelW: reminderFormLabelWidth, active: active, valueStyle: ui.text}
	if active {
		line.valueStyle = ui.bold
	}
	switch field {
	case reminderFieldRepeat:
		line.value = "‹ " + env.loc.Text("remind.form.repeat."+repeatChoiceKey(f.freq())) + " ›"
		line.styled = line.valueStyle.Render(line.value)
		return line
	case reminderFieldOn:
		line.styled = f.renderOn(env, ui, active)
		return line
	}

	line.value = f.values[field]
	if line.value == "" && !active {
		switch field {
		case reminderFieldTime:
			line.value, line.valueStyle = env.loc.Text("remind.all_day"), ui.muted
		case reminderFieldUntil:
			line.value, line.valueStyle = env.loc.Text("remind.form.never"), ui.muted
		}
		return line
	}
	if field == reminderFieldDate || field == reminderFieldUntil {
		if day, err := timeutil.ParseDay(line.value, now); err == nil && line.value != day.Format(time.DateOnly) {
			line.note = "→ " + formatReminderDay(env.loc, day, now)
		}
	}
	return line
}

func repeatChoiceKey(freq models.RepeatFreq) string {
	if freq == models.RepeatNone {
		return repeatSpecNone
	}
	return string(freq)
}

func (f *reminderForm) renderOn(env reminderFormEnv, ui chromeStyles, active bool) string {
	selected := ui.reminder.Bold(true)

	if f.freq() == models.RepeatWeekly {
		parts := make([]string, 0, daysInWeek)
		for i, name := range localizedWeekdayShortNames(env.loc) {
			cell := " " + name + " "
			style := lipgloss.NewStyle()
			if f.weekdays[i] {
				cell = "[" + name + "]"
				style = selected
			}
			if active && i == f.dayCursor {
				style = style.Underline(true)
			}
			parts = append(parts, style.Render(cell))
		}
		return strings.Join(parts, "")
	}

	day := time.Now()
	if parsed, err := timeutil.ParseDay(f.values[reminderFieldDate], time.Now()); err == nil {
		day = parsed
	}
	options := [monthlyModeCount]string{
		env.loc.Format("remind.form.monthly.day", day.Day()),
		monthlyWeekdayLabel(env.loc, day),
		env.loc.Text("remind.repeat.ordinal.last") + " " + localizedWeekdayAbbr(env.loc, day.Weekday()),
		env.loc.Text("remind.repeat.last_day"),
	}
	parts := make([]string, 0, len(options))
	for i, option := range options {
		if monthlyMode(i) == f.monthly {
			parts = append(parts, selected.Render("["+option+"]"))
		} else {
			parts = append(parts, " "+option+" ")
		}
	}
	return strings.Join(parts, " ")
}

func monthlyWeekdayLabel(loc *localization.Localizer, day time.Time) string {
	week := weekOfMonth(day)
	ordinal := "last"
	if week != models.WeekLast {
		ordinal = strconv.Itoa(week)
	}
	return loc.Text("remind.repeat.ordinal."+ordinal) + " " + localizedWeekdayAbbr(loc, day.Weekday())
}

// renderPreview is the month of the next occurrence with the occurrences marked, and the
// next few of them.
func (f *reminderForm) renderPreview(env reminderFormEnv, ui chromeStyles, now time.Time) string {
	preview, ok := f.preview(env.tf, now)
	month := now
	if ok {
		month = preview.Due
	}

	today, _ := timeutil.LocalDayBounds(now)
	var occurrences []models.ReminderOccurrence
	if ok {
		occurrences = preview.Occurrences(today, today.AddDate(0, 0, reminderFormPreviewDays), now)
	}

	lines := renderOccurrenceMonth(env, ui, month, now, occurrences)
	lines = append(lines, "", ui.renderSection(env.loc.Text("remind.form.next")))
	for i, occurrence := range occurrences {
		if i == reminderFormNextCount {
			break
		}
		style := ui.muted
		if i == 0 {
			style = ui.bold
		}
		lines = append(lines, style.Render(formatReminderDue(env.loc, env.tf, occurrence.Due, occurrence.Reminder.AllDay, now)))
	}
	return strings.Join(lines, "\n")
}

// renderOccurrenceMonth is the month of day like the calendar's, its occurrences in the
// reminder color.
func renderOccurrenceMonth(env reminderFormEnv, ui chromeStyles, day, now time.Time, occurrences []models.ReminderOccurrence) []string {
	onDay := make(map[string]bool, len(occurrences))
	for _, occurrence := range occurrences {
		onDay[occurrence.Key()] = true
	}

	weekdays := make([]string, 0, daysInWeek)
	for _, name := range localizedWeekdayShortNames(env.loc) {
		weekdays = append(weekdays, padText(name, calendarDayCellW-1, true))
	}
	lines := []string{ui.renderSection(formatLocalizedMonthYear(env.loc, day)), ui.muted.Render(strings.Join(weekdays, " "))}

	first := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.Local)
	column := mondayFirstIndex(first.Weekday())
	var week strings.Builder
	week.WriteString(strings.Repeat(" ", column*calendarDayCellW))
	for date := first; date.Month() == first.Month(); date = date.AddDate(0, 0, 1) {
		style := ui.muted
		switch {
		case sameDay(date, now):
			style = ui.accentBold
		case onDay[models.OccurrenceKey(date)]:
			style = ui.reminder.Bold(true)
		}
		week.WriteString(style.Render(fmt.Sprintf("%3d", date.Day())) + " ")
		if column++; column == daysInWeek {
			lines = append(lines, week.String())
			week.Reset()
			column = 0
		}
	}
	if column > 0 {
		lines = append(lines, week.String())
	}
	return lines
}

// reminderFormProgram runs the form on its own, for `remind add` and `remind edit`.
type reminderFormProgram struct {
	ctx    context.Context //nolint:containedctx // the model outlives each Bubble Tea message
	svc    ports.ReminderResolver
	env    reminderFormEnv
	form   *reminderForm
	width  int
	saved  *models.Reminder
	status string
}

var runReminderFormProgram = func(model *reminderFormProgram) error {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return errors.Wrap(err, "run program")
	}
	return nil
}

// runReminderForm opens the form and repeats its result on the terminal once it closes.
func runReminderForm(cmd *cobra.Command, form *reminderForm) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	rt := getRuntime(cmd)
	model := &reminderFormProgram{
		ctx:  cmd.Context(),
		svc:  svc,
		env:  newReminderFormEnv(rt.Localizer, rt.TimeFormatter, GetTheme(rt.Config.Theme)),
		form: form,
	}
	if err = runReminderFormProgram(model); err != nil {
		return err
	}
	if model.status != "" {
		fmt.Fprintln(cmd.OutOrStdout(), model.status)
	}
	return nil
}

func (m *reminderFormProgram) Init() tea.Cmd { return nil }

func (m *reminderFormProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
		switch m.form.update(msg) {
		case formCancel:
			return m, tea.Quit
		case formSave:
			now := reminderNow()
			saved, err := saveReminderForm(m.ctx, m.svc, m.form, m.env.tf, now)
			if err != nil {
				m.form.err = reminderErrorText(m.env.loc, err)
				return m, nil
			}
			m.saved = saved
			key := "remind.added"
			if m.form.id != 0 {
				key = "remind.updated"
			}
			m.status = strings.TrimRight(m.env.loc.Format(key,
				saved.ID, reminderLabel(*saved), formatReminderSchedule(m.env.loc, m.env.tf, *saved, now)), "\n")
			return m, tea.Quit
		case formContinue:
		}
	}
	return m, nil
}

func (m *reminderFormProgram) View() string {
	return "\n" + m.form.view(m.env, reminderNow(), m.width)
}
