package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/dto"
)

func NewCurrentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Lists all currently running activities",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)
			tf := getTimeFormatter(cmd)
			ctx := context.Background()

			isRunning := true
			filter := dto.ActivityFilter{
				IsRunning: &isRunning,
			}

			activities, err := service.List(ctx, filter)
			if err != nil {
				return errors.Wrap(err, "list activities")
			}

			if len(activities) == 0 {
				fmt.Println("No currently running activities.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "Start\tDescription\tProject\tDuration")

			for _, a := range activities {
				duration := time.Since(a.StartTime).Round(time.Second)
				fmt.Fprintf(
					w,
					"%s\t%s\t%s\t%s\n",
					a.StartTime.Format(tf.GetDisplayFormatWithDate()),
					a.Description,
					a.Project,
					duration,
				)
			}

			w.Flush() //nolint:gosec // Ignore error on flush
			return nil
		},
	}

	return cmd
}
