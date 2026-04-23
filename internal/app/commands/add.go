package commands

import (
	"fmt"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	ce "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
)

type addOptions struct {
	Description string
	Project     string
	DayStr      string
	StartStr    string
	EndStr      string
	DurationStr string
	Notes       string
	Tags        []string
	JSONOutput  bool
}

func NewAddCmd() *cobra.Command {
	var opts addOptions

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a completed activity",
		ValidArgsFunction: func(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
			var completions []string
			cmd.Flags().VisitAll(func(f *pflag.Flag) {
				completions = append(completions, fmt.Sprintf("--%s\t%s", f.Name, f.Usage))
				if f.Shorthand != "" {
					completions = append(completions, fmt.Sprintf("-%s\t%s", f.Shorthand, f.Usage))
				}
			})
			return completions, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := runAdd(cmd, &opts)
			if errors.Is(err, ce.ErrCancelled) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().StringVarP(&opts.Description, "description", "d", "", defaultText("add.flag.description"))
	cmd.Flags().StringVarP(&opts.Project, "project", "p", "", defaultText("add.flag.project"))
	cmd.Flags().StringVar(&opts.DayStr, "day", "", defaultText("add.flag.day"))
	cmd.Flags().StringVarP(&opts.StartStr, "start", "s", "", defaultText("add.flag.start"))
	cmd.Flags().StringVarP(&opts.EndStr, "end", "e", "", defaultText("add.flag.end"))
	cmd.Flags().StringVar(&opts.DurationStr, "duration", "", defaultText("add.flag.duration"))
	cmd.Flags().StringVar(&opts.Notes, "note", "", defaultText("add.flag.note"))
	cmd.Flags().StringSliceVar(&opts.Tags, "tag", nil, defaultText("add.flag.tag"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("add.flag.json"))

	_ = cmd.RegisterFlagCompletionFunc("description", descriptionRegisterFlagCompletion)
	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	return cmd
}

func runAdd(cmd *cobra.Command, opts *addOptions) error {
	defer runUpdateCheck(cmd)

	rt := getRuntime(cmd)
	service := rt.ActivityService
	theme := GetTheme(rt.Config.Theme)
	out := cmd.OutOrStdout()
	tf := rt.TimeFormatter

	if opts.Project == "" || opts.Description == "" {
		activities, _ := service.List(cmd.Context(), models.ActivityFilter{})

		var err error
		opts.Project, opts.Description, err = SelectActivityMetadata(activities, opts.Project, opts.Description, theme)
		if err != nil {
			return errors.Wrap(err, "select activity metadata")
		}
	}

	startStr, err := resolveStartTime(opts.StartStr, theme)
	if err != nil {
		return err
	}
	startStr, err = normalizeAddDateTimeInput(tf, opts.DayStr, startStr)
	if err != nil {
		return err
	}

	endStr, durationStr, err := resolveEndTimeOrDuration(opts.EndStr, opts.DurationStr, theme)
	if err != nil {
		return err
	}
	endStr, err = normalizeAddDateTimeInput(tf, opts.DayStr, endStr)
	if err != nil {
		return err
	}

	startTime, err := tf.ParseTimeWithDate(startStr)
	if err != nil {
		return errors.Wrap(err, "parse start time")
	}

	endTime, err := calculateEndTime(tf, startTime, endStr, durationStr)
	if err != nil {
		return err
	}

	req := models.AddActivityRequest{
		Description: opts.Description,
		Project:     opts.Project,
		StartTime:   startTime,
		EndTime:     endTime,
		Notes:       opts.Notes,
		Tags:        opts.Tags,
	}

	activity, err := service.Add(cmd.Context(), req)
	if err != nil {
		return errors.Wrap(err, "add activity")
	}

	if opts.JSONOutput {
		return writeJSONTo(out, activity)
	}

	_, err = fmt.Fprintf(out, text(cmd, "message.activity_added"),
		activity.Project,
		activity.Description,
		activity.StartTime.Format(tf.GetDisplayFormat()),
		activity.EndTime.Format(tf.GetDisplayFormat()),
	)
	return err
}

func resolveStartTime(startStr string, theme Theme) (string, error) {
	if startStr != "" {
		return startStr, nil
	}

	nowStr := time.Now().Format("15:04")
	customOption := defaultText("add.prompt.custom_time")
	options := []string{
		nowStr,
		customOption,
		"08:00",
		"09:00",
		"10:00",
		"11:00",
		"12:00",
		"13:00",
		"14:00",
		"15:00",
		"16:00",
		"17:00",
		"18:00",
		"19:00",
		"20:00",
		"21:00",
		"22:00",
		"23:00",
	}

	sel, err := RunInteractiveList(options, defaultText("add.prompt.select_start_time"), theme)
	if err != nil {
		return "", errors.Wrap(err, "select start time")
	}

	if sel == customOption {
		return RunInteractiveInput(defaultText("add.prompt.start_time"), "HH:MM", theme)
	}
	return sel, nil
}

func resolveEndTimeOrDuration(endStr, durationStr string, theme Theme) (string, string, error) {
	if endStr != "" || durationStr != "" {
		return endStr, durationStr, nil
	}

	input, err := RunInteractiveInput(defaultText("add.prompt.duration_or_end"), "1h", theme)
	if err != nil {
		return "", "", errors.Wrap(err, "input duration or end time")
	}

	if len(input) == 5 && input[2] == ':' {
		return input, "", nil
	}
	return "", input, nil
}
