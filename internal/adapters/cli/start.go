package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/adapters/cli/timeutil"
	"github.com/kriuchkov/tock/internal/core/dto"

	"github.com/spf13/cobra"
)

func NewStartCmd() *cobra.Command {
	var description string
	var project string
	var at string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a new activity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)
			startTime := time.Now()
			if at != "" {
				var err error
				startTime, err = timeutil.ParseTime(at)
				if err != nil {
					return errors.Wrap(err, "parse time")
				}
			}

			req := dto.StartActivityRequest{
				Description: description,
				Project:     project,
				StartTime:   startTime,
			}

			activity, err := service.Start(context.Background(), req)
			if err != nil {
				return errors.Wrap(err, "start activity")
			}

			fmt.Printf("Started activity: %s | %s at %s\n", activity.Project, activity.Description, activity.StartTime.Format(timeutil.GetDisplayFormat()))
			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "Activity description")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&at, "time", "t", "", "Start time (HH:MM)")
	if err := cmd.MarkFlagRequired("description"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("project"); err != nil {
		panic(err)
	}

	return cmd
}
