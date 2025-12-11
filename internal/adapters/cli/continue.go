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

func NewContinueCmd() *cobra.Command {
	var description string
	var project string
	var at string

	cmd := &cobra.Command{
		Use:   "continue [NUMBER]",
		Short: "Continues a previous activity",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			service := getService(cmd)
			ctx := context.Background()

			number := 0
			if len(args) > 0 {
				var err error
				number, err = strconv.Atoi(args[0])
				if err != nil {
					return errors.Wrap(err, "invalid number")
				}
			}

			// Fetch recent activities to find the one to continue
			// We need at least number+1 activities
			activities, err := service.GetRecent(ctx, number+1)
			if err != nil {
				return errors.Wrap(err, "get recent activities")
			}

			if number >= len(activities) {
				return errors.Errorf("activity number %d not found (only %d recent activities available)", number, len(activities))
			}

			activityToContinue := activities[number]

			// Determine new activity details
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
				// Parse 'at' time. Assuming HH:MM format for today, similar to start command
				parsed, parseErr := time.ParseInLocation("15:04", at, time.Local)
				if parseErr != nil {
					return errors.Wrap(parseErr, "parse time")
				}
				// Combine with today's date
				now := time.Now()
				startTime = time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, time.Local)
			}

			req := dto.StartActivityRequest{
				Description: newDescription,
				Project:     newProject,
				StartTime:   startTime,
			}

			startedActivity, err := service.Start(ctx, req)
			if err != nil {
				return errors.Wrap(err, "start activity")
			}

			fmt.Printf(
				"Started activity: %s | %s at %s\n",
				startedActivity.Project,
				startedActivity.Description,
				startedActivity.StartTime.Format("15:04"),
			)
			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "the description of the new activity")
	cmd.Flags().StringVarP(&project, "project", "p", "", "the project to which the new activity belongs")
	cmd.Flags().StringVarP(&at, "time", "t", "", "the time for changing the activity status (HH:MM)")
	return cmd
}
