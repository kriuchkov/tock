package journal

import (
	"cmp"
	"encoding/json"
	"slices"
	"time"
)

// Stop is an end the one-running rule gives an activity.
type Stop struct {
	UID string
	// End is the start of the next running activity, as its record holds it.
	End json.RawMessage
}

// Stops applies the rule that one activity runs at a time. Two devices can each start an activity
// before they see each other's change; then every running activity but the last by start (ties go
// by uid) stops at the start of the next one. The device writes the returned ends as changes.
func (s *State) Stops() []Stop {
	type running struct {
		uid   string
		start time.Time
		raw   json.RawMessage
	}

	var activities []running
	for _, record := range s.Records(TypeActivity) {
		activity, ok := record.Activity()
		if ok && activity.EndTime == nil {
			activities = append(activities, running{uid: record.UID, start: activity.StartTime, raw: record.Fields[fieldStart].Value})
		}
	}
	slices.SortFunc(activities, func(a, b running) int {
		if c := a.start.Compare(b.start); c != 0 {
			return c
		}
		return cmp.Compare(a.uid, b.uid)
	})

	var stops []Stop
	for i := 1; i < len(activities); i++ {
		stops = append(stops, Stop{UID: activities[i-1].uid, End: activities[i].raw})
	}
	return stops
}
