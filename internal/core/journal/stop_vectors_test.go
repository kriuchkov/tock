package journal_test

import "encoding/json"

// stopVectors returns the merge vectors of the one-running rule: every device shows the ends it
// gives and writes them, an activity's owner as its end and every other device as its auto_end.
func stopVectors() []mergeVector {
	return []mergeVector{
		mergeOneRunning(),
		stopTwoRunning(),
		stopOwnerOfEarlier(),
		stopOwnerMoved(),
		stopOwnerStopUnseen(),
		stopOwnerStop(),
		stopDeliberate(),
	}
}

func mergeOneRunning() mergeVector {
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
		Name: "of the activities started on several devices, each but the last shows as stopped at the start of the next one; " +
			"each device writes those ends: as end for the activities it owns, as auto_end for the others",
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
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{"end": "2026-11-18T08:30:00+01:00"})),
				merged(act(2), with(release, fields{"end": "2026-11-18T07:30:00Z"})),
				merged(act(3), notes),
				merged(act(5), standUp),
			},
			Writes: map[string][]write{
				devA: {stopWrite(act(1), "2026-11-18T08:30:00+01:00"), autoEndWrite(act(2), "2026-11-18T07:30:00Z")},
				devB: {autoEndWrite(act(1), "2026-11-18T08:30:00+01:00"), stopWrite(act(2), "2026-11-18T07:30:00Z")},
				devC: {autoEndWrite(act(1), "2026-11-18T08:30:00+01:00"), autoEndWrite(act(2), "2026-11-18T07:30:00Z")},
			},
		},
	}
}

func stopTwoRunning() mergeVector {
	api := running("2026-11-18T09:15:00+01:00", "Backend", "API")
	release := running("2026-11-18T03:45:00-05:00", "Mobile", "Release")
	return mergeVector{
		File: "stop-two-running",
		Name: "two devices each run one activity: every device shows the earlier one stopped at the start of the later one, " +
			"as its record holds it; the earlier one's owner writes that end, and every other device, " +
			"a third one without a journal included, writes it as auto_end",
		Journals: map[string][]json.RawMessage{
			devA: {change(devA, at(75, 0), typeActivity, act(2), api)},
			devB: {change(devB, at(105, 0), typeActivity, act(1), release)},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), release),
				merged(act(2), with(api, fields{"end": "2026-11-18T03:45:00-05:00"})),
			},
			Writes: map[string][]write{
				devA: {stopWrite(act(2), "2026-11-18T03:45:00-05:00")},
				devB: {autoEndWrite(act(2), "2026-11-18T03:45:00-05:00")},
				devC: {autoEndWrite(act(2), "2026-11-18T03:45:00-05:00")},
			},
		},
	}
}

func stopOwnerOfEarlier() mergeVector {
	api := running("2026-11-18T09:00:00Z", "Backend", "API")
	review := running("2026-11-18T09:40:00Z", "Backend", "Code review")
	return mergeVector{
		File: "stop-owner-of-earlier",
		Name: "the device whose change holds an activity's start owns it and writes its end, even when another device " +
			"changed the activity later and started the next one while it ran; that device writes the end as auto_end",
		Journals: map[string][]json.RawMessage{
			devA: {change(devA, at(120, 0), typeActivity, act(1), api)},
			devB: {
				change(devB, at(130, 0), typeActivity, act(1), fields{"description": "API, part 2", "tags/focus": true}),
				change(devB, at(160, 0), typeActivity, act(2), review),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{"description": "API, part 2", "tags/focus": true, "end": "2026-11-18T09:40:00Z"})),
				merged(act(2), review),
			},
			Writes: map[string][]write{
				devA: {stopWrite(act(1), "2026-11-18T09:40:00Z")},
				devB: {autoEndWrite(act(1), "2026-11-18T09:40:00Z")},
			},
		},
	}
}

