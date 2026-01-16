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
		tf := getTimeFormatter(cmd)
		endTime := time.Now()
		if at != "" {
			var err error
			endTime, err = tf.ParseTime(at)
			if err != nil {
				return errors.Wrap(err, "parse time")
			}
		}

		req := dto.StopActivityRequest{EndTime: endTime}

		activity, err := service.Stop(cmd.Context(), req)
		if err != nil {
			return errors.Wrap(err, "stop activity")
		}

		fmt.Printf(
			"Stopped activity: %s | %s at %s\n",
			activity.Project,
			activity.Description,
			activity.EndTime.Format(tf.GetDisplayFormat()),
		)
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
