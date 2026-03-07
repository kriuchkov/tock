package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	stdErrors "errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

type exportOptions struct {
	Today       bool
	Yesterday   bool
	Date        string
	Project     string
	Description string
	Format      string
	Path        string
	Stdout      bool
}

func NewExportCmd() *cobra.Command {
	var opt exportOptions

	cmd := &cobra.Command{
		Use:     "export",
		Aliases: []string{"e"},
		Short:   "Export report data to file",
		Long:    "Export report output as txt, csv, or json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runExportCmd(cmd, &opt)
		},
	}

	cmd.Flags().BoolVar(&opt.Today, "today", false, "Report for today")
	cmd.Flags().BoolVar(&opt.Yesterday, "yesterday", false, "Report for yesterday")
	cmd.Flags().StringVar(&opt.Date, "date", "", "Report for specific date (YYYY-MM-DD)")
	cmd.Flags().StringVarP(&opt.Project, "project", "p", "", "Filter by project")
	cmd.Flags().StringVarP(&opt.Description, "description", "d", "", "Filter by description")
	cmd.Flags().StringVarP(&opt.Format, "format", "m", "txt", "Export format: txt, csv, json")
	cmd.Flags().StringVar(&opt.Format, "fmt", "txt", "Export format: txt, csv, json")
	cmd.Flags().StringVarP(&opt.Path, "path", "o", "", "Output directory")
	cmd.Flags().BoolVar(&opt.Stdout, "stdout", false, "Print output to stdout instead of writing a file")

	_ = cmd.RegisterFlagCompletionFunc("project", projectRegisterFlagCompletion)
	_ = cmd.RegisterFlagCompletionFunc("description", descriptionRegisterFlagCompletion)
	return cmd
}

func runExportCmd(cmd *cobra.Command, opt *exportOptions) error {
	service := getService(cmd)
	tf := getTimeFormatter(cmd)
	ctx := context.Background()

	filter, err := buildExportFilter(opt)
	if err != nil {
		return err
	}

	report, err := service.GetReport(ctx, filter)
	if err != nil {
		return errors.Wrap(err, "generate report")
	}

	format := strings.ToLower(strings.TrimSpace(opt.Format))
	output, err := renderExportOutput(format, report, tf)
	if err != nil {
		return err
	}

	if opt.Stdout {
		_, err = os.Stdout.Write(output)
		if err != nil {
			return errors.Wrap(err, "write stdout")
		}
		if len(output) == 0 || output[len(output)-1] != '\n' {
			fmt.Println()
		}
		return nil
	}

	outputDir := opt.Path
	if outputDir == "" {
		outputDir, err = getDefaultExportDir(cmd)
		if err != nil {
			return err
		}
	}

	writtenPath, err := writeExportFile(outputDir, format, output)
	if err != nil {
		return err
	}

	fmt.Println(writtenPath)
	return nil
}

func buildExportFilter(opt *exportOptions) (dto.ActivityFilter, error) {
	filter := dto.ActivityFilter{}

	switch {
	case opt.Today:
		start, end := timeutil.LocalDayBounds(time.Now())
		filter.FromDate = &start
		filter.ToDate = &end
	case opt.Yesterday:
		todayStart, _ := timeutil.LocalDayBounds(time.Now())
		start := todayStart.AddDate(0, 0, -1)
		end := todayStart
		filter.FromDate = &start
		filter.ToDate = &end
	case opt.Date != "":
		parsedDate, err := time.ParseInLocation("2006-01-02", opt.Date, time.Local)
		if err != nil {
			return dto.ActivityFilter{}, errors.Wrap(err, "invalid date format (use YYYY-MM-DD)")
		}
		start, end := timeutil.LocalDayBounds(parsedDate)
		filter.FromDate = &start
		filter.ToDate = &end
	}

	if opt.Project != "" {
		filter.Project = &opt.Project
	}
	if opt.Description != "" {
		filter.Description = &opt.Description
	}

	return filter, nil
}

func renderExportOutput(format string, report *dto.Report, tf *timeutil.Formatter) ([]byte, error) {
	switch format {
	case "txt":
		return []byte(renderTextReport(report, tf)), nil
	case "csv":
		return renderCSVReport(report.Activities)
	case "json":
		return renderJSONReport(report.Activities)
	default:
		return nil, fmt.Errorf("unsupported format: %s (use txt, csv, or json)", format)
	}
}

