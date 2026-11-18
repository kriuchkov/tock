package journal_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
)

// mergeVectors returns the merge vectors.
func mergeVectors() []mergeVector {
	vectors := []mergeVector{
		mergeLastWriterWins(),
		mergeEqualClocks(),
		mergeDeleteAndUndo(),
		mergeHiddenRecords(),
		mergeInvalidLines(),
		mergeTags(),
		mergeOneRunning(),
		mergeReminders(),
	}
	for i := range vectors {
		if vectors[i].Expect.Activities == nil {
			vectors[i].Expect.Activities = []record{}
		}
		if vectors[i].Expect.Reminders == nil {
			vectors[i].Expect.Reminders = []record{}
		}
	}
	return vectors
}

func mergeLastWriterWins() mergeVector {
	return mergeVector{
		File: "last-writer-wins",
		Name: "each field takes the value of the latest change that sets it",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(0, 0), typeActivity, act(1), fields{
					"start": "2026-11-18T10:00:00+03:00", "end": "2026-11-18T11:00:00+03:00",
					"project": "Backend", "description": "API", "notes": "",
				}),
				change(devA, at(5, 0), typeActivity, act(1), fields{"project": "Platform"}),
				change(devA, at(10, 0), typeActivity, act(1), fields{"description": "API design"}),
			},
			devB: {
				change(devB, at(5, 0), typeActivity, act(1), fields{"description": "API review"}),
				change(devB, at(7, 0), typeActivity, act(1), fields{"end": "2026-11-18T11:30:00+03:00", "notes": "Moved to Thursday"}),
			},
		},
		Expect: mergeExpect{Activities: []record{
			merged(act(1), fields{
				"start": "2026-11-18T10:00:00+03:00", "end": "2026-11-18T11:30:00+03:00",
				"project": "Platform", "description": "API design", "notes": "Moved to Thursday",
			}),
		}},
	}
}

func mergeEqualClocks() mergeVector {
	return mergeVector{
		File: "equal-clocks",
		Name: "equal clocks: the larger dev wins; within one journal the later line wins; a duplicated line does no harm",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(0, 0), typeActivity, act(1), fields{
					"start": "2026-11-18T09:00:00Z", "end": "2026-11-18T10:00:00Z", "project": "Docs", "description": "", "notes": "",
				}),
				change(devA, at(5, 0), typeActivity, act(1), fields{"project": "Docs A"}),
				change(devA, at(10, 0), typeActivity, act(1), fields{"description": "first"}),
				change(devA, at(10, 0), typeActivity, act(1), fields{"description": "second"}),
			},
			devB: {
				change(devB, at(5, 0), typeActivity, act(1), fields{"project": "Docs B"}),
				change(devB, at(10, 0), typeActivity, act(1), fields{"notes": "From the phone"}),
				change(devB, at(10, 0), typeActivity, act(1), fields{"notes": "From the phone"}),
			},
		},
		Expect: mergeExpect{Activities: []record{
			merged(act(1), fields{
				"start": "2026-11-18T09:00:00Z", "end": "2026-11-18T10:00:00Z",
				"project": "Docs A", "description": "second", "notes": "From the phone",
			}),
		}},
	}
}

func mergeDeleteAndUndo() mergeVector {
	debrief := fields{
		"start": "2026-11-17T16:00:00+01:00", "end": "2026-11-17T17:00:00+01:00",
		"project": "Hiring", "description": "Debrief", "notes": "",
	}
	return mergeVector{
		File: "delete-and-undo",
		Name: "a deleted record stays hidden after later edits; deleted false brings it back with all its fields",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(0, 0), typeActivity, act(1), fields{
					"start": "2026-11-17T14:00:00+01:00", "end": "2026-11-17T15:00:00+01:00",
					"project": "Hiring", "description": "Interview", "notes": "",
				}),
				change(devA, at(1, 0), typeActivity, act(2), debrief),
				change(devA, at(5, 0), typeActivity, act(2), fields{"deleted": true}),
				change(devA, at(10, 0), typeActivity, act(1), fields{"description": "Interview, round 2"}),
				change(devA, at(20, 0), typeReminder, rem(1), fields{"deleted": true}),
			},
			devB: {
				change(devB, at(2, 0), typeReminder, rem(1), fields{
					"title": "Send offer", "project": "Hiring", "due": "2026-11-20T10:00", "all_day": false,
					"every": "", "until": "", "count": 0, "notify": 0, "notes": "", "created": "2026-11-18T07:02:00Z",
				}),
				change(devB, at(5, 0), typeActivity, act(1), fields{"deleted": true}),
				change(devB, at(15, 0), typeActivity, act(2), fields{"deleted": false}),
			},
		},
		Expect: mergeExpect{Activities: []record{
			merged(act(2), with(debrief, fields{"deleted": false})),
		}},
	}
}

