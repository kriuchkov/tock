package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

type remindDoneOptions struct {
	Date       string
	Undo       bool
	JSONOutput bool
}

func newRemindDoneCmd() *cobra.Command {
	var opts remindDoneOptions

	cmd := &cobra.Command{
		Use:   "done ID",
		Short: defaultText("remind.done.short"),
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runRemindDone(cmd, args[0], &opts) },
	}
	cmd.Flags().StringVar(&opts.Date, "date", "", defaultText("remind.flag.date"))
	cmd.Flags().BoolVar(&opts.Undo, "undo", false, defaultText("remind.done.flag.undo"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("remind.flag.json"))
	return cmd
}

func runRemindDone(cmd *cobra.Command, arg string, opts *remindDoneOptions) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	id, err := parseReminderID(cmd, arg)
	if err != nil {
		return err
	}
	now := reminderNow()

	target, err := resolveDoneTarget(cmd, svc, id, opts, now)
	if err != nil {
		return err
	}

	occurrence, err := svc.SetDone(cmd.Context(), target.Ref(), !opts.Undo, now)
	if err != nil {
		return reminderError(cmd, id, err)
	}

	out := cmd.OutOrStdout()
	if opts.JSONOutput {
		return writeJSONTo(out, newReminderOccurrenceJSON(*occurrence, now))
	}

	rt := getRuntime(cmd)
	when := formatReminderDue(rt.Localizer, rt.TimeFormatter, occurrence.Due, occurrence.Reminder.AllDay, now)
	if opts.Undo {
		_, err = fmt.Fprintf(out, text(cmd, "remind.undone"), occurrence.Reminder.Title, when)
		return err
	}
	_, err = fmt.Fprintf(out, text(cmd, "remind.done"), occurrence.Reminder.Title, when, nextOccurrenceSuffix(cmd, svc, id, now))
	return err
}

// resolveDoneTarget picks the occurrence to mark: --date, else the current one; undoing without
// --date picks the latest one marked done up to today.
func resolveDoneTarget(
	cmd *cobra.Command,
	svc ports.ReminderResolver,
	id int,
	opts *remindDoneOptions,
	now time.Time,
) (*models.ReminderOccurrence, error) {
	if !opts.Undo || opts.Date != "" {
		return resolveOccurrence(cmd, svc, id, opts.Date, now)
	}

	reminder, err := svc.Get(cmd.Context(), id)
	if err != nil {
		return nil, reminderError(cmd, id, err)
	}
	tomorrow := now.AddDate(0, 0, 1)
	occurrences := reminder.Occurrences(reminder.Due.AddDate(0, 0, -1), tomorrow, now)
	for i := len(occurrences) - 1; i >= 0; i-- {
		if occurrences[i].State.DoneAt != nil {
			return &occurrences[i], nil
		}
	}
	return nil, errors.New(text(cmd, "remind.error.nothing_done", id))
}

// nextOccurrenceSuffix is " Next: Wed 16 Dec 13:00." when the reminder comes back, else "".
func nextOccurrenceSuffix(cmd *cobra.Command, svc ports.ReminderResolver, id int, now time.Time) string {
	next, err := svc.Current(cmd.Context(), id, now)
	if err != nil || next.State.DoneAt != nil {
		return ""
	}
	rt := getRuntime(cmd)
	return " " + text(cmd, "remind.next", formatReminderDue(rt.Localizer, rt.TimeFormatter, next.Due, next.Reminder.AllDay, now))
}

type remindSnoozeOptions struct {
	Date       string
	JSONOutput bool
}

