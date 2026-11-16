package commands

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	appruntime "github.com/kriuchkov/tock/internal/app/runtime"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// reminderNow is the clock the reminder commands read; tests replace it.
var reminderNow = time.Now

const (
	defaultReminderListDays = 7
	defaultReminderSnooze   = 10 * time.Minute
	// repeatSpecNone clears a repeat bound, and names "no repeat" in the form.
	repeatSpecNone = "none"
)

func NewRemindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remind",
		Aliases: []string{"reminders"},
		Short:   defaultText("remind.short"),
		Long:    defaultText("remind.long"),
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return runRemindTUI(cmd) },
	}

	cmd.AddCommand(newRemindAddCmd())
	cmd.AddCommand(newRemindListCmd())
	cmd.AddCommand(newRemindDoneCmd())
	cmd.AddCommand(newRemindSnoozeCmd())
	cmd.AddCommand(newRemindStartCmd())
	cmd.AddCommand(newRemindEditCmd())
	cmd.AddCommand(newRemindRemoveCmd())
	return cmd
}

func runtimeFromCmd(cmd *cobra.Command) (*appruntime.Runtime, bool) {
	if cmd == nil || cmd.Context() == nil {
		return nil, false
	}
	return appruntime.FromContext(cmd.Context())
}

func getReminderService(cmd *cobra.Command) (ports.ReminderResolver, error) {
	svc := getRuntime(cmd).ReminderService
	if svc == nil {
		return nil, errors.New(text(cmd, "remind.error.unavailable"))
	}
	return svc, nil
}

// parseReminderID reads a reminder ID given as "4" or "#4".
func parseReminderID(cmd *cobra.Command, arg string) (int, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(arg), "#"))
	if err != nil || id < 1 {
		return 0, errors.New(text(cmd, "remind.error.invalid_id", arg))
	}
	return id, nil
}

// reminderError turns the reminder sentinel errors into user-facing text.
func reminderError(cmd *cobra.Command, id int, err error) error {
	switch {
	case errors.Is(err, coreErrors.ErrReminderNotFound):
		return errors.New(text(cmd, "remind.error.not_found", id))
	case errors.Is(err, coreErrors.ErrNoReminderOccurrence):
		return errors.New(text(cmd, "remind.error.no_occurrence", id))
	case errors.Is(err, coreErrors.ErrReminderTitleRequired):
		return errors.New(text(cmd, "remind.error.title_required"))
	case errors.Is(err, coreErrors.ErrProjectRequired):
		return errors.New(text(cmd, "remind.error.project_required"))
	case errors.Is(err, coreErrors.ErrReminderNeverDue):
		return errors.New(text(cmd, "remind.error.never_due"))
	}
	return err
}

// resolveOccurrence picks the occurrence a command acts on: the one on --date when given, else
// the overdue one, else the next one not done.
func resolveOccurrence(
	cmd *cobra.Command,
	svc ports.ReminderResolver,
	id int,
	date string,
	now time.Time,
) (*models.ReminderOccurrence, error) {
	ctx := cmd.Context()
	if date == "" {
		occurrence, err := svc.Current(ctx, id, now)
		if err != nil {
			return nil, reminderError(cmd, id, err)
		}
		return occurrence, nil
	}

	day, err := timeutil.ParseDay(date, now)
	if err != nil {
		return nil, errors.Wrap(err, "parse --date")
	}
	reminder, err := svc.Get(ctx, id)
	if err != nil {
		return nil, reminderError(cmd, id, err)
	}
	occurrence, ok := reminder.OccurrenceOn(day, now)
	if !ok {
		return nil, reminderError(cmd, id, coreErrors.ErrNoReminderOccurrence)
	}
	return &occurrence, nil
}

type remindListOptions struct {
	Today      bool
	Week       bool
	Overdue    bool
	All        bool
	JSONOutput bool
}