func renderTextReport(report *dto.Report, tf *timeutil.Formatter) string {
	if len(report.Activities) == 0 {
		return "No activities found for the specified period.\n"
	}

	projectNames := make([]string, 0, len(report.ByProject))
	for name := range report.ByProject {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)

	sortedActivities := make([]models.Activity, len(report.Activities))
	copy(sortedActivities, report.Activities)
	sort.Slice(sortedActivities, func(i, j int) bool {
		return sortedActivities[i].StartTime.Before(sortedActivities[j].StartTime)
	})

	activityIDs := make(map[int64]string)
	dayCounts := make(map[string]int)
	for _, act := range sortedActivities {
		day := act.StartTime.Format("2006-01-02")
		dayCounts[day]++
		id := fmt.Sprintf("%s-%02d", day, dayCounts[day])
		activityIDs[act.StartTime.UnixNano()] = id
	}

	var b strings.Builder
	b.WriteString("\n📊 Time Tracking Report\n")
	b.WriteString("========================\n\n")

	for _, projectName := range projectNames {
		projectReport := report.ByProject[projectName]
		hours := projectReport.Duration.Hours()
		minutes := int(projectReport.Duration.Minutes()) % 60

		fmt.Fprintf(&b, "📁 %s: %dh %dm\n", projectReport.ProjectName, int(hours), minutes)

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

			fmt.Fprintf(&b, "   [%s] %s - %s (%dh %dm) | %s\n", id, startTime, endTime, actHours, actMinutes, activity.Description)
		}
		b.WriteString("\n")
	}

	totalHours := report.TotalDuration.Hours()
	totalMinutes := int(report.TotalDuration.Minutes()) % 60
	fmt.Fprintf(&b, "⏱️  Total: %dh %dm\n", int(totalHours), totalMinutes)
	return b.String()
}

func renderCSVReport(activities []models.Activity) ([]byte, error) {
	sortedActivities := make([]models.Activity, len(activities))
	copy(sortedActivities, activities)
	sort.Slice(sortedActivities, func(i, j int) bool {
		return sortedActivities[i].StartTime.Before(sortedActivities[j].StartTime)
	})

	var b bytes.Buffer
	w := csv.NewWriter(&b)

	if err := w.Write([]string{"project", "description", "start_time", "end_time", "duration_minutes"}); err != nil {
		return nil, errors.Wrap(err, "write csv header")
	}

	for _, act := range sortedActivities {
		endTime := ""
		if act.EndTime != nil {
			endTime = act.EndTime.Format(time.RFC3339)
		}

		durationMinutes := math.Floor((act.Duration().Seconds()/60)*100) / 100
		record := []string{
			act.Project,
			act.Description,
			act.StartTime.Format(time.RFC3339),
			endTime,
			fmt.Sprintf("%.2f", durationMinutes),
		}
		if err := w.Write(record); err != nil {
			return nil, errors.Wrap(err, "write csv row")
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, errors.Wrap(err, "flush csv")
	}

	return b.Bytes(), nil
}

func renderJSONReport(activities []models.Activity) ([]byte, error) {
	payload, err := json.MarshalIndent(activities, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "marshal json")
	}
	return append(payload, '\n'), nil
}

func writeExportFile(outputDir, format string, content []byte) (string, error) {
	if outputDir == "" {
		return "", errors.New("output path is empty")
	}

	if err := os.MkdirAll(outputDir, 0750); err != nil {
		return "", errors.Wrap(err, "create output directory")
	}

	baseName := "tock-report-" + time.Now().Format("20060102-150405.000000000")
	for attempt := range 1000 {
		filename := fmt.Sprintf("%s.%s", baseName, format)
		if attempt > 0 {
			filename = fmt.Sprintf("%s-%d.%s", baseName, attempt, format)
		}

		fullPath := filepath.Join(outputDir, filename)
		file, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			if stdErrors.Is(err, os.ErrExist) {
				continue
			}
			return "", errors.Wrap(err, "create output file")
		}

		if _, err = file.Write(content); err != nil {
			_ = file.Close()
			return "", errors.Wrap(err, "write output file")
		}
		if err = file.Close(); err != nil {
			return "", errors.Wrap(err, "close output file")
		}
		return fullPath, nil
	}

	return "", errors.New("unable to allocate unique output filename")
}

func getDefaultExportDir(cmd *cobra.Command) (string, error) {
	cfg := getConfig(cmd)

	backend, err := cmd.Root().PersistentFlags().GetString("backend")
	if err != nil {
		return "", errors.Wrap(err, "read backend flag")
	}
	if backend == "" {
		backend = cfg.Backend
	}

	filePath, err := cmd.Root().PersistentFlags().GetString("file")
	if err != nil {
		return "", errors.Wrap(err, "read file flag")
	}
	if filePath == "" {
		if backend == backendTimewarrior {
			filePath = cfg.Timewarrior.DataPath
		} else {
			filePath = cfg.File.Path
		}
	}

	if backend == backendTimewarrior {
		if filePath == "" {
			return "", errors.New("timewarrior data path is empty")
		}
		return filePath, nil
	}

	if filePath == "" {
		return "", errors.New("activity file path is empty")
	}

	return filepath.Dir(filePath), nil
}
