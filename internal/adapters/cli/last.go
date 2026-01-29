package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

	cmd.Flags().BoolVar(&opt.JSONOutput, "json", false, "Output in JSON format")
	cmd.Flags().IntVarP(&opt.Limit, "number", "n", 10, "Number of recent activities to show")
	return cmd
}

func runLastCmd(cmd *cobra.Command, opt *lastOptions) error {
	service := getService(cmd)
	ctx := context.Background()

	activities, err := service.GetRecent(ctx, opt.Limit)
	if err != nil {
		return errors.Wrap(err, "get recent activities")
	}

	if opt.JSONOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(activities)
	}

	if len(activities) == 0 {
		fmt.Println("No activities found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, " #\tDescription\tProject")

	for i := len(activities) - 1; i >= 0; i-- {
		a := activities[i]
		fmt.Fprintf(w, "[%d]\t%s\t%s\n", i, a.Description, a.Project)
	}

	w.Flush() //nolint:gosec // Ignore error on flush
	return nil
}
