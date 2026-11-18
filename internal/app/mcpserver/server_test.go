package mcpserver_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/app/mcpserver"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
)

func fixedNow() time.Time {
	return time.Date(2026, time.November, 18, 15, 30, 0, 0, time.Local)
}

type harness struct {
	activities *portsmocks.MockActivityResolver
	reminders  *portsmocks.MockReminderResolver
	session    *mcp.ClientSession
}

func newHarness(t *testing.T, configure func(*mcpserver.Options)) *harness {
	t.Helper()
	h := &harness{
		activities: portsmocks.NewMockActivityResolver(t),
		reminders:  portsmocks.NewMockReminderResolver(t),
	}
	opts := mcpserver.Options{
		Activities: h.activities,
		Reminders:  h.reminders,
		Version:    "test",
		Now:        fixedNow,
	}
	if configure != nil {
		configure(&opts)
	}

	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := mcpserver.New(opts).Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	h.session, err = mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.session.Close() })
	return h
}

func (h *harness) call(t *testing.T, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := h.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return res
}

func decode[T any](t *testing.T, res *mcp.CallToolResult) T {
	t.Helper()
	require.False(t, res.IsError, "tool error: %v", errorText(res))
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var out T
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func errorText(res *mcp.CallToolResult) string {
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

func toolNames(t *testing.T, h *harness) map[string]*mcp.Tool {
	t.Helper()
	res, err := h.session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	tools := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		tools[tool.Name] = tool
	}
	return tools
}

func runningFilter() any {
	return mock.MatchedBy(func(f models.ActivityFilter) bool { return f.IsRunning != nil && *f.IsRunning })
}

func dayFilter() any {
	return mock.MatchedBy(func(f models.ActivityFilter) bool { return f.IsRunning == nil && f.FromDate != nil })
}

func TestToolsFollowOptions(t *testing.T) {
	all := toolNames(t, newHarness(t, nil))
	assert.Len(t, all, 19)
	require.Contains(t, all, "remove")
	assert.True(t, *all["remove"].Annotations.DestructiveHint)
	assert.False(t, *all["start"].Annotations.DestructiveHint)
	assert.True(t, all["list"].Annotations.ReadOnlyHint)

	readOnly := toolNames(t, newHarness(t, func(o *mcpserver.Options) { o.ReadOnly = true }))
	assert.ElementsMatch(t, []string{"current", "list", "report", "recent", "analyze", "reminders"}, keys(readOnly))

	trimmed := toolNames(t, newHarness(t, func(o *mcpserver.Options) {
		o.DisabledTools = []string{"remove", " remind_remove "}
		o.Reminders = nil
	}))
	assert.NotContains(t, trimmed, "remove")
	assert.NotContains(t, trimmed, "reminders")
	assert.Contains(t, trimmed, "start")
}

func keys(m map[string]*mcp.Tool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestStartReportsTheStoppedActivity(t *testing.T) {
	h := newHarness(t, nil)
	runningStart := fixedNow().Add(-2 * time.Hour)
	running := models.Activity{Project: "Tock", Description: "Review", StartTime: runningStart}
	at := time.Date(2026, time.November, 18, 15, 0, 0, 0, time.Local)

	h.activities.EXPECT().List(mock.Anything, runningFilter()).Return([]models.Activity{running}, nil).Once()
	h.activities.EXPECT().Start(mock.Anything, mock.MatchedBy(func(req models.StartActivityRequest) bool {
		return req.Project == "Tock" && req.Description == "MCP" && req.StartTime.Equal(at) &&
			assert.ObjectsAreEqual([]string{"ai"}, req.Tags)
	})).Return(&models.Activity{Project: "Tock", Description: "MCP", StartTime: at, Tags: []string{"ai"}}, nil)
	h.activities.EXPECT().List(mock.Anything, dayFilter()).Return([]models.Activity{
		{Project: "Tock", Description: "Review", StartTime: runningStart, EndTime: &at},
		{Project: "Tock", Description: "MCP", StartTime: at, Tags: []string{"ai"}},
	}, nil)

	out := decode[mcpserver.StartOutput](t, h.call(t, "start", map[string]any{
		"project": "Tock", "description": "MCP", "at": "15:00", "tags": []string{" ai ", ""},
	}))
	assert.Equal(t, "2026-11-18-02", out.Started.Key)
	assert.True(t, out.Started.Running)
	require.NotNil(t, out.Stopped)
	assert.Equal(t, "2026-11-18-01", out.Stopped.Key)
	assert.Equal(t, 90, out.Stopped.DurationMinutes)
	assert.Equal(t, fixedNow().Format(time.RFC3339), out.Now)
}

func TestStartRequiresProjectAndDescription(t *testing.T) {
	h := newHarness(t, nil)
	res := h.call(t, "start", map[string]any{"project": "Tock", "description": " "})
	assert.True(t, res.IsError)
	assert.Contains(t, errorText(res), "description is required")
}

func TestListReturnsKeysAndTotal(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Date(2026, time.November, 17, 9, 0, 0, 0, time.Local)
	end := start.Add(45 * time.Minute)

	h.activities.EXPECT().GetReport(mock.Anything, mock.MatchedBy(func(f models.ActivityFilter) bool {
		return f.FromDate != nil && f.FromDate.Day() == 17 && f.Project != nil && *f.Project == "Tock"
	})).Return(&models.Report{
		Activities:    []models.Activity{{Project: "Tock", Description: "Plan", StartTime: start, EndTime: &end}},
		Keys:          []string{"2026-11-17-01"},
		TotalDuration: 45 * time.Minute,
	}, nil)

	out := decode[mcpserver.ListOutput](t, h.call(t, "list", map[string]any{"period": "yesterday", "project": "Tock"}))
	require.Len(t, out.Activities, 1)
	assert.Equal(t, "2026-11-17-01", out.Activities[0].Key)
	assert.Equal(t, 45, out.TotalMinutes)
	assert.False(t, out.Activities[0].Running)
}

func TestListRejectsUnknownPeriod(t *testing.T) {
	res := newHarness(t, nil).call(t, "list", map[string]any{"period": "fortnight"})
	assert.True(t, res.IsError)
}

func TestEveryCallStopsAfterWorkingHoursFirst(t *testing.T) {
	weekdays, err := models.ParseWorkingWeekdays("wed")
	require.NoError(t, err)
	hours := models.WorkingHours{StopHour: 17, Weekdays: weekdays}
	h := newHarness(t, func(o *mcpserver.Options) {
		o.WorkingHours = func() (models.WorkingHours, bool, error) { return hours, true, nil }
	})

	h.activities.EXPECT().StopAfterWorkingHours(mock.Anything, hours, fixedNow()).Return(nil, nil).Once()
	h.activities.EXPECT().List(mock.Anything, runningFilter()).Return(nil, nil)
	h.activities.EXPECT().GetLast(mock.Anything).Return(nil, coreErrors.ErrActivityNotFound)

	out := decode[mcpserver.CurrentOutput](t, h.call(t, "current", map[string]any{}))
	assert.Nil(t, out.Running)
	assert.Nil(t, out.Last)
}

func TestStopWithoutRunningActivityIsAToolError(t *testing.T) {
	h := newHarness(t, nil)
	h.activities.EXPECT().Stop(mock.Anything, mock.Anything).Return(nil, coreErrors.ErrNoActiveActivity)

	res := h.call(t, "stop", map[string]any{})
	assert.True(t, res.IsError)
	assert.Contains(t, errorText(res), "no active activity")
}

func TestEditBuildsPartialUpdate(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Date(2026, time.November, 18, 9, 0, 0, 0, time.Local)
	end := start.Add(time.Hour)
	original := models.Activity{Project: "Tock", Description: "Old", StartTime: start, EndTime: &end}
	newStart := time.Date(2026, time.November, 18, 8, 30, 0, 0, time.Local)

	h.activities.EXPECT().GetByKey(mock.Anything, "2026-11-18-01").Return(&original, nil)
	h.activities.EXPECT().Update(mock.Anything, original, mock.MatchedBy(func(req models.UpdateActivityRequest) bool {
		return req.Description != nil && *req.Description == "New" && req.Project == nil &&
			req.StartTime != nil && req.StartTime.Equal(newStart) && req.ClearEndTime && req.Tags != nil && len(*req.Tags) == 0
	})).Return(&models.Activity{Project: "Tock", Description: "New", StartTime: newStart}, nil)
	h.activities.EXPECT().List(mock.Anything, dayFilter()).Return([]models.Activity{
		{Project: "Tock", Description: "New", StartTime: newStart},
	}, nil)

	out := decode[mcpserver.ActivityOutput](t, h.call(t, "edit", map[string]any{
		"key": "2026-11-18-01", "description": "New", "start": "2026-11-18 08:30", "end": "", "tags": []string{},
	}))
	assert.Equal(t, "New", out.Activity.Description)
	assert.True(t, out.Activity.Running)
}

func TestRemoveDeletesByKey(t *testing.T) {
	h := newHarness(t, nil)
	start := time.Date(2026, time.November, 18, 9, 0, 0, 0, time.Local)
	activity := models.Activity{Project: "Tock", Description: "Gone", StartTime: start, EndTime: &start}

	h.activities.EXPECT().GetByKey(mock.Anything, "2026-11-18-01").Return(&activity, nil)
	h.activities.EXPECT().Remove(mock.Anything, activity).Return(nil)

	out := decode[mcpserver.ActivityOutput](t, h.call(t, "remove", map[string]any{"key": "2026-11-18-01"}))
	assert.Equal(t, "Gone", out.Activity.Description)
}

func TestRemindAddBuildsRequest(t *testing.T) {
	h := newHarness(t, nil)
	due := time.Date(2026, time.November, 19, 9, 30, 0, 0, time.Local)

	h.reminders.EXPECT().Add(mock.Anything, mock.MatchedBy(func(req models.AddReminderRequest) bool {
		return req.Title == "Invoice" && req.Due.Equal(due) && !req.AllDay &&
			req.Repeat.Freq == models.RepeatMonthly && req.Repeat.Count == 3 && req.Notify == 15*time.Minute
	})).Return(&models.Reminder{ID: 4, Title: "Invoice", Due: due, Repeat: models.Repeat{Freq: models.RepeatMonthly}}, nil)

	out := decode[mcpserver.ReminderOutput](t, h.call(t, "remind_add", map[string]any{
		"title": "Invoice", "day": "2026-11-19", "at": "09:30", "repeat": "monthly", "count": 3, "notify": "15m",
	}))
	assert.Equal(t, 4, out.Reminder.ID)
	assert.Equal(t, "monthly", out.Reminder.Repeat)
}

func TestRemindAddRejectsBoundsWithoutRepeat(t *testing.T) {
	res := newHarness(t, nil).call(t, "remind_add", map[string]any{"title": "Once", "count": 2})
	assert.True(t, res.IsError)
}

func TestRemindersListOverdueFirst(t *testing.T) {
	h := newHarness(t, nil)
	overdue := models.ReminderOccurrence{
		Reminder: models.Reminder{ID: 1, Title: "Late", Due: fixedNow().Add(-time.Hour)},
		Due:      fixedNow().Add(-time.Hour),
	}
	upcoming := models.ReminderOccurrence{
		Reminder: models.Reminder{ID: 2, Title: "Soon", Due: fixedNow().Add(time.Hour)},
		Due:      fixedNow().Add(time.Hour),
	}

	h.reminders.EXPECT().Overdue(mock.Anything, fixedNow()).Return([]models.ReminderOccurrence{overdue}, nil)
	h.reminders.EXPECT().Occurrences(mock.Anything, mock.Anything, mock.Anything, fixedNow()).
		Return([]models.ReminderOccurrence{overdue, upcoming}, nil)

	out := decode[mcpserver.RemindersOutput](t, h.call(t, "reminders", map[string]any{}))
	require.Len(t, out.Occurrences, 2)
	assert.Equal(t, "overdue", out.Occurrences[0].Status)
	assert.Equal(t, "pending", out.Occurrences[1].Status)
	assert.Equal(t, "2026-11-18", out.Occurrences[1].Day)
}

func TestRemindTrackStartsActivity(t *testing.T) {
	h := newHarness(t, func(o *mcpserver.Options) { o.OnStarted = func() {} })
	due := time.Date(2026, time.November, 18, 0, 0, 0, 0, time.Local)
	occurrence := models.ReminderOccurrence{Reminder: models.Reminder{ID: 3, Title: "Call", Project: "Ops"}, Due: due}

	h.reminders.EXPECT().Current(mock.Anything, 3, fixedNow()).Return(&occurrence, nil)
	h.reminders.EXPECT().Track(mock.Anything, occurrence.Ref(), models.TrackReminderRequest{StartTime: fixedNow()}).
		Return(&models.Activity{Project: "Ops", Description: "Call", StartTime: fixedNow()}, nil)
	h.activities.EXPECT().List(mock.Anything, dayFilter()).Return(nil, nil)

	out := decode[mcpserver.TrackOutput](t, h.call(t, "remind_track", map[string]any{"id": 3}))
	assert.Equal(t, "Ops", out.Started.Project)
}

func TestStandupPromptCarriesData(t *testing.T) {
	h := newHarness(t, nil)
	yesterday := time.Date(2026, time.November, 17, 10, 0, 0, 0, time.Local)
	end := yesterday.Add(time.Hour)

	h.activities.EXPECT().List(mock.Anything, dayFilter()).
		Return([]models.Activity{{Project: "Tock", Description: "Sync", StartTime: yesterday, EndTime: &end}}, nil)
	h.activities.EXPECT().GetReport(mock.Anything, mock.Anything).Return(&models.Report{
		Activities: []models.Activity{{Project: "Tock", Description: "Sync", StartTime: yesterday, EndTime: &end}},
		Keys:       []string{"2026-11-17-01"},
	}, nil)
	h.reminders.EXPECT().Overdue(mock.Anything, fixedNow()).Return(nil, nil)

	res, err := h.session.GetPrompt(context.Background(), &mcp.GetPromptParams{Name: "standup"})
	require.NoError(t, err)
	require.Len(t, res.Messages, 1)
	text, ok := res.Messages[0].Content.(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "previous_day")
	assert.Contains(t, text.Text, "2026-11-17-01")
}
