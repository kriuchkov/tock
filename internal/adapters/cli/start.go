package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/dto"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

//nolint:gocognit,funlen // Include all logic in one function
func NewStartCmd() *cobra.Command {
	var description string
	var project string
	var at string
	var notes string
	var tags []string

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
			service := getService(cmd)
			tf := getTimeFormatter(cmd)
			startTime := time.Now()

			if at != "" {
				var err error
				startTime, err = tf.ParseTime(at)
				if err != nil {
					return errors.Wrap(err, "parse time")
				}
			}

			// Support positional arguments: tock start Project Description [Notes] [Tags]
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

			// Interactive mode if project or description is missing
			if project == "" || description == "" {
				activities, _ := service.List(cmd.Context(), dto.ActivityFilter{})
				theme := GetTheme(getConfig(cmd).Theme)

				var err error
				project, description, err = SelectActivityMetadata(activities, project, description, theme)
				if err != nil {
					return errors.Wrap(err, "select activity metadata")
				}

				if project == "" {
					return errors.New("project name is required")
				}

				if description == "" {
					return errors.New("description is required")
				}
			}

			req := dto.StartActivityRequest{
				Description: description,
				Project:     project,
				StartTime:   startTime,
				Notes:       notes,
				Tags:        tags,
			}

			activity, err := service.Start(cmd.Context(), req)
			if err != nil {
				return errors.Wrap(err, "start activity")
			}

			fmt.Printf(
				"Started activity: %s | %s at %s\n",
				activity.Project,
				activity.Description,
				activity.StartTime.Format(tf.GetDisplayFormat()),
			)
			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "Activity description")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&at, "time", "t", "", "Start time (HH:MM)")
	cmd.Flags().StringVar(&notes, "note", "", "Activity notes")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Activity tags")

	_ = cmd.RegisterFlagCompletionFunc("description", descriptionRegisterFlagCompletion)
	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	return cmd
}
