package cli

import (
	"fmt"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/kriuchkov/tock/internal/core/dto"
	ce "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/extra"
)

type addOptions struct {
	Description string
	Project     string
	StartStr    string
	EndStr      string
	DurationStr string
	Notes       string
	Tags        []string
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

	cmd.Flags().StringVarP(&opts.Description, "description", "d", "", "Activity description")
	cmd.Flags().StringVarP(&opts.Project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&opts.StartStr, "start", "s", "", "Start time (HH:MM)")
	cmd.Flags().StringVarP(&opts.EndStr, "end", "e", "", "End time (HH:MM)")
	cmd.Flags().StringVar(&opts.DurationStr, "duration", "", "Duration (e.g. 1h, 30m)")
	cmd.Flags().StringVar(&opts.Notes, "note", "", "Activity notes")
	cmd.Flags().StringSliceVar(&opts.Tags, "tag", nil, "Activity tags")

	_ = cmd.RegisterFlagCompletionFunc("description", descriptionRegisterFlagCompletion)
	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	return cmd
}

func runAdd(cmd *cobra.Command, opts *addOptions) error {
	service := getService(cmd)
	theme := GetTheme(getConfig(cmd).Theme)

	if opts.Project == "" || opts.Description == "" {
		activities, _ := service.List(cmd.Context(), dto.ActivityFilter{})

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

	endStr, durationStr, err := resolveEndTimeOrDuration(opts.EndStr, opts.DurationStr, theme)
	if err != nil {
		return err
	}

	tf := getTimeFormatter(cmd)
	startTime, err := tf.ParseTimeWithDate(startStr)
	if err != nil {
		return errors.Wrap(err, "parse start time")
	}

	endTime, err := extra.CalculateEndTime(tf, startTime, endStr, durationStr)
	if err != nil {
		return err
	}

	req := dto.AddActivityRequest{
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

	fmt.Printf("Added activity: %s | %s (%s - %s)\n",
		activity.Project,
		activity.Description,
		activity.StartTime.Format(tf.GetDisplayFormat()),
		activity.EndTime.Format(tf.GetDisplayFormat()),
	)
	return nil
}

func resolveStartTime(startStr string, theme Theme) (string, error) {
	if startStr != "" {
		return startStr, nil
	}

	nowStr := time.Now().Format("15:04")
	const customOption = "➕ Other Time"
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

	sel, err := RunInteractiveList(options, "Select Start Time", theme)
	if err != nil {
		return "", errors.Wrap(err, "select start time")
	}

	if sel == customOption {
		return RunInteractiveInput("Start Time (HH:MM)", "HH:MM", theme)
	}
	return sel, nil
}

func resolveEndTimeOrDuration(endStr, durationStr string, theme Theme) (string, string, error) {
	if endStr != "" || durationStr != "" {
		return endStr, durationStr, nil
	}

	input, err := RunInteractiveInput("Duration (e.g. 1h, 30m) or End Time", "1h", theme)
	if err != nil {
		return "", "", errors.Wrap(err, "input duration or end time")
	}

	if len(input) == 5 && input[2] == ':' {
		return input, "", nil
	}
	return "", input, nil
}
