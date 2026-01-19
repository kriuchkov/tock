package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
)

type currentCmdActivity struct {
	models.Activity
}

func (a currentCmdActivity) Duration() time.Duration {
	return a.Activity.Duration().Round(time.Second)
}

func (a currentCmdActivity) DurationHMS() string {
	d := a.Activity.Duration().Round(time.Second)
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func NewCurrentCmd() *cobra.Command {
	var format string

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
				if format == "" {
					fmt.Println("No currently running activities.")
				}
				return nil
			}

			if format != "" {
				var tmpl *template.Template
				tmpl, err = template.New("current").Parse(format + "\n")
				if err != nil {
					return errors.Wrap(err, "parse format template")
				}

				for _, a := range activities {
					if err = tmpl.Execute(os.Stdout, currentCmdActivity{Activity: a}); err != nil {
						return errors.Wrap(err, "execute format template")
					}
				}
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

	cmd.Flags().StringVarP(&format, "format", "F", "", "Format output using a Go template (e.g. '{{.Project}}: {{.Duration}}')")

	return cmd
}
