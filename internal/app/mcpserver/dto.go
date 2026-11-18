package mcpserver

import (
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// Activity is an activity as the tools return it.
type Activity struct {
	Key             string   `json:"key,omitempty"    jsonschema:"DATE-INDEX key (YYYY-MM-DD-NN) that edit, note, tag and remove take"`
	Project         string   `json:"project"`
	Description     string   `json:"description"`
	Start           string   `json:"start"            jsonschema:"RFC 3339"`
	End             string   `json:"end,omitempty"    jsonschema:"RFC 3339; empty while it runs"`
	Running         bool     `json:"running"`
	DurationMinutes int      `json:"duration_minutes"`
	Notes           string   `json:"notes,omitempty"`
	Tags            []string `json:"tags,omitempty"`
}

func (s *server) activity(a models.Activity, key string) Activity {
	end := s.opts.Now()
	out := Activity{
		Key:         key,
		Project:     a.Project,
		Description: a.Description,
		Start:       a.StartTime.Format(time.RFC3339),
		Running:     a.EndTime == nil,
		Notes:       a.Notes,
		Tags:        a.Tags,
	}
	if a.EndTime != nil {
		end = *a.EndTime
		out.End = end.Format(time.RFC3339)
	}
	out.DurationMinutes = minutes(end.Sub(a.StartTime))
	return out
}

// Occurrence is one dated instance of a reminder.
type Occurrence struct {
	ReminderID   int      `json:"reminder_id"`
	Day          string   `json:"day"                     jsonschema:"YYYY-MM-DD; names this occurrence in the remind_* tools"`
	Title        string   `json:"title"`
	Project      string   `json:"project,omitempty"`
	Due          string   `json:"due"                     jsonschema:"RFC 3339"`
	AllDay       bool     `json:"all_day,omitempty"`
	Status       string   `json:"status"                  jsonschema:"pending, snoozed, overdue, missed or done"`
	Repeat       string   `json:"repeat,omitempty"        jsonschema:"repeat rule in the form remind_add takes"`
	SnoozedUntil string   `json:"snoozed_until,omitempty" jsonschema:"RFC 3339"`
	Notes        string   `json:"notes,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

func (s *server) occurrence(o models.ReminderOccurrence) Occurrence {
	out := Occurrence{
		ReminderID: o.Reminder.ID,
		Day:        o.Key(),
		Title:      o.Reminder.Title,
		Project:    o.Reminder.Project,
		Due:        o.Due.Format(time.RFC3339),
		AllDay:     o.Reminder.AllDay,
		Status:     string(o.Status(s.opts.Now())),
		Repeat:     o.Reminder.Repeat.Spec(),
		Notes:      o.Reminder.Notes,
		Tags:       o.Reminder.Tags,
	}
	if until := o.State.SnoozedUntil; until != nil {
		out.SnoozedUntil = until.Format(time.RFC3339)
	}
	return out
}

func minutes(d time.Duration) int {
	return int(d.Round(time.Minute) / time.Minute)
}

// localLayouts are the time inputs read in the local zone, besides RFC 3339.
func localLayouts() []string {
	return []string{"2006-01-02T15:04:05", "2006-01-02T15:04", time.DateTime, "2006-01-02 15:04"}
}

// parseTime reads a time input: RFC 3339, a local "YYYY-MM-DD HH:MM", or "HH:MM" for the day of now.
// An empty input gives fallback.
func parseTime(input string, now, fallback time.Time) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return fallback, nil
	}
	if t, err := time.Parse(time.RFC3339, input); err == nil {
		return t.In(time.Local), nil
	}
	for _, layout := range localLayouts() {
		if t, err := time.ParseInLocation(layout, input, time.Local); err == nil {
			return t, nil
		}
	}
	if clock, err := time.ParseInLocation("15:04", input, time.Local); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, time.Local), nil
	}
	return time.Time{}, errors.Errorf("invalid time %q (use RFC 3339, YYYY-MM-DD HH:MM or HH:MM)", input)
}

// parseDay reads a day input: YYYY-MM-DD, today, tomorrow or a weekday; empty means today.
func parseDay(input string, now time.Time) (time.Time, error) {
	day, err := timeutil.ParseDay(input, now)
	if err != nil {
		return time.Time{}, errors.Wrapf(err, "parse day %q", input)
	}
	return day, nil
}

// cleanTags trims tags and drops empty ones.
func cleanTags(tags []string) []string {
	var out []string
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	return out
}
