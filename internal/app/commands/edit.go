package commands

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	flagProject     = "project"
	flagDescription = "description"
	flagDay         = "day"
	flagStart       = "start"
	flagEnd         = "end"
	flagDuration    = "duration"
	flagNote        = "note"
	flagTag         = "tag"
	flagClearEnd    = "clear-end"
)

type editOptions struct {
	Description string
	Project     string
	DayStr      string
	StartStr    string
	EndStr      string
	DurationStr string
	Notes       string
	Tags        []string
	ClearEnd    bool
	JSONOutput  bool
}

func NewEditCmd() *cobra.Command {
	var opts editOptions

	cmd := &cobra.Command{
		Use:     "edit [DATE-INDEX]",
		Aliases: []string{"change"},
		Short:   defaultText("edit.short"),
		Long:    defaultText("edit.long"),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEditCmd(cmd, args, &opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Description, flagDescription, "d", "", defaultText("edit.flag.description"))
	cmd.Flags().StringVarP(&opts.Project, flagProject, "p", "", defaultText("edit.flag.project"))
	cmd.Flags().StringVar(&opts.DayStr, flagDay, "", defaultText("edit.flag.day"))
	cmd.Flags().StringVarP(&opts.StartStr, flagStart, "s", "", defaultText("edit.flag.start"))
	cmd.Flags().StringVarP(&opts.EndStr, flagEnd, "e", "", defaultText("edit.flag.end"))
	cmd.Flags().StringVar(&opts.DurationStr, flagDuration, "", defaultText("edit.flag.duration"))
	cmd.Flags().StringVar(&opts.Notes, flagNote, "", defaultText("edit.flag.note"))
	cmd.Flags().StringSliceVar(&opts.Tags, flagTag, nil, defaultText("edit.flag.tag"))
	cmd.Flags().BoolVar(&opts.ClearEnd, flagClearEnd, false, defaultText("edit.flag.clear_end"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("edit.flag.json"))

	_ = cmd.RegisterFlagCompletionFunc(flagDescription, descriptionRegisterFlagCompletion)
	_ = cmd.RegisterFlagCompletionFunc(flagProject, projectRegisterFlagCompletion)
	return cmd
}

func runEditCmd(cmd *cobra.Command, args []string, opts *editOptions) error {
	ctx := cmd.Context()
	rt := getRuntime(cmd)
	svc := rt.ActivityService

	hasFlags := editFlagsChanged(cmd)
	if !hasFlags && opts.JSONOutput {
		return errors.New(defaultText("edit.error.json_needs_flags"))
	}

	if len(args) == 0 && !hasFlags {
		return runEditPicker(cmd)
	}

	activity, err := resolveEditTarget(ctx, cmd, args)
	if err != nil {
		return err
	}

	if !hasFlags {
		return runEditForm(cmd, activity)
	}

	req, err := buildEditRequest(cmd, rt.TimeFormatter, activity, opts)
	if err != nil {
		return err
	}

	updated, err := svc.Update(ctx, activity, req)
	if err != nil {
		return errors.Wrap(err, "update activity")
	}

	return printEditedActivity(cmd, *updated, opts.JSONOutput)
}

func resolveEditTarget(ctx context.Context, cmd *cobra.Command, args []string) (models.Activity, error) {
	svc := getRuntime(cmd).ActivityService
	if len(args) == 0 {
		return findLastActivity(ctx, svc)
	}
	return findActivityByIndex(ctx, svc, args[0])
}

func printEditedActivity(cmd *cobra.Command, activity models.Activity, jsonOutput bool) error {
	out := cmd.OutOrStdout()
	if jsonOutput {
		return writeJSONTo(out, activity)
	}

	tf := getRuntime(cmd).TimeFormatter
	endStr := text(cmd, "edit.running")
	if activity.EndTime != nil {
		endStr = activity.EndTime.Format(tf.GetDisplayFormat())
	}

	_, err := fmt.Fprintf(out, text(cmd, "edit.done"),
		activity.Project,
		activity.Description,
		activity.StartTime.Format(tf.GetDisplayFormatWithDate()),
		endStr,
	)
	return err
}

func editFlagsChanged(cmd *cobra.Command) bool {
	return slices.ContainsFunc([]string{
		flagProject, flagDescription, flagDay, flagStart, flagEnd, flagDuration, flagNote, flagTag, flagClearEnd,
	}, cmd.Flags().Changed)
}

// buildEditRequest translates the changed flags into a partial update request.
func buildEditRequest(
	cmd *cobra.Command,
	tf *timeutil.Formatter,
	activity models.Activity,
	opts *editOptions,
) (models.UpdateActivityRequest, error) {
	var req models.UpdateActivityRequest

	if opts.ClearEnd && (cmd.Flags().Changed(flagEnd) || cmd.Flags().Changed(flagDuration)) {
		return req, errors.New(defaultText("edit.error.clear_end_conflict"))
	}

	if cmd.Flags().Changed(flagProject) {
		req.Project = &opts.Project
	}
	if cmd.Flags().Changed(flagDescription) {
		req.Description = &opts.Description
	}
	if cmd.Flags().Changed(flagNote) {
		req.Notes = &opts.Notes
	}
	if cmd.Flags().Changed(flagTag) {
		tags := opts.Tags
		req.Tags = &tags
	}
	if opts.ClearEnd {
		req.ClearEndTime = true
	}

	if err := applyEditTimes(cmd, tf, activity, opts, &req); err != nil {
		return models.UpdateActivityRequest{}, err
	}
	return req, nil
}

func applyEditTimes(
	cmd *cobra.Command,
	tf *timeutil.Formatter,
	activity models.Activity,
	opts *editOptions,
	req *models.UpdateActivityRequest,
) error {
	if cmd.Flags().Changed(flagStart) {
		startInput, err := normalizeAddDateTimeInput(tf, opts.DayStr, opts.StartStr)
		if err != nil {
			return err
		}
		startTime, parseErr := parseEditTime(tf, activity.StartTime, startInput)
		if parseErr != nil {
			return errors.Wrap(parseErr, "parse start time")
		}
		req.StartTime = &startTime
	}

	if cmd.Flags().Changed(flagDay) {
		if err := applyEditDay(cmd, activity, opts, req); err != nil {
			return err
		}
	}

	return applyEditEnd(cmd, tf, activity, opts, req)
}

// applyEditDay moves the activity to another day. Combined with an explicit
// --start the start time is already absolute, so only the end time still has to
// follow; otherwise the model moves the whole entry.
func applyEditDay(
	cmd *cobra.Command,
	activity models.Activity,
	opts *editOptions,
	req *models.UpdateActivityRequest,
) error {
	day, err := time.ParseInLocation(time.DateOnly, opts.DayStr, time.Local)
	if err != nil {
		return errors.Wrap(err, "parse day")
	}

	if req.StartTime == nil {
		req.MoveToDay = &day
		return nil
	}

	keepsOwnEnd := cmd.Flags().Changed(flagEnd) || cmd.Flags().Changed(flagDuration) || opts.ClearEnd
	if keepsOwnEnd || activity.EndTime == nil {
		return nil
	}

	endTime := activity.EndTime.Add(req.StartTime.Sub(activity.StartTime))
	req.EndTime = &endTime
	return nil
}

func applyEditEnd(
	cmd *cobra.Command,
	tf *timeutil.Formatter,
	activity models.Activity,
	opts *editOptions,
	req *models.UpdateActivityRequest,
) error {
	startTime := activity.StartTime
	if req.StartTime != nil {
		startTime = *req.StartTime
	}
	if req.MoveToDay != nil {
		startTime = models.MoveActivityToDay(activity, *req.MoveToDay, false).StartTime
	}

	if cmd.Flags().Changed(flagEnd) {
		endInput, err := normalizeAddDateTimeInput(tf, opts.DayStr, opts.EndStr)
		if err != nil {
			return err
		}
		endTime, parseErr := parseEditTime(tf, startTime, endInput)
		if parseErr != nil {
			return errors.Wrap(parseErr, "parse end time")
		}
		req.EndTime = &endTime
		return nil
	}

	if cmd.Flags().Changed(flagDuration) {
		duration, err := time.ParseDuration(opts.DurationStr)
		if err != nil {
			return errors.Wrap(err, "parse duration")
		}
		endTime := startTime.Add(duration)
		req.EndTime = &endTime
	}
	return nil
}

// parseEditTime parses a time input, defaulting a time-only value to the day of
// reference.
func parseEditTime(tf *timeutil.Formatter, reference time.Time, input string) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return time.Time{}, errors.New(defaultText("edit.error.empty_time"))
	}

	if parsed, err := tf.ParseTime(input); err == nil {
		return time.Date(
			reference.Year(), reference.Month(), reference.Day(),
			parsed.Hour(), parsed.Minute(), parsed.Second(), 0,
			time.Local,
		), nil
	}

	return tf.ParseTimeWithDate(input)
}
