package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/dto"
)

const (
	defaultRecentActivitiesForContinuation = 10
)

//nolint:funlen // Include all logic in one function
func NewContinueCmd() *cobra.Command {
	var description string
	var project string
	var at string
	var notes string
	var tags []string

	cmd := &cobra.Command{
		Use:     "continue [NUMBER]",
		Aliases: []string{"c"},
		Short:   "Continues a previous activity",
		Args:    cobra.MaximumNArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}

			svc, err := getServiceForCompletion(cmd)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			activities, err := svc.GetRecent(cmd.Context(), defaultRecentActivitiesForContinuation)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}

			var suggestions []string
			for i, a := range activities {
				suggestions = append(suggestions, fmt.Sprintf("%d\t%s | %s", i, a.Project, a.Description))
			}

			return suggestions, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		},

		RunE: func(cmd *cobra.Command, args []string) error {
			defer runUpdateCheck(cmd)

			service := getService(cmd)
			tf := getTimeFormatter(cmd)
			ctx := context.Background()

			number := 0
			if len(args) > 0 {
				var err error
				number, err = strconv.Atoi(args[0])
				if err != nil {
					return errors.Wrap(err, "invalid number")
				}
			}

			activities, err := service.GetRecent(ctx, number+1)
			if err != nil {
				return errors.Wrap(err, "get recent activities")
			}

			if number >= len(activities) {
				return errors.Errorf("activity number %d not found (only %d recent activities available)", number, len(activities))
			}

			activityToContinue := activities[number]

			newDescription := activityToContinue.Description
			if description != "" {
				newDescription = description
			}

			newProject := activityToContinue.Project
			if project != "" {
				newProject = project
			}

			startTime := time.Now()
			if at != "" {
				var parseErr error
				startTime, parseErr = tf.ParseTime(at)
				if parseErr != nil {
					return errors.Wrap(parseErr, "parse time")
				}
			}

			req := dto.StartActivityRequest{
				Description: newDescription,
				Project:     newProject,
				StartTime:   startTime,
				Notes:       notes,
				Tags:        tags,
			}

			startedActivity, err := service.Start(ctx, req)
			if err != nil {
				return errors.Wrap(err, "start activity")
			}

			fmt.Printf(
				"Started activity: %s | %s at %s\n",
				startedActivity.Project,
				startedActivity.Description,
				startedActivity.StartTime.Format(tf.GetDisplayFormat()),
			)
			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "the description of the new activity")
	cmd.Flags().StringVarP(&project, "project", "p", "", "the project to which the new activity belongs")
	cmd.Flags().StringVarP(&at, "time", "t", "", "the time for changing the activity status (HH:MM)")
	cmd.Flags().StringVar(&notes, "note", "", "Activity notes")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Activity tags")
	return cmd
}
