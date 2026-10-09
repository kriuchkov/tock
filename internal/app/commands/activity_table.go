package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// newActivityTable builds the day table shared by the list and edit TUIs.
func newActivityTable(columns []table.Column, height int) table.Model {
	activityTable := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(height),
	)

	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	styles.Selected = styles.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	activityTable.SetStyles(styles)
	return activityTable
}

// activitiesOnDay returns the activities starting on the given day, ordered the
// same way `tock edit`/`tock remove` resolve a DATE-INDEX key.
func activitiesOnDay(activities []models.Activity, day time.Time) []models.Activity {
	var dayActivities []models.Activity
	for _, activity := range activities {
		if sameDay(activity.StartTime, day) {
			dayActivities = append(dayActivities, activity)
		}
	}
	return models.SortActivitiesByStart(dayActivities)
}

// activityDayKey formats the DATE-INDEX key that `tock edit`/`tock remove`
// resolve. It numbers by position, because two activities started in the same
// minute share a start time on the minute-precision backends.
func activityDayKey(activity models.Activity, index int) string {
	return fmt.Sprintf("%s-%02d", activity.StartTime.Format(time.DateOnly), index+1)
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

// adjacentActivityDay returns the next (dir > 0) or previous day that has
// activities, or the current selection when there is none.
func adjacentActivityDay(activities []models.Activity, selected time.Time, dir int) time.Time {
	current, _ := timeutil.LocalDayBounds(selected)
	if target := models.FindTargetDate(activities, current, dir); target != nil {
		return *target
	}
	return selected
}

func formatActivityTimeRange(tf *timeutil.Formatter, activity models.Activity) string {
	timeRange := activity.StartTime.Format(tf.GetDisplayFormat())
	if activity.EndTime == nil {
		return timeRange + " - ..."
	}
	return timeRange + " - " + activity.EndTime.Format(tf.GetDisplayFormat())
}

func formatActivityTags(activity models.Activity) string {
	return strings.Join(activity.Tags, ", ")
}

func formatActivityDuration(activity models.Activity) string {
	return activity.Duration().Round(time.Minute).String()
}
