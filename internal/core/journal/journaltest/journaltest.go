// Package journaltest holds the sync test vectors of docs/sync.md. The journal tests run them, and
// `make sync-vectors` writes them as JSON files for tock-ios.
package journaltest

import (
	"encoding/json"
	"fmt"
)

// HLCVector checks how clocks parse and compare, and how a device's clock moves.
type HLCVector struct {
	File    string      `json:"-"`
	Name    string      `json:"name"`
	Valid   []string    `json:"valid,omitempty"`
	Invalid []string    `json:"invalid,omitempty"`
	Order   [][2]string `json:"order,omitempty"`
	Last    string      `json:"last,omitempty"`
	Steps   []HLCStep   `json:"steps,omitempty"`
}

// HLCStep is one step on a clock: "new" makes a change at Now, "read" reads HLC at Now.
type HLCStep struct {
	Op     string `json:"op"`
	Now    int64  `json:"now"`
	HLC    string `json:"hlc,omitempty"`
	Expect string `json:"expect"`
}

// MergeVector merges journals and lists the records left visible after the rules.
type MergeVector struct {
	File     string                       `json:"-"`
	Name     string                       `json:"name"`
	Journals map[string][]json.RawMessage `json:"journals"`
	Expect   MergeExpect                  `json:"expect"`
}

// MergeExpect is the result of a merge: visible records in uid order and the number of skipped lines.
type MergeExpect struct {
	Activities []Record `json:"activities"`
	Reminders  []Record `json:"reminders"`
	Skipped    int      `json:"skipped"`
}

// Record is a visible record with all its merged fields.
type Record struct {
	UID    string         `json:"uid"`
	Fields map[string]any `json:"fields"`
}

// RepeatVector lists the occurrences of a reminder on the days [From, To) at Now, in time zone TZ.
type RepeatVector struct {
	File     string         `json:"-"`
	Name     string         `json:"name"`
	TZ       string         `json:"tz"`
	Reminder map[string]any `json:"reminder"`
	From     string         `json:"from"`
	To       string         `json:"to"`
	Now      string         `json:"now"`
	Expect   []Occurrence   `json:"expect"`
}

// Occurrence is the due wall time (a date for an all-day reminder) and status of one occurrence.
type Occurrence struct {
	Due    string `json:"due"`
	Status string `json:"status"`
}

type fields = map[string]any

const (
	devA = "7f3a9c2e5b1d4a60"
	devB = "2b91c4d7e0f3a815"
	devC = "5e8d1a2b3c4f6071"

	typeActivity = "activity"
	typeReminder = "reminder"

	// baseWall is 2026-11-18T07:00:00Z in milliseconds since the Unix epoch.
	baseWall      = 1794985200000
	msPerMinute   = 60_000
	berlin        = "Europe/Berlin"
	statusPending = "pending"
)

// at returns the clock minutes after 07:00 UTC on 18 November 2026, with a counter.
func at(minutes, counter int) string {
	return fmt.Sprintf("%016d.%06d", baseWall+int64(minutes)*msPerMinute, counter)
}

// act and rem return the uid of the n-th activity or reminder of a vector.
func act(n int) string { return fmt.Sprintf("019a9b3c-0000-7000-8000-0000000000a%d", n) }
func rem(n int) string { return fmt.Sprintf("019a9b3c-0000-7000-8000-0000000000b%d", n) }

// change is a journal line of dev that sets fields of a record.
func change(dev, hlc, typ, uid string, set fields) json.RawMessage {
	return mustJSON(struct {
		V    int    `json:"v"`
		HLC  string `json:"hlc"`
		Dev  string `json:"dev"`
		Type string `json:"type"`
		UID  string `json:"uid"`
		Set  fields `json:"set"`
	}{V: 1, HLC: hlc, Dev: dev, Type: typ, UID: uid, Set: set})
}

// object is a journal line holding any JSON object, for changes that are not valid.
func object(obj fields) json.RawMessage { return mustJSON(obj) }

// text is a journal line written exactly as it is: not JSON, or cut off before its "\n".
func text(s string) json.RawMessage { return mustJSON(s) }

func want(uid string, merged fields) Record { return Record{UID: uid, Fields: merged} }

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("journaltest: %v", err))
	}
	return data
}
