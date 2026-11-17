package commands

import (
	"context"
	"fmt"
	"slices"
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
	editFieldProject = iota
	editFieldDescription
	editFieldStart
	editFieldEnd
	editFieldTags
	editFieldNotes
	editFieldCount
)

// editFormLabelW is the label column of the edit form.
const editFormLabelW = 13

// Notes are edited on a single-line field, so newlines are escaped on the way
// in and out; a literal backslash survives the round trip.
var (
	editNotesEncoder = strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	editNotesDecoder = strings.NewReplacer(`\\`, `\`, `\n`, "\n")
)

var runEditProgram = func(model *editModel) error {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return errors.Wrap(err, "run program")
	}
	return model.err
}

// runEditPicker opens the browser UI: navigate days, pick an entry, edit it.
func runEditPicker(cmd *cobra.Command) error {
	model := newEditModelForCmd(cmd)
	model.loadHistory()
	return runEditModel(cmd, model)
}

// runEditForm opens the edit form for a single, already resolved activity.
func runEditForm(cmd *cobra.Command, activity models.Activity) error {
	model := newEditModelForCmd(cmd)
	model.selectedDate = activity.StartTime
	model.openForm(activity)
	model.closeAfterSave = true
	return runEditModel(cmd, model)
}

func newEditModelForCmd(cmd *cobra.Command) *editModel {
	rt := getRuntime(cmd)
	return newEditModel(cmd.Context(), rt.ActivityService, rt.TimeFormatter, getLocalizer(cmd), GetTheme(rt.Config.Theme))
}

// runEditModel runs the editor and repeats its last result on the terminal,
// because the alternate screen is gone once the program exits.
func runEditModel(cmd *cobra.Command, model *editModel) error {
	if err := runEditProgram(model); err != nil {
		return err
	}
	if model.status == "" {
		return nil
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), model.status)
	return err
}

type editModel struct {
	ctx            context.Context //nolint:containedctx // the model outlives each Bubble Tea message
	service        ports.ActivityResolver
	timeFormat     *timeutil.Formatter
	loc            *localization.Localizer
	ui             chromeStyles
	selectedDate   time.Time
	allActivities  []models.Activity
	list           activityList
	form           editForm
	formOpen       bool
	closeAfterSave bool
	status         string
	formErr        string
	err            error
	width          int
	height         int
}

type editFormField struct {
	label string
	value string
}

type editForm struct {
	original models.Activity
	fields   []editFormField
	cursor   int
}

func newEditModel(
	ctx context.Context,
	service ports.ActivityResolver,
	tf *timeutil.Formatter,
	loc *localization.Localizer,
	theme Theme,
) *editModel {
	ui := newChromeStyles(theme)
	return &editModel{
		ctx:          ctx,
		service:      service,
		timeFormat:   tf,
		loc:          loc,
		ui:           ui,
		selectedDate: time.Now(),
		list:         newActivityList(ui, loc, tf),
	}
}

// loadHistory reads the log once, so navigating days does not scan it again,
// and opens on the most recent day that has entries instead of an empty today.
func (m *editModel) loadHistory() {
	activities, err := m.service.List(m.ctx, models.ActivityFilter{})
	if err != nil {
		m.err = errors.Wrap(err, "list activities")
		return
	}
	m.allActivities = activities

	if len(activitiesOnDay(activities, m.selectedDate)) == 0 {
		m.selectedDate = adjacentActivityDay(activities, m.selectedDate, -1)
	}
	m.list.set(activitiesOnDay(activities, m.selectedDate))
}

func (m *editModel) navigate(dir int) {
	m.selectedDate = adjacentActivityDay(m.allActivities, m.selectedDate, dir)
	m.list.set(activitiesOnDay(m.allActivities, m.selectedDate))
}

func (m *editModel) openForm(activity models.Activity) {
	m.form = newEditForm(activity, m.timeFormat)
	m.formOpen = true
	m.formErr = ""
	m.status = ""
}

func newEditForm(activity models.Activity, tf *timeutil.Formatter) editForm {
	endValue := ""
	if activity.EndTime != nil {
		endValue = activity.EndTime.Format(tf.GetDisplayFormatWithDate())
	}

	fields := make([]editFormField, editFieldCount)
	fields[editFieldProject] = editFormField{label: "edit.form.project", value: activity.Project}
	fields[editFieldDescription] = editFormField{label: "edit.form.description", value: activity.Description}
	fields[editFieldStart] = editFormField{
		label: "edit.form.start",
		value: activity.StartTime.Format(tf.GetDisplayFormatWithDate()),
	}
	fields[editFieldEnd] = editFormField{label: "edit.form.end", value: endValue}
	fields[editFieldTags] = editFormField{label: "edit.form.tags", value: strings.Join(activity.Tags, ", ")}
	fields[editFieldNotes] = editFormField{
		label: "edit.form.notes",
		value: editNotesEncoder.Replace(activity.Notes),
	}

	return editForm{original: activity, fields: fields}
}

// update applies a key to the form: tab and the arrows move between the fields, the others edit
// the value of the current one.
func (f *editForm) update(msg tea.KeyMsg) formOutcome {
	field := &f.fields[f.cursor]
	//nolint:exhaustive // bubbletea key types are an open set; unlisted keys are ignored on purpose
	switch msg.Type {
	case tea.KeyEsc:
		return formCancel
	case tea.KeyEnter:
		return formSave
	case tea.KeyTab, tea.KeyDown:
		f.cursor = (f.cursor + 1) % len(f.fields)
	case tea.KeyShiftTab, tea.KeyUp:
		f.cursor = (f.cursor - 1 + len(f.fields)) % len(f.fields)
	case tea.KeyBackspace, tea.KeyDelete:
		if runes := []rune(field.value); len(runes) > 0 {
			field.value = string(runes[:len(runes)-1])
		}
	case tea.KeyCtrlU:
		field.value = ""
	case tea.KeySpace:
		field.value += " "
	case tea.KeyRunes:
		field.value += string(msg.Runes)
	}
	return formContinue
}

// lines are the fields of the form, tags in the tag color, and the hint under them.
func (f *editForm) lines(ui chromeStyles, loc *localization.Localizer, width int) []string {
	lines := make([]string, 0, len(f.fields)+2)
	for i, field := range f.fields {
		line := formField{
			label: loc.Text(field.label), labelW: editFormLabelW, value: field.value, valueStyle: ui.text, active: i == f.cursor,
		}
		switch {
		case i == editFieldTags && field.value != "":
			line.valueStyle = ui.tag
		case line.active:
			line.valueStyle = ui.bold
		}
		lines = append(lines, ui.renderFormField(line, width))
	}
	hint := loc.Text("edit.form.hint")
	if width > 0 {
		hint = cutText(hint, width-1)
	}
	return append(lines, "", " "+ui.muted.Render(hint))
}

// summary names what the form edits: "Tock · Mon 16 Nov".
func (f *editForm) summary(ui chromeStyles, loc *localization.Localizer) []rulerItem {
	day := formatReminderDay(loc, f.original.StartTime, time.Now())
	long := ui.muted.Render(f.original.Project + " · " + day)
	return []rulerItem{{long: long, short: ui.muted.Render(day)}}
}

// editFormKeys are the keys of the edit form; esc goes back with the action given.
func editFormKeys(loc *localization.Localizer, back string) []footerKey {
	key := func(k, action string) footerKey { return footerKey{key: k, action: loc.Text(action)} }
	return []footerKey{key(keyTab, "tui.key.next_field"), key("enter", "tui.key.save"), key("ctrl+u", "tui.key.clear"), key("esc", back)}
}

// saveEditForm applies the form through the service: the updated activity, or the error to show.
func saveEditForm(
	ctx context.Context,
	service ports.ActivityResolver,
	tf *timeutil.Formatter,
	loc *localization.Localizer,
	form editForm,
) (*models.Activity, string) {
	req, err := buildFormRequest(tf, form)
	if err != nil {
		return nil, editErrorText(loc, err)
	}
	updated, err := service.Update(ctx, form.original, req)
	if err != nil {
		return nil, editErrorText(loc, err)
	}
	return updated, ""
}

func (m *editModel) Init() tea.Cmd { return nil }

func (m *editModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.formOpen {
			return m, m.updateForm(msg)
		}
		return m, m.updateList(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	}
	return m, nil
}

func (m *editModel) updateList(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", keyCtrlC, keyEsc:
		return tea.Quit
	case keyLeft, "h":
		m.navigate(-1)
	case keyRight, "l":
		m.navigate(1)
	case "enter":
		if selected, ok := m.list.selected(); ok {
			m.openForm(selected)
		}
	default:
		m.list.move(msg.String())
	}
	return nil
}

func (m *editModel) updateForm(msg tea.KeyMsg) tea.Cmd {
	if msg.Type == tea.KeyCtrlC {
		return tea.Quit
	}
	switch m.form.update(msg) {
	case formCancel:
		m.formOpen = false
		if m.closeAfterSave {
			return tea.Quit
		}
	case formSave:
		return m.save()
	case formContinue:
	}
	return nil
}

func (m *editModel) save() tea.Cmd {
	updated, errText := saveEditForm(m.ctx, m.service, m.timeFormat, m.loc, m.form)
	if updated == nil {
		m.formErr = errText
		return nil
	}

	m.formOpen = false
	m.formErr = ""
	m.status = m.loc.Format("edit.saved", updated.Project, updated.Description)
	if m.closeAfterSave {
		return tea.Quit
	}

	m.selectedDate = updated.StartTime
	m.loadHistory()
	return nil
}

// editErrorText renders the domain errors the form can provoke with the same
// localized messages the rest of the UI uses.
func editErrorText(loc *localization.Localizer, err error) string {
	switch {
	case errors.Is(err, coreErrors.ErrProjectRequired):
		return loc.Text("validation.project_required")
	case errors.Is(err, coreErrors.ErrDescriptionRequired):
		return loc.Text("validation.description_required")
	case errors.Is(err, coreErrors.ErrInvalidTimeRange):
		return loc.Text("edit.error.invalid_time_range")
	case errors.Is(err, coreErrors.ErrStartTimeConflict):
		return loc.Text("edit.error.start_conflict")
	case errors.Is(err, coreErrors.ErrAmbiguousStartTime):
		return loc.Text("edit.error.ambiguous_start")
	case errors.Is(err, coreErrors.ErrActivityAlreadyStarted):
		return loc.Text("edit.error.already_running")
	}
	return err.Error()
}

// buildFormRequest turns the edited form values into a partial update request,
// keeping fields the user did not touch untouched.
func buildFormRequest(tf *timeutil.Formatter, form editForm) (models.UpdateActivityRequest, error) {
	original := form.original
	var req models.UpdateActivityRequest

	if project := strings.TrimSpace(form.fields[editFieldProject].value); project != original.Project {
		req.Project = &project
	}

	if description := strings.TrimSpace(form.fields[editFieldDescription].value); description != original.Description {
		req.Description = &description
	}

	if err := applyFormTimes(tf, form, &req); err != nil {
		return models.UpdateActivityRequest{}, err
	}

	if tags := parseTagValues([]string{form.fields[editFieldTags].value}); !slices.Equal(tags, original.Tags) {
		req.Tags = &tags
	}

	notes := editNotesDecoder.Replace(form.fields[editFieldNotes].value)
	if notes != original.Notes {
		req.Notes = &notes
	}
	return req, nil
}

func applyFormTimes(tf *timeutil.Formatter, form editForm, req *models.UpdateActivityRequest) error {
	original := form.original

	startTime, err := parseEditTime(tf, original.StartTime, form.fields[editFieldStart].value)
	if err != nil {
		return errors.Wrap(err, "parse start time")
	}
	if !startTime.Equal(original.StartTime) {
		req.StartTime = &startTime
	}

	endValue := strings.TrimSpace(form.fields[editFieldEnd].value)
	if endValue == "" {
		if original.EndTime != nil {
			req.ClearEndTime = true
		}
		return nil
	}

	endTime, err := parseEditTime(tf, startTime, endValue)
	if err != nil {
		return errors.Wrap(err, "parse end time")
	}
	// The form shows minutes only, so an untouched end must keep its seconds.
	if original.EndTime == nil || !endTime.Equal(original.EndTime.Truncate(time.Minute)) {
		req.EndTime = &endTime
	}
	return nil
}

func (m *editModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v", m.err)
	}
	if m.formOpen {
		return m.formView()
	}
	return m.listView()
}

// listView is the picker: the entries of a day under the header naming it.
func (m *editModel) listView() string {
	summary := m.ui.muted.Render(fmt.Sprintf("%d · %s", len(m.list.activities), formatHM(m.list.total())))
	header := screenHeader{
		tabs:    []string{m.loc.Text("tui.tab.edit")},
		title:   formatLocalizedLongDateShortMonth(m.loc, m.selectedDate),
		hint:    "←→",
		summary: []rulerItem{{long: summary, short: summary}},
	}
	top := m.ui.renderHeader(header, m.width) + "\n"
	key := func(k, action string) footerKey { return footerKey{key: k, action: m.loc.Text(action)} }
	keys := []footerKey{key("←→", "tui.key.day"), key("↑↓", "tui.key.entry")}
	if len(m.list.activities) > 0 {
		keys = append(keys, key("enter", "tui.key.edit"))
	}
	keys = append(keys, key("q", "tui.key.quit"))
	bottom := "\n" + m.ui.renderFooter(keys, m.ui.renderStatus(m.status, false), m.width)
	bodyH := m.height - lipgloss.Height(top) - lipgloss.Height(bottom)
	return renderScreen(top, m.list.view(m.width, bodyH, m.loc.Text("list.empty")), bottom, m.width, m.height)
}

// formView is the form of the picked entry under the header naming it.
func (m *editModel) formView() string {
	back := "tui.key.back"
	if m.closeAfterSave {
		back = "tui.key.cancel"
	}
	header := screenHeader{
		tabs:    []string{m.loc.Text("tui.tab.edit")},
		title:   m.formTitle(),
		summary: m.form.summary(m.ui, m.loc),
	}
	top := m.ui.renderHeader(header, m.width) + "\n"
	body := strings.Join(m.form.lines(m.ui, m.loc, m.width), "\n")
	bottom := "\n" + m.ui.renderFooter(editFormKeys(m.loc, back), m.ui.renderStatus(m.formErr, true), m.width)
	return renderScreen(top, body, bottom, m.width, m.height)
}

// formTitle is the key of the edited entry, "2026-03-14-02", or its day when the log was not read.
func (m *editModel) formTitle() string {
	original := m.form.original
	for i, activity := range activitiesOnDay(m.allActivities, original.StartTime) {
		if activity.StartTime.Equal(original.StartTime) {
			return activityDayKey(activity, i)
		}
	}
	return original.StartTime.Format(time.DateOnly)
}
