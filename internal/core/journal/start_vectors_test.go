package journal_test

import "encoding/json"

// startVectors returns the vectors of starting an activity (docs/sync.md, rule 1): the start stops the
// activity that runs as shown after the rules, with its auto_end when another device owns it and the
// new activity comes after it in the rule's order, and with its end otherwise.
func startVectors() []startVector {
	release := running("2026-11-18T09:00:00+01:00", "Mobile", "Release")
	late := running("2026-11-18T09:30:00+01:00", "Mobile", "Release")
	sameInstant := running("2026-11-18T10:00:00+01:00", "Mobile", "Release")
	at10 := newActivity{UID: act(9), Start: "2026-11-18T10:00:00+01:00"}
	backdated := newActivity{UID: act(9), Start: "2026-11-18T09:10:00+01:00"}
	return []startVector{
		{
			File: "start-after-other", Name: "another device's activity runs and the new one starts after it: its auto_end is " +
				"set to the new start, the text the new record holds",
			Journals: map[string][]json.RawMessage{devB: {change(devB, at(120, 0), typeActivity, act(1), release)}},
			Device:   devA, Now: "2026-11-18T09:00:05Z", TZ: berlin, Start: at10,
			Expect: []write{autoEndWrite(act(1), "2026-11-18T10:00:00+01:00")},
		},
		{
			File: "start-after-own", Name: "the device's own activity runs and the new one starts after it: its end is the new start",
			Journals: map[string][]json.RawMessage{devA: {change(devA, at(120, 0), typeActivity, act(1), release)}},
			Device:   devA, Now: "2026-11-18T09:00:05Z", TZ: berlin, Start: at10,
			Expect: []write{stopWrite(act(1), "2026-11-18T10:00:00+01:00")},
		},
		{
			File: "start-before-other", Name: "a start before another device's running activity stops it with end, at the " +
				"current time in whole seconds, written in the device's zone",
			Journals: map[string][]json.RawMessage{devB: {change(devB, at(120, 0), typeActivity, act(1), late)}},
			Device:   devA, Now: "2026-11-18T08:31:25.750Z", TZ: berlin, Start: backdated,
			Expect: []write{stopWrite(act(1), "2026-11-18T09:31:25+01:00")},
		},
		{
			File: "start-before-clock-behind", Name: "a start before the running activity, on a device whose clock is behind " +
				"that activity's start, ends it at its start, never before, written in the device's zone",
			Journals: map[string][]json.RawMessage{devB: {
				change(devB, at(120, 0), typeActivity, act(1), running("2026-11-18T08:30:00Z", "Mobile", "Release")),
			}},
			Device: devA, Now: "2026-11-18T08:20:00Z", TZ: berlin, Start: backdated,
			Expect: []write{stopWrite(act(1), "2026-11-18T09:30:00+01:00")},
		},
		{
			File: "start-same-instant-after", Name: "a start at the same instant as another device's running activity, with a " +
				"larger uid, comes after it: its auto_end is set",
			Journals: map[string][]json.RawMessage{devB: {change(devB, at(120, 0), typeActivity, act(5), sameInstant)}},
			Device:   devA, Now: "2026-11-18T09:00:05Z", TZ: berlin, Start: at10,
			Expect: []write{autoEndWrite(act(5), "2026-11-18T10:00:00+01:00")},
		},
		{
			File: "start-same-instant-before", Name: "a start at the same instant with a smaller uid comes before it: its end is " +
				"set, at that instant",
			Journals: map[string][]json.RawMessage{devB: {change(devB, at(120, 0), typeActivity, act(5), sameInstant)}},
			Device:   devA, Now: "2026-11-18T09:00:05Z", TZ: berlin,
			Start:  newActivity{UID: act(1), Start: "2026-11-18T10:00:00+01:00"},
			Expect: []write{stopWrite(act(5), "2026-11-18T10:00:00+01:00")},
		},
		{
			File: "start-stops-the-shown-one", Name: "two activities of other devices run; the start stops only the one that runs " +
				"as shown after the rules; the earlier one, which the rule shows stopped but nobody wrote yet, is left as it is, " +
				"and runs until the new start once the later one has its auto_end (docs/sync.md, Open questions)",
			Journals: map[string][]json.RawMessage{
				devB: {change(devB, at(120, 0), typeActivity, act(1), release)},
				devC: {change(devC, at(150, 0), typeActivity, act(2), late)},
			},
			Device: devA, Now: "2026-11-18T09:00:05Z", TZ: berlin, Start: at10,
			Expect: []write{autoEndWrite(act(2), "2026-11-18T10:00:00+01:00")},
		},
		{
			File: "start-nothing-runs", Name: "an activity stopped by its auto_end does not run, so the start stops nothing",
			Journals: map[string][]json.RawMessage{
				devB: {change(devB, at(120, 0), typeActivity, act(1), release)},
				devC: {
					change(devC, at(150, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T09:30:00+01:00"}),
					change(devC, at(150, 1), typeActivity, act(2), late),
					change(devC, at(170, 0), typeActivity, act(2), fields{"end": "2026-11-18T09:50:00+01:00"}),
				},
			},
			Device: devA, Now: "2026-11-18T09:00:05Z", TZ: berlin, Start: at10,
			Expect: []write{},
		},
		{
			File: "start-stale-auto-end", Name: "an auto_end before the activity's start counts as missing, so the activity " +
				"runs and the start sets its auto_end",
			Journals: map[string][]json.RawMessage{
				devB: {change(devB, at(120, 0), typeActivity, act(1), late)},
				devC: {change(devC, at(110, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T09:10:00+01:00"})},
			},
			Device: devA, Now: "2026-11-18T09:00:05Z", TZ: berlin, Start: at10,
			Expect: []write{autoEndWrite(act(1), "2026-11-18T10:00:00+01:00")},
		},
	}
}

// reopenVectors returns the vectors of reopening an activity on purpose: one change sets end and
// auto_end to null.
func reopenVectors() []reopenVector {
	api := running("2026-11-18T09:00:00Z", "Backend", "API")
	deploy := running("2026-11-18T09:30:00Z", "Ops", "Deploy")
	reopen := []write{{Type: typeActivity, UID: act(1), Set: fields{"end": nil, "auto_end": nil}}}
	return []reopenVector{
		{
			File: "reopen-auto-end", Name: "reopening an activity stopped at its auto_end clears end and auto_end in one change, " +
				"and it runs again",
			Journals: map[string][]json.RawMessage{
				devA: {change(devA, at(120, 0), typeActivity, act(1), api)},
				devB: {
					change(devB, at(150, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T09:30:00Z"}),
					change(devB, at(150, 1), typeActivity, act(2), deploy),
					change(devB, at(170, 0), typeActivity, act(2), fields{"end": "2026-11-18T09:50:00Z"}),
				},
			},
			Device: devA, UID: act(1),
			Expect: reopenExpect{
				Edits: reopen,
				Activities: []record{
					merged(act(1), with(api, fields{"auto_end": nil})),
					merged(act(2), with(deploy, fields{"end": "2026-11-18T09:50:00Z"})),
				},
			},
		},
		{
			File: "reopen-before-a-running-one", Name: "any device may reopen an activity, its owner or not; while a later one " +
				"runs, the rule then shows it stopped at that one's start",
			Journals: map[string][]json.RawMessage{
				devA: {
					change(devA, at(120, 0), typeActivity, act(1), api),
					change(devA, at(140, 0), typeActivity, act(1), fields{"end": "2026-11-18T09:20:00Z"}),
				},
				devB: {change(devB, at(150, 0), typeActivity, act(2), deploy)},
			},
			Device: devB, UID: act(1),
			Expect: reopenExpect{
				Edits: reopen,
				Activities: []record{
					merged(act(1), with(api, fields{"end": "2026-11-18T09:30:00Z", "auto_end": nil})),
					merged(act(2), deploy),
				},
			},
		},
	}
}
