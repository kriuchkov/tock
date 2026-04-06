package insights

import (
	"sort"
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
)

type Stats struct {
	TotalDuration      time.Duration
	DeepWorkDuration   time.Duration
	DeepWorkScore      float64
	ContextSwitches    int
	AvgSwitchesPerDay  float64
	Chronotype         string
	PeakHour           int
	FocusDistribution  map[string]int
	MostProductiveDay  string
	AvgSessionDuration time.Duration
}

const (
	FocusDistributionDeep       = "deep"
	FocusDistributionFlow       = "flow"
	FocusDistributionFragmented = "fragmented"
)

func AnalyzeActivities(activities []models.Activity) Stats {
	accumulator := newStatsAccumulator()
	for _, activity := range sortActivitiesByStart(activities) {
		accumulator.observe(activity)
	}
	accumulator.finalize(len(activities))
	return accumulator.stats
}

type statsAccumulator struct {
	stats              Stats
	hourlyDistribution map[int]time.Duration
	dailyDuration      map[string]time.Duration
	switchesPerDay     map[string]int
	lastProject        string
	lastDate           string
}

func newStatsAccumulator() *statsAccumulator {
	return &statsAccumulator{
		stats:              Stats{FocusDistribution: make(map[string]int)},
		hourlyDistribution: make(map[int]time.Duration),
		dailyDuration:      make(map[string]time.Duration),
		switchesPerDay:     make(map[string]int),
	}
}

func sortActivitiesByStart(activities []models.Activity) []models.Activity {
	sorted := make([]models.Activity, len(activities))
	copy(sorted, activities)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartTime.Before(sorted[j].StartTime)
	})
	return sorted
}

func (acc *statsAccumulator) observe(activity models.Activity) {
	duration := activity.Duration()
	acc.stats.TotalDuration += duration
	acc.observeFocus(duration)
	acc.hourlyDistribution[activity.StartTime.Hour()] += duration
	acc.observeContextSwitch(activity)
	acc.dailyDuration[activity.StartTime.Weekday().String()] += duration
}

func (acc *statsAccumulator) observeFocus(duration time.Duration) {
	switch {
	case duration >= time.Hour:
		acc.stats.DeepWorkDuration += duration
		acc.stats.FocusDistribution[FocusDistributionDeep]++
	case duration >= 15*time.Minute:
		acc.stats.FocusDistribution[FocusDistributionFlow]++
	default:
		acc.stats.FocusDistribution[FocusDistributionFragmented]++
	}
}

func (acc *statsAccumulator) observeContextSwitch(activity models.Activity) {
	dateKey := activity.StartTime.Format(time.DateOnly)
	if dateKey != acc.lastDate {
		acc.lastProject = ""
		acc.lastDate = dateKey
	}

	if acc.lastProject != "" && activity.Project != acc.lastProject {
		acc.stats.ContextSwitches++
		acc.switchesPerDay[dateKey]++
	}

	acc.lastProject = activity.Project
}

func (acc *statsAccumulator) finalize(activityCount int) {
	acc.finalizeDurations(activityCount)
	acc.stats.PeakHour = peakHour(acc.hourlyDistribution)
	acc.stats.Chronotype = determineChronotype(acc.hourlyDistribution)
	acc.stats.MostProductiveDay = mostProductiveDay(acc.dailyDuration)
}

func (acc *statsAccumulator) finalizeDurations(activityCount int) {
	if acc.stats.TotalDuration > 0 && activityCount > 0 {
		acc.stats.DeepWorkScore = float64(acc.stats.DeepWorkDuration) / float64(acc.stats.TotalDuration) * 100
		acc.stats.AvgSessionDuration = acc.stats.TotalDuration / time.Duration(activityCount)
	}

	activeDays := len(acc.switchesPerDay)
	if activeDays == 0 {
		activeDays = 1
	}
	acc.stats.AvgSwitchesPerDay = float64(acc.stats.ContextSwitches) / float64(activeDays)
}

func peakHour(hourlyDistribution map[int]time.Duration) int {
	var (
		peak       int
		maxHourDur time.Duration
	)

	for hour, duration := range hourlyDistribution {
		if duration > maxHourDur {
			maxHourDur = duration
			peak = hour
		}
	}

	return peak
}

func determineChronotype(hourlyDistribution map[int]time.Duration) string {
	periods := map[string]time.Duration{
		"Morning Lark 🐦":     0,
		"Afternoon Power 🔋":  0,
		"Evening Sprinter 🏃": 0,
		"Night Owl 🦉":        0,
	}

	for hour, duration := range hourlyDistribution {
		switch {
		case hour >= 5 && hour < 12:
			periods["Morning Lark 🐦"] += duration
		case hour >= 12 && hour < 18:
			periods["Afternoon Power 🔋"] += duration
		case hour >= 18 && hour < 23:
			periods["Evening Sprinter 🏃"] += duration
		default:
			periods["Night Owl 🦉"] += duration
		}
	}

	chronotype := "Morning Lark 🐦"
	maxDuration := periods[chronotype]
	for _, candidate := range []string{"Afternoon Power 🔋", "Evening Sprinter 🏃", "Night Owl 🦉"} {
		if periods[candidate] > maxDuration {
			chronotype = candidate
			maxDuration = periods[candidate]
		}
	}

	return chronotype
}

func mostProductiveDay(dailyDuration map[string]time.Duration) string {
	var (
		bestDay     string
		maxDuration time.Duration
	)

	for day, duration := range dailyDuration {
		if duration > maxDuration {
			bestDay = day
			maxDuration = duration
		}
	}

	return bestDay
}