func mergeHiddenRecords() mergeVector {
	return mergeVector{
		File: "hidden-records",
		Name: "records missing a required field are hidden; a value of the wrong type counts as missing; " +
			"a cut-off last line is not read",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(0, 0), typeActivity, act(1), fields{
					"start": "2026-11-18T09:00:00+01:00", "end": nil, "project": "", "description": "Empty project", "notes": "",
				}),
				change(devA, at(0, 1), typeActivity, act(2), fields{
					"start": "2026-11-18T09:00:00.500+01:00", "end": "2026-11-18T10:00:00+01:00",
					"project": "Ops", "description": "Fractional seconds", "notes": "",
				}),
				change(devA, at(0, 2), typeActivity, act(3), fields{"description": "An edit that arrived before the record"}),
				change(devA, at(0, 3), typeActivity, act(4), fields{
					"start": "2026-11-18T09:00:00+01:00", "end": "2026-11-18T10:00:00+01:00",
					"project": 42, "description": "Project is a number", "notes": "",
				}),
				change(devA, at(0, 4), typeActivity, act(5), fields{
					"start": "2026-11-18T11:00:00+01:00", "end": "later", "project": "Ops", "description": 7, "notes": "",
				}),
				change(devA, at(0, 5), typeActivity, act(6), fields{
					"start": "2026-11-18", "end": nil, "project": "Ops", "description": "Start is a date", "notes": "",
				}),
				change(devA, at(1, 0), typeReminder, rem(1), fields{"title": "No due date", "all_day": false, "every": ""}),
				change(devA, at(1, 1), typeReminder, rem(2), fields{
					"title": "No such day", "due": "2026-11-31T10:00", "all_day": false, "every": "",
				}),
				change(devA, at(1, 2), typeReminder, rem(3), fields{"title": "", "due": "2026-11-20T10:00", "all_day": false, "every": ""}),
				change(devA, at(1, 3), typeReminder, rem(4), fields{
					"title": "Rule not in canonical form", "due": "2026-11-20T10:00", "all_day": false, "every": "Daily",
				}),
				text(string(change(devA, at(2, 0), typeActivity, act(7), fields{
					"start": "2026-11-18T12:00:00+01:00", "end": nil, "project": "Ops", "description": "Cut off", "notes": "",
				}))),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(
					act(5),
					fields{"start": "2026-11-18T11:00:00+01:00", "end": "later", "project": "Ops", "description": 7, "notes": ""},
				),
			},
			Reminders: []record{
				merged(
					rem(4),
					fields{"title": "Rule not in canonical form", "due": "2026-11-20T10:00", "all_day": false, "every": "Daily"},
				),
			},
		},
	}
}

func mergeInvalidLines() mergeVector {
	// line returns a valid change of devA at 07:01 with the given counter, edited by edit.
	line := func(counter int, description string, edit func(fields)) json.RawMessage {
		obj := fields{
			"v": 1, "hlc": at(1, counter), "dev": devA, "type": typeActivity, "uid": act(1),
			"set": fields{"description": description},
		}
		edit(obj)
		return object(obj)
	}
	return mergeVector{
		File: "invalid-lines",
		Name: "a complete line that is not a valid change of the journal's device is skipped; unknown keys are ignored",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(0, 0), typeActivity, act(1), fields{
					"start": "2026-11-18T09:00:00Z", "end": "2026-11-18T10:00:00Z", "project": "Ops", "description": "Deploy", "notes": "",
				}),
				text("not json\n"),
				text("\n"),
				text("[]\n"),
				line(0, "version 2", func(o fields) { o["v"] = 2 }),
				line(1, "version is a string", func(o fields) { o["v"] = "1" }),
				line(2, "another device's change", func(o fields) { o["dev"] = devB }),
				line(3, "uppercase uid", func(o fields) { o["uid"] = strings.ToUpper(act(1)) }),
				line(4, "UUIDv4", func(o fields) { o["uid"] = "019a9b3c-0000-4000-8000-0000000000a1" }),
				line(5, "", func(o fields) { o["set"] = fields{} }),
				line(6, "", func(o fields) { o["set"] = fields{"": "empty path"} }),
				line(7, "short clock", func(o fields) { o["hlc"] = "1794985260000.000007" }),
				line(8, "unknown type", func(o fields) { o["type"] = "note" }),
				line(9, "no type", func(o fields) { delete(o, "type") }),
				line(10, "key in the wrong case", func(o fields) { o["HLC"] = o["hlc"]; delete(o, "hlc") }),
				line(11, "", func(o fields) { o["set"] = []string{"description"} }),
				object(fields{
					"v": 1, "hlc": at(5, 0), "dev": devA, "type": typeActivity, "uid": act(1),
					"set": fields{"description": "Deploy v2"}, "comment": "an unknown key",
				}),
				text(string(change(devA, at(10, 0), typeActivity, act(1), fields{"description": "cut off"}))),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), fields{
					"start":       "2026-11-18T09:00:00Z",
					"end":         "2026-11-18T10:00:00Z",
					"project":     "Ops",
					"description": "Deploy v2",
					"notes":       "",
				}),
			},
			Skipped: 15,
		},
	}
}

