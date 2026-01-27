package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"
)

func NewLastCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:     "last",
		Aliases: []string{"lt"},
		Short:   "List recent unique activities",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)
			ctx := context.Background()

			activities, err := service.GetRecent(ctx, limit)
			if err != nil {
				return errors.Wrap(err, "get recent activities")
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
		},
	}

	cmd.Flags().IntVarP(&limit, "number", "n", 10, "Number of recent activities to show")
	return cmd
}
