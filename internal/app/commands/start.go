package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type startOptions struct {
	Description string
	Project     string
	At          string
	Notes       string
	Tags        []string
	JSONOutput  bool
}

func NewStartCmd() *cobra.Command {
	var opts startOptions

	cmd := &cobra.Command{
		Use:   "start [project] [description] [notes] [tags]",
		Short: "Start a new activity",
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStartCmd(cmd, args, &opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Description, "description", "d", "", defaultText("start.flag.description"))
	cmd.Flags().StringVarP(&opts.Project, "project", "p", "", defaultText("start.flag.project"))
	cmd.Flags().StringVarP(&opts.At, "time", "t", "", defaultText("start.flag.time"))
	cmd.Flags().StringVar(&opts.Notes, "note", "", defaultText("start.flag.note"))
	cmd.Flags().StringSliceVar(&opts.Tags, "tag", nil, defaultText("start.flag.tag"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("start.flag.json"))

	_ = cmd.RegisterFlagCompletionFunc("description", descriptionRegisterFlagCompletion)
	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	return cmd
}

func runStartCmd(cmd *cobra.Command, args []string, opts *startOptions) error {
	defer runUpdateCheck(cmd)

	rt := getRuntime(cmd)
	service := rt.ActivityService
	tf := rt.TimeFormatter
	out := cmd.OutOrStdout()

	project := opts.Project
	description := opts.Description
	notes := opts.Notes
	tags := append([]string(nil), opts.Tags...)

	startTime := time.Now()
	if opts.At != "" {
		var err error
		startTime, err = tf.ParseTime(opts.At)
		if err != nil {
			return errors.Wrap(err, "parse time")
		}
	}

	if project == "" && len(args) > 0 {
		project = args[0]
	}
	if description == "" && len(args) > 1 {
		description = args[1]
	}
	if notes == "" && len(args) > 2 {
		notes = args[2]
	}
	if len(tags) == 0 && len(args) > 3 {
		for t := range strings.SplitSeq(args[3], ",") {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				tags = append(tags, trimmed)
			}
		}
	}

	if project == "" || description == "" {
		activities, _ := service.List(cmd.Context(), models.ActivityFilter{})
		theme := GetTheme(rt.Config.Theme)

		var err error
		project, description, err = SelectActivityMetadata(activities, project, description, theme)
		if err != nil {
			return errors.Wrap(err, "select activity metadata")
		}
		if project == "" {
			return errors.New(text(cmd, "validation.project_required"))
		}
		if description == "" {
			return errors.New(text(cmd, "validation.description_required"))
		}
	}

	activity, err := service.Start(cmd.Context(), models.StartActivityRequest{
		Description: description,
		Project:     project,
		StartTime:   startTime,
		Notes:       notes,
		Tags:        tags,
	})
	if err != nil {
		return errors.Wrap(err, "start activity")
	}

	if opts.JSONOutput {
		return writeJSONTo(out, activity)
	}

	_, err = fmt.Fprintf(out, text(cmd, "message.activity_started"),
		activity.Project,
		activity.Description,
		activity.StartTime.Format(tf.GetDisplayFormat()),
	)
	return err
}
