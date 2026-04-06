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
	"github.com/kriuchkov/tock/internal/timeutil"

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

func runReportCmd(cmd *cobra.Command, opt *reportOptions) error {
	rt := getRuntime(cmd)
	service := rt.ActivityService
	out := cmd.OutOrStdout()
	tf := rt.TimeFormatter

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

	return writeReportOutput(cmd, out, tf, report, opt)
}

func writeReportOutput(
	cmd *cobra.Command,
	out io.Writer,
	tf *timeutil.Formatter,
	report *models.Report,
	opt *reportOptions,
) error {
	if opt.TotalOnly {
		return writeTotalDuration(out, report.TotalDuration)
	}

	if opt.JSONOutput {
		return writeReportJSON(out, report.Activities)
	}

	if len(report.Activities) == 0 {
		fmt.Fprintln(out, text(cmd, "report.empty"))
		return nil
	}

	if _, err := io.WriteString(out, text(cmd, "report.header")); err != nil {
		return errors.Wrap(err, "write report header")
	}

	activityIDs := models.ActivitySequenceIDs(report.Activities)
	for _, projectName := range sortedProjectNames(report.ByProject) {
		if err := writeProjectSection(cmd, out, tf, report.ByProject[projectName], activityIDs, opt); err != nil {
			return err
		}
	}

	return writeReportTotalLine(cmd, out, report.TotalDuration)
}

func writeReportJSON(out io.Writer, activities []models.Activity) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(activities)
}

func writeTotalDuration(out io.Writer, duration time.Duration) error {
	rounded := duration.Round(time.Minute)
	hours := rounded / time.Hour
	minutes := (rounded % time.Hour) / time.Minute
	_, err := fmt.Fprintf(out, "%dh %dm\n", hours, minutes)
	return err
}

func sortedProjectNames(byProject map[string]models.ProjectReport) []string {
	projectNames := make([]string, 0, len(byProject))
	for name := range byProject {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)

	return projectNames
}

func writeProjectSection(
	cmd *cobra.Command,
	out io.Writer,
	tf *timeutil.Formatter,
	projectReport models.ProjectReport,
	activityIDs map[int64]string,
	opt *reportOptions,
) error {
	hours := projectReport.Duration.Hours()
	minutes := int(projectReport.Duration.Minutes()) % 60
	if _, err := fmt.Fprintf(out, text(cmd, "report.project_line"), projectReport.ProjectName, int(hours), minutes); err != nil {
		return errors.Wrap(err, "write project summary")
	}

	if opt.Project != "" {
		return writeProjectDescriptionSummary(cmd, out, projectReport)
	}
	if opt.Summary {
		return nil
	}

	return writeProjectActivities(cmd, out, tf, projectReport.Activities, activityIDs)
}

func writeProjectDescriptionSummary(cmd *cobra.Command, out io.Writer, projectReport models.ProjectReport) error {
	descriptions := make(map[string]time.Duration)
	for _, activity := range projectReport.Activities {
		descriptions[activity.Description] += activity.Duration()
	}

	descriptionKeys := make([]string, 0, len(descriptions))
	for description := range descriptions {
		descriptionKeys = append(descriptionKeys, description)
	}
	sort.Strings(descriptionKeys)

	for _, description := range descriptionKeys {
		duration := descriptions[description]
		hours := int(duration.Hours())
		minutes := int(duration.Minutes()) % 60
		if _, err := fmt.Fprintf(out, text(cmd, "report.project_description_line"), description, hours, minutes); err != nil {
			return errors.Wrap(err, "write project description summary")
		}
	}

	fmt.Fprintln(out)
	return nil
}

func writeProjectActivities(
	cmd *cobra.Command,
	out io.Writer,
	tf *timeutil.Formatter,
	activities []models.Activity,
	activityIDs map[int64]string,
) error {
	for _, activity := range activities {
		startTime := activity.StartTime.Format(tf.GetDisplayFormat())
		endTime := "--:--"
		if activity.EndTime != nil {
			endTime = activity.EndTime.Format(tf.GetDisplayFormat())
		}

		duration := activity.Duration()
		hours := int(duration.Hours())
		minutes := int(duration.Minutes()) % 60
		id := activityIDs[activity.StartTime.UnixNano()]

		if _, err := fmt.Fprintf(
			out,
			text(cmd, "report.activity_line"),
			id,
			startTime,
			endTime,
			hours,
			minutes,
			activity.Description,
		); err != nil {
			return errors.Wrap(err, "write activity line")
		}
	}

	fmt.Fprintln(out)
	return nil
}

func writeReportTotalLine(cmd *cobra.Command, out io.Writer, duration time.Duration) error {
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	if _, err := fmt.Fprintf(out, text(cmd, "report.total_line"), hours, minutes); err != nil {
		return errors.Wrap(err, "write total duration")
	}
	return nil
}
