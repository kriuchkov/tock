package insights

import (
	"sort"
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
)

type MonthData struct {
	MonthReports map[int]*models.Report
	DailyReports map[string]*models.Report
}

type ProductivityStats struct {
	TotalDuration    time.Duration
	AvgDuration      time.Duration
	MaxDailyDuration time.Duration
	ActiveDays       int
	LongestStreak    int
}

type ProjectDuration struct {
	Name     string
	Duration time.Duration
}

type WeeklyActivityData struct {
	CurrentWeekDurations  [7]time.Duration
	PreviousWeekDurations [7]time.Duration
	CurrentWeekTotal      time.Duration
	MaxDuration           time.Duration
	StartOfWeek           time.Time
}

func BuildMonthData(activities []models.Activity, year int, month time.Month, now time.Time) MonthData {
	monthReports := make(map[int]*models.Report)
	dailyReports := make(map[string]*models.Report)

	for _, act := range activities {
		for _, dailyAct := range SplitActivityByDay(act, now) {
			activityDate := time.Date(
				dailyAct.StartTime.Year(), dailyAct.StartTime.Month(), dailyAct.StartTime.Day(),
				0, 0, 0, 0, time.Local,
			)

			dailyKey := DateKey(activityDate)
			dailyReport, ok := dailyReports[dailyKey]
			if !ok {
				dailyReport = newDailyReport()
				dailyReports[dailyKey] = dailyReport
			}
			addActivityToReport(dailyReport, dailyAct)

			if activityDate.Year() == year && activityDate.Month() == month {
				monthReport, exists := monthReports[activityDate.Day()]
				if !exists {
					monthReport = newDailyReport()
					monthReports[activityDate.Day()] = monthReport
				}
				addActivityToReport(monthReport, dailyAct)
			}
		}
	}

	return MonthData{MonthReports: monthReports, DailyReports: dailyReports}
}

func ComputeProductivityStats(monthReports map[int]*models.Report, daysInMonth int) ProductivityStats {
	stats := ProductivityStats{}
	currentStreak := 0

	for day := 1; day <= daysInMonth; day++ {
		dur := time.Duration(0)
		if report, ok := monthReports[day]; ok {
			dur = report.TotalDuration
		}

		if dur > 0 {
			stats.ActiveDays++
			stats.TotalDuration += dur
			if dur > stats.MaxDailyDuration {
				stats.MaxDailyDuration = dur
			}
			currentStreak++
			continue
		}

		if currentStreak > stats.LongestStreak {
			stats.LongestStreak = currentStreak
		}
		currentStreak = 0
	}

	if currentStreak > stats.LongestStreak {
		stats.LongestStreak = currentStreak
	}

	if stats.ActiveDays > 0 {
		stats.AvgDuration = stats.TotalDuration / time.Duration(stats.ActiveDays)
	}

	return stats
}

func AggregateProjectDurations(monthReports map[int]*models.Report) []ProjectDuration {
	projectDurations := make(map[string]time.Duration)
	for _, report := range monthReports {
		for project, projectReport := range report.ByProject {
			projectDurations[project] += projectReport.Duration
		}
	}

	projects := make([]ProjectDuration, 0, len(projectDurations))
	for project, duration := range projectDurations {
		projects = append(projects, ProjectDuration{Name: project, Duration: duration})
	}

	sort.Slice(projects, func(i, j int) bool {
		return projects[i].Duration > projects[j].Duration
	})

	return projects
}

func BuildWeeklyActivityData(dailyReports map[string]*models.Report, currentDate time.Time) WeeklyActivityData {
	weekday := int(currentDate.Weekday())
	if weekday == 0 {
		weekday = 7
	}

	startOfWeek := time.Date(
		currentDate.Year(), currentDate.Month(), currentDate.Day(),
		0, 0, 0, 0, time.Local,
	).AddDate(0, 0, -weekday+1)
	startOfPrevWeek := startOfWeek.AddDate(0, 0, -7)

	data := WeeklyActivityData{StartOfWeek: startOfWeek}
	for i := range 7 {
		day := startOfWeek.AddDate(0, 0, i)
		currentDuration := totalDurationForDate(dailyReports, day)
		data.CurrentWeekDurations[i] = currentDuration
		data.CurrentWeekTotal += currentDuration
		if currentDuration > data.MaxDuration {
			data.MaxDuration = currentDuration
		}

		prevDay := startOfPrevWeek.AddDate(0, 0, i)
		previousDuration := totalDurationForDate(dailyReports, prevDay)
		data.PreviousWeekDurations[i] = previousDuration
		if previousDuration > data.MaxDuration {
			data.MaxDuration = previousDuration
		}
	}

	return data
}

func SplitActivityByDay(act models.Activity, now time.Time) []models.Activity {
	segmentEnd := now
	if act.EndTime != nil {
		segmentEnd = *act.EndTime
	}
	if !segmentEnd.After(act.StartTime) {
		return nil
	}

	segmentStart := act.StartTime
	dayStart := time.Date(segmentStart.Year(), segmentStart.Month(), segmentStart.Day(), 0, 0, 0, 0, time.Local)
	segments := make([]models.Activity, 0, 1)

	for segmentStart.Before(segmentEnd) {
		nextDayStart := dayStart.AddDate(0, 0, 1)
		currentEnd := segmentEnd
		if nextDayStart.Before(currentEnd) {
			currentEnd = nextDayStart
		}

		segment := act
		segment.StartTime = segmentStart
		if act.EndTime == nil && currentEnd.Equal(segmentEnd) {
			segment.EndTime = nil
		} else {
			clippedEnd := currentEnd
			segment.EndTime = &clippedEnd
		}
		segments = append(segments, segment)

		segmentStart = currentEnd
		dayStart = nextDayStart
	}

	return segments
}

func DateKey(date time.Time) string {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.Local).Format(time.DateOnly)
}

func newDailyReport() *models.Report {
	return &models.Report{
		Activities: []models.Activity{},
		ByProject:  make(map[string]models.ProjectReport),
	}
}

func addActivityToReport(report *models.Report, act models.Activity) {
	report.Activities = append(report.Activities, act)
	dur := act.Duration()
	report.TotalDuration += dur

	projectReport, ok := report.ByProject[act.Project]
	if !ok {
		projectReport = models.ProjectReport{ProjectName: act.Project, Activities: []models.Activity{}}
	}
	projectReport.Duration += dur
	projectReport.Activities = append(projectReport.Activities, act)
	report.ByProject[act.Project] = projectReport
}

func totalDurationForDate(dailyReports map[string]*models.Report, date time.Time) time.Duration {
	report, ok := dailyReports[DateKey(date)]
	if !ok || report == nil {
		return 0
	}
	return report.TotalDuration
}