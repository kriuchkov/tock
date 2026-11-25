package sync

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

// Field paths of docs/sync.md.
const (
	fieldStart       = "start"
	fieldEnd         = "end"
	fieldProject     = "project"
	fieldDescription = "description"
	fieldNotes       = "notes"
	fieldDeleted     = "deleted"
	fieldTitle       = "title"
	fieldDue         = "due"
	fieldAllDay      = "all_day"
	fieldEvery       = "every"
	fieldUntil       = "until"
	fieldCount       = "count"
	fieldNotify      = "notify"
	fieldCreated     = "created"
	tagPrefix        = "tags/"
	occurrencePrefix = "occ/"
	occurrenceDone   = "done"
	occurrenceSnooze = "snoozed"
	occurrenceTrack  = "activity"
	occurrenceSkip   = "skipped"
	dueLayout        = "2006-01-02T15:04"
)

// fields maps field paths to JSON values, the way a journal line's set holds them.
type fields map[string]json.RawMessage

// equal reports whether both hold the same paths with the same values.
func (f fields) equal(other fields) bool {
	if len(f) != len(other) {
		return false
	}
	for path, value := range f {
		if otherValue, ok := other[path]; !ok || !bytes.Equal(value, otherValue) {
			return false
		}
	}
	return true
}

// clone returns a copy that can be changed without touching f.
func (f fields) clone() fields {
	out := make(fields, len(f))
	maps.Copy(out, f)
	return out
}

// form says how a record's fields are written: as a journal line holds them, or as an image
// compares them, at the backend's precision.
type form struct {
	// precision truncates activity times; zero writes whole seconds.
	precision time.Duration
	// image writes activity times at precision with the offset they are read at, which tells the wall
	// time a wall-clock backend holds, and the other times (a reminder's) in UTC, so they compare as
	// they are kept, not as they are shown.
	image bool
}

// journalForm writes fields for a journal line: instants in local time with whole seconds.
func journalForm() form {
	return form{precision: time.Second}
}

// imageForm writes fields for comparing with the backend at its precision.
func imageForm(precision time.Duration) form {
	return form{precision: max(precision, time.Second), image: true}
}

// instant writes an activity time in local time.
func (f form) instant(t time.Time) json.RawMessage {
	return jsonString(t.Truncate(f.precision).In(time.Local).Format(time.RFC3339))
}

// exact writes a time kept to the second whatever the backend: reminder times.
func (f form) exact(t time.Time) json.RawMessage {
	t = t.Truncate(time.Second)
	if f.image {
		return jsonString(t.UTC().Format(time.RFC3339))
	}
	return jsonString(t.In(time.Local).Format(time.RFC3339))
}

// activityFields writes an activity's fields: start, end, project, description, notes and its tags.
func activityFields(activity models.Activity, f form) fields {
	out := fields{
		fieldStart:       f.instant(activity.StartTime),
		fieldEnd:         jsonNull(),
		fieldProject:     jsonString(activity.Project),
		fieldDescription: jsonString(activity.Description),
		fieldNotes:       jsonString(strings.TrimSpace(activity.Notes)),
	}
	if activity.EndTime != nil {
		out[fieldEnd] = f.instant(*activity.EndTime)
	}
	for _, tag := range normalTags(activity.Tags) {
		out[tagPrefix+tag] = jsonBool(true)
	}
	return out
}

