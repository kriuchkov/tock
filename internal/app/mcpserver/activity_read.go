package mcpserver

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/go-faster/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kriuchkov/tock/internal/app/insights"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	defaultRecentLimit = 10
	defaultAnalyzeDays = 30
	maxRecentLimit     = 100
	maxAnalyzeDays     = 366
	periodToday        = "today"
	periodYesterday    = "yesterday"
	periodWeek         = "week"
	periodMonth        = "month"
	periodQuarter      = "quarter"
	periodYear         = "year"
	periodDescription  = "today, yesterday, week, month, quarter or year"
)

// PeriodInput picks the activities a tool looks at.
type PeriodInput struct {
	Period      string `json:"period,omitempty"      jsonschema:"today, yesterday, week, month, quarter or year (the current one); today when no date is given"`
	Date        string `json:"date,omitempty"        jsonschema:"a single day, YYYY-MM-DD"`
	From        string `json:"from,omitempty"        jsonschema:"first day, YYYY-MM-DD"`
	To          string `json:"to,omitempty"          jsonschema:"last day, YYYY-MM-DD, included"`
	Project     string `json:"project,omitempty"     jsonschema:"only this project"`
	Description string `json:"description,omitempty" jsonschema:"only activities with this description"`
}

func (s *server) filter(in PeriodInput) (models.ActivityFilter, error) {
	opts := models.ActivityFilterOptions{
		Now:         s.opts.Now(),
		Date:        strings.TrimSpace(in.Date),
		From:        strings.TrimSpace(in.From),
		To:          strings.TrimSpace(in.To),
		Project:     strings.TrimSpace(in.Project),
		Description: strings.TrimSpace(in.Description),
	}

	period := strings.ToLower(strings.TrimSpace(in.Period))
	if period == "" && opts.Date == "" && opts.From == "" && opts.To == "" {
		period = periodToday
	}
	switch period {
	case "":
	case periodToday:
		opts.Today = true
	case periodYesterday:
		opts.Yesterday = true
	case periodWeek:
		opts.Week = true
	case periodMonth:
		opts.Month = true
	case periodQuarter:
		opts.Quarter = true
	case periodYear:
		opts.Year = true
	default:
		return models.ActivityFilter{}, errors.Errorf("unknown period %q (use %s)", in.Period, periodDescription)
	}

	filter, err := models.BuildActivityFilter(opts)
	if err != nil {
		return models.ActivityFilter{}, errors.Wrap(err, "build filter")
	}
	return filter, nil
}

func (s *server) report(ctx context.Context, in PeriodInput) (*models.Report, error) {
	filter, err := s.filter(in)
	if err != nil {
		return nil, err
	}
	report, err := s.opts.Activities.GetReport(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "build report")
	}
	return report, nil
}

func (s *server) reportActivities(report *models.Report, project *string) []Activity {
	activities := make([]Activity, 0, len(report.Activities))
	for i, activity := range report.Activities {
		if project != nil && activity.Project != *project {
			continue
		}
		key := ""
		if i < len(report.Keys) {
			key = report.Keys[i]
		}
		activities = append(activities, s.activity(activity, key))
	}
	return activities
}

// keyOf finds the DATE-INDEX key of a; it is empty when the lookup fails, which only costs the client a list call.
func (s *server) keyOf(ctx context.Context, a models.Activity) string {
	from, to := timeutil.LocalDayBounds(a.StartTime)
	all, err := s.opts.Activities.List(ctx, models.ActivityFilter{FromDate: &from, ToDate: &to})
	if err != nil {
		return ""
	}
	return models.DayKeys(all, []models.Activity{a})[0]
}

func (s *server) keyed(ctx context.Context, a *models.Activity) *Activity {
	if a == nil {
		return nil
	}
	out := s.activity(*a, s.keyOf(ctx, *a))
	return &out
}

