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
	reminderFormLabelWidth  = 14
	reminderFormPreviewDays = 366 * 2
	reminderFormNextCount   = 5
	// reminderFormPreviewMinWidth is the terminal width from which the month preview shows.
	reminderFormPreviewMinWidth = 100
	reminderFormLeftWidth       = 70
	maxWeekOfMonthIndex         = 4
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
	loc    *localization.Localizer
	tf     *timeutil.Formatter
	theme  Theme
	styles Styles
}

func newReminderFormEnv(loc *localization.Localizer, tf *timeutil.Formatter, theme Theme) reminderFormEnv {
	return reminderFormEnv{loc: loc, tf: tf, theme: theme, styles: InitStyles(theme)}
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

func (f *reminderForm) view(env reminderFormEnv, now time.Time, width int) string {
	left := f.renderFields(env, now)
	if width < reminderFormPreviewMinWidth {
		return left
	}

	right := f.renderPreview(env, now)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(reminderFormLeftWidth).Render(left), "   ", right)
}

func (f *reminderForm) renderFields(env reminderFormEnv, now time.Time) string {
	loc := env.loc
	header := loc.Text("remind.form.new")
	if f.id != 0 {
		header = loc.Format("remind.form.edit", f.id)
	}

	labelStyle := lipgloss.NewStyle().Foreground(env.theme.SubText).Width(reminderFormLabelWidth)
	lines := []string{lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).Render(header), ""}

	labels := [reminderFieldCount]string{
		"remind.form.title", "remind.form.project", "remind.form.date", "remind.form.time", "remind.form.repeat",
		"remind.form.on", "remind.form.until", "remind.form.notify", "remind.form.tags", "remind.form.notes",
	}
	for field := range reminderFieldCount {
		if !f.visible(field) {
			continue
		}
		marker := "  "
		if field == f.cursor {
			marker = "> "
		}
		lines = append(lines, marker+labelStyle.Render(loc.Text(labels[field]))+f.renderValue(env, field))
	}

	lines = append(lines, "")
	if preview, ok := f.preview(env.tf, now); ok {
		lines = append(lines, env.styles.Reminder.Render(formatReminderSummary(loc, env.tf, preview, now)))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(env.theme.SubText).Render(loc.Text("remind.form.invalid")))
	}
	lines = append(lines, "", lipgloss.NewStyle().Width(reminderFormLeftWidth).Render(loc.Text("remind.form.help")))
	if f.err != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(env.theme.Secondary).Render(f.err))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (f *reminderForm) renderValue(env reminderFormEnv, field int) string {
	active := field == f.cursor
	activeStyle := lipgloss.NewStyle().Foreground(env.theme.Highlight).Bold(true)
	faint := lipgloss.NewStyle().Foreground(env.theme.SubText)

	switch field {
	case reminderFieldRepeat:
		value := "‹ " + env.loc.Text("remind.form.repeat."+repeatChoiceKey(f.freq())) + " ›"
		if active {
			return activeStyle.Render(value)
		}
		return value
	case reminderFieldOn:
		return f.renderOn(env, active)
	}

	value := f.values[field]
	if active {
		return activeStyle.Render(value + "█")
	}
	if value == "" {
		switch field {
		case reminderFieldTime:
			return faint.Render(env.loc.Text("remind.all_day"))
		case reminderFieldUntil:
			return faint.Render(env.loc.Text("remind.form.never"))
		}
	}
	return value
}

func repeatChoiceKey(freq models.RepeatFreq) string {
	if freq == models.RepeatNone {
		return repeatSpecNone
	}
	return string(freq)
}

func (f *reminderForm) renderOn(env reminderFormEnv, active bool) string {
	selected := lipgloss.NewStyle().Foreground(env.theme.Highlight).Bold(true)

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

func (f *reminderForm) renderPreview(env reminderFormEnv, now time.Time) string {
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

	var b strings.Builder
	b.WriteString(renderOccurrenceMonth(env, month, now, occurrences))
	b.WriteString("\n\n" + lipgloss.NewStyle().Bold(true).Render(env.loc.Text("remind.form.next")) + "\n")
	for i, occurrence := range occurrences {
		if i == reminderFormNextCount {
			break
		}
		mark, style := "·", lipgloss.NewStyle().Foreground(env.theme.SubText)
		if i == 0 {
			mark, style = "›", lipgloss.NewStyle().Foreground(env.theme.Highlight)
		}
		when := formatReminderDue(env.loc, env.tf, occurrence.Due, occurrence.Reminder.AllDay, now)
		fmt.Fprintf(&b, "%s %s\n", env.styles.Line.Render(mark), style.Render(when))
	}
	return b.String()
}

// renderOccurrenceMonth draws the month of day with the given occurrences highlighted.
func renderOccurrenceMonth(env reminderFormEnv, day, now time.Time, occurrences []models.ReminderOccurrence) string {
	onDay := make(map[string]bool, len(occurrences))
	for _, occurrence := range occurrences {
		onDay[occurrence.Key()] = true
	}

	var b strings.Builder
	b.WriteString(env.styles.Header.Render(formatLocalizedMonthYear(env.loc, day)) + "\n\n")
	for _, weekday := range localizedWeekdayShortNames(env.loc) {
		b.WriteString(env.styles.Weekday.Render(weekday))
	}
	b.WriteString("\n")

	first := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.Local)
	column := mondayFirstIndex(first.Weekday())
	b.WriteString(strings.Repeat("    ", column))

	occurStyle := env.styles.Day.Foreground(env.theme.Reminder).Bold(true)
	for date := first; date.Month() == first.Month(); date = date.AddDate(0, 0, 1) {
		style := env.styles.Day.Foreground(env.theme.SubText)
		switch {
		case sameDay(date, now):
			style = env.styles.Today
		case onDay[models.OccurrenceKey(date)]:
			style = occurStyle
		}
		b.WriteString(style.Render(strconv.Itoa(date.Day())))

		column++
		if column == daysInWeek {
			column = 0
			b.WriteString("\n")
		}
	}
	return env.styles.Wrapper.Render(strings.TrimRight(b.String(), "\n"))
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
	return m.form.view(m.env, reminderNow(), m.width)
}
