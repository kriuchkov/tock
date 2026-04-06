package commands

import (
	"fmt"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"

	"github.com/spf13/cobra"
)

type stopOptions struct {
	At         string
	Notes      string
	Tags       []string
	JSONOutput bool
}

func NewStopCmd() *cobra.Command {
	var opts stopOptions

	cmd := &cobra.Command{
		Use:     "stop",
		Aliases: []string{"s"},
		Short:   defaultText("stop.short"),
		RunE:    func(cmd *cobra.Command, _ []string) error { return runStopCmd(cmd, &opts) },
	}
	cmd.Flags().StringVarP(&opts.At, "time", "t", "", defaultText("stop.flag.time"))
	cmd.Flags().StringVar(&opts.Notes, "note", "", defaultText("stop.flag.note"))
	cmd.Flags().StringSliceVar(&opts.Tags, "tag", nil, defaultText("stop.flag.tag"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("stop.flag.json"))
	return cmd
}

func runStopCmd(cmd *cobra.Command, opts *stopOptions) error {
	defer runUpdateCheck(cmd)

	rt := getRuntime(cmd)
	service := rt.ActivityService
	tf := rt.TimeFormatter
	out := cmd.OutOrStdout()

	endTime := time.Now()
	if opts.At != "" {
		var err error
		endTime, err = tf.ParseTime(opts.At)
		if err != nil {
			return errors.Wrap(err, "parse time")
		}
	}

	req := models.StopActivityRequest{
		EndTime: endTime,
		Notes:   opts.Notes,
		Tags:    opts.Tags,
	}

	activity, err := service.Stop(cmd.Context(), req)
	if err != nil {
		return errors.Wrap(err, "stop activity")
	}

	if opts.JSONOutput {
		return writeJSONTo(out, activity)
	}

	_, err = fmt.Fprintf(out, text(cmd, "message.activity_stopped"),
		activity.Project,
		activity.Description,
		activity.EndTime.Format(tf.GetDisplayFormat()),
	)
	return err
}