// running returns the latest running activity, or nil.
func (s *server) running(ctx context.Context) (*models.Activity, error) {
	isRunning := true
	running, err := s.opts.Activities.List(ctx, models.ActivityFilter{IsRunning: &isRunning})
	if err != nil {
		return nil, errors.Wrap(err, "find running activity")
	}
	if len(running) == 0 {
		return nil, nil //nolint:nilnil // nothing running is not an error
	}
	latest := slices.MaxFunc(running, func(a, b models.Activity) int { return a.StartTime.Compare(b.StartTime) })
	return &latest, nil
}

// CurrentOutput is what runs now.
type CurrentOutput struct {
	Clock

	Running *Activity `json:"running,omitempty" jsonschema:"the running activity; absent when nothing runs"`
	Last    *Activity `json:"last,omitempty"    jsonschema:"the latest activity, when nothing runs"`
}

// ListOutput holds the activities of a period.
type ListOutput struct {
	Clock

	Activities   []Activity `json:"activities"`
	TotalMinutes int        `json:"total_minutes" jsonschema:"tracked time, clipped to the period"`
}

// ReportInput picks a period and how much to return.
type ReportInput struct {
	PeriodInput

	Details bool `json:"details,omitempty" jsonschema:"also list each project's activities"`
}

// ProjectTotal is the time spent on one project.
type ProjectTotal struct {
	Project    string     `json:"project"`
	Minutes    int        `json:"minutes"`
	Activities []Activity `json:"activities,omitempty"`
}

// ReportOutput holds the time per project.
type ReportOutput struct {
	Clock

	TotalMinutes int            `json:"total_minutes"`
	Projects     []ProjectTotal `json:"projects"      jsonschema:"projects, most time first"`
}

