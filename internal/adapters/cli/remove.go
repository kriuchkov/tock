package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/dto"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

func NewRemoveCmd() *cobra.Command {
	var skipConfirm bool

	cmd := &cobra.Command{
		Use:     "remove [DATE-INDEX]",
		Aliases: []string{"rm"},
		Short:   "Remove an activity",
		Long: `Remove an activity from the log.

If no argument is provided, removes the last activity.
To remove a specific activity, provide its index ID (YYYY-MM-DD-NN).

Examples:
  tock remove                     # Remove last activity
  tock remove -y                  # Remove last activity without confirmation
  tock remove 2023-10-15-01       # Remove specific activity`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			svc := getService(cmd)

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

			if !skipConfirm {
				fmt.Printf("About to remove:\n")
				fmt.Printf("  Project:     %s\n", activity.Project)
				fmt.Printf("  Description: %s\n", activity.Description)
				fmt.Printf("  Start:       %s\n", activity.StartTime.Format("2006-01-02 15:04"))
				if activity.EndTime != nil {
					fmt.Printf("  End:         %s\n", activity.EndTime.Format("15:04"))
				}
				fmt.Printf("\nAre you sure? [y/N]: ")

				reader := bufio.NewReader(os.Stdin)
				response, err := reader.ReadString('\n')
				if err != nil {
					return errors.Wrap(err, "read input")
				}

				response = strings.ToLower(strings.TrimSpace(response))
				if response != "y" && response != "yes" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			if err := svc.Remove(ctx, activity); err != nil {
				return errors.Wrap(err, "remove activity")
			}

			fmt.Println("Activity removed.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&skipConfirm, "yes", "y", false, "Skip confirmation")
	return cmd
}

func parseRemoveIndex(s string) (time.Time, int, error) {
	parts := strings.Split(s, "-")
	if len(parts) < 4 {
		return time.Time{}, 0, errors.New("invalid format: expected YYYY-MM-DD-NN")
	}

	seqStr := parts[len(parts)-1]
	seq, err := strconv.Atoi(seqStr)
	if err != nil {
		return time.Time{}, 0, errors.Wrapf(err, "invalid sequence: %s", seqStr)
	}

	dateStr := strings.Join(parts[:len(parts)-1], "-")
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return time.Time{}, 0, errors.Wrapf(err, "invalid date: %s", dateStr)
	}

	return date, seq, nil
}

func findLastActivity(ctx context.Context, svc ports.ActivityResolver) (models.Activity, error) {
	last, err := svc.GetLast(ctx)
	if err != nil {
		if errors.Is(err, coreErrors.ErrActivityNotFound) {
			return models.Activity{}, errors.New("no activities found")
		}
		return models.Activity{}, errors.Wrap(err, "get last activity")
	}
	return *last, nil
}

func findActivityByIndex(ctx context.Context, svc ports.ActivityResolver, index string) (models.Activity, error) {
	date, seq, parseErr := parseRemoveIndex(index)
	if parseErr != nil {
		return models.Activity{}, errors.Wrap(parseErr, "parse index")
	}

	fromDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	toDate := fromDate.AddDate(0, 0, 1).Add(-time.Nanosecond)

	filter := dto.ActivityFilter{
		FromDate: &fromDate,
		ToDate:   &toDate,
	}

	activities, err := svc.List(ctx, filter)
	if err != nil {
		return models.Activity{}, errors.Wrap(err, "list activities")
	}

	sort.Slice(activities, func(i, j int) bool {
		return activities[i].StartTime.Before(activities[j].StartTime)
	})

	if seq < 1 || seq > len(activities) {
		return models.Activity{}, errors.Errorf("activity #%d not found on %s", seq, date.Format("2006-01-02"))
	}

	return activities[seq-1], nil
}
