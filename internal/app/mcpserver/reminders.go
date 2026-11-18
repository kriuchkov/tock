package mcpserver

import (
	"cmp"
	"context"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	defaultReminderDays = 7
	maxReminderDays     = 366
	defaultSnooze       = 10 * time.Minute
)

// RemindersInput picks the reminder occurrences to list.
type RemindersInput struct {
	Days        int  `json:"days,omitempty"         jsonschema:"how many days ahead, today included; 7 by default"`
	OverdueOnly bool `json:"overdue_only,omitempty" jsonschema:"only the overdue ones"`
}

// RemindersOutput lists overdue occurrences first, then upcoming ones.
type RemindersOutput struct {
	Clock

	Occurrences []Occurrence `json:"occurrences"`
}

// RemindAddInput describes a new reminder.
type RemindAddInput struct {
	Title   string   `json:"title"`
	Day     string   `json:"day,omitempty"     jsonschema:"YYYY-MM-DD, today, tomorrow or a weekday; today by default"`
	At      string   `json:"at,omitempty"      jsonschema:"time of day, HH:MM; an all-day reminder when absent"`
	Repeat  string   `json:"repeat,omitempty"  jsonschema:"daily, weekdays, weekly[:mon,thu], monthly[:16|:last|:2-tue|:last-fri], yearly, or an interval like 3d, 2w, 6m, 1y"`
	Until   string   `json:"until,omitempty"   jsonschema:"last day a repeat can fall on, YYYY-MM-DD"`
	Count   int      `json:"count,omitempty"   jsonschema:"stop a repeat after this many occurrences"`
	Notify  string   `json:"notify,omitempty"  jsonschema:"how long before to notify, like 10m or 1h"`
	Project string   `json:"project,omitempty" jsonschema:"project an activity tracked from it goes to"`
	Notes   string   `json:"notes,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// Reminder is a reminder as the tools return it.
type Reminder struct {
	ID      int      `json:"id"`
	Title   string   `json:"title"`
	Project string   `json:"project,omitempty"`
	Due     string   `json:"due"               jsonschema:"first occurrence, RFC 3339"`
	AllDay  bool     `json:"all_day,omitempty"`
	Repeat  string   `json:"repeat,omitempty"`
	Notes   string   `json:"notes,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// ReminderOutput is a reminder after a change.
type ReminderOutput struct {
	Clock

	Reminder Reminder `json:"reminder"`
}

// OccurrenceInput names a reminder occurrence.
type OccurrenceInput struct {
	ID  int    `json:"id"            jsonschema:"reminder id"`
	Day string `json:"day,omitempty" jsonschema:"day of the occurrence, YYYY-MM-DD; the overdue or next one by default"`
}

// RemindDoneInput marks an occurrence done or not done.
type RemindDoneInput struct {
	OccurrenceInput

	Undo bool `json:"undo,omitempty" jsonschema:"mark it not done again"`
}

// RemindSnoozeInput moves an occurrence's alert.
type RemindSnoozeInput struct {
	OccurrenceInput

	Until string `json:"until,omitempty" jsonschema:"when to alert again; 10 minutes from now by default"`
	Clear bool   `json:"clear,omitempty" jsonschema:"drop the snooze instead"`
}

// RemindTrackInput starts an activity from an occurrence.
type RemindTrackInput struct {
	OccurrenceInput

	At      string `json:"at,omitempty"      jsonschema:"start time; now by default"`
	Project string `json:"project,omitempty" jsonschema:"project to track under; required when the reminder has none"`
}

// OccurrenceOutput is an occurrence after a change.
type OccurrenceOutput struct {
	Clock

	Occurrence Occurrence `json:"occurrence"`
}

// TrackOutput is the activity started from an occurrence.
type TrackOutput struct {
	Clock

	Started Activity `json:"started"`
}

// RemoveReminderInput names a reminder.
type RemoveReminderInput struct {
	ID int `json:"id" jsonschema:"reminder id"`
}

// RemoveReminderOutput is the removed reminder.
type RemoveReminderOutput struct {
	Clock

	Removed Reminder `json:"removed"`
}

func (s *server) addReminderTools() {
	addTool(s, readAccess, &mcp.Tool{
		Name:        "reminders",
		Title:       "List reminders",
		Description: "List reminder occurrences: overdue ones first, then the ones due in the next days.",
	}, s.reminders)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "remind_add",
		Title:       "Add reminder",
		Description: "Create a reminder, optionally repeating.",
	}, s.remindAdd)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "remind_done",
		Title:       "Complete reminder",
		Description: "Mark a reminder occurrence done, or not done with undo.",
	}, s.remindDone)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "remind_snooze",
		Title:       "Snooze reminder",
		Description: "Move a reminder occurrence's alert to a later time.",
	}, s.remindSnooze)

	addTool(s, writeAccess, &mcp.Tool{
		Name:        "remind_track",
		Title:       "Track reminder",
		Description: "Start an activity from a reminder occurrence and mark it done.",
	}, s.remindTrack)

	addTool(s, destructiveAccess, &mcp.Tool{
		Name:        "remind_remove",
		Title:       "Remove reminder",
		Description: "Delete a reminder with all its occurrences. This cannot be undone.",
	}, s.remindRemove)
}

