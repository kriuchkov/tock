package journal

import (
	"cmp"
	"encoding/json"
	"maps"
	"slices"
	"time"
)

// Stop is an end the one-running rule gives an activity.
type Stop struct {
	UID string
	// End is the start of the next running activity, as its record holds it.
	End json.RawMessage
	// Owner is the device that owns the activity (see Record.Owner): the only one that writes End.
	Owner string
}

// Rules is what the rules after the merge give one device.
type Rules struct {
	// Stops are the ends the one-running rule gives, in the order of the activities' starts. Every
	// device shows them, whether it writes them or not.
	Stops []Stop
	// Writes are the stops the device writes as changes: those of the activities it owns.
	Writes []Stop
}

// Owner returns the device that owns the record: the one whose change holds the merged value of its
// start. Ownership moves to the device that edits the start. It is "" for a record without a start.
func (r Record) Owner() string {
	return r.Fields[fieldStart].Dev
}

// ApplyRules applies the rules after the merge as device dev does. Every device shows the same
// stops, but only an activity's owner writes its end: an end the owner wrote and another device has
// not read yet must never lose to an end that device computed without it.
func (s *State) ApplyRules(dev string) Rules {
	return RulesFor(s.Records(TypeActivity), dev)
}

// RulesFor applies the rules after the merge to merged records as device dev does, like
// State.ApplyRules, for a caller that keeps the merged state elsewhere. It needs every visible
// activity that runs; it ignores hidden records, finished activities and reminders.
func RulesFor(records []Record, dev string) Rules {
	rules := Rules{Stops: stops(records)}
	for _, stop := range rules.Stops {
		if stop.Owner == dev {
			rules.Writes = append(rules.Writes, stop)
		}
	}
	return rules
}

// Show returns the record as every device shows it after the rules: an activity the one-running
// rule stops gets its end. That end is computed, not written, so its field has no clock and no
// device.
func (r Rules) Show(record Record) Record {
	if record.Type != TypeActivity {
		return record
	}
	for _, stop := range r.Stops {
		if stop.UID == record.UID {
			record.Fields = maps.Clone(record.Fields)
			record.Fields[fieldEnd] = Field{Value: stop.End}
			return record
		}
	}
	return record
}

// Stops applies the rule that one activity runs at a time. Two devices can each start an activity
// before they see each other's change; then every running activity but the last by start (ties go
// by uid) stops at the start of the next one. Every device shows these ends; only an activity's
// owner writes its end as a change (see ApplyRules).
func (s *State) Stops() []Stop {
	return stops(s.Records(TypeActivity))
}

// stops gives the ends of the one-running rule for the running activities among records.
func stops(records []Record) []Stop {
	type running struct {
		uid   string
		owner string
		start time.Time
		raw   json.RawMessage
	}

	var activities []running
	for _, record := range records {
		activity, ok := record.Activity()
		if ok && activity.EndTime == nil {
			activities = append(activities, running{
				uid:   record.UID,
				owner: record.Owner(),
				start: activity.StartTime,
				raw:   record.Fields[fieldStart].Value,
			})
		}
	}
	slices.SortFunc(activities, func(a, b running) int {
		if c := a.start.Compare(b.start); c != 0 {
			return c
		}
		return cmp.Compare(a.uid, b.uid)
	})

	var out []Stop
	for i := 1; i < len(activities); i++ {
		out = append(out, Stop{UID: activities[i-1].uid, End: activities[i].raw, Owner: activities[i-1].owner})
	}
	return out
}
