package commands

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/go-faster/errors"
	"github.com/spf13/cobra"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/config"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func NewAnalyzeCmd() *cobra.Command {
	var (
		days int
	)

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze your productivity patterns",
		Long:  defaultText("analyze.long"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAnalyzeCmd(cmd, days)
		},
	}

	cmd.Flags().IntVarP(&days, "days", "n", 30, defaultText("analyze.flag.days"))

	return cmd
}

func runAnalyzeCmd(cmd *cobra.Command, days int) error {
	rt := getRuntime(cmd)
	service := rt.ActivityService
	out := cmd.OutOrStdout()

	if days <= 0 {
		days = 30
	}

	end := time.Now()
	start := end.AddDate(0, 0, -days)
	filter := models.ActivityFilter{FromDate: &start, ToDate: &end}

	report, err := service.GetReport(cmd.Context(), filter)
	if err != nil {
		return errors.Wrap(err, "generate report")
	}

	if len(report.Activities) == 0 {
		fmt.Fprintln(out, text(cmd, "analyze.empty"))
		return nil
	}

	stats := insights.AnalyzeActivities(report.Activities)
	return renderAnalysis(out, stats, rt.Config, rt.TimeFormatter, getLocalizer(cmd))
}

//nolint:funlen // render function is inherently long for output formatting
func renderAnalysis(
	out io.Writer,
	stats insights.Stats,
	cfg *config.Config,
	tf *timeutil.Formatter,
	loc interface{ Format(string, ...any) string },
) error {
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

	fmt.Fprintln(out, titleStyle.Render(loc.Format("analyze.title")))

	// 1. Deep Work Score
	fmt.Fprintln(out, sectionStyle.Render(loc.Format("analyze.section.focus")))

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

	if _, err := fmt.Fprintf(out, "%s %s\n",
		labelStyle.Render(loc.Format("analyze.label.deep_work")),
		lipgloss.NewStyle().Foreground(scoreColor).Render(fmt.Sprintf("%.1f%%", stats.DeepWorkScore))); err != nil {
		return errors.Wrap(err, "write deep work score")
	}
	fmt.Fprintln(out, lipgloss.NewStyle().Foreground(scoreColor).Render(scoreBar))

	if _, err := fmt.Fprintf(
		out,
		"%s %s\n",
		labelStyle.Render(loc.Format("analyze.label.avg_session")),
		valueStyle.Render(stats.AvgSessionDuration.Round(time.Minute).String()),
	); err != nil {
		return errors.Wrap(err, "write average session")
	}
	fmt.Fprintln(out)

	// 2. Chronotype
	fmt.Fprintln(out, sectionStyle.Render(loc.Format("analyze.section.chronotype")))
	if _, err := fmt.Fprintf(
		out,
		"%s %s\n",
		labelStyle.Render(loc.Format("analyze.label.type")),
		valueStyle.Render(stats.Chronotype),
	); err != nil {
		return errors.Wrap(err, "write chronotype")
	}

	startTime := time.Date(2000, 1, 1, stats.PeakHour, 0, 0, 0, time.Local)
	endTime := time.Date(2000, 1, 1, stats.PeakHour+1, 0, 0, 0, time.Local)
	format := tf.GetDisplayFormat()
	if _, err := fmt.Fprintf(out, "%s %s - %s\n",
		labelStyle.Render(loc.Format("analyze.label.peak_hour")),
		valueStyle.Render(startTime.Format(format)),
		endTime.Format(format),
	); err != nil {
		return errors.Wrap(err, "write peak hour")
	}

	if _, err := fmt.Fprintf(
		out,
		"%s %s\n",
		labelStyle.Render(loc.Format("analyze.label.best_day")),
		valueStyle.Render(stats.MostProductiveDay),
	); err != nil {
		return errors.Wrap(err, "write best day")
	}
	fmt.Fprintln(out)

	// 3. Context Switching
	fmt.Fprintln(out, sectionStyle.Render(loc.Format("analyze.section.switching")))
	if _, err := fmt.Fprintf(
		out,
		"%s %s\n",
		labelStyle.Render(loc.Format("analyze.label.avg_switches")),
		valueStyle.Render(fmt.Sprintf("%.1f", stats.AvgSwitchesPerDay)),
	); err != nil {
		return errors.Wrap(err, "write context switches")
	}

	switchMsg := loc.Format("analyze.verdict.excellent")
	if stats.AvgSwitchesPerDay > 10 {
		switchMsg = loc.Format("analyze.verdict.high")
	} else if stats.AvgSwitchesPerDay > 5 {
		switchMsg = loc.Format("analyze.verdict.moderate")
	}
	if _, err := fmt.Fprintf(
		out,
		"%s %s\n",
		labelStyle.Render(loc.Format("analyze.label.verdict")),
		lipgloss.NewStyle().Foreground(theme.SubText).Render(switchMsg),
	); err != nil {
		return errors.Wrap(err, "write verdict")
	}
	fmt.Fprintln(out)

	// 4. Session Distribution
	fmt.Fprintln(out, sectionStyle.Render(loc.Format("analyze.section.distribution")))

	// Find max for scaling
	maxCount := 0
	for _, count := range stats.FocusDistribution {
		if count > maxCount {
			maxCount = count
		}
	}

	keys := []string{insights.FocusDistributionFragmented, insights.FocusDistributionFlow, insights.FocusDistributionDeep}
	for _, key := range keys {
		count := stats.FocusDistribution[key]
		barLen := 0
		if maxCount > 0 {
			barLen = int((float64(count) / float64(maxCount)) * 40)
		}
		bar := strings.Repeat("█", barLen)

		color := theme.Faint
		switch key {
		case insights.FocusDistributionDeep:
			color = lipgloss.Color("76") // Green
		case insights.FocusDistributionFlow:
			color = theme.Primary
		}

		label := localizeFocusDistributionLabel(loc, key)

		if _, err := fmt.Fprintf(out, "%s %s %d\n",
			lipgloss.NewStyle().Foreground(theme.SubText).Width(20).Render(label),
			lipgloss.NewStyle().Foreground(color).Render(bar),
			count,
		); err != nil {
			return errors.Wrap(err, "write distribution line")
		}
	}
	fmt.Fprintln(out)
	return nil
}

func localizeFocusDistributionLabel(loc interface{ Format(string, ...any) string }, key string) string {
	switch key {
	case insights.FocusDistributionFragmented:
		return loc.Format("analyze.dist.fragmented")
	case insights.FocusDistributionFlow:
		return loc.Format("analyze.dist.flow")
	case insights.FocusDistributionDeep:
		return loc.Format("analyze.dist.deep")
	default:
		return key
	}
}
