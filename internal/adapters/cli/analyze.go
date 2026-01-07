package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
)

func NewAnalyzeCmd() *cobra.Command {
	var (
		days int
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze your productivity patterns",
		Long:  "Generate a scientific analysis of your work habits, including deep work score, context switching, and chronotype estimation.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := getService(cmd)
			ctx := context.Background()

			// Default to last 30 days if not specified
			if days <= 0 {
				days = 30
			}

			end := time.Now()
			start := end.AddDate(0, 0, -days)

			filter := dto.ActivityFilter{
				FromDate: &start,
				ToDate:   &end,
			}

			report, err := service.GetReport(ctx, filter)
			if err != nil {
				return errors.Wrap(err, "generate report")
			}

			if len(report.Activities) == 0 {
				fmt.Println("No activities found for analysis.")
				return nil
			}

			analysis := analyzeData(report.Activities)
			cfg := getConfig(cmd)
			renderAnalysis(analysis, cfg)

			return nil
		},
	}

	cmd.Flags().IntVarP(&days, "days", "n", 30, "Number of days to analyze")

	return cmd
}

type AnalysisStats struct {
	TotalDuration      time.Duration
	DeepWorkDuration   time.Duration
	DeepWorkScore      float64 // 0-100
	ContextSwitches    int
	AvgSwitchesPerDay  float64
	Chronotype         string // "Morning Lark", "Night Owl", etc.
	PeakHour           int
	FocusDistribution  map[string]int // "Short (<15m)", "Medium (15-60m)", "Long (>60m)"
	MostProductiveDay  string
	AvgSessionDuration time.Duration
}

