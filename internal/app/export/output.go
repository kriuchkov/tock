package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func RenderOutput(format string, report *models.Report, tf *timeutil.Formatter) ([]byte, error) {
	switch format {
	case "txt":
		return []byte(RenderTextReport(report, tf)), nil
	case "csv":
		return RenderCSVReport(report.Activities)
	case "json":
		return RenderJSONReport(report.Activities)
	default:
		return nil, fmt.Errorf("unsupported format: %s (use txt, csv, or json)", format)
	}
}

func RenderTextReport(report *models.Report, tf *timeutil.Formatter) string {
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
		day := act.StartTime.Format(time.DateOnly)
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

func RenderCSVReport(activities []models.Activity) ([]byte, error) {
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

func RenderJSONReport(activities []models.Activity) ([]byte, error) {
	payload, err := json.MarshalIndent(activities, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "marshal json")
	}
	return append(payload, '\n'), nil
}