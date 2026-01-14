package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/dto"
)

func NewAddCmd() *cobra.Command {
	var description string
	var project string
	var startStr string
	var endStr string
	var durationStr string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a completed activity",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)

			if startStr == "" {
				return errors.New("start time is required")
			}
			if endStr == "" && durationStr == "" {
				return errors.New("end time or duration is required")
			}

			tf := getTimeFormatter(cmd)
			startTime, err := tf.ParseTimeWithDate(startStr)
			if err != nil {
				return errors.Wrap(err, "parse start time")
			}

			var endTime time.Time
			if endStr != "" {
				endTime, err = tf.ParseTimeWithDate(endStr)
				if err != nil {
					return errors.Wrap(err, "parse end time")
				}
			} else {
				var duration time.Duration
				duration, err = time.ParseDuration(durationStr)
				if err != nil {
					return errors.Wrap(err, "parse duration")
				}
				endTime = startTime.Add(duration)
			}

			if endTime.Before(startTime) {
				return errors.New("end time cannot be before start time")
			}

			req := dto.AddActivityRequest{
				Description: description,
				Project:     project,
				StartTime:   startTime,
				EndTime:     endTime,
			}

			activity, err := service.Add(context.Background(), req)
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
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "Activity description")
	cmd.Flags().StringVarP(&project, "project", "p", "", "Project name")
	cmd.Flags().StringVarP(&startStr, "start", "s", "", "Start time (HH:MM)")
	cmd.Flags().StringVarP(&endStr, "end", "e", "", "End time (HH:MM)")
	cmd.Flags().StringVar(&durationStr, "duration", "", "Duration (e.g. 1h, 30m)")

	if err := cmd.MarkFlagRequired("description"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("project"); err != nil {
		panic(err)
	}

	return cmd
}