//nolint:funlen // analysis function is inherently long
func analyzeData(activities []models.Activity) AnalysisStats {
	stats := AnalysisStats{
		FocusDistribution: make(map[string]int),
	}

	hourlyDistribution := make(map[int]time.Duration)
	dailyDuration := make(map[string]time.Duration)
	switchesPerDay := make(map[string]int)

	// Sort activities by time
	sort.Slice(activities, func(i, j int) bool {
		return activities[i].StartTime.Before(activities[j].StartTime)
	})

	var lastProject string
	var lastDate string

	for _, act := range activities {
		dur := act.Duration()
		stats.TotalDuration += dur

		// Deep Work Analysis (> 60 mins)
		//nolint:gocritic // ignore if-else chain for clarity
		if dur.Minutes() >= 60 {
			stats.DeepWorkDuration += dur
			stats.FocusDistribution["Deep Focus (>1h)"]++
		} else if dur.Minutes() >= 15 {
			stats.FocusDistribution["Flow (15m-1h)"]++
		} else {
			stats.FocusDistribution["Fragmented (<15m)"]++
		}

		// Chronotype Analysis
		startHour := act.StartTime.Hour()
		hourlyDistribution[startHour] += dur

		// Also distribute duration across hours if it spans multiple
		// (Simplified: just attributing to start hour for peak detection to avoid complex splitting logic here,
		// or we could reuse the logic from calendar_sidebar)

		// Context Switching
		dateStr := act.StartTime.Format("2006-01-02")
		if dateStr != lastDate {
			lastProject = "" // Reset for new day
			lastDate = dateStr
		}

		if lastProject != "" && act.Project != lastProject {
			stats.ContextSwitches++
			switchesPerDay[dateStr]++
		}
		lastProject = act.Project

		// Daily stats
		weekday := act.StartTime.Weekday().String()
		dailyDuration[weekday] += dur
	}

	// Calculate Averages
	if stats.TotalDuration > 0 {
		stats.DeepWorkScore = (float64(stats.DeepWorkDuration) / float64(stats.TotalDuration)) * 100
		stats.AvgSessionDuration = stats.TotalDuration / time.Duration(len(activities))
	}

	activeDays := len(switchesPerDay)
	if activeDays == 0 {
		activeDays = 1 // Avoid division by zero
	}
	stats.AvgSwitchesPerDay = float64(stats.ContextSwitches) / float64(activeDays)

	// Determine Peak Hour
	var maxHourDur time.Duration
	for h, dur := range hourlyDistribution {
		if dur > maxHourDur {
			maxHourDur = dur
			stats.PeakHour = h
		}
	}

	// Determine Chronotype
	// Morning: 5-11, Afternoon: 12-17, Evening: 18-22, Night: 23-4
	var morning, afternoon, evening, night time.Duration
	for h, dur := range hourlyDistribution {
		switch {
		case h >= 5 && h < 12:
			morning += dur
		case h >= 12 && h < 18:
			afternoon += dur
		case h >= 18 && h < 23:
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

	// Most Productive Day
	var maxDayDur time.Duration
	for d, dur := range dailyDuration {
		if dur > maxDayDur {
			maxDayDur = dur
			stats.MostProductiveDay = d
		}
	}

	return stats
}

func renderAnalysis(stats AnalysisStats, cfg *config.Config) {
	theme := GetTheme(cfg.Theme)

	// Custom styles for analysis
	titleStyle := lipgloss.NewStyle().
		Foreground(theme.Highlight).
		Bold(true).
		Padding(1, 0).
		Border(lipgloss.DoubleBorder(), false, false, true, false).
		BorderForeground(theme.Faint).
		Width(60).
		Align(lipgloss.Center)

	sectionStyle := lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true).
		MarginTop(1)

	valueStyle := lipgloss.NewStyle().
		Foreground(theme.Text).
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Foreground(theme.SubText).
		Width(25)

	fmt.Println(titleStyle.Render("🧠 Productivity Analysis"))

	// 1. Deep Work Score
	fmt.Println(sectionStyle.Render("Focus Quality"))

	scoreColor := theme.Secondary // Red (bad)
	if stats.DeepWorkScore > 70 {
		scoreColor = lipgloss.Color("76") // Green
	} else if stats.DeepWorkScore > 40 {
		scoreColor = theme.Highlight // Yellow/Orange
	}

	//nolint:wastedassign // scoreBar is used below
	scoreBar := ""
	width := int(stats.DeepWorkScore / 2) // Scale to 50 chars
	scoreBar = strings.Repeat("█", width) + strings.Repeat("░", 50-width)

	fmt.Printf("%s %s\n",
		labelStyle.Render("Deep Work Score:"),
		lipgloss.NewStyle().Foreground(scoreColor).Render(fmt.Sprintf("%.1f%%", stats.DeepWorkScore)))
	fmt.Println(lipgloss.NewStyle().Foreground(scoreColor).Render(scoreBar))

	fmt.Printf("%s %s\n", labelStyle.Render("Avg Session:"), valueStyle.Render(stats.AvgSessionDuration.Round(time.Minute).String()))
	fmt.Println()

	// 2. Chronotype
	fmt.Println(sectionStyle.Render("Chronotype & Rhythm"))
	fmt.Printf("%s %s\n", labelStyle.Render("Type:"), valueStyle.Render(stats.Chronotype))
	fmt.Printf(
		"%s %s:00 - %02d:00\n",
		labelStyle.Render("Peak Hour:"),
		valueStyle.Render(fmt.Sprintf("%02d", stats.PeakHour)),
		stats.PeakHour+1,
	)
	fmt.Printf("%s %s\n", labelStyle.Render("Best Day:"), valueStyle.Render(stats.MostProductiveDay))
	fmt.Println()

	// 3. Context Switching
	fmt.Println(sectionStyle.Render("Context Switching"))
	fmt.Printf("%s %s\n", labelStyle.Render("Avg Switches/Day:"), valueStyle.Render(fmt.Sprintf("%.1f", stats.AvgSwitchesPerDay)))

	switchMsg := "Excellent focus! 🎯"
	if stats.AvgSwitchesPerDay > 10 {
		switchMsg = "High fragmentation 🧩"
	} else if stats.AvgSwitchesPerDay > 5 {
		switchMsg = "Moderate switching ⚖️"
	}
	fmt.Printf("%s %s\n", labelStyle.Render("Verdict:"), lipgloss.NewStyle().Foreground(theme.SubText).Render(switchMsg))
	fmt.Println()

	// 4. Session Distribution
	fmt.Println(sectionStyle.Render("Session Distribution"))

	// Find max for scaling
	maxCount := 0
	for _, count := range stats.FocusDistribution {
		if count > maxCount {
			maxCount = count
		}
	}

	keys := []string{"Fragmented (<15m)", "Flow (15m-1h)", "Deep Focus (>1h)"}
	for _, k := range keys {
		count := stats.FocusDistribution[k]
		barLen := 0
		if maxCount > 0 {
			barLen = int((float64(count) / float64(maxCount)) * 40)
		}
		bar := strings.Repeat("█", barLen)

		color := theme.Faint
		switch k {
		case "Deep Focus (>1h)":
			color = lipgloss.Color("76") // Green
		case "Flow (15m-1h)":
			color = theme.Primary
		}

		fmt.Printf("%s %s %d\n",
			lipgloss.NewStyle().Foreground(theme.SubText).Width(20).Render(k),
			lipgloss.NewStyle().Foreground(color).Render(bar),
			count,
		)
	}
	fmt.Println()
}