func (s *server) reminderDTO(r models.Reminder) Reminder {
	return Reminder{
		ID:      r.ID,
		Title:   r.Title,
		Project: r.Project,
		Due:     r.Due.Format(time.RFC3339),
		AllDay:  r.AllDay,
		Repeat:  r.Repeat.Spec(),
		Notes:   r.Notes,
		Tags:    r.Tags,
	}
}

func (s *server) reminders(ctx context.Context, _ *mcp.CallToolRequest, in RemindersInput) (*mcp.CallToolResult, RemindersOutput, error) {
	out := RemindersOutput{Clock: s.clock(), Occurrences: []Occurrence{}}
	occurrences, err := s.listOccurrences(ctx, in)
	if err != nil {
		return nil, out, err
	}
	for _, occurrence := range occurrences {
		out.Occurrences = append(out.Occurrences, s.occurrence(occurrence))
	}
	return nil, out, nil
}

func (s *server) listOccurrences(ctx context.Context, in RemindersInput) ([]models.ReminderOccurrence, error) {
	now := s.opts.Now()
	overdue, err := s.opts.Reminders.Overdue(ctx, now)
	if err != nil {
		return nil, errors.Wrap(err, "list overdue reminders")
	}
	if in.OverdueOnly {
		return models.OverdueOnly(overdue, now), nil
	}

	days := min(cmp.Or(max(in.Days, 0), defaultReminderDays), maxReminderDays)
	from, _ := timeutil.LocalDayBounds(now)
	upcoming, err := s.opts.Reminders.Occurrences(ctx, from, from.AddDate(0, 0, days), now)
	if err != nil {
		return nil, errors.Wrap(err, "list reminders")
	}
	return models.MergeOccurrences(overdue, upcoming), nil
}

func (s *server) remindAdd(ctx context.Context, _ *mcp.CallToolRequest, in RemindAddInput) (*mcp.CallToolResult, ReminderOutput, error) {
	out := ReminderOutput{Clock: s.clock()}
	req, err := s.addReminderRequest(in)
	if err != nil {
		return nil, out, err
	}
	added, err := s.opts.Reminders.Add(ctx, req)
	if err != nil {
		return nil, out, errors.Wrap(err, "add reminder")
	}
	out.Reminder = s.reminderDTO(*added)
	return nil, out, nil
}

func (s *server) addReminderRequest(in RemindAddInput) (models.AddReminderRequest, error) {
	now := s.opts.Now()
	day, err := parseDay(in.Day, now)
	if err != nil {
		return models.AddReminderRequest{}, err
	}

	due, allDay := day, true
	if at := strings.TrimSpace(in.At); at != "" {
		clock, clockErr := time.ParseInLocation("15:04", at, time.Local)
		if clockErr != nil {
			return models.AddReminderRequest{}, errors.Errorf("invalid time of day %q (use HH:MM)", at)
		}
		due = time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, time.Local)
		allDay = false
	}

	repeat, err := s.repeat(in, now)
	if err != nil {
		return models.AddReminderRequest{}, err
	}

	var notify time.Duration
	if raw := strings.TrimSpace(in.Notify); raw != "" && raw != "0" {
		if notify, err = time.ParseDuration(raw); err != nil || notify < 0 {
			return models.AddReminderRequest{}, errors.Errorf("invalid notify %q (use a duration like 10m or 1h)", raw)
		}
	}

	return models.AddReminderRequest{
		Title:   strings.TrimSpace(in.Title),
		Project: strings.TrimSpace(in.Project),
		Due:     due,
		AllDay:  allDay,
		Repeat:  repeat,
		Notify:  notify,
		Tags:    cleanTags(in.Tags),
		Notes:   in.Notes,
	}, nil
}