// reminderFields writes a reminder's fields; times without a value are left out.
func reminderFields(reminder models.Reminder, f form) fields {
	due := reminder.Due.In(time.Local)
	dueText := due.Format(dueLayout)
	if reminder.AllDay {
		dueText = due.Format(time.DateOnly)
	}
	until := ""
	if reminder.Repeat.Until != nil {
		until = reminder.Repeat.Until.In(time.Local).Format(time.DateOnly)
	}

	out := fields{
		fieldTitle:   jsonString(reminder.Title),
		fieldProject: jsonString(reminder.Project),
		fieldDue:     jsonString(dueText),
		fieldAllDay:  jsonBool(reminder.AllDay),
		fieldEvery:   jsonString(reminder.Repeat.Spec()),
		fieldUntil:   jsonString(until),
		fieldCount:   jsonNumber(int64(reminder.Repeat.Count)),
		fieldNotify:  jsonNumber(int64(reminder.Notify / time.Second)),
		fieldNotes:   jsonString(reminder.Notes),
	}
	if !reminder.Created.IsZero() {
		out[fieldCreated] = f.exact(reminder.Created)
	}
	for _, tag := range normalTags(reminder.Tags) {
		out[tagPrefix+tag] = jsonBool(true)
	}
	for day, state := range reminder.States {
		prefix := occurrencePrefix + day + "/"
		if state.DoneAt != nil {
			out[prefix+occurrenceDone] = f.exact(*state.DoneAt)
		}
		if state.SnoozedUntil != nil {
			out[prefix+occurrenceSnooze] = f.exact(*state.SnoozedUntil)
		}
		if state.ActivityStart != nil {
			out[prefix+occurrenceTrack] = f.exact(*state.ActivityStart)
		}
		if state.Skipped {
			out[prefix+occurrenceSkip] = jsonBool(true)
		}
	}
	return out
}

// diff returns the changes that turn the record old describes into the one current describes. Both
// are in image form; written holds the values to write, in journal form, for the paths of current.
// A path current no longer has is cleared.
func diff(old, current, written fields) fields {
	set := fields{}
	for path, value := range current {
		if oldValue, ok := old[path]; !ok || !bytes.Equal(oldValue, value) {
			set[path] = written[path]
		}
	}
	for path := range old {
		if _, ok := current[path]; !ok {
			set[path] = cleared(path)
		}
	}
	return set
}

// cleared is the value that clears a field a record no longer has: false for a tag or a skipped
// occurrence, null for the rest.
func cleared(path string) json.RawMessage {
	if strings.HasPrefix(path, tagPrefix) || (strings.HasPrefix(path, occurrencePrefix) && strings.HasSuffix(path, "/"+occurrenceSkip)) {
		return jsonBool(false)
	}
	return jsonNull()
}

// sameValue reports whether a set value states what the fields hold: the same value, or a cleared
// value for a field they do not hold. Activity times compare as instants.
func sameValue(path string, value json.RawMessage, in fields) bool {
	held, ok := in[path]
	switch {
	case !ok:
		return bytes.Equal(value, cleared(path))
	case isTimePath(path):
		return sameTime(value, held, false)
	}
	return bytes.Equal(value, held)
}

// isTimePath reports whether path holds an activity time, which images keep with the offset it was
// read at.
func isTimePath(path string) bool {
	return path == fieldStart || path == fieldEnd
}

// sameTime reports whether two activity time values read the same: the same wall time when wall is
// set, as a wall-clock backend compares them, else the same instant. Two values that are not times,
// such as two nulls, are the same when they are equal.
func sameTime(a, b json.RawMessage, wall bool) bool {
	ta, okA := parseInstant(a)
	tb, okB := parseInstant(b)
	switch {
	case !okA || !okB:
		return bytes.Equal(a, b)
	case wall:
		return ta.Format(wallKeyLayout) == tb.Format(wallKeyLayout)
	}
	return ta.Equal(tb)
}

// sameActivity reports whether two activity images describe the same values: the times as instants,
// whatever offset each was read at, the rest as they are.
func sameActivity(a, b fields) bool {
	if len(a) != len(b) {
		return false
	}
	for path, value := range a {
		other, ok := b[path]
		switch {
		case !ok:
			return false
		case isTimePath(path):
			if !sameTime(value, other, false) {
				return false
			}
		case !bytes.Equal(value, other):
			return false
		}
	}
	return true
}

// normalTags returns the tags trimmed, without empty ones and repeats, sorted.
func normalTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			out = append(out, tag)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// wallKeyLayout is a wall time with no offset: the key of an activity in a wall-clock backend.
const wallKeyLayout = "2006-01-02T15:04:05"

