package journal

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"time"
)

// Stop is an end the one-running rule gives an activity.
type Stop struct {
	UID string
	// End is the start of the next running activity, as its record holds it.
	End json.RawMessage
	// Owner is the device that owns the activity (see Record.Owner): the one that writes End as its
	// end. Every other device writes it as its auto_end (see StopField).
	Owner string
}

// Write is a change the rules give one device to write: Value at the path Field of the activity UID.
type Write struct {
	UID string
	// Field is FieldEnd for an activity the device owns, FieldAutoEnd for any other.
	Field string
	Value json.RawMessage
}

// Set returns the fields of the change that makes the write.
func (w Write) Set() map[string]json.RawMessage {
	return map[string]json.RawMessage{w.Field: w.Value}
}

// Rules is what the rules after the merge give one device.
type Rules struct {
	// Stops are the ends the one-running rule gives, in the order of the activities' starts. Every
	// device shows them.
	Stops []Stop
	// Writes are the changes the device writes for them, one per stop and in the same order: the end
	// of an activity it owns, the auto_end of any other. Once they are written each of those
	// activities shows the end the rule gives it, so the rule gives them no more.
	Writes []Write
}

// StopField returns the field in which device dev writes an end the one-running rule gives an
// activity of owner: FieldEnd when dev is the owner, FieldAutoEnd otherwise. Starting an activity
// that comes after a running one in the rule's order stops that one by the same choice
// (docs/sync.md, rule 1).
func StopField(owner, dev string) string {
	if owner == dev {
		return FieldEnd
	}
	return FieldAutoEnd
}

// Reopen returns the fields of a change that makes an activity run again on purpose, as an undo of
// a stop or an edit that clears its end: end and auto_end both null, so that neither stops it.
func Reopen() map[string]json.RawMessage {
	return map[string]json.RawMessage{FieldEnd: json.RawMessage("null"), FieldAutoEnd: json.RawMessage("null")}
}

// Owner returns the device that owns the record: the one whose change holds the merged value of its
// start. Ownership moves to the device that edits the start. It is "" for a record without a start.
func (r Record) Owner() string {
	return r.Fields[fieldStart].Dev
}

// ApplyRules applies the rules after the merge as device dev does. Every device shows the same
// stops and writes each of them: the owner as the activity's end, every other device as its
// auto_end. An end always shows over an auto_end, so an end the owner wrote and another device has
// not read yet never loses to an auto_end that device computed without it.
func (s *State) ApplyRules(dev string) Rules {
	return RulesFor(s.Records(TypeActivity), dev)
}

// RulesFor applies the rules after the merge to merged records as device dev does, like
// State.ApplyRules, for a caller that keeps the merged state elsewhere. It needs every visible
// activity that runs, and every one whose shown end is an auto_end later than the earliest start
// among those; it ignores the other records.
func RulesFor(records []Record, dev string) Rules {
	rules := Rules{Stops: stops(records)}
	for _, stop := range rules.Stops {
		rules.Writes = append(rules.Writes, Write{UID: stop.UID, Field: StopField(stop.Owner, dev), Value: stop.End})
	}
	return rules
}

// Show returns the record as every device shows it after the rules. The end of an activity is end
// when that holds an instant, otherwise the end the one-running rule gives it, if any, otherwise its
// auto_end when that holds one (see shownEnd). An end filled in so is computed, not written, so its
// field has no clock and no device; auto_end itself stays as merged. Any other record comes back as
// it is.
func (r Rules) Show(record Record) Record {
	if record.Type != TypeActivity {
		return record
	}
	_, path, shown := record.shownEnd()
	if shown && path == FieldEnd {
		return record
	}
	var end json.RawMessage
	switch i := slices.IndexFunc(r.Stops, func(stop Stop) bool { return stop.UID == record.UID }); {
	case i >= 0:
		end = r.Stops[i].End
	case shown:
		end = record.Fields[path].Value
	default:
		return record
	}
	record.Fields = maps.Clone(record.Fields)
	record.Fields[FieldEnd] = Field{Value: end}
	return record
}

// Stops applies the rule that one activity runs at a time. Two devices can each start an activity
// before they see each other's change; then every running activity (visible, with no shown end) but
// the last by start (ties go by uid) stops at the start of the next one. An activity whose shown end
// is its auto_end stops at the start of the first running activity after it in that order, when that
// start is before its auto_end: two devices that each started an activity while it ran, before they
// read each other's start, both wrote its auto_end, and the later clock won whatever the instants.
// Every device shows these ends and writes them (see ApplyRules).
func (s *State) Stops() []Stop {
	return stops(s.Records(TypeActivity))
}

// ruled is an activity as the one-running rule sorts it.
type ruled struct {
	uid   string
	owner string
	start time.Time
	// raw is start as the record holds it.
	raw json.RawMessage
	// until is the auto_end that ends an activity stopped by one; zero while it runs.
	until time.Time
}