func newRemindListCmd() *cobra.Command {
	var opts remindListOptions

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   defaultText("remind.list.short"),
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, _ []string) error { return runRemindList(cmd, &opts) },
	}

	cmd.Flags().BoolVar(&opts.Today, "today", false, defaultText("remind.list.flag.today"))
	cmd.Flags().BoolVar(&opts.Week, "week", false, defaultText("remind.list.flag.week"))
	cmd.Flags().BoolVar(&opts.Overdue, "overdue", false, defaultText("remind.list.flag.overdue"))
	cmd.Flags().BoolVar(&opts.All, "all", false, defaultText("remind.list.flag.all"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("remind.flag.json"))
	cmd.MarkFlagsMutuallyExclusive("today", "week", "overdue", "all")
	return cmd
}

// reminderOccurrenceJSON is the JSON form of one listed occurrence.
type reminderOccurrenceJSON struct {
	ID      int       `json:"id"`
	Title   string    `json:"title"`
	Project string    `json:"project,omitempty"`
	Due     time.Time `json:"due"`
	AllDay  bool      `json:"all_day,omitempty"`
	Every   string    `json:"every,omitempty"`
	Status  string    `json:"status"`
	Tags    []string  `json:"tags,omitempty"`
	Notes   string    `json:"notes,omitempty"`
}

func newReminderOccurrenceJSON(occurrence models.ReminderOccurrence, now time.Time) reminderOccurrenceJSON {
	reminder := occurrence.Reminder
	return reminderOccurrenceJSON{
		ID:      reminder.ID,
		Title:   reminder.Title,
		Project: reminder.Project,
		Due:     occurrence.Due,
		AllDay:  reminder.AllDay,
		Every:   reminder.Repeat.Spec(),
		Status:  string(occurrence.Status(now)),
		Tags:    reminder.Tags,
		Notes:   reminder.Notes,
	}
}

func runRemindList(cmd *cobra.Command, opts *remindListOptions) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	now := reminderNow()

	if opts.All {
		return listAllReminders(cmd, svc, opts.JSONOutput, now)
	}

	occurrences, err := listedOccurrences(cmd, svc, opts, now)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if opts.JSONOutput {
		items := make([]reminderOccurrenceJSON, 0, len(occurrences))
		for _, occurrence := range occurrences {
			items = append(items, newReminderOccurrenceJSON(occurrence, now))
		}
		return writeJSONTo(out, items)
	}

	if len(occurrences) == 0 {
		fmt.Fprintln(out, text(cmd, "remind.list.empty"))
		return nil
	}

	rt := getRuntime(cmd)
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, text(cmd, "remind.list.header"))
	for _, occurrence := range occurrences {
		reminder := occurrence.Reminder
		fmt.Fprintf(w, "%3d\t%s\t%s\t%s\t%s\t%s\n",
			reminder.ID,
			formatReminderDue(rt.Localizer, rt.TimeFormatter, occurrence.Due, reminder.AllDay, now),
			reminder.Title,
			orDash(formatRepeat(rt.Localizer, reminder)),
			reminder.Project,
			formatReminderStatusLine(rt.Localizer, rt.TimeFormatter, occurrence, now, 0),
		)
	}
	if err = w.Flush(); err != nil {
		return errors.Wrap(err, "flush table")
	}
	return nil
}

// listedOccurrences returns the overdue occurrences followed by the ones in the listed period.
func listedOccurrences(
	cmd *cobra.Command,
	svc ports.ReminderResolver,
	opts *remindListOptions,
	now time.Time,
) ([]models.ReminderOccurrence, error) {
	ctx := cmd.Context()
	overdue, err := svc.Overdue(ctx, now)
	if err != nil {
		return nil, errors.Wrap(err, "list overdue reminders")
	}
	if opts.Overdue {
		return overdueOnly(overdue, now), nil
	}

	from, _ := timeutil.LocalDayBounds(now)
	to := from.AddDate(0, 0, defaultReminderListDays)
	if opts.Today {
		to = from.AddDate(0, 0, 1)
	}

	upcoming, err := svc.Occurrences(ctx, from, to, now)
	if err != nil {
		return nil, errors.Wrap(err, "list reminders")
	}
	return mergeOccurrences(overdue, upcoming), nil
}