// keying makes the key of an activity's image: its start at the backend's precision, as the local
// wall time with no offset for a wall-clock backend, else in UTC (RFC 3339). A wall-clock backend
// keeps the wall time an activity was written at, so its key does not change with the Mac's time zone
// or in the hour repeated when daylight saving time ends.
type keying struct {
	precision time.Duration
	wall      bool
}

// key returns the key of an activity that starts at start.
func (k keying) key(start time.Time) string {
	t := start.Truncate(max(k.precision, time.Second))
	if k.wall {
		return t.In(time.Local).Format(wallKeyLayout)
	}
	return t.UTC().Format(time.RFC3339)
}

// writtenKey returns the key a wall-clock backend gives an activity whose line was written in the zone
// of start's own offset: its wall time there. A merged start keeps the offset it was journaled with, so
// this is the key of a line the Mac wrote before its time zone changed. It is "" for other backends and
// when it equals key(start).
func (k keying) writtenKey(start time.Time) string {
	if !k.wall {
		return ""
	}
	written := start.Truncate(max(k.precision, time.Second)).Format(wallKeyLayout)
	if written == k.key(start) {
		return ""
	}
	return written
}

// zoneSlack is how far the instant of a wall key read in one zone may lie from that of the same wall
// time in another: the widest gap between two UTC offsets, for a wall-clock backend.
func (k keying) zoneSlack() time.Duration {
	if k.wall {
		return 26 * time.Hour //nolint:mnd // UTC+14 to UTC-12
	}
	return 0
}

// keepWrittenTimes keeps, in the image of an activity read from a wall-clock backend, the merged start
// and end whose wall time, in the zone each was journaled in, is the one the backend holds: the line
// was written in that zone, so it holds the merged instant, not an edit of it.
func (r *run) keepWrittenTimes(current fields, record journal.Record) {
	if !r.wall {
		return
	}
	for _, path := range []string{fieldStart, fieldEnd} {
		merged, ok := record.Fields[path]
		if !ok {
			continue
		}
		t, parsed := parseInstant(merged.Value)
		if !parsed {
			continue
		}
		written := jsonString(t.Truncate(r.precision).Format(time.RFC3339))
		if sameTime(written, current[path], true) {
			current[path] = written
		}
	}
}

// parse reads a key back as a time: a wall key in local time, which in the repeated hour is the
// first of the two instants.
func (k keying) parse(key string) (time.Time, bool) {
	if k.wall {
		t, err := time.ParseInLocation(wallKeyLayout, key, time.Local)
		return t, err == nil
	}
	t, err := time.Parse(time.RFC3339, key)
	return t, err == nil
}

// slack is how far a key's instant may lie from that of an activity with the key: an hour for a
// wall-clock backend, whose key in the repeated hour reads as its first instant.
func (k keying) slack() time.Duration {
	if k.wall {
		return time.Hour
	}
	return 0
}

// parseInstant reads a JSON string holding an RFC 3339 instant.
func parseInstant(value json.RawMessage) (time.Time, bool) {
	var s string
	if json.Unmarshal(value, &s) != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// isDeleted reports whether a merged record holds deleted: true.
func isDeleted(record journal.Record) bool {
	var deleted bool
	field, ok := record.Fields[fieldDeleted]
	return ok && json.Unmarshal(field.Value, &deleted) == nil && deleted
}

// jsonString writes s as JSON without escaping HTML characters, as other writers do.
func jsonString(s string) json.RawMessage {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(s)
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}

func jsonBool(b bool) json.RawMessage {
	if b {
		return json.RawMessage("true")
	}
	return json.RawMessage("false")
}

func jsonNumber(n int64) json.RawMessage {
	return json.RawMessage(strconv.FormatInt(n, 10))
}

func jsonNull() json.RawMessage {
	return json.RawMessage("null")
}