func mergeTags() mergeVector {
	return mergeVector{
		File: "tags",
		Name: "each tag merges on its own, and a tag may contain a slash",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(0, 0), typeActivity, act(1), fields{
					"start": "2026-11-18T09:00:00+01:00", "end": "2026-11-18T11:00:00+01:00", "project": "Research",
					"description": "Paper", "notes": "", "tags/deep-work": true, "tags/client/acme": true,
				}),
				change(devA, at(10, 0), typeActivity, act(1), fields{"tags/review": true}),
			},
			devB: {
				change(devB, at(5, 0), typeActivity, act(1), fields{"tags/deep-work": false}),
			},
		},
		Expect: mergeExpect{Activities: []record{
			merged(act(1), fields{
				"start": "2026-11-18T09:00:00+01:00", "end": "2026-11-18T11:00:00+01:00", "project": "Research",
				"description": "Paper", "notes": "", "tags/deep-work": false, "tags/client/acme": true, "tags/review": true,
			}),
		}},
	}
}

func mergeOneRunning() mergeVector {
	running := func(start, project, description string) fields {
		return fields{"start": start, "end": nil, "project": project, "description": description, "notes": ""}
	}
	standUp := fields{
		"start":       "2026-11-18T06:00:00Z",
		"end":         "2026-11-18T06:30:00Z",
		"project":     "Ops",
		"description": "Stand-up",
		"notes":       "",
	}
	api := running("2026-11-18T10:00:00+03:00", "Backend", "API")
	release := running("2026-11-18T08:30:00+01:00", "Mobile", "Release")
	notes := running("2026-11-18T07:30:00Z", "Mobile", "Release notes")
	return mergeVector{
		File: "one-running",
		Name: "of the activities started on several devices, each but the last stops at the start of the next one",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(0, 0), typeActivity, act(5), standUp),
				change(devA, at(0, 1), typeActivity, act(1), api),
				change(devA, at(0, 2), typeActivity, act(4), running("2026-11-18T07:45:00Z", "Ops", "Started by mistake")),
				change(devA, at(1, 0), typeActivity, act(4), fields{"deleted": true}),
			},
			devB: {change(devB, at(30, 0), typeActivity, act(2), release)},
			devC: {change(devC, at(31, 0), typeActivity, act(3), notes)},
		},
		Expect: mergeExpect{Activities: []record{
			merged(act(1), with(api, fields{"end": "2026-11-18T08:30:00+01:00"})),
			merged(act(2), with(release, fields{"end": "2026-11-18T07:30:00Z"})),
			merged(act(3), notes),
			merged(act(5), standUp),
		}},
	}
}

func mergeReminders() mergeVector {
	created := fields{
		"title": "Pay rent", "project": "", "due": "2026-12-01", "all_day": true, "every": "monthly",
		"until": "", "count": 0, "notify": 0, "notes": "", "created": "2026-11-18T07:00:00Z",
	}
	return mergeVector{
		File: "reminders",
		Name: "occurrence states merge field by field; fields a reader does not know are kept",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(0, 0), typeReminder, rem(1), created),
				change(devA, at(5, 0), typeReminder, rem(1), fields{"occ/2026-12-01/snoozed": "2026-12-01T12:00:00+01:00"}),
				change(devA, at(10, 0), typeReminder, rem(1), fields{"occ/2027-01-01/skipped": true}),
				change(devA, at(20, 0), typeReminder, rem(1), fields{"tags/home": true}),
			},
			devB: {
				change(devB, at(5, 0), typeReminder, rem(1), fields{"occ/2026-12-01/done": "2026-12-01T09:12:00+01:00"}),
				change(devB, at(15, 0), typeReminder, rem(1), fields{"occ/2027-01-01/skipped": false}),
				change(devB, at(15, 1), typeReminder, rem(1), fields{"title": "Pay the rent", "color": "green"}),
			},
		},
		Expect: mergeExpect{Reminders: []record{
			merged(rem(1), with(created, fields{
				"title":                  "Pay the rent",
				"occ/2026-12-01/snoozed": "2026-12-01T12:00:00+01:00",
				"occ/2026-12-01/done":    "2026-12-01T09:12:00+01:00",
				"occ/2027-01-01/skipped": false,
				"tags/home":              true,
				"color":                  "green",
			})),
		}},
	}
}

// with returns a copy of base with the values of changes set.
func with(base, changes fields) fields {
	out := maps.Clone(base)
	maps.Copy(out, changes)
	return out
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

// merged is a visible record with all its merged fields.
func merged(uid string, values fields) record { return record{UID: uid, Fields: values} }

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("encode vector: %v", err))
	}
	return data
}
