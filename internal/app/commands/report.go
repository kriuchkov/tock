package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/go-faster/errors"

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
		Long:  defaultText("report.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := runReportCmd(cmd, &opt)
			if errors.Is(err, ce.ErrCancelled) {
				return nil
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&opt.Today, "today", false, defaultText("report.flag.today"))
	cmd.Flags().BoolVar(&opt.Yesterday, "yesterday", false, defaultText("report.flag.yesterday"))
	cmd.Flags().StringVar(&opt.Date, "date", "", defaultText("report.flag.date"))
	cmd.Flags().BoolVarP(&opt.Summary, "summary", "s", false, defaultText("report.flag.summary"))
	cmd.Flags().StringVarP(&opt.Project, "project", "p", "", defaultText("report.flag.project"))
	cmd.Flags().StringVarP(&opt.Description, "description", "d", "", defaultText("report.flag.description"))
	cmd.Flags().BoolVar(&opt.TotalOnly, "total-only", false, defaultText("report.flag.total_only"))
	cmd.Flags().BoolVar(&opt.JSONOutput, "json", false, defaultText("report.flag.json"))

	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	return cmd
}

//nolint:funlen // Report command is long but straightforward.
func runReportCmd(cmd *cobra.Command, opt *reportOptions) error {
	rt := getRuntime(cmd)
	service := rt.ActivityService
	tf := rt.TimeFormatter
	out := cmd.OutOrStdout()
	filter, err := models.BuildActivityFilter(models.ActivityFilterOptions{
		Now:         time.Now(),
		Today:       opt.Today,
		Yesterday:   opt.Yesterday,
		Date:        opt.Date,
		Project:     opt.Project,
		Description: opt.Description,
	})
	if err != nil {
		return err
	}

	report, err := service.GetReport(cmd.Context(), filter)
	if err != nil {
		return errors.Wrap(err, "generate report")
	}

	if opt.TotalOnly {
		d := report.TotalDuration.Round(time.Minute)
		h := d / time.Hour
		m := (d % time.Hour) / time.Minute
		_, err = fmt.Fprintf(out, "%dh %dm\n", h, m)
		return nil
	}

	if opt.JSONOutput {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report.Activities)
	}

	if len(report.Activities) == 0 {
		_, err = fmt.Fprintln(out, text(cmd, "report.empty"))
		return nil
	}

	projectNames := make([]string, 0, len(report.ByProject))
	for name := range report.ByProject {
		projectNames = append(projectNames, name)
	}

	sort.Strings(projectNames)
	activityIDs := models.ActivitySequenceIDs(report.Activities)

	if _, err = io.WriteString(out, text(cmd, "report.header")); err != nil {
		return errors.Wrap(err, "write report header")
	}

	for _, projectName := range projectNames {
		projectReport := report.ByProject[projectName]
		hours := projectReport.Duration.Hours()
		minutes := int(projectReport.Duration.Minutes()) % 60

		if _, err = fmt.Fprintf(out, text(cmd, "report.project_line"), projectReport.ProjectName, int(hours), minutes); err != nil {
			return errors.Wrap(err, "write project summary")
		}

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
				if _, err = fmt.Fprintf(out, text(cmd, "report.project_description_line"), desc, h, m); err != nil {
					return errors.Wrap(err, "write project description summary")
				}
			}
			if _, err = fmt.Fprintln(out); err != nil {
				return errors.Wrap(err, "write project separator")
			}
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
				if _, err = fmt.Fprintf(out, text(cmd, "report.activity_line"),
					id, startTime, endTime, actHours, actMinutes, activity.Description); err != nil {
					return errors.Wrap(err, "write activity line")
				}
			}
			if _, err = fmt.Fprintln(out); err != nil {
				return errors.Wrap(err, "write activity separator")
			}
		}
	}

	totalHours := report.TotalDuration.Hours()
	totalMinutes := int(report.TotalDuration.Minutes()) % 60
	if _, err = fmt.Fprintf(out, text(cmd, "report.total_line"), int(totalHours), totalMinutes); err != nil {
		return errors.Wrap(err, "write total duration")
	}
	return nil
}
