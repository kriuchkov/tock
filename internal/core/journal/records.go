package journal

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

const (
	tagPrefix        = "tags/"
	occurrencePrefix = "occ/"
	fieldDeleted     = "deleted"
	fieldStart       = "start"
	dueLayout        = "2006-01-02T15:04"
	// maxNumber bounds count and notify, so converting them cannot overflow.
	maxNumber = math.MaxInt32
)

// The fields that end an activity. An activity shows its end when that holds an instant, otherwise
// its auto_end when that holds one that is not before its start; with neither it runs.
const (
	// FieldEnd is the end of an activity: written by a stop made on purpose, on any device, and by the
	// activity's owner for the end the one-running rule gives it.
	FieldEnd = "end"
	// FieldAutoEnd is the end the one-running rule gives an activity, written by every device that
	// does not own it. An end always shows over it, whatever the clocks.
	FieldAutoEnd = "auto_end"
)

// Activity returns the activity the record holds. ok is false when it is hidden: deleted, or
// without a start or a project. A field holding a value of the wrong type counts as missing. EndTime
// is the shown end: end, or else auto_end (see shownEnd).
func (r Record) Activity() (models.Activity, bool) {
	if r.Type != TypeActivity || r.flag(fieldDeleted) {
		return models.Activity{}, false
	}
	start, hasStart := r.instant(fieldStart)
	project := r.text("project")
	if !hasStart || project == "" {
		return models.Activity{}, false
	}

	activity := models.Activity{
		Description: r.text("description"),
		Project:     project,
		StartTime:   start,
		Notes:       r.text("notes"),
		Tags:        r.tags(),
	}
	if end, _, hasEnd := r.shownEnd(); hasEnd {
		activity.EndTime = &end
	}
	return activity, true
}

// ShownEndField returns the field that holds an activity's shown end: FieldEnd, FieldAutoEnd, or ""
// while it runs (see shownEnd).
func (r Record) ShownEndField() string {
	_, path, _ := r.shownEnd()
	return path
}

// shownEnd returns an activity's shown end and the field that holds it: end when it holds an
// instant, otherwise auto_end when it holds one that is not before the start. ok is false while the
// activity runs. An auto_end only ever holds the start of an activity that came after this one, so
// one before its start is stale, as when a device that had not read it moved the start past it, and
// counts as missing.
func (r Record) shownEnd() (time.Time, string, bool) {
	if end, ok := r.instant(FieldEnd); ok {
		return end, FieldEnd, true
	}
	end, ok := r.instant(FieldAutoEnd)
	if start, hasStart := r.instant(fieldStart); !ok || (hasStart && end.Before(start)) {
		return time.Time{}, "", false
	}
	return end, FieldAutoEnd, true
}

// Reminder returns the reminder the record holds, with ID 0: reminder numbers are local. ok is
// false when it is hidden: deleted, or without a title or a due date. A field holding a value of
// the wrong type, or a repeat rule not in its canonical form, counts as missing.
func (r Record) Reminder() (models.Reminder, bool) {
	if r.Type != TypeReminder || r.flag(fieldDeleted) {
		return models.Reminder{}, false
	}
	title := r.text("title")
	due, allDay, hasDue := r.due()
	if title == "" || !hasDue {
		return models.Reminder{}, false
	}

	repeat, err := models.ParseRepeat(r.text("every"))
	if err != nil || repeat.Spec() != r.text("every") {
		repeat = models.Repeat{}
	}
	repeat.Count = r.number("count")
	if until, ok := r.date("until"); ok {
		repeat.Until = &until
	}
	created, _ := r.instant("created")

	return models.Reminder{
		Title:   title,
		Project: r.text("project"),
		Due:     due,
		AllDay:  allDay,
		Repeat:  repeat,
		Notify:  time.Duration(r.number("notify")) * time.Second,
		Tags:    r.tags(),
		Notes:   r.text("notes"),
		Created: created,
		States:  r.states(),
	}, true
}

