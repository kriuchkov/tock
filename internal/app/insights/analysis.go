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
	stats := Stats{
		FocusDistribution: make(map[string]int),
	}

	hourlyDistribution := make(map[int]time.Duration)
	dailyDuration := make(map[string]time.Duration)
	switchesPerDay := make(map[string]int)

	sorted := make([]models.Activity, len(activities))
	copy(sorted, activities)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartTime.Before(sorted[j].StartTime)
	})

	var lastProject string
	var lastDate string

	for _, act := range sorted {
		dur := act.Duration()
		stats.TotalDuration += dur

		switch {
		case dur.Minutes() >= 60:
			stats.DeepWorkDuration += dur
			stats.FocusDistribution[FocusDistributionDeep]++
		case dur.Minutes() >= 15:
			stats.FocusDistribution[FocusDistributionFlow]++
		default:
			stats.FocusDistribution[FocusDistributionFragmented]++
		}

		startHour := act.StartTime.Hour()
		hourlyDistribution[startHour] += dur

		dateStr := act.StartTime.Format(time.DateOnly)
		if dateStr != lastDate {
			lastProject = ""
			lastDate = dateStr
		}

		if lastProject != "" && act.Project != lastProject {
			stats.ContextSwitches++
			switchesPerDay[dateStr]++
		}
		lastProject = act.Project

		weekday := act.StartTime.Weekday().String()
		dailyDuration[weekday] += dur
	}

	if stats.TotalDuration > 0 {
		stats.DeepWorkScore = (float64(stats.DeepWorkDuration) / float64(stats.TotalDuration)) * 100
		stats.AvgSessionDuration = stats.TotalDuration / time.Duration(len(sorted))
	}

	activeDays := len(switchesPerDay)
	if activeDays == 0 {
		activeDays = 1
	}
	stats.AvgSwitchesPerDay = float64(stats.ContextSwitches) / float64(activeDays)

	var maxHourDur time.Duration
	for hour, dur := range hourlyDistribution {
		if dur > maxHourDur {
			maxHourDur = dur
			stats.PeakHour = hour
		}
	}

	var morning, afternoon, evening, night time.Duration
	for hour, dur := range hourlyDistribution {
		switch {
		case hour >= 5 && hour < 12:
			morning += dur
		case hour >= 12 && hour < 18:
			afternoon += dur
		case hour >= 18 && hour < 23:
			evening += dur
		default:
			night += dur
		}
	}

	maxPeriod := morning
	stats.Chronotype = "Morning Lark 🐦"
	if afternoon > maxPeriod {
		maxPeriod = afternoon
		stats.Chronotype = "Afternoon Power 🔋"
	}
	if evening > maxPeriod {
		maxPeriod = evening
		stats.Chronotype = "Evening Sprinter 🏃"
	}
	if night > maxPeriod {
		stats.Chronotype = "Night Owl 🦉"
	}

	var maxDayDur time.Duration
	for day, dur := range dailyDuration {
		if dur > maxDayDur {
			maxDayDur = dur
			stats.MostProductiveDay = day
		}
	}

	return stats
}