// overdueOnly drops the occurrences Overdue returns because they are snoozed past their due time:
// they stay listed, but are not counted or shown as overdue.
func overdueOnly(occurrences []models.ReminderOccurrence, now time.Time) []models.ReminderOccurrence {
	var overdue []models.ReminderOccurrence
	for _, occurrence := range occurrences {
		if occurrence.Status(now) == models.ReminderOverdue {
			overdue = append(overdue, occurrence)
		}
	}
	return overdue
}

// mergeOccurrences appends upcoming to overdue, dropping the ones already listed as overdue.
func mergeOccurrences(overdue, upcoming []models.ReminderOccurrence) []models.ReminderOccurrence {
	seen := make(map[string]bool, len(overdue))
	merged := make([]models.ReminderOccurrence, 0, len(overdue)+len(upcoming))
	for _, occurrence := range overdue {
		seen[occurrenceID(occurrence)] = true
		merged = append(merged, occurrence)
	}
	for _, occurrence := range upcoming {
		if !seen[occurrenceID(occurrence)] {
			merged = append(merged, occurrence)
		}
	}
	return merged
}

func occurrenceID(occurrence models.ReminderOccurrence) string {
	return strconv.Itoa(occurrence.Reminder.ID) + "@" + occurrence.Key()
}

func listAllReminders(cmd *cobra.Command, svc ports.ReminderResolver, jsonOutput bool, now time.Time) error {
	reminders, err := svc.List(cmd.Context())
	if err != nil {
		return errors.Wrap(err, "list reminders")
	}

	out := cmd.OutOrStdout()
	if jsonOutput {
		if reminders == nil {
			reminders = []models.Reminder{}
		}
		return writeJSONTo(out, reminders)
	}
	if len(reminders) == 0 {
		fmt.Fprintln(out, text(cmd, "remind.list.empty"))
		return nil
	}

	rt := getRuntime(cmd)
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, text(cmd, "remind.list.all_header"))
	for _, reminder := range reminders {
		next := "—"
		if occurrence, ok := reminder.Current(now); ok {
			next = formatReminderDue(rt.Localizer, rt.TimeFormatter, occurrence.Due, reminder.AllDay, now)
		}
		fmt.Fprintf(w, "%3d\t%s\t%s\t%s\t%s\n",
			reminder.ID,
			next,
			reminder.Title,
			orDash(formatRepeat(rt.Localizer, reminder)+formatRepeatBounds(rt.Localizer, reminder.Repeat)),
			reminder.Project,
		)
	}
	if err = w.Flush(); err != nil {
		return errors.Wrap(err, "flush table")
	}
	return nil
}

func orDash(value string) string {
	if value == "" {
		return "—"
	}
	return value
}

// printOverdueReminders tells the user on stderr about overdue reminders, e.g. after `tock start`.
func printOverdueReminders(cmd *cobra.Command) {
	rt, ok := runtimeFromCmd(cmd)
	if !ok || rt.ReminderService == nil {
		return
	}

	now := reminderNow()
	overdue, err := rt.ReminderService.Overdue(cmd.Context(), now)
	if err != nil {
		return
	}
	if overdue = overdueOnly(overdue, now); len(overdue) == 0 {
		return
	}

	first := overdue[0]
	due := formatReminderClock(rt.Localizer, rt.TimeFormatter, first.Due, first.Reminder.AllDay)
	if len(overdue) == 1 {
		cmd.PrintErrln(text(cmd, "remind.hint.overdue_one", first.Reminder.Title, due))
		return
	}
	cmd.PrintErrln(text(cmd, "remind.hint.overdue_many", len(overdue), first.Reminder.Title, due))
}