// due reads `due`. A due date without a time makes the reminder all day, and the time of an
// all-day reminder's due date is ignored.
func (r Record) due() (time.Time, bool, bool) {
	value := r.text("due")
	allDay := r.flag("all_day")
	switch len(value) {
	case len(time.DateOnly):
		day, ok := parseDate(value)
		return day, true, ok
	case len(dueLayout):
		t, err := time.Parse(dueLayout, value)
		if err != nil {
			return time.Time{}, false, false
		}
		if allDay {
			return timeutil.WallTime(t.Year(), t.Month(), t.Day(), 0, 0, time.Local), true, true
		}
		return timeutil.WallTime(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), time.Local), false, true
	}
	return time.Time{}, false, false
}

// states reads the occ/<YYYY-MM-DD>/<key> fields.
func (r Record) states() map[string]models.OccurrenceState {
	states := make(map[string]models.OccurrenceState)
	for path, field := range r.Fields {
		rest, ok := strings.CutPrefix(path, occurrencePrefix)
		if !ok {
			continue
		}
		day, key, ok := strings.Cut(rest, "/")
		if _, valid := parseDate(day); !ok || !valid {
			continue
		}

		state := states[day]
		switch key {
		case "done":
			state.DoneAt = instantPtr(field.Value)
		case "snoozed":
			state.SnoozedUntil = instantPtr(field.Value)
		case "activity":
			state.ActivityStart = instantPtr(field.Value)
		case "skipped":
			state.Skipped = decodeBool(field.Value)
		default:
			continue
		}
		states[day] = state
	}

	for day, state := range states {
		if state.IsZero() {
			delete(states, day)
		}
	}
	if len(states) == 0 {
		return nil
	}
	return states
}

// tags returns the tags set to true, sorted.
func (r Record) tags() []string {
	var tags []string
	for path, field := range r.Fields {
		if tag, ok := strings.CutPrefix(path, tagPrefix); ok && tag != "" && decodeBool(field.Value) {
			tags = append(tags, tag)
		}
	}
	slices.Sort(tags)
	return tags
}

func (r Record) text(path string) string {
	var s string
	if field, ok := r.Fields[path]; ok && json.Unmarshal(field.Value, &s) == nil {
		return s
	}
	return ""
}

func (r Record) flag(path string) bool {
	return decodeBool(r.Fields[path].Value)
}

// number reads a whole number from 0 to maxNumber; anything else counts as 0.
func (r Record) number(path string) int {
	var n float64
	if field, ok := r.Fields[path]; !ok || json.Unmarshal(field.Value, &n) != nil {
		return 0
	}
	if n < 0 || n > maxNumber || n != math.Trunc(n) {
		return 0
	}
	return int(n)
}

func (r Record) instant(path string) (time.Time, bool) {
	return parseInstant(r.text(path))
}

func (r Record) date(path string) (time.Time, bool) {
	return parseDate(r.text(path))
}

func decodeBool(value json.RawMessage) bool {
	var b bool
	return json.Unmarshal(value, &b) == nil && b
}

func instantPtr(value json.RawMessage) *time.Time {
	var s string
	if json.Unmarshal(value, &s) != nil {
		return nil
	}
	if t, ok := parseInstant(s); ok {
		return &t
	}
	return nil
}

// parseInstant reads an instant in RFC 3339 with whole seconds and a Z or ±hh:mm offset.
func parseInstant(s string) (time.Time, bool) {
	if (len(s) != len("2006-01-02T15:04:05Z") && len(s) != len(time.RFC3339)) || strings.Contains(s, ".") {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// parseDate reads YYYY-MM-DD as the start of that day in local time.
func parseDate(s string) (time.Time, bool) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil || len(s) != len(time.DateOnly) {
		return time.Time{}, false
	}
	return timeutil.WallTime(t.Year(), t.Month(), t.Day(), 0, 0, time.Local), true
}