func newRemindSnoozeCmd() *cobra.Command {
	var opts remindSnoozeOptions

	cmd := &cobra.Command{
		Use:   "snooze ID [DURATION|HH:MM]",
		Short: defaultText("remind.snooze.short"),
		Long:  defaultText("remind.snooze.long"),
		Args:  cobra.RangeArgs(1, 2),
		RunE:  func(cmd *cobra.Command, args []string) error { return runRemindSnooze(cmd, args, &opts) },
	}
	cmd.Flags().StringVar(&opts.Date, "date", "", defaultText("remind.flag.date"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("remind.flag.json"))
	return cmd
}

func runRemindSnooze(cmd *cobra.Command, args []string, opts *remindSnoozeOptions) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	id, err := parseReminderID(cmd, args[0])
	if err != nil {
		return err
	}
	now := reminderNow()

	spec := ""
	if len(args) > 1 {
		spec = args[1]
	}
	until, err := snoozeUntil(cmd, spec, now)
	if err != nil {
		return err
	}

	target, err := resolveOccurrence(cmd, svc, id, opts.Date, now)
	if err != nil {
		return err
	}
	occurrence, err := svc.Snooze(cmd.Context(), target.Ref(), until)
	if err != nil {
		return reminderError(cmd, id, err)
	}

	out := cmd.OutOrStdout()
	if opts.JSONOutput {
		return writeJSONTo(out, newReminderOccurrenceJSON(*occurrence, now))
	}
	_, err = fmt.Fprintf(out, text(cmd, "remind.snoozed"), occurrence.Reminder.Title, formatSnoozeTime(cmd, until, now))
	return err
}

// snoozeUntil reads how long to snooze: a duration ("15m"), a time of day ("17:45", the next
// one to come), or nothing for the configured default.
func snoozeUntil(cmd *cobra.Command, spec string, now time.Time) (time.Time, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return now.Add(defaultSnooze(cmd)), nil
	}
	if d, err := time.ParseDuration(spec); err == nil {
		if d <= 0 {
			return time.Time{}, errors.New(text(cmd, "remind.error.invalid_snooze", spec))
		}
		return now.Add(d), nil
	}

	clock, err := getRuntime(cmd).TimeFormatter.ParseTime(spec)
	if err != nil {
		return time.Time{}, errors.New(text(cmd, "remind.error.invalid_snooze", spec))
	}
	until := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, time.Local)
	if !until.After(now) {
		until = until.AddDate(0, 0, 1)
	}
	return until, nil
}

func defaultSnooze(cmd *cobra.Command) time.Duration {
	if rt, ok := runtimeFromCmd(cmd); ok && rt.Config != nil && rt.Config.Reminders.Snooze > 0 {
		return rt.Config.Reminders.Snooze
	}
	return defaultReminderSnooze
}

// formatSnoozeTime is "17:45" today, else "Tue 17 Nov 09:00".
func formatSnoozeTime(cmd *cobra.Command, until, now time.Time) string {
	rt := getRuntime(cmd)
	if sameDay(until, now) {
		return until.Format(rt.TimeFormatter.GetDisplayFormat())
	}
	return formatReminderDue(rt.Localizer, rt.TimeFormatter, until, false, now)
}

type remindStartOptions struct {
	Date       string
	Project    string
	At         string
	JSONOutput bool
}

func newRemindStartCmd() *cobra.Command {
	var opts remindStartOptions

	cmd := &cobra.Command{
		Use:   "start ID",
		Short: defaultText("remind.start.short"),
		Long:  defaultText("remind.start.long"),
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runRemindStart(cmd, args[0], &opts) },
	}
	cmd.Flags().StringVar(&opts.Date, "date", "", defaultText("remind.flag.date"))
	cmd.Flags().StringVarP(&opts.Project, "project", "p", "", defaultText("remind.start.flag.project"))
	cmd.Flags().StringVarP(&opts.At, "time", "t", "", defaultText("start.flag.time"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("remind.start.flag.json"))
	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	return cmd
}

