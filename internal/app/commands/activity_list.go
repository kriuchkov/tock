package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/kriuchkov/tock/internal/app/localization"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// Columns of an activity row.
const (
	activityKeyW      = 15 // "2026-11-16-03"
	activityClockGap  = 2  // after the start and the end, "15:04" or "03:04 PM"
	activityProjectW  = 16
	activityDescW     = 28 // the description when the tags and notes have columns
	activityTagsW     = 18
	activityDurationW = 8
	// activityKeyMin is the width from which the key column shows, activityNotesMin the one
	// from which the tags and notes do.
	activityKeyMin   = 80
	activityNotesMin = 120
)

// activityList is the activities of a day as rows under a line of column names, one of them
// selected; the list tab and the edit picker share it.
type activityList struct {
	ui         chromeStyles
	loc        *localization.Localizer
	tf         *timeutil.Formatter
	activities []models.Activity
	cursor     int
	offset     int
	clockW     int // the start and end columns, as wide as the time format needs
	page       int // the rows on screen at the last view
}

func newActivityList(ui chromeStyles, loc *localization.Localizer, tf *timeutil.Formatter) activityList {
	return activityList{ui: ui, loc: loc, tf: tf, clockW: clockWidth(tf.GetDisplayFormat()) + activityClockGap}
}

// set lists the activities of a day, keeping the cursor on a row.
func (l *activityList) set(activities []models.Activity) {
	l.activities = activities
	l.cursor = max(min(l.cursor, len(activities)-1), 0)
}

func (l *activityList) selected() (models.Activity, bool) {
	if l.cursor < 0 || l.cursor >= len(l.activities) {
		return models.Activity{}, false
	}
	return l.activities[l.cursor], true
}

// move moves the selection on the keys of moveCursor.
func (l *activityList) move(key string) {
	l.cursor, _ = moveCursor(key, l.cursor, len(l.activities), l.page)
}

// total is the time of the day's activities.
func (l *activityList) total() time.Duration {
	var total time.Duration
	for _, activity := range l.activities {
		total += activity.Duration()
	}
	return total
}

// view is the line of column names and the rows in width×height, scrolled to the selection.
func (l *activityList) view(width, height int, empty string) string {
	if len(l.activities) == 0 {
		return " " + l.ui.muted.Render(empty)
	}
	lines := []string{l.headerRow(width)}
	rowsH := len(l.activities)
	if height > 1 {
		rowsH = height - 1
	}
	l.offset, l.page = scrollOffset(l.offset, l.cursor, rowsH, len(l.activities)), rowsH
	for i := l.offset; i < min(len(l.activities), l.offset+rowsH); i++ {
		lines = append(lines, l.row(l.activities[i], i, i == l.cursor, width))
	}
	return strings.Join(lines, "\n")
}

func (l *activityList) headerRow(width int) string {
	head := func(key string, w int, right bool) rowCell {
		return rowCell{text: strings.ToUpper(l.loc.Text(key)), style: l.ui.muted, width: w, right: right}
	}
	cells := make([]rowCell, 0, rowCellsCap)
	if width >= activityKeyMin {
		cells = append(cells, head("list.table.key", activityKeyW, false))
	}
	cells = append(cells,
		head("list.table.start", l.clockW, false), head("list.table.end", l.clockW, false),
		head("list.table.project", activityProjectW, false))
	if width >= activityNotesMin {
		cells = append(cells, head("list.table.description", activityDescW, false),
			head("list.table.tags", activityTagsW, false), head("list.table.notes", 0, false))
	} else {
		cells = append(cells, head("list.table.description", 0, false))
	}
	cells = append(cells, head("list.table.time", activityDurationW, true))
	return l.ui.renderRow("", l.ui.muted, cells, false, width)
}

// row is an activity: its key, start and end, project, description, tags and notes as far as
// they fit, and its time. A running one is marked ● and ends "now".
func (l *activityList) row(activity models.Activity, index int, selected bool, width int) string {
	running := activity.EndTime == nil
	glyph, timeStyle := "", l.ui.text
	end := l.loc.Text("calendar.now")
	endStyle := l.ui.ok
	if running {
		glyph, timeStyle = glyphRunning, l.ui.ok
	} else {
		end, endStyle = activity.EndTime.Format(l.tf.GetDisplayFormat()), l.ui.text
	}
	keyStyle := l.ui.muted
	if selected {
		keyStyle = l.ui.accentBold
	}

	cells := make([]rowCell, 0, rowCellsCap)
	if width >= activityKeyMin {
		cells = append(cells, rowCell{text: activityDayKey(activity, index), style: keyStyle, width: activityKeyW})
	}
	cells = append(cells,
		rowCell{text: activity.StartTime.Format(l.tf.GetDisplayFormat()), style: l.ui.text, width: l.clockW},
		rowCell{text: end, style: endStyle, width: l.clockW},
		rowCell{text: activity.Project, style: l.ui.text, width: activityProjectW},
	)
	if width >= activityNotesMin {
		tags := ""
		if len(activity.Tags) > 0 {
			tags = "#" + strings.Join(activity.Tags, " #")
		}
		cells = append(cells,
			rowCell{text: activity.Description, style: l.ui.text, width: activityDescW},
			rowCell{text: tags, style: l.ui.tag, width: activityTagsW},
			rowCell{text: oneLine(activity.Notes), style: l.ui.muted},
		)
	} else {
		cells = append(cells, rowCell{text: activity.Description, style: l.ui.text})
	}
	cells = append(cells, rowCell{text: formatHM(activity.Duration()), style: timeStyle, width: activityDurationW, right: true})
	return l.ui.renderRow(glyph, l.ui.ok, cells, selected, width)
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