func stopOwnerMoved() mergeVector {
	api := running("2026-11-18T09:00:00Z", "Backend", "API")
	deploy := running("2026-11-18T10:00:00Z", "Ops", "Deploy")
	return mergeVector{
		File: "stop-owner-moved",
		Name: "a device that edits an activity's start becomes its owner and writes its end; the device that created it " +
			"writes it as auto_end, as every other device does, even after it changes another field",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(120, 0), typeActivity, act(1), api),
				change(devA, at(200, 0), typeActivity, act(1), fields{"notes": "Started late"}),
			},
			devB: {change(devB, at(190, 0), typeActivity, act(1), fields{"start": "2026-11-18T08:50:00Z"})},
			devC: {change(devC, at(180, 0), typeActivity, act(2), deploy)},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{
					"start": "2026-11-18T08:50:00Z", "notes": "Started late", "end": "2026-11-18T10:00:00Z",
				})),
				merged(act(2), deploy),
			},
			Writes: map[string][]write{
				devA: {autoEndWrite(act(1), "2026-11-18T10:00:00Z")},
				devB: {stopWrite(act(1), "2026-11-18T10:00:00Z")},
				devC: {autoEndWrite(act(1), "2026-11-18T10:00:00Z")},
			},
		},
	}
}

// ownerStop is the activity of the owner-stop vectors: started at 10:00 on devA, which stops it at
// 10:30, and the one devB starts at 11:00 without having read that stop.
func ownerStop(stopArrived bool) map[string][]json.RawMessage {
	stop := change(devA, at(150, 0), typeActivity, act(1), fields{"end": "2026-11-18T10:30:00+01:00"})
	if !stopArrived {
		stop = text(string(stop))
	}
	return map[string][]json.RawMessage{
		devA: {
			change(devA, at(120, 0), typeActivity, act(1), running("2026-11-18T10:00:00+01:00", "Backend", "API")),
			stop,
		},
		devB: {change(devB, at(180, 0), typeActivity, act(2), running("2026-11-18T11:00:00+01:00", "Docs", "Release notes"))},
	}
}

func stopOwnerStopUnseen() mergeVector {
	return mergeVector{
		File: "stop-owner-stop-unseen",
		Name: "until the owner's stop at 10:30 arrives, devices show the activity stopped at 11:00, when the next one " +
			"started; the device that started it does not own the first one and writes 11:00 as its auto_end, " +
			"which the owner's end shows over once it arrives (see auto-end-owner-end-wins)",
		Journals: ownerStop(false),
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(running("2026-11-18T10:00:00+01:00", "Backend", "API"), fields{"end": "2026-11-18T11:00:00+01:00"})),
				merged(act(2), running("2026-11-18T11:00:00+01:00", "Docs", "Release notes")),
			},
			Writes: map[string][]write{devB: {autoEndWrite(act(1), "2026-11-18T11:00:00+01:00")}},
		},
	}
}

func stopOwnerStop() mergeVector {
	return mergeVector{
		File: "stop-owner-stop",
		Name: "once the owner's stop at 10:30 arrives it shows, though a device had shown 11:00; the activity no longer runs, " +
			"so nobody writes that end",
		Journals: ownerStop(true),
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(running("2026-11-18T10:00:00+01:00", "Backend", "API"), fields{"end": "2026-11-18T10:30:00+01:00"})),
				merged(act(2), running("2026-11-18T11:00:00+01:00", "Docs", "Release notes")),
			},
			Writes: map[string][]write{devA: {}, devB: {}},
		},
	}
}

func stopDeliberate() mergeVector {
	api := running("2026-11-18T09:00:00Z", "Backend", "API")
	guide := running("2026-11-18T10:00:00Z", "Docs", "Guide")
	return mergeVector{
		File: "stop-deliberate",
		Name: "a stop made on purpose is a change like any other, whichever device makes it: " +
			"its end shows instead of the one the rule would give",
		Journals: map[string][]json.RawMessage{
			devA: {change(devA, at(120, 0), typeActivity, act(1), api)},
			devB: {
				change(devB, at(160, 0), typeActivity, act(1), fields{"end": "2026-11-18T09:40:00Z"}),
				change(devB, at(180, 0), typeActivity, act(2), guide),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{"end": "2026-11-18T09:40:00Z"})),
				merged(act(2), guide),
			},
			Writes: map[string][]write{devA: {}, devB: {}},
		},
	}
}

// running is the fields of a new activity that runs.
func running(start, project, description string) fields {
	return fields{"start": start, "end": nil, "project": project, "description": description, "notes": ""}
}

// stopWrite is the change that writes an activity's end, as its owner writes the end rule 1 gives it.
func stopWrite(uid, end string) write {
	return write{Type: typeActivity, UID: uid, Set: fields{"end": end}}
}

// autoEndWrite is the change that writes an activity's auto_end, as every device but its owner writes
// the end rule 1 gives it.
func autoEndWrite(uid, end string) write {
	return write{Type: typeActivity, UID: uid, Set: fields{"auto_end": end}}
}
