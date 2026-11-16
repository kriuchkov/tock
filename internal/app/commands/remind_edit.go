package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// reminderFlags are the reminder fields shared by `remind add` and `remind edit`.
type reminderFlags struct {
	Title      string
	Project    string
	On         string
	At         string
	AllDay     bool
	Every      string
	Until      string
	Count      int
	Notify     string
	Tags       []string
	Note       string
	JSONOutput bool
}

func bindReminderFlags(cmd *cobra.Command, flags *reminderFlags, edit bool) {
	if edit {
		cmd.Flags().StringVar(&flags.Title, "title", "", defaultText("remind.flag.title"))
		cmd.Flags().BoolVar(&flags.AllDay, "all-day", false, defaultText("remind.flag.all_day"))
	}
	cmd.Flags().StringVarP(&flags.Project, "project", "p", "", defaultText("remind.flag.project"))
	cmd.Flags().StringVar(&flags.On, "on", "", defaultText("remind.flag.on"))
	cmd.Flags().StringVar(&flags.At, "at", "", defaultText("remind.flag.at"))
	cmd.Flags().StringVar(&flags.Every, "every", "", defaultText("remind.flag.every"))
	cmd.Flags().StringVar(&flags.Until, "until", "", defaultText("remind.flag.until"))
	cmd.Flags().IntVar(&flags.Count, "count", 0, defaultText("remind.flag.count"))
	cmd.Flags().StringVar(&flags.Notify, "notify", "", defaultText("remind.flag.notify"))
	cmd.Flags().StringSliceVar(&flags.Tags, "tag", nil, defaultText("remind.flag.tag"))
	cmd.Flags().StringVar(&flags.Note, "note", "", defaultText("remind.flag.note"))
	cmd.Flags().BoolVar(&flags.JSONOutput, "json", false, defaultText("remind.flag.json"))
	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
}

func newRemindAddCmd() *cobra.Command {
	var flags reminderFlags

	cmd := &cobra.Command{
		Use:   "add [title]",
		Short: defaultText("remind.add.short"),
		Long:  defaultText("remind.add.long"),
		RunE:  func(cmd *cobra.Command, args []string) error { return runRemindAdd(cmd, args, &flags) },
	}
	bindReminderFlags(cmd, &flags, false)
	return cmd
}

func runRemindAdd(cmd *cobra.Command, args []string, flags *reminderFlags) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	now := reminderNow()

	req, err := buildAddReminderRequest(cmd, strings.TrimSpace(strings.Join(args, " ")), flags, now)
	if err != nil {
		return err
	}

	if req.Title == "" {
		if flags.JSONOutput {
			return errors.New(text(cmd, "remind.error.title_required"))
		}
		return runReminderForm(cmd, newReminderFormFromRequest(getRuntime(cmd).TimeFormatter, req))
	}

	reminder, err := svc.Add(cmd.Context(), req)
	if err != nil {
		return reminderError(cmd, 0, err)
	}

	out := cmd.OutOrStdout()
	if flags.JSONOutput {
		return writeJSONTo(out, reminder)
	}
	rt := getRuntime(cmd)
	_, err = fmt.Fprintf(out, text(cmd, "remind.added"),
		reminder.ID, reminderLabel(*reminder), formatReminderSchedule(rt.Localizer, rt.TimeFormatter, *reminder, now))
	return err
}

func buildAddReminderRequest(cmd *cobra.Command, title string, flags *reminderFlags, now time.Time) (models.AddReminderRequest, error) {
	day, err := timeutil.ParseDay(flags.On, now)
	if err != nil {
		return models.AddReminderRequest{}, errors.Wrap(err, "parse --on")
	}

	due, allDay := day, true
	if flags.At != "" {
		if due, err = atClock(cmd, day, flags.At); err != nil {
			return models.AddReminderRequest{}, err
		}
		allDay = false
	}

	repeat, err := models.ParseRepeat(flags.Every)
	if err != nil {
		return models.AddReminderRequest{}, errors.Wrap(err, "parse --every")
	}
	if repeat, err = applyRepeatBounds(cmd, repeat, flags.Until, flags.Count, now); err != nil {
		return models.AddReminderRequest{}, err
	}

	notify, err := parseNotify(flags.Notify)
	if err != nil {
		return models.AddReminderRequest{}, err
	}

	return models.AddReminderRequest{
		Title:   title,
		Project: flags.Project,
		Due:     due,
		AllDay:  allDay,
		Repeat:  repeat,
		Notify:  notify,
		Tags:    flags.Tags,
		Notes:   flags.Note,
	}, nil
}

// atClock returns day at the time of day given as HH:MM (or 3:04 PM in 12-hour mode).
func atClock(cmd *cobra.Command, day time.Time, input string) (time.Time, error) {
	clock, err := getRuntime(cmd).TimeFormatter.ParseTime(input)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "parse --at")
	}
	return time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, time.Local), nil
}

// applyRepeatBounds sets when a repeat ends; repeatSpecNone as until clears it.
func applyRepeatBounds(cmd *cobra.Command, repeat models.Repeat, until string, count int, now time.Time) (models.Repeat, error) {
	until = strings.TrimSpace(until)
	if repeat.Freq == models.RepeatNone && ((until != "" && until != repeatSpecNone) || count > 0) {
		return models.Repeat{}, errors.New(text(cmd, "remind.error.bounds_need_repeat"))
	}

	switch until {
	case "":
	case repeatSpecNone:
		repeat.Until = nil
	default:
		day, err := timeutil.ParseDay(until, now)
		if err != nil {
			return models.Repeat{}, errors.Wrap(err, "parse --until")
		}
		repeat.Until = &day
	}
	if count < 0 {
		return models.Repeat{}, errors.New(text(cmd, "remind.error.negative_count"))
	}
	repeat.Count = count
	return repeat, nil
}