// after reports whether a comes after b in the rule's order: by start, then by uid.
func (a ruled) after(b ruled) bool {
	if c := a.start.Compare(b.start); c != 0 {
		return c > 0
	}
	return a.uid > b.uid
}

// stops gives the ends of the one-running rule for the activities among records, in the order of
// their starts.
func stops(records []Record) []Stop {
	running, autoEnded := ruledActivities(records)
	slices.SortFunc(running, func(a, b ruled) int {
		if c := a.start.Compare(b.start); c != 0 {
			return c
		}
		return cmp.Compare(a.uid, b.uid)
	})

	type stopped struct {
		ruled

		end json.RawMessage
	}
	var out []stopped
	for i := 1; i < len(running); i++ {
		out = append(out, stopped{ruled: running[i-1], end: running[i].raw})
	}
	for _, a := range autoEnded {
		next := slices.IndexFunc(running, func(r ruled) bool { return r.after(a) })
		if next >= 0 && running[next].start.Before(a.until) {
			out = append(out, stopped{ruled: a, end: running[next].raw})
		}
	}
	slices.SortFunc(out, func(a, b stopped) int {
		if c := a.start.Compare(b.start); c != 0 {
			return c
		}
		return cmp.Compare(a.uid, b.uid)
	})

	result := make([]Stop, 0, len(out))
	for _, s := range out {
		result = append(result, Stop{UID: s.uid, End: s.end, Owner: s.owner})
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ruledActivities returns the visible activities of records that run, and those whose shown end is
// their auto_end.
func ruledActivities(records []Record) ([]ruled, []ruled) {
	var running, autoEnded []ruled
	for _, record := range records {
		activity, ok := record.Activity()
		if !ok {
			continue
		}
		a := ruled{uid: record.UID, owner: record.Owner(), start: activity.StartTime, raw: record.Fields[fieldStart].Value}
		end, path, shown := record.shownEnd()
		switch {
		case !shown:
			running = append(running, a)
		case path == FieldAutoEnd:
			a.until = end
			autoEnded = append(autoEnded, a)
		}
	}
	return running, autoEnded
}

// StartStops returns the changes device dev writes, besides the new activity's own, when it starts the
// activity of uid at start, the text its record holds, at now: the stop of the activity that runs as
// shown after the rules, if one does, as StartStop gives it, with an end at now written in loc.
func (s *State) StartStops(dev, uid string, start json.RawMessage, now time.Time, loc *time.Location) []Write {
	records := s.Records(TypeActivity)
	rules := RulesFor(records, dev)
	next := Record{Type: TypeActivity, UID: uid, Fields: map[string]Field{fieldStart: {Value: start}}}
	var out []Write
	for _, record := range records {
		if activity, ok := rules.Show(record).Activity(); ok && activity.EndTime == nil && record.UID != uid {
			out = append(out, StartStop(record, next, dev, now, loc))
		}
	}
	return out
}

// StartStop returns what device dev writes for the activity of record, which runs as shown after the
// rules, when it starts the activity of next at now (docs/sync.md, rule 1). next needs only its uid
// and start. One another device owns, when next comes after it in the rule's order, gets its auto_end
// set to next's start, the text next's record holds (StartStopField). Any other gets its end at the
// time StartStopTime gives: next's start as next's record holds it, or now, written in loc.
func StartStop(record, next Record, dev string, now time.Time, loc *time.Location) Write {
	nextStart := next.Fields[fieldStart].Value
	field := StartStopField(record, next, dev)
	start, _ := record.instant(fieldStart)
	at, _ := next.instant(fieldStart)
	if field == FieldAutoEnd || !at.Before(start) {
		return Write{UID: record.UID, Field: field, Value: nextStart}
	}
	end := StartStopTime(start, at, now).In(loc).Format(time.RFC3339)
	return Write{UID: record.UID, Field: field, Value: json.RawMessage(strconv.Quote(end))}
}

// StartStopField returns the field in which device dev writes the stop of the activity of record when
// it starts the activity of next while that one runs as shown after the rules: FieldAutoEnd when
// another device owns it and next comes after it in the rule's order (a later start, or the same
// instant and a larger uid), the end the rule gives it once next runs, written at once so that it
// holds even when next stops before the owner syncs; FieldEnd otherwise.
func StartStopField(record, next Record, dev string) string {
	start, _ := record.instant(fieldStart)
	at, _ := next.instant(fieldStart)
	later := ruled{uid: next.UID, start: at}.after(ruled{uid: record.UID, start: start})
	if record.Owner() != dev && later {
		return FieldAutoEnd
	}
	return FieldEnd
}

// StartStopTime returns the end that starting an activity at next gives an activity that runs from
// start, at now: next, or, when next is before start, now in whole seconds, never before start.
func StartStopTime(start, next, now time.Time) time.Time {
	if !next.Before(start) {
		return next
	}
	now = now.Truncate(time.Second)
	if now.Before(start) {
		return start
	}
	return now
}
