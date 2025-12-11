package cli

import (
	"fmt"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/dto"

	"github.com/spf13/cobra"
)

func NewStopCmd() *cobra.Command {
	var at string

	fn := func(cmd *cobra.Command, _ []string) error {
		service := getService(cmd)
		endTime := time.Now()
		if at != "" {
			parsed, err := time.ParseInLocation("15:04", at, time.Local)
			if err != nil {
				return errors.Wrap(err, "parse time")
			}

			now := time.Now()
			endTime = time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, time.Local)
		}

		req := dto.StopActivityRequest{EndTime: endTime}

		activity, err := service.Stop(cmd.Context(), req)
		if err != nil {
			return errors.Wrap(err, "stop activity")
		}

		fmt.Printf("Stopped activity: %s | %s at %s\n", activity.Project, activity.Description, activity.EndTime.Format("15:04"))
		return nil
	}

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the current activity",
		RunE:  fn,
	}
	cmd.Flags().StringVarP(&at, "time", "t", "", "End time (HH:MM)")
	return cmd
}
