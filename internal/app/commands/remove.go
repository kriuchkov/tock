package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

type removeOptions struct {
	SkipConfirm bool
	JSONOutput  bool
}

func NewRemoveCmd() *cobra.Command {
	var opts removeOptions

	cmd := &cobra.Command{
		Use:     "remove [DATE-INDEX]",
		Aliases: []string{"rm"},
		Short:   "Remove an activity",
		Long:    defaultText("remove.long"),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemoveCmd(cmd, args, &opts)
		},
	}

	cmd.Flags().BoolVarP(&opts.SkipConfirm, "yes", "y", false, defaultText("remove.flag.yes"))
	cmd.Flags().BoolVar(&opts.JSONOutput, "json", false, defaultText("remove.flag.json"))
	return cmd
}

func runRemoveCmd(cmd *cobra.Command, args []string, opts *removeOptions) error {
	ctx := cmd.Context()
	svc := getRuntime(cmd).ActivityService
	out := cmd.OutOrStdout()
	in := cmd.InOrStdin()

	var activity models.Activity
	if len(args) == 0 {
		var err error
		activity, err = findLastActivity(ctx, svc)
		if err != nil {
			return err
		}
	} else {
		var err error
		activity, err = findActivityByIndex(ctx, svc, args[0])
		if err != nil {
			return err
		}
	}

	if !opts.SkipConfirm {
		confirmed, err := confirmRemoval(out, in, activity)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := svc.Remove(ctx, activity); err != nil {
		return errors.Wrap(err, "remove activity")
	}

	if opts.JSONOutput {
		return writeJSONTo(out, activity)
	}

	fmt.Fprintln(out, defaultText("remove.done"))
	return nil
}

func confirmRemoval(out io.Writer, in io.Reader, activity models.Activity) (bool, error) {
	fmt.Fprintln(out, defaultText("remove.confirm.title"))
	if _, err := fmt.Fprintf(out, defaultText("remove.confirm.project"), activity.Project); err != nil {
		return false, errors.Wrap(err, "write confirmation")
	}
	if _, err := fmt.Fprintf(out, defaultText("remove.confirm.description"), activity.Description); err != nil {
		return false, errors.Wrap(err, "write confirmation")
	}
	if _, err := fmt.Fprintf(out, defaultText("remove.confirm.start"), activity.StartTime.Format("2006-01-02 15:04")); err != nil {
		return false, errors.Wrap(err, "write confirmation")
	}
	if activity.EndTime != nil {
		if _, err := fmt.Fprintf(out, defaultText("remove.confirm.end"), activity.EndTime.Format("15:04")); err != nil {
			return false, errors.Wrap(err, "write confirmation")
		}
	}
	if _, err := fmt.Fprint(out, defaultText("remove.confirm.prompt")); err != nil {
		return false, errors.Wrap(err, "write confirmation prompt")
	}

	reader := bufio.NewReader(in)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, errors.Wrap(err, "read input")
	}

	response = strings.ToLower(strings.TrimSpace(response))
	if response != "y" && response != "yes" {
		fmt.Fprintln(out, defaultText("remove.aborted"))
		return false, nil
	}

	return true, nil
}

func findLastActivity(ctx context.Context, svc ports.ActivityResolver) (models.Activity, error) {
	last, err := svc.GetLast(ctx)
	if err != nil {
		if errors.Is(err, coreErrors.ErrActivityNotFound) {
			return models.Activity{}, errors.New(defaultText("common.no_activities"))
		}
		return models.Activity{}, errors.Wrap(err, "get last activity")
	}
	return *last, nil
}

func findActivityByIndex(ctx context.Context, svc ports.ActivityResolver, index string) (models.Activity, error) {
	date, seq, parseErr := models.ParseActivityKey(index)
	if parseErr != nil {
		return models.Activity{}, errors.Wrap(parseErr, "parse index")
	}

	fromDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local)
	toDate := fromDate.AddDate(0, 0, 1)

	filter := models.ActivityFilter{
		FromDate: &fromDate,
		ToDate:   &toDate,
	}

	activities, err := svc.List(ctx, filter)
	if err != nil {
		return models.Activity{}, errors.Wrap(err, "list activities")
	}

	activity, sequenceErr := models.ActivityForSequence(activities, seq)
	if sequenceErr != nil {
		return models.Activity{}, errors.Errorf("activity #%d not found on %s", seq, date.Format("2006-01-02"))
	}

	return activity, nil
}
