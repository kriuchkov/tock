package commands

import (
	"fmt"
	"strconv"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/models"
)

const (
	defaultRecentActivitiesForContinuation = 10
)

type continueOptions struct {
	Description string
	Project     string
	At          string
	Notes       string
	Tags        []string
	JSONOutput  bool
}

func NewContinueCmd() *cobra.Command {
	var opts continueOptions

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
			return runContinueCmd(cmd, args, &opts)
		},
	}

	cmd.Flags().StringVarP(&opts.Description, "description", "d", "", defaultText("continue.flag.description"))
	cmd.Flags().StringVarP(&opts.Project, "project", "p", "", defaultText("continue.flag.project"))
	cmd.Flags().StringVarP(&opts.At, "time", "t", "", defaultText("continue.flag.time"))
	cmd.Flags().StringVar(&opts.Notes, "note", "", defaultText("continue.flag.note"))
	cmd.Flags().StringSliceVar(&opts.Tags, "tag", nil, defaultText("continue.flag.tag"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("continue.flag.json"))
	return cmd
}

func runContinueCmd(cmd *cobra.Command, args []string, opts *continueOptions) error {
	defer runUpdateCheck(cmd)

	rt := getRuntime(cmd)
	service := rt.ActivityService
	tf := rt.TimeFormatter
	out := cmd.OutOrStdout()

	number := 0
	if len(args) > 0 {
		var err error
		number, err = strconv.Atoi(args[0])
		if err != nil {
			return errors.Wrap(err, "invalid number")
		}
	}

	activities, err := service.GetRecent(cmd.Context(), number+1)
	if err != nil {
		return errors.Wrap(err, "get recent activities")
	}
	if number >= len(activities) {
		return errors.Errorf("activity number %d not found (only %d recent activities available)", number, len(activities))
	}

	activityToContinue := activities[number]
	newDescription := activityToContinue.Description
	if opts.Description != "" {
		newDescription = opts.Description
	}
	newProject := activityToContinue.Project
	if opts.Project != "" {
		newProject = opts.Project
	}

	startTime := time.Now()
	if opts.At != "" {
		var parseErr error
		startTime, parseErr = tf.ParseTime(opts.At)
		if parseErr != nil {
			return errors.Wrap(parseErr, "parse time")
		}
	}

	startedActivity, err := service.Start(cmd.Context(), models.StartActivityRequest{
		Description: newDescription,
		Project:     newProject,
		StartTime:   startTime,
		Notes:       opts.Notes,
		Tags:        opts.Tags,
	})
	if err != nil {
		return errors.Wrap(err, "start activity")
	}

	if opts.JSONOutput {
		return writeJSONTo(out, startedActivity)
	}

	_, err = fmt.Fprintf(out, text(cmd, "message.activity_started"),
		startedActivity.Project,
		startedActivity.Description,
		startedActivity.StartTime.Format(tf.GetDisplayFormat()),
	)
	return err
}