func parseNotify(input string) (time.Duration, error) {
	input = strings.TrimSpace(input)
	if input == "" || input == "0" {
		return 0, nil
	}
	notify, err := time.ParseDuration(input)
	if err != nil || notify < 0 {
		return 0, errors.Errorf("invalid --notify %q (use a duration like 10m or 1h)", input)
	}
	return notify, nil
}

func newRemindEditCmd() *cobra.Command {
	var flags reminderFlags

	cmd := &cobra.Command{
		Use:   "edit ID",
		Short: defaultText("remind.edit.short"),
		Long:  defaultText("remind.edit.long"),
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runRemindEdit(cmd, args[0], &flags) },
	}
	bindReminderFlags(cmd, &flags, true)
	return cmd
}

func runRemindEdit(cmd *cobra.Command, arg string, flags *reminderFlags) error {
	svc, err := getReminderService(cmd)
	if err != nil {
		return err
	}
	id, err := parseReminderID(cmd, arg)
	if err != nil {
		return err
	}
	current, err := svc.Get(cmd.Context(), id)
	if err != nil {
		return reminderError(cmd, id, err)
	}
	now := reminderNow()

	req, err := buildUpdateReminderRequest(cmd, *current, flags, now)
	if err != nil {
		return err
	}
	if req.IsEmpty() {
		if flags.JSONOutput {
			return errors.New(text(cmd, "remind.error.json_needs_flags"))
		}
		return runReminderForm(cmd, newReminderFormFromReminder(getRuntime(cmd).TimeFormatter, *current))
	}

	updated, err := svc.Update(cmd.Context(), id, req)
	if err != nil {
		return reminderError(cmd, id, err)
	}

	out := cmd.OutOrStdout()
	if flags.JSONOutput {
		return writeJSONTo(out, updated)
	}
	rt := getRuntime(cmd)
	_, err = fmt.Fprintf(out, text(cmd, "remind.updated"),
		updated.ID, reminderLabel(*updated), formatReminderSchedule(rt.Localizer, rt.TimeFormatter, *updated, now))
	return err
}

func buildUpdateReminderRequest(
	cmd *cobra.Command,
	current models.Reminder,
	flags *reminderFlags,
	now time.Time,
) (models.UpdateReminderRequest, error) {
	changed := cmd.Flags().Changed
	var req models.UpdateReminderRequest

	if changed("title") {
		req.Title = &flags.Title
	}
	if changed("project") {
		req.Project = &flags.Project
	}
	if changed("note") {
		req.Notes = &flags.Note
	}
	if changed("tag") {
		req.Tags = &flags.Tags
	}
	if changed("notify") {
		notify, err := parseNotify(flags.Notify)
		if err != nil {
			return req, err
		}
		req.Notify = &notify
	}

	if changed("on") || changed("at") || changed("all-day") {
		due, allDay, err := editedDue(cmd, current, flags, now)
		if err != nil {
			return req, err
		}
		req.Due, req.AllDay = &due, &allDay
	}

	if changed("every") || changed("until") || changed("count") {
		repeat, err := editedRepeat(cmd, current.Repeat, flags, now)
		if err != nil {
			return req, err
		}
		req.Repeat = &repeat
	}
	return req, nil
}

// editedDue moves the reminder to --on keeping its time of day, sets --at, or drops the time with --all-day.
func editedDue(cmd *cobra.Command, current models.Reminder, flags *reminderFlags, now time.Time) (time.Time, bool, error) {
	changed := cmd.Flags().Changed
	due := current.Due.In(time.Local)
	allDay := current.AllDay

	if changed("on") {
		day, err := timeutil.ParseDay(flags.On, now)
		if err != nil {
			return time.Time{}, false, errors.Wrap(err, "parse --on")
		}
		due = time.Date(day.Year(), day.Month(), day.Day(), due.Hour(), due.Minute(), 0, 0, time.Local)
	}
	if changed("at") {
		var err error
		if due, err = atClock(cmd, due, flags.At); err != nil {
			return time.Time{}, false, err
		}
		allDay = false
	}
	if changed("all-day") && flags.AllDay {
		allDay = true
		due = time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.Local)
	}
	return due, allDay, nil
}

// editedRepeat replaces the repeat rule with --every and its bounds with --until/--count,
// keeping whatever is not given.
func editedRepeat(cmd *cobra.Command, current models.Repeat, flags *reminderFlags, now time.Time) (models.Repeat, error) {
	changed := cmd.Flags().Changed
	repeat := current

	if changed("every") {
		parsed, err := models.ParseRepeat(flags.Every)
		if err != nil {
			return models.Repeat{}, errors.Wrap(err, "parse --every")
		}
		parsed.Until, parsed.Count = current.Until, current.Count
		repeat = parsed
	}
	if repeat.Freq == models.RepeatNone {
		repeat.Until, repeat.Count = nil, 0
	}

	until := ""
	if changed("until") {
		until = flags.Until
	}
	count := repeat.Count
	if changed("count") {
		count = flags.Count
	}
	return applyRepeatBounds(cmd, repeat, until, count, now)
}
