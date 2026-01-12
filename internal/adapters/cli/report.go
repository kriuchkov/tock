package cli

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/timeutil"
	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"

	"github.com/spf13/cobra"
)

//nolint:funlen // Report command is long but straightforward.
func NewReportCmd() *cobra.Command {
	var (
		today     bool
		yesterday bool
		date      string
	)

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate time tracking report",
		Long:  "Generate a report of tracked activities aggregated by project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)
			ctx := context.Background()

			filter := dto.ActivityFilter{}

			// Determine date range based on flags
			switch {
			case today:
				start := time.Now().Truncate(24 * time.Hour)
				end := start.Add(24 * time.Hour)
				filter.FromDate = &start
				filter.ToDate = &end
			case yesterday:
				start := time.Now().Truncate(24 * time.Hour).Add(-24 * time.Hour)
				end := start.Add(24 * time.Hour)
				filter.FromDate = &start
				filter.ToDate = &end
			case date != "":
				parsedDate, err := time.Parse("2006-01-02", date)
				if err != nil {
					return errors.Wrap(err, "invalid date format (use YYYY-MM-DD)")
				}
				start := parsedDate.Truncate(24 * time.Hour)
				end := start.Add(24 * time.Hour)
				filter.FromDate = &start
				filter.ToDate = &end
			}

			report, err := service.GetReport(ctx, filter)
			if err != nil {
				return errors.Wrap(err, "generate report")
			}

			// Display report
			if len(report.Activities) == 0 {
				fmt.Println("No activities found for the specified period.")
				return nil
			}

			// Sort projects by name for consistent output
			projectNames := make([]string, 0, len(report.ByProject))
			for name := range report.ByProject {
				projectNames = append(projectNames, name)
			}
			sort.Strings(projectNames)

			var sortedActivities = make([]models.Activity, len(report.Activities))

			copy(sortedActivities, report.Activities)

			sort.Slice(sortedActivities, func(i, j int) bool {
				return sortedActivities[i].StartTime.Before(sortedActivities[j].StartTime)
			})

			activityIDs := make(map[int64]string)
			dayCounts := make(map[string]int)

			for _, act := range sortedActivities {
				d := act.StartTime.Format("2006-01-02")
				dayCounts[d]++

				// ID format: YYYY-MM-DD-NN
				id := fmt.Sprintf("%s-%02d", d, dayCounts[d])
				activityIDs[act.StartTime.UnixNano()] = id
			}

			fmt.Println("\n📊 Time Tracking Report")
			fmt.Println("=" + "=======================")
			fmt.Println()

			for _, projectName := range projectNames {
				projectReport := report.ByProject[projectName]
				hours := projectReport.Duration.Hours()
				minutes := int(projectReport.Duration.Minutes()) % 60

				fmt.Printf("📁 %s: %dh %dm\n", projectReport.ProjectName, int(hours), minutes)
				for _, activity := range projectReport.Activities {
					startTime := activity.StartTime.Format(timeutil.GetDisplayFormat())
					endTime := "--:--"
					if activity.EndTime != nil {
						endTime = activity.EndTime.Format(timeutil.GetDisplayFormat())
					}
					duration := activity.Duration()
					actHours := int(duration.Hours())
					actMinutes := int(duration.Minutes()) % 60

					id := activityIDs[activity.StartTime.UnixNano()]
					fmt.Printf("   [%s] %s - %s (%dh %dm) | %s\n",
						id, startTime, endTime, actHours, actMinutes, activity.Description)
				}
				fmt.Println()
			}

			totalHours := report.TotalDuration.Hours()
			totalMinutes := int(report.TotalDuration.Minutes()) % 60
			fmt.Printf("⏱️  Total: %dh %dm\n", int(totalHours), totalMinutes)
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().BoolVar(&today, "today", false, "Report for today")
	cmd.Flags().BoolVar(&yesterday, "yesterday", false, "Report for yesterday")
	cmd.Flags().StringVar(&date, "date", "", "Report for specific date (YYYY-MM-DD)")
	return cmd
}