func (s *server) repeat(in RemindAddInput, now time.Time) (models.Repeat, error) {
	repeat, err := models.ParseRepeat(in.Repeat)
	if err != nil {
		return models.Repeat{}, errors.Wrap(err, "parse repeat")
	}

	until := strings.TrimSpace(in.Until)
	if repeat.Freq == models.RepeatNone && (until != "" || in.Count != 0) {
		return models.Repeat{}, errors.New("until and count need a repeat")
	}
	if in.Count < 0 {
		return models.Repeat{}, errors.New("count cannot be negative")
	}
	if until != "" {
		day, dayErr := parseDay(until, now)
		if dayErr != nil {
			return models.Repeat{}, dayErr
		}
		repeat.Until = &day
	}
	repeat.Count = in.Count
	return repeat, nil
}

// occurrenceRef resolves the occurrence in: the one on its day, else the overdue or next one.
func (s *server) occurrenceRef(ctx context.Context, in OccurrenceInput) (models.OccurrenceRef, error) {
	now := s.opts.Now()
	if strings.TrimSpace(in.Day) == "" {
		current, err := s.opts.Reminders.Current(ctx, in.ID, now)
		if err != nil {
			return models.OccurrenceRef{}, errors.Wrapf(err, "reminder #%d", in.ID)
		}
		return current.Ref(), nil
	}

	day, err := parseDay(in.Day, now)
	if err != nil {
		return models.OccurrenceRef{}, err
	}
	reminder, err := s.opts.Reminders.Get(ctx, in.ID)
	if err != nil {
		return models.OccurrenceRef{}, errors.Wrapf(err, "reminder #%d", in.ID)
	}
	occurrence, ok := reminder.OccurrenceOn(day, now)
	if !ok {
		return models.OccurrenceRef{}, errors.Wrapf(
			coreErrors.ErrNoReminderOccurrence,
			"reminder #%d on %s",
			in.ID,
			day.Format(time.DateOnly),
		)
	}
	return occurrence.Ref(), nil
}

func (s *server) remindDone(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in RemindDoneInput,
) (*mcp.CallToolResult, OccurrenceOutput, error) {
	out := OccurrenceOutput{Clock: s.clock()}
	ref, err := s.occurrenceRef(ctx, in.OccurrenceInput)
	if err != nil {
		return nil, out, err
	}
	occurrence, err := s.opts.Reminders.SetDone(ctx, ref, !in.Undo, s.opts.Now())
	if err != nil {
		return nil, out, errors.Wrap(err, "mark reminder")
	}
	out.Occurrence = s.occurrence(*occurrence)
	return nil, out, nil
}

func (s *server) remindSnooze(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in RemindSnoozeInput,
) (*mcp.CallToolResult, OccurrenceOutput, error) {
	out := OccurrenceOutput{Clock: s.clock()}
	ref, err := s.occurrenceRef(ctx, in.OccurrenceInput)
	if err != nil {
		return nil, out, err
	}

	var until time.Time
	if !in.Clear {
		now := s.opts.Now()
		if until, err = parseTime(in.Until, now, now.Add(cmp.Or(s.opts.Snooze, defaultSnooze))); err != nil {
			return nil, out, err
		}
		if !until.After(now) {
			return nil, out, errors.New("until must be in the future")
		}
	}

	occurrence, err := s.opts.Reminders.Snooze(ctx, ref, until)
	if err != nil {
		return nil, out, errors.Wrap(err, "snooze reminder")
	}
	out.Occurrence = s.occurrence(*occurrence)
	return nil, out, nil
}

func (s *server) remindTrack(ctx context.Context, _ *mcp.CallToolRequest, in RemindTrackInput) (*mcp.CallToolResult, TrackOutput, error) {
	out := TrackOutput{Clock: s.clock()}
	ref, err := s.occurrenceRef(ctx, in.OccurrenceInput)
	if err != nil {
		return nil, out, err
	}
	now := s.opts.Now()
	at, err := parseTime(in.At, now, now)
	if err != nil {
		return nil, out, err
	}

	started, err := s.opts.Reminders.Track(ctx, ref, models.TrackReminderRequest{StartTime: at, Project: strings.TrimSpace(in.Project)})
	if err != nil {
		return nil, out, errors.Wrap(err, "track reminder")
	}
	s.started()
	out.Started = *s.keyed(ctx, started)
	return nil, out, nil
}

func (s *server) remindRemove(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in RemoveReminderInput,
) (*mcp.CallToolResult, RemoveReminderOutput, error) {
	out := RemoveReminderOutput{Clock: s.clock()}
	reminder, err := s.opts.Reminders.Get(ctx, in.ID)
	if err != nil {
		return nil, out, errors.Wrapf(err, "reminder #%d", in.ID)
	}
	if err = s.opts.Reminders.Remove(ctx, in.ID); err != nil {
		return nil, out, errors.Wrap(err, "remove reminder")
	}
	out.Removed = s.reminderDTO(*reminder)
	return nil, out, nil
}
