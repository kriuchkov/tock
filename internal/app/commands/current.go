package commands

import (
	"context"
	"fmt"
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
)

type currentCmdActivity struct {
	models.Activity
}

type currentOptions struct {
	Format     string
	JSONOutput bool
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
	var opt currentOptions

	cmd := &cobra.Command{
		Use:   "current",
		Short: "Lists all currently running activities",
		Long:  defaultText("current.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCurrentCmd(cmd, &opt)
		},
	}

	cmd.Flags().BoolVar(&opt.JSONOutput, "json", false, defaultText("current.flag.json"))
	cmd.Flags().
		StringVarP(&opt.Format, "format", "F", "", defaultText("current.flag.format"))
	return cmd
}

func runCurrentCmd(cmd *cobra.Command, opt *currentOptions) error {
	defer runUpdateCheck(cmd)

	rt := getRuntime(cmd)
	service := rt.ActivityService
	tf := rt.TimeFormatter
	out := cmd.OutOrStdout()
	ctx := context.Background()

	isRunning := true
	filter := models.ActivityFilter{IsRunning: &isRunning}

	activities, err := service.List(ctx, filter)
	if err != nil {
		return errors.Wrap(err, "list activities")
	}

	if opt.JSONOutput {
		return writeJSONTo(out, activities)
	}

	if len(activities) == 0 {
		if opt.Format == "" {
			fmt.Fprintln(out, text(cmd, "current.empty"))
			return nil
		}
		return nil
	}

	if opt.Format != "" {
		parsedTemplate, parseErr := template.New("current").Parse(opt.Format + "\n")
		if parseErr != nil {
			return errors.Wrap(parseErr, "parse format template")
		}

		for _, activity := range activities {
			if err = parsedTemplate.Execute(out, currentCmdActivity{Activity: activity}); err != nil {
				return errors.Wrap(err, "execute format template")
			}
		}
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, text(cmd, "current.table.header"))

	for _, activity := range activities {
		duration := time.Since(activity.StartTime).Round(time.Second)
		_, _ = fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%s\n",
			activity.StartTime.Format(tf.GetDisplayFormatWithDate()),
			activity.Description,
			activity.Project,
			duration,
		)
	}

	if err = w.Flush(); err != nil {
		return errors.Wrap(err, "flush current activity table")
	}
	return nil
}
