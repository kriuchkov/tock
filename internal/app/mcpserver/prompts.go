package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kriuchkov/tock/internal/app/insights"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// standupLookback is how far back the standup looks for the last day with tracked time.
const standupLookback = 7

const standupInstructions = `Write my standup update from the tracked time below. Keep it short, grouped by
project: what I did on the previous working day, what I am on today, and blockers (overdue reminders), if any.
Merge entries of the same task; don't list times unless they matter.`

const weeklyReviewInstructions = `Review my week from the tracked time below: total time and its split by
project, how focused the work was (deep work, context switches), anything that stands out, and one or two
concrete suggestions for next week. Be brief and specific.`

func (s *server) addPrompts() {
	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "standup",
		Title:       "Standup",
		Description: "Draft a standup update from the previous working day and today.",
	}, s.standup)

	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "weekly-review",
		Title:       "Weekly review",
		Description: "Review a week of tracked time.",
		Arguments: []*mcp.PromptArgument{{
			Name:        "week",
			Description: `"this" (default) or "last"`,
		}},
	}, s.weeklyReview)
}

func (s *server) standup(ctx context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.calls.Lock()
	defer s.calls.Unlock()

	if err := s.reconcile(ctx); err != nil {
		return nil, err
	}

	now := s.opts.Now()
	today, tomorrow := timeutil.LocalDayBounds(now)
	data := map[string]any{"clock": s.clock()}

	previous, err := s.lastWorkedDay(ctx, today)
	if err != nil {
		return nil, err
	}
	if !previous.IsZero() {
		end := previous.AddDate(0, 0, 1)
		if data["previous_day"], err = s.periodActivities(ctx, previous, end); err != nil {
			return nil, err
		}
	}
	if data["today"], err = s.periodActivities(ctx, today, tomorrow); err != nil {
		return nil, err
	}
	if s.opts.Reminders != nil {
		overdue, overdueErr := s.listOccurrences(ctx, RemindersInput{OverdueOnly: true})
		if overdueErr != nil {
			return nil, overdueErr
		}
		data["overdue_reminders"] = s.occurrences(overdue)
	}
	return promptResult("Standup draft", standupInstructions, data)
}

// lastWorkedDay is the latest day before today with tracked time, within the lookback; zero if none.
func (s *server) lastWorkedDay(ctx context.Context, today time.Time) (time.Time, error) {
	from := today.AddDate(0, 0, -standupLookback)
	activities, err := s.opts.Activities.List(ctx, models.ActivityFilter{FromDate: &from, ToDate: &today})
	if err != nil {
		return time.Time{}, errors.Wrap(err, "list activities")
	}

	var latest time.Time
	for _, activity := range activities {
		if activity.StartTime.Before(today) && activity.StartTime.After(latest) {
			latest = activity.StartTime
		}
	}
	if latest.IsZero() {
		return time.Time{}, nil
	}
	day, _ := timeutil.LocalDayBounds(latest)
	return day, nil
}

func (s *server) weeklyReview(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	s.calls.Lock()
	defer s.calls.Unlock()

	if err := s.reconcile(ctx); err != nil {
		return nil, err
	}

	from, to := timeutil.LocalWeekBounds(s.opts.Now())
	switch week := strings.ToLower(strings.TrimSpace(req.Params.Arguments["week"])); week {
	case "", "this":
	case "last":
		from, to = from.AddDate(0, 0, -7), from
	default:
		return nil, errors.Errorf(`unknown week %q (use "this" or "last")`, week)
	}

	report, err := s.opts.Activities.GetReport(ctx, models.ActivityFilter{FromDate: &from, ToDate: &to})
	if err != nil {
		return nil, errors.Wrap(err, "build report")
	}

	projects := make(map[string]int, len(report.ByProject))
	for name, project := range report.ByProject {
		projects[name] = minutes(project.Duration)
	}
	stats := insights.AnalyzeActivities(report.Activities)
	data := map[string]any{
		"clock":               s.clock(),
		"from":                from.Format(time.DateOnly),
		"to":                  to.AddDate(0, 0, -1).Format(time.DateOnly),
		"total_minutes":       minutes(report.TotalDuration),
		"minutes_by_project":  projects,
		"activities":          s.reportActivities(report, nil),
		"deep_work_percent":   stats.DeepWorkScore,
		"context_switches":    stats.ContextSwitches,
		"avg_session_minutes": minutes(stats.AvgSessionDuration),
		"most_productive_day": stats.MostProductiveDay,
	}
	return promptResult("Weekly review", weeklyReviewInstructions, data)
}

func (s *server) periodActivities(ctx context.Context, from, to time.Time) ([]Activity, error) {
	report, err := s.opts.Activities.GetReport(ctx, models.ActivityFilter{FromDate: &from, ToDate: &to})
	if err != nil {
		return nil, errors.Wrap(err, "build report")
	}
	return s.reportActivities(report, nil), nil
}

func (s *server) occurrences(occurrences []models.ReminderOccurrence) []Occurrence {
	out := make([]Occurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		out = append(out, s.occurrence(occurrence))
	}
	return out
}

func promptResult(description, instructions string, data any) (*mcp.GetPromptResult, error) {
	payload, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return nil, errors.Wrap(err, "encode prompt data")
	}
	return &mcp.GetPromptResult{
		Description: description,
		Messages: []*mcp.PromptMessage{{
			Role:    "user",
			Content: &mcp.TextContent{Text: instructions + "\n\n```json\n" + string(payload) + "\n```"},
		}},
	}, nil
}
