package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/dto"
	ce "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"

	"github.com/spf13/cobra"
)

type reportOptions struct {
	Today       bool
	Yesterday   bool
	Date        string
	Summary     bool
	Project     string
	Description string
	TotalOnly   bool
	JSONOutput  bool
}

func NewReportCmd() *cobra.Command {
	var opt reportOptions

	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate time tracking report",
		Long:  "Generate a report of tracked activities aggregated by project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := runReportCmd(cmd, &opt)
			if errors.Is(err, ce.ErrCancelled) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&opt.Today, "today", false, "Report for today")
	cmd.Flags().BoolVar(&opt.Yesterday, "yesterday", false, "Report for yesterday")
	cmd.Flags().StringVar(&opt.Date, "date", "", "Report for specific date (YYYY-MM-DD)")
	cmd.Flags().BoolVarP(&opt.Summary, "summary", "s", false, "Show only project summaries")
	cmd.Flags().StringVarP(&opt.Project, "project", "p", "", "Filter by project and aggregate by description")
	cmd.Flags().StringVarP(&opt.Description, "description", "d", "", "Filter by description")
	cmd.Flags().BoolVar(&opt.TotalOnly, "total-only", false, "Show only total duration")
	cmd.Flags().BoolVar(&opt.JSONOutput, "json", false, "Output in JSON format")

	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	return cmd
}

//nolint:funlen // Report command is long but straightforward.
func runReportCmd(cmd *cobra.Command, opt *reportOptions) error {
	service := getService(cmd)
	tf := getTimeFormatter(cmd)
	ctx := context.Background()

	filter := dto.ActivityFilter{}

	// Determine date range based on flags
	switch {
	case opt.Today:
		start := time.Now().Truncate(24 * time.Hour)
		end := start.Add(24 * time.Hour)
		filter.FromDate = &start
		filter.ToDate = &end
	case opt.Yesterday:
		start := time.Now().Truncate(24 * time.Hour).Add(-24 * time.Hour)
		end := start.Add(24 * time.Hour)
		filter.FromDate = &start
		filter.ToDate = &end
	case opt.Date != "":
		parsedDate, err := time.Parse("2006-01-02", opt.Date)
		if err != nil {
			return errors.Wrap(err, "invalid date format (use YYYY-MM-DD)")
		}
		start := parsedDate.Truncate(24 * time.Hour)
		end := start.Add(24 * time.Hour)
		filter.FromDate = &start
		filter.ToDate = &end
	}

	if opt.Project != "" {
		filter.Project = &opt.Project
	}
	if opt.Description != "" {
		filter.Description = &opt.Description
	}

	report, err := service.GetReport(ctx, filter)
	if err != nil {
		return errors.Wrap(err, "generate report")
	}

	if opt.TotalOnly {
		d := report.TotalDuration.Round(time.Minute)
		h := d / time.Hour
		m := (d % time.Hour) / time.Minute
		fmt.Printf("%dh %dm\n", h, m)
		return nil
	}

	if opt.JSONOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report.Activities)
	}

	if len(report.Activities) == 0 {
		fmt.Println("No activities found for the specified period.")
		return nil
	}

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

		if opt.Project != "" {
			// Aggregation by description
			descs := make(map[string]time.Duration)
			for _, act := range projectReport.Activities {
				descs[act.Description] += act.Duration()
			}

			var descKeys []string
			for k := range descs {
				descKeys = append(descKeys, k)
			}
			sort.Strings(descKeys)

			for _, desc := range descKeys {
				dur := descs[desc]
				h := int(dur.Hours())
				m := int(dur.Minutes()) % 60
				fmt.Printf("   - %s: %dh %dm\n", desc, h, m)
			}
			fmt.Println()
		} else if !opt.Summary {
			for _, activity := range projectReport.Activities {
				startTime := activity.StartTime.Format(tf.GetDisplayFormat())
				endTime := "--:--"
				if activity.EndTime != nil {
					endTime = activity.EndTime.Format(tf.GetDisplayFormat())
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
	}

	totalHours := report.TotalDuration.Hours()
	totalMinutes := int(report.TotalDuration.Minutes()) % 60
	fmt.Printf("⏱️  Total: %dh %dm\n", int(totalHours), totalMinutes)
	return nil
}
