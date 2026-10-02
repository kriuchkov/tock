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

// editNotesEscaper renders a note on the single-line form field and back, so a
// literal backslash in a note survives the round trip.
var editNotesEscaper = struct{ encode, decode *strings.Replacer }{
	encode: strings.NewReplacer(`\`, `\\`, "\n", `\n`),
	decode: strings.NewReplacer(`\\`, `\`, `\n`, "\n"),
}

var runEditProgram = func(model *editModel) error {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return errors.Wrap(err, "run program")
	}
	return model.err
}

// runEditPicker opens the browser UI: navigate days, pick an entry, edit it.
func runEditPicker(cmd *cobra.Command) error {
	return runEditModel(cmd, newEditModelForCmd(cmd))
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
	width          int
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
	m.reload()
	m.selectLatestDayWithActivities()
	return m
}

// selectLatestDayWithActivities falls back to the most recent day that has
// entries, so the picker does not open on an empty today.
func (m *editModel) selectLatestDayWithActivities() {
	if len(m.activities) > 0 {
		return
	}
	m.navigate(-1)
}

func (m *editModel) initTable() {
	columns := []table.Column{
		{Title: m.loc.Text("list.table.key"), Width: 13},
		{Title: m.loc.Text("list.table.time"), Width: 20},
		{Title: m.loc.Text("list.table.project"), Width: 20},
		{Title: m.loc.Text("list.table.description"), Width: 32},
		{Title: m.loc.Text("list.table.duration"), Width: 10},
		{Title: m.loc.Text("list.table.tags"), Width: 15},
	}

	t := table.New(table.WithColumns(columns), table.WithFocused(true), table.WithHeight(10))

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)
	m.table = t
}

// reload re-reads the log; the result is cached so navigating days does not
// scan the whole history again.
func (m *editModel) reload() {
	activities, err := m.service.List(m.ctx, models.ActivityFilter{})
	if err != nil {
		m.err = errors.Wrap(err, "list activities")
		return
	}
	m.allActivities = activities
	m.renderTable(activities)
}

func (m *editModel) navigate(dir int) {
	current := time.Date(
		m.selectedDate.Year(), m.selectedDate.Month(), m.selectedDate.Day(), 0, 0, 0, 0, m.selectedDate.Location(),
	)
	if target := models.FindTargetDate(m.allActivities, current, dir); target != nil {
		m.selectedDate = *target
	}
	m.renderTable(m.allActivities)
}

func (m *editModel) renderTable(activities []models.Activity) {
	var dayActivities []models.Activity
	for _, a := range activities {
		if sameDay(a.StartTime, m.selectedDate) {
			dayActivities = append(dayActivities, a)
		}
	}
	dayActivities = models.SortActivitiesByStart(dayActivities)
	m.activities = dayActivities

	rows := make([]table.Row, 0, len(dayActivities))
	for i, a := range dayActivities {
		rows = append(rows, table.Row{
			fmt.Sprintf("%s-%02d", a.StartTime.Format(time.DateOnly), i+1),
			m.timeRange(a),
			a.Project,
			a.Description,
			a.Duration().Round(time.Minute).String(),
			strings.Join(a.Tags, ", "),
		})
	}
	m.table.SetRows(rows)

	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(rows) {
		m.table.SetCursor(0)
	}
}

func (m *editModel) timeRange(a models.Activity) string {
	timeStr := a.StartTime.Format(m.timeFormat.GetDisplayFormat())
	if a.EndTime != nil {
		return timeStr + " - " + a.EndTime.Format(m.timeFormat.GetDisplayFormat())
	}
	return timeStr + " - ..."
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
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
		value: editNotesEscaper.encode.Replace(activity.Notes),
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
		m.width = msg.Width
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
		m.editCurrentField(func(value string) string {
			if value == "" {
				return value
			}
			runes := []rune(value)
			return string(runes[:len(runes)-1])
		})
	case tea.KeyCtrlU:
		m.editCurrentField(func(string) string { return "" })
	case tea.KeySpace:
		m.editCurrentField(func(value string) string { return value + " " })
	case tea.KeyRunes:
		m.editCurrentField(func(value string) string { return value + string(msg.Runes) })
	}
	return nil
}

func (m *editModel) editCurrentField(apply func(string) string) {
	field := m.form.fields[m.form.cursor]
	field.value = apply(field.value)
	m.form.fields[m.form.cursor] = field
}

func (m *editModel) save() tea.Cmd {
	req, err := buildFormRequest(m.timeFormat, m.form)
	if err != nil {
		m.formErr = err.Error()
		return nil
	}

	updated, err := m.service.Update(m.ctx, m.form.original, req)
	if err != nil {
		m.formErr = err.Error()
		return nil
	}

	m.formOpen = false
	m.formErr = ""
	m.status = m.loc.Format("edit.saved", updated.Project, updated.Description)
	if m.closeAfterSave {
		return tea.Quit
	}

	m.selectedDate = updated.StartTime
	m.reload()
	return nil
}

// buildFormRequest turns the edited form values into a partial update request,
// keeping fields the user did not touch untouched.
func buildFormRequest(tf *timeutil.Formatter, form editForm) (models.UpdateActivityRequest, error) {
	original := form.original
	var req models.UpdateActivityRequest

	if project := strings.TrimSpace(form.fields[editFieldProject].value); project != original.Project {
		if project == "" {
			return req, errors.New(defaultText("validation.project_required"))
		}
		req.Project = &project
	}

	if description := strings.TrimSpace(form.fields[editFieldDescription].value); description != original.Description {
		if description == "" {
			return req, errors.New(defaultText("validation.description_required"))
		}
		req.Description = &description
	}

	if err := applyFormTimes(tf, form, &req); err != nil {
		return models.UpdateActivityRequest{}, err
	}

	if tags := parseTagList(form.fields[editFieldTags].value); !slices.Equal(tags, original.Tags) {
		req.Tags = &tags
	}

	notes := editNotesEscaper.decode.Replace(form.fields[editFieldNotes].value)
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

func parseTagList(value string) []string {
	var tags []string
	for tag := range strings.SplitSeq(value, ",") {
		if trimmed := strings.TrimSpace(tag); trimmed != "" {
			tags = append(tags, trimmed)
		}
	}
	return tags
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
