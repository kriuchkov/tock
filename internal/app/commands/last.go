package commands

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	ce "github.com/kriuchkov/tock/internal/core/errors"
)

type lastOptions struct {
	Limit      int
	JSONOutput bool
}

func NewLastCmd() *cobra.Command {
	var opt lastOptions

	cmd := &cobra.Command{
		Use:     "last",
		Aliases: []string{"lt"},
		Short:   "List recent unique activities",
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := runLastCmd(cmd, &opt)
			if errors.Is(err, ce.ErrCancelled) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&opt.JSONOutput, "json", false, defaultText("last.flag.json"))
	cmd.Flags().IntVarP(&opt.Limit, "number", "n", 10, defaultText("last.flag.number"))
	return cmd
}

func runLastCmd(cmd *cobra.Command, opt *lastOptions) error {
	service := getRuntime(cmd).ActivityService
	ctx := context.Background()
	out := cmd.OutOrStdout()

	activities, err := service.GetRecent(ctx, opt.Limit)
	if err != nil {
		return errors.Wrap(err, "get recent activities")
	}

	if opt.JSONOutput {
		return writeJSONTo(out, activities)
	}

	if len(activities) == 0 {
		_, err = fmt.Fprintln(out, text(cmd, "common.no_activities"))
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, text(cmd, "last.table.header"))

	for i := len(activities) - 1; i >= 0; i-- {
		a := activities[i]
		fmt.Fprintf(w, "[%d]\t%s\t%s\n", i, a.Description, a.Project)
	}

	if err = w.Flush(); err != nil {
		return errors.Wrap(err, "flush recent activity table")
	}
	return nil
}
