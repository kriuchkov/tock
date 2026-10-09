package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
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
	return newEditModel(cmd.Context(), rt.ActivityService, rt.TimeFormatter, getLocalizer(cmd))
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
	selectedDate   time.Time
	activities     []models.Activity
	allActivities  []models.Activity
	table          table.Model
	form           editForm
	formOpen       bool
	closeAfterSave bool
	status         string
	formErr        string
	err            error
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
) *editModel {
	m := &editModel{
		ctx:          ctx,
		service:      service,
		timeFormat:   tf,
		loc:          loc,
		selectedDate: time.Now(),
	}
	m.initTable()
	return m
}

func (m *editModel) initTable() {
	m.table = newActivityTable([]table.Column{
		{Title: m.loc.Text("list.table.key"), Width: 13},
		{Title: m.loc.Text("list.table.time"), Width: 20},
		{Title: m.loc.Text("list.table.project"), Width: 20},
		{Title: m.loc.Text("list.table.description"), Width: 32},
		{Title: m.loc.Text("list.table.duration"), Width: 10},
		{Title: m.loc.Text("list.table.tags"), Width: 15},
	}, defaultDailyTableHeight)
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
	m.renderTable()
}

func (m *editModel) navigate(dir int) {
	m.selectedDate = adjacentActivityDay(m.allActivities, m.selectedDate, dir)
	m.renderTable()
}

func (m *editModel) renderTable() {
	m.activities = activitiesOnDay(m.allActivities, m.selectedDate)
	rows := make([]table.Row, 0, len(m.activities))
	for i, activity := range m.activities {
		rows = append(rows, table.Row{
			activityDayKey(activity, i),
			formatActivityTimeRange(m.timeFormat, activity),
			activity.Project,
			activity.Description,
			formatActivityDuration(activity),
			formatActivityTags(activity),
		})
	}
	m.table.SetRows(rows)

	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(rows) {
		m.table.SetCursor(0)
	}
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

func (m *editModel) Init() tea.Cmd { return nil }

func (m *editModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.formOpen {
			return m, m.updateForm(msg)
		}
		return m, m.updateList(msg)
	case tea.WindowSizeMsg:
		m.table.SetWidth(msg.Width - 4)
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
		if selected, ok := m.selectedActivity(); ok {
			m.openForm(selected)
		}
	default:
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return cmd
	}
	return nil
}

// selectedActivity returns the activity under the table cursor, if any.
func (m *editModel) selectedActivity() (models.Activity, bool) {
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.activities) {
		return models.Activity{}, false
	}
	return m.activities[cursor], true
}

func (m *editModel) updateForm(msg tea.KeyMsg) tea.Cmd {
	//nolint:exhaustive // bubbletea key types are an open set; unlisted keys are ignored on purpose
	switch msg.Type {
	case tea.KeyCtrlC:
		return tea.Quit
	case tea.KeyEsc:
		m.formOpen = false
		if m.closeAfterSave {
			return tea.Quit
		}
		return nil
	case tea.KeyEnter:
		return m.save()
	case tea.KeyTab, tea.KeyDown:
		m.form.cursor = (m.form.cursor + 1) % len(m.form.fields)
	case tea.KeyShiftTab, tea.KeyUp:
		m.form.cursor = (m.form.cursor - 1 + len(m.form.fields)) % len(m.form.fields)
	case tea.KeyBackspace, tea.KeyDelete:
		field := &m.form.fields[m.form.cursor]
		if runes := []rune(field.value); len(runes) > 0 {
			field.value = string(runes[:len(runes)-1])
		}
	case tea.KeyCtrlU:
		m.form.fields[m.form.cursor].value = ""
	case tea.KeySpace:
		m.form.fields[m.form.cursor].value += " "
	case tea.KeyRunes:
		m.form.fields[m.form.cursor].value += string(msg.Runes)
	}
	return nil
}

func (m *editModel) save() tea.Cmd {
	req, err := buildFormRequest(m.timeFormat, m.form)
	if err != nil {
		m.formErr = editErrorText(m.loc, err)
		return nil
	}

	updated, err := m.service.Update(m.ctx, m.form.original, req)
	if err != nil {
		m.formErr = editErrorText(m.loc, err)
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
	if original.EndTime == nil || !endTime.Equal(*original.EndTime) {
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

func (m *editModel) listView() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).
		Render(m.loc.Format("edit.list.header", formatLocalizedLongDateShortMonth(m.loc, m.selectedDate)))

	body := m.table.View()
	if len(m.activities) == 0 {
		body = m.loc.Text("edit.list.empty")
	}

	sections := []string{header, "", body, "", m.loc.Text("edit.list.help")}
	if m.status != "" {
		sections = append(sections, lipgloss.NewStyle().Foreground(lipgloss.Color("120")).Render(m.status))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *editModel) formView() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")).
		Render(m.loc.Format("edit.form.title", m.form.original.StartTime.Format(time.DateOnly)))

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("248")).Width(14)
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)

	lines := make([]string, 0, len(m.form.fields)+4)
	lines = append(lines, header, "")

	for i, field := range m.form.fields {
		marker := "  "
		value := field.value
		if i == m.form.cursor {
			marker = "> "
			value = activeStyle.Render(value + "█")
		}
		lines = append(lines, marker+labelStyle.Render(m.loc.Text(field.label))+value)
	}

	lines = append(lines, "", m.loc.Text("edit.form.help"))
	if m.formErr != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.formErr))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