// RecentInput limits the recent activities.
type RecentInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"how many, 10 by default"`
}

// RecentActivity is an activity continue can start again.
type RecentActivity struct {
	Activity

	Index int `json:"index" jsonschema:"what continue takes; 0 is the latest"`
}

// RecentOutput lists distinct recent activities.
type RecentOutput struct {
	Clock

	Activities []RecentActivity `json:"activities"`
}

// AnalyzeInput picks the days to analyze.
type AnalyzeInput struct {
	Days int `json:"days,omitempty" jsonschema:"how many days back from now, 30 by default"`
}

// AnalyzeOutput holds work-pattern statistics.
type AnalyzeOutput struct {
	Clock

	Days                int            `json:"days"`
	Activities          int            `json:"activities"`
	TotalMinutes        int            `json:"total_minutes"`
	DeepWorkMinutes     int            `json:"deep_work_minutes"`
	DeepWorkPercent     float64        `json:"deep_work_percent"`
	ContextSwitches     int            `json:"context_switches"`
	AvgSwitchesPerDay   float64        `json:"avg_switches_per_day"`
	AvgSessionMinutes   int            `json:"avg_session_minutes"`
	PeakHour            int            `json:"peak_hour"            jsonschema:"hour of day with the most tracked time"`
	MostProductiveDay   string         `json:"most_productive_day"`
	Chronotype          string         `json:"chronotype"`
	SessionDistribution map[string]int `json:"session_distribution" jsonschema:"sessions per focus kind: deep, flow, fragmented"`
}

func (s *server) addActivityReadTools() {
	addTool(s, readAccess, &mcp.Tool{
		Name:        "current",
		Title:       "Current activity",
		Description: "Show the running activity, or the latest one when nothing runs.",
	}, s.current)

	addTool(s, readAccess, &mcp.Tool{
		Name:        "list",
		Title:       "List activities",
		Description: "List the activities of a period with their keys. Activities crossing the period's edges are clipped to it.",
	}, s.list)

	addTool(s, readAccess, &mcp.Tool{
		Name:        "report",
		Title:       "Time report",
		Description: "Sum the time of a period per project.",
	}, s.reportTool)

	addTool(s, readAccess, &mcp.Tool{
		Name:        "recent",
		Title:       "Recent activities",
		Description: "List distinct recent activities, newest first, with the index continue takes.",
	}, s.recent)

	addTool(s, readAccess, &mcp.Tool{
		Name:        "analyze",
		Title:       "Analyze work patterns",
		Description: "Work-pattern statistics over the last days: deep work, context switches, peak hour.",
	}, s.analyze)
}

func (s *server) current(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, CurrentOutput, error) {
	out := CurrentOutput{Clock: s.clock()}
	running, err := s.running(ctx)
	if err != nil {
		return nil, out, err
	}
	if running != nil {
		out.Running = s.keyed(ctx, running)
		return nil, out, nil
	}

	last, err := s.opts.Activities.GetLast(ctx)
	if err != nil && !errors.Is(err, coreErrors.ErrActivityNotFound) {
		return nil, out, errors.Wrap(err, "find last activity")
	}
	out.Last = s.keyed(ctx, last)
	return nil, out, nil
}

func (s *server) list(ctx context.Context, _ *mcp.CallToolRequest, in PeriodInput) (*mcp.CallToolResult, ListOutput, error) {
	out := ListOutput{Clock: s.clock()}
	report, err := s.report(ctx, in)
	if err != nil {
		return nil, out, err
	}
	out.Activities = s.reportActivities(report, nil)
	out.TotalMinutes = minutes(report.TotalDuration)
	return nil, out, nil
}

func (s *server) reportTool(ctx context.Context, _ *mcp.CallToolRequest, in ReportInput) (*mcp.CallToolResult, ReportOutput, error) {
	out := ReportOutput{Clock: s.clock(), Projects: []ProjectTotal{}}
	report, err := s.report(ctx, in.PeriodInput)
	if err != nil {
		return nil, out, err
	}

	out.TotalMinutes = minutes(report.TotalDuration)
	for name, project := range report.ByProject {
		total := ProjectTotal{Project: name, Minutes: minutes(project.Duration)}
		if in.Details {
			total.Activities = s.reportActivities(report, &name)
		}
		out.Projects = append(out.Projects, total)
	}
	slices.SortFunc(out.Projects, func(a, b ProjectTotal) int {
		return cmp.Or(cmp.Compare(b.Minutes, a.Minutes), cmp.Compare(a.Project, b.Project))
	})
	return nil, out, nil
}

func (s *server) recent(ctx context.Context, _ *mcp.CallToolRequest, in RecentInput) (*mcp.CallToolResult, RecentOutput, error) {
	out := RecentOutput{Clock: s.clock(), Activities: []RecentActivity{}}
	limit := min(cmp.Or(max(in.Limit, 0), defaultRecentLimit), maxRecentLimit)
	recent, err := s.opts.Activities.GetRecent(ctx, limit)
	if err != nil {
		return nil, out, errors.Wrap(err, "get recent activities")
	}
	for i, activity := range recent {
		out.Activities = append(out.Activities, RecentActivity{Index: i, Activity: s.activity(activity, "")})
	}
	return nil, out, nil
}

func (s *server) analyze(ctx context.Context, _ *mcp.CallToolRequest, in AnalyzeInput) (*mcp.CallToolResult, AnalyzeOutput, error) {
	days := min(cmp.Or(max(in.Days, 0), defaultAnalyzeDays), maxAnalyzeDays)
	out := AnalyzeOutput{Clock: s.clock(), Days: days}

	now := s.opts.Now()
	from := now.AddDate(0, 0, -days)
	report, err := s.opts.Activities.GetReport(ctx, models.ActivityFilter{FromDate: &from, ToDate: &now})
	if err != nil {
		return nil, out, errors.Wrap(err, "build report")
	}

	stats := insights.AnalyzeActivities(report.Activities)
	out.Activities = len(report.Activities)
	out.TotalMinutes = minutes(stats.TotalDuration)
	out.DeepWorkMinutes = minutes(stats.DeepWorkDuration)
	out.DeepWorkPercent = stats.DeepWorkScore
	out.ContextSwitches = stats.ContextSwitches
	out.AvgSwitchesPerDay = stats.AvgSwitchesPerDay
	out.AvgSessionMinutes = minutes(stats.AvgSessionDuration)
	out.PeakHour = stats.PeakHour
	out.MostProductiveDay = stats.MostProductiveDay
	out.Chronotype = stats.Chronotype
	out.SessionDistribution = stats.FocusDistribution
	if out.SessionDistribution == nil {
		out.SessionDistribution = map[string]int{}
	}
	return nil, out, nil
}