func runRemindStart(cmd *cobra.Command, arg string, opts *remindStartOptions) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	id, err := parseReminderID(cmd, arg)
	if err != nil {
		return err
	}
	rt := getRuntime(cmd)
	now := reminderNow()

	startTime := now
	if opts.At != "" {
		if startTime, err = rt.TimeFormatter.ParseTime(opts.At); err != nil {
			return errors.Wrap(err, "parse time")
		}
	}

	target, err := resolveOccurrence(cmd, svc, id, opts.Date, now)
	if err != nil {
		return err
	}
	activity, err := svc.Track(cmd.Context(), target.Ref(), models.TrackReminderRequest{StartTime: startTime, Project: opts.Project})
	if err != nil {
		return reminderError(cmd, id, err)
	}

	ensureTrayRunning(cmd)

	out := cmd.OutOrStdout()
	if opts.JSONOutput {
		return writeJSONTo(out, activity)
	}
	if _, err = fmt.Fprintf(out, text(cmd, "message.activity_started"),
		activity.Project, activity.Description, activity.StartTime.Format(rt.TimeFormatter.GetDisplayFormat())); err != nil {
		return err
	}
	when := formatReminderDue(rt.Localizer, rt.TimeFormatter, target.Due, target.Reminder.AllDay, now)
	_, err = fmt.Fprintf(out, text(cmd, "remind.started"), id, when, nextOccurrenceSuffix(cmd, svc, id, now))
	return err
}

type remindRemoveOptions struct {
	Date        string
	SkipConfirm bool
	JSONOutput  bool
}

func newRemindRemoveCmd() *cobra.Command {
	var opts remindRemoveOptions

	cmd := &cobra.Command{
		Use:     "remove ID",
		Aliases: []string{"rm"},
		Short:   defaultText("remind.remove.short"),
		Long:    defaultText("remind.remove.long"),
		Args:    cobra.ExactArgs(1),
		RunE:    func(cmd *cobra.Command, args []string) error { return runRemindRemove(cmd, args[0], &opts) },
	}
	cmd.Flags().StringVar(&opts.Date, "date", "", defaultText("remind.remove.flag.date"))
	cmd.Flags().BoolVarP(&opts.SkipConfirm, "yes", "y", false, defaultText("remove.flag.yes"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("remind.flag.json"))
	return cmd
}

func runRemindRemove(cmd *cobra.Command, arg string, opts *remindRemoveOptions) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	id, err := parseReminderID(cmd, arg)
	if err != nil {
		return err
	}
	reminder, err := svc.Get(cmd.Context(), id)
	if err != nil {
		return reminderError(cmd, id, err)
	}
	rt := getRuntime(cmd)
	now := reminderNow()
	out := cmd.OutOrStdout()

	if opts.Date != "" {
		occurrence, resolveErr := resolveOccurrence(cmd, svc, id, opts.Date, now)
		if resolveErr != nil {
			return resolveErr
		}
		if err = svc.Skip(cmd.Context(), occurrence.Ref()); err != nil {
			return reminderError(cmd, id, err)
		}
		if opts.JSONOutput {
			return writeJSONTo(out, newReminderOccurrenceJSON(*occurrence, now))
		}
		when := formatReminderDue(rt.Localizer, rt.TimeFormatter, occurrence.Due, reminder.AllDay, now)
		_, err = fmt.Fprintf(out, text(cmd, "remind.occurrence_removed"), reminder.Title, when)
		return err
	}

	if !opts.SkipConfirm {
		prompt := text(
			cmd,
			"remind.remove.confirm",
			id,
			reminderLabel(*reminder),
			formatReminderSchedule(rt.Localizer, rt.TimeFormatter, *reminder, now),
		)
		if confirmed, confirmErr := confirmPrompt(cmd, prompt); confirmErr != nil || !confirmed {
			return confirmErr
		}
	}

	if err = svc.Remove(cmd.Context(), id); err != nil {
		return reminderError(cmd, id, err)
	}
	if opts.JSONOutput {
		return writeJSONTo(out, reminder)
	}
	fmt.Fprintln(out, text(cmd, "remind.removed"))
	return nil
}

// confirmPrompt asks a yes/no question on the command's streams; anything but y/yes is no.
func confirmPrompt(cmd *cobra.Command, prompt string) (bool, error) {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprint(out, prompt); err != nil {
		return false, errors.Wrap(err, "write confirmation prompt")
	}

	return readConfirmation(out, cmd.InOrStdin())
}
