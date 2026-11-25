package journal_test

import "encoding/json"

// autoEndVectors returns the merge vectors of auto_end: the end the one-running rule gives an
// activity, as the devices that do not own it write it. An activity shows its end when that holds an
// instant, otherwise its auto_end when that holds one; with neither it runs.
func autoEndVectors() []mergeVector {
	return []mergeVector{
		autoEndResurrection(true),
		autoEndResurrection(false),
		autoEndOwnerEndWins(),
		autoEndReopen(),
		autoEndWrongType(),
		autoEndThirdDevice(),
		autoEndWithEnd(),
		autoEndBeforeStart(),
		autoEndConcurrentStarts(false),
		autoEndConcurrentStarts(true),
	}
}

// autoEndResurrection is the phone's activity the Mac stops by starting its own at 11:00 and then
// stops its own at 11:30, before the phone syncs. written says whether the Mac wrote the phone's
// auto_end when it started its own, as the start rule gives.
func autoEndResurrection(written bool) mergeVector {
	release := running("2026-11-18T10:00:00+01:00", "Mobile", "Release")
	api := running("2026-11-18T11:00:00+01:00", "Backend", "API")
	mac := []json.RawMessage{
		change(devA, at(180, 1), typeActivity, act(2), api),
		change(devA, at(210, 0), typeActivity, act(2), fields{"end": "2026-11-18T11:30:00+01:00"}),
	}
	vector := mergeVector{
		File: "auto-end-resurrection",
		Name: "the Mac starts an activity while the phone's runs and writes the phone's auto_end at the new start; " +
			"once the Mac stops its own, the phone's stays stopped on every device, and nobody writes more",
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(release, fields{"auto_end": "2026-11-18T11:00:00+01:00", "end": "2026-11-18T11:00:00+01:00"})),
				merged(act(2), with(api, fields{"end": "2026-11-18T11:30:00+01:00"})),
			},
			Writes: map[string][]write{devA: {}, devB: {}},
		},
	}
	if written {
		mac = append([]json.RawMessage{
			change(devA, at(180, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T11:00:00+01:00"}),
		}, mac...)
	} else {
		vector.File = "auto-end-resurrection-unwritten"
		vector.Name = "without that auto_end, as from a tock that does not know it, the phone's activity runs again once " +
			"the Mac stops its own: the rule stops only activities that run, so nobody writes an end"
		vector.Expect.Activities[0] = merged(act(1), release)
	}
	vector.Journals = map[string][]json.RawMessage{
		devB: {change(devB, at(120, 0), typeActivity, act(1), release)},
		devA: mac,
	}
	return vector
}

func autoEndOwnerEndWins() mergeVector {
	api := running("2026-11-18T10:00:00+01:00", "Backend", "API")
	notes := running("2026-11-18T11:00:00+01:00", "Docs", "Release notes")
	return mergeVector{
		File: "auto-end-owner-end-wins",
		Name: "the owner stops its activity at 10:30; a device that has not read that stop starts one at 11:00 and writes " +
			"the first one's auto_end with a later clock: the owner's end shows, whatever the clocks, and nobody writes more",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(120, 0), typeActivity, act(1), api),
				change(devA, at(150, 0), typeActivity, act(1), fields{"end": "2026-11-18T10:30:00+01:00"}),
			},
			devB: {
				change(devB, at(180, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T11:00:00+01:00"}),
				change(devB, at(180, 1), typeActivity, act(2), notes),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{"end": "2026-11-18T10:30:00+01:00", "auto_end": "2026-11-18T11:00:00+01:00"})),
				merged(act(2), notes),
			},
			Writes: map[string][]write{devA: {}, devB: {}},
		},
	}
}

func autoEndReopen() mergeVector {
	api := running("2026-11-18T09:00:00Z", "Backend", "API")
	deploy := running("2026-11-18T09:30:00Z", "Ops", "Deploy")
	interview := running("2026-11-18T07:00:00Z", "Hiring", "Interview")
	return mergeVector{
		File: "auto-end-reopen",
		Name: "reopening an activity on purpose writes end and auto_end null in one change, and it runs again; " +
			"a change that clears only end, as from a tock that does not know auto_end, leaves it stopped at its auto_end",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(120, 0), typeActivity, act(1), api),
				change(devA, at(200, 0), typeActivity, act(1), fields{"end": nil, "auto_end": nil}),
			},
			devB: {
				change(devB, at(20, 0), typeActivity, act(3), fields{"auto_end": "2026-11-18T07:20:00Z"}),
				change(devB, at(150, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T09:30:00Z"}),
				change(devB, at(150, 1), typeActivity, act(2), deploy),
				change(devB, at(170, 0), typeActivity, act(2), fields{"end": "2026-11-18T09:50:00Z"}),
			},
			devC: {
				change(devC, at(0, 0), typeActivity, act(3), interview),
				change(devC, at(40, 0), typeActivity, act(3), fields{"end": nil}),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{"auto_end": nil})),
				merged(act(2), with(deploy, fields{"end": "2026-11-18T09:50:00Z"})),
				merged(act(3), with(interview, fields{"auto_end": "2026-11-18T07:20:00Z", "end": "2026-11-18T07:20:00Z"})),
			},
			Writes: map[string][]write{devA: {}, devB: {}, devC: {}},
		},
	}
}

func autoEndWrongType() mergeVector {
	api := running("2026-11-18T09:00:00Z", "Backend", "API")
	review := running("2026-11-18T09:10:00Z", "Backend", "Review")
	standUp := running("2026-11-18T09:20:00Z", "Ops", "Stand-up")
	triage := running("2026-11-18T09:30:00Z", "Ops", "Triage")
	guide := running("2026-11-18T10:00:00Z", "Docs", "Guide")
	// The interview's first change has no end field at all.
	interview := fields{"start": "2026-11-18T08:00:00Z", "project": "Hiring", "description": "Interview", "notes": ""}
	return mergeVector{
		File: "auto-end-wrong-type",
		Name: "an auto_end that is not an instant with whole seconds (a fraction, a number, a date, an empty string, a " +
			"boolean) counts as missing, and so does an end of the wrong type: the activity runs, the rule stops it like " +
			"any other, and the auto_end the other devices write replaces the value; an auto_end shows for an activity " +
			"whose changes never set end",
		Journals: map[string][]json.RawMessage{
			devA: {change(devA, at(120, 0), typeActivity, act(1), api)},
			devB: {
				change(devB, at(150, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T09:30:00.250Z"}),
				change(devB, at(180, 0), typeActivity, act(2), guide),
			},
			devC: {
				change(devC, at(60, 0), typeActivity, act(6), interview),
				change(devC, at(90, 0), typeActivity, act(6), fields{"auto_end": "2026-11-18T08:30:00Z"}),
				change(devC, at(130, 0), typeActivity, act(3), with(review, fields{"end": "later", "auto_end": "2026-11-18"})),
				change(devC, at(140, 0), typeActivity, act(4), with(standUp, fields{"auto_end": ""})),
				change(devC, at(150, 0), typeActivity, act(5), with(triage, fields{"auto_end": true})),
				change(devC, at(200, 0), typeActivity, act(2), fields{"auto_end": 1800}),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{"auto_end": "2026-11-18T09:30:00.250Z", "end": "2026-11-18T09:10:00Z"})),
				merged(act(2), with(guide, fields{"auto_end": 1800})),
				merged(act(3), with(review, fields{"auto_end": "2026-11-18", "end": "2026-11-18T09:20:00Z"})),
				merged(act(4), with(standUp, fields{"auto_end": "", "end": "2026-11-18T09:30:00Z"})),
				merged(act(5), with(triage, fields{"auto_end": true, "end": "2026-11-18T10:00:00Z"})),
				merged(act(6), with(interview, fields{"auto_end": "2026-11-18T08:30:00Z", "end": "2026-11-18T08:30:00Z"})),
			},
			Writes: map[string][]write{
				devA: {
					stopWrite(act(1), "2026-11-18T09:10:00Z"), autoEndWrite(act(3), "2026-11-18T09:20:00Z"),
					autoEndWrite(act(4), "2026-11-18T09:30:00Z"), autoEndWrite(act(5), "2026-11-18T10:00:00Z"),
				},
				devB: {
					autoEndWrite(act(1), "2026-11-18T09:10:00Z"), autoEndWrite(act(3), "2026-11-18T09:20:00Z"),
					autoEndWrite(act(4), "2026-11-18T09:30:00Z"), autoEndWrite(act(5), "2026-11-18T10:00:00Z"),
				},
				devC: {
					autoEndWrite(act(1), "2026-11-18T09:10:00Z"), stopWrite(act(3), "2026-11-18T09:20:00Z"),
					stopWrite(act(4), "2026-11-18T09:30:00Z"), stopWrite(act(5), "2026-11-18T10:00:00Z"),
				},
			},
		},
	}
}

func autoEndThirdDevice() mergeVector {
	api := running("2026-11-18T10:00:00+01:00", "Backend", "API")
	release := running("2026-11-18T10:05:00+01:00", "Mobile", "Release")
	return mergeVector{
		File: "auto-end-third-device",
		Name: "two devices start activities before reading each other's; a third that reads both writes the earlier " +
			"one's auto_end: every device shows that end, and neither the owner nor anyone else writes more",
		Journals: map[string][]json.RawMessage{
			devA: {change(devA, at(120, 0), typeActivity, act(1), api)},
			devB: {change(devB, at(125, 0), typeActivity, act(2), release)},
			devC: {change(devC, at(140, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T10:05:00+01:00"})},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{"auto_end": "2026-11-18T10:05:00+01:00", "end": "2026-11-18T10:05:00+01:00"})),
				merged(act(2), release),
			},
			Writes: map[string][]write{devA: {}, devB: {}, devC: {}},
		},
	}
}

func autoEndWithEnd() mergeVector {
	deploy := running("2026-11-18T08:00:00Z", "Ops", "Deploy")
	guide := running("2026-11-18T08:45:00Z", "Docs", "Guide")
	return mergeVector{
		File: "auto-end-with-end",
		Name: "an end shows over an auto_end, whichever device wrote either and whatever their clocks; " +
			"an end of the wrong type counts as missing, so the auto_end shows",
		Journals: map[string][]json.RawMessage{
			devA: {
				change(devA, at(60, 0), typeActivity, act(1), deploy),
				change(devA, at(100, 0), typeActivity, act(2), guide),
			},
			devB: {
				change(devB, at(90, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T08:30:00Z"}),
				change(devB, at(130, 0), typeActivity, act(2), fields{"auto_end": "2026-11-18T09:10:00Z"}),
			},
			devC: {
				change(devC, at(80, 0), typeActivity, act(1), fields{"end": "2026-11-18T08:20:00Z"}),
				change(devC, at(140, 0), typeActivity, act(2), fields{"end": "later"}),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(deploy, fields{"end": "2026-11-18T08:20:00Z", "auto_end": "2026-11-18T08:30:00Z"})),
				merged(act(2), with(guide, fields{"end": "2026-11-18T09:10:00Z", "auto_end": "2026-11-18T09:10:00Z"})),
			},
			Writes: map[string][]write{devA: {}, devB: {}, devC: {}},
		},
	}
}

func autoEndBeforeStart() mergeVector {
	release := running("2026-11-18T09:00:00Z", "Mobile", "Release")
	api := running("2026-11-18T09:10:00Z", "Backend", "API")
	return mergeVector{
		File: "auto-end-before-start",
		Name: "the Mac starts an activity at 9:10 while the phone's runs and writes the phone's auto_end; the phone, before " +
			"it reads that, moves its activity's start to 9:20: an auto_end before the start counts as missing, so the " +
			"phone's activity runs from 9:20 and the rule stops the Mac's there",
		Journals: map[string][]json.RawMessage{
			devB: {
				change(devB, at(120, 0), typeActivity, act(1), release),
				change(devB, at(145, 0), typeActivity, act(1), fields{"start": "2026-11-18T09:20:00Z"}),
			},
			devA: {
				change(devA, at(130, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T09:10:00Z"}),
				change(devA, at(130, 1), typeActivity, act(2), api),
			},
		},
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(release, fields{"start": "2026-11-18T09:20:00Z", "auto_end": "2026-11-18T09:10:00Z"})),
				merged(act(2), with(api, fields{"end": "2026-11-18T09:20:00Z"})),
			},
			Writes: map[string][]write{
				devA: {stopWrite(act(2), "2026-11-18T09:20:00Z")},
				devB: {autoEndWrite(act(2), "2026-11-18T09:20:00Z")},
				devC: {autoEndWrite(act(2), "2026-11-18T09:20:00Z")},
			},
		},
	}
}

// autoEndConcurrentStarts is the Mac's activity, which runs from 10:00 while the phone starts one at
// 11:00 and the tablet one at 11:30, each before it reads the other's start: both write the Mac's
// auto_end, and the tablet's later clock wins. stopped says whether the phone stopped its own at 11:20
// before it read the tablet's.
func autoEndConcurrentStarts(stopped bool) mergeVector {
	api := running("2026-11-18T10:00:00+01:00", "Backend", "API")
	release := running("2026-11-18T11:00:00+01:00", "Mobile", "Release")
	notes := running("2026-11-18T11:30:00+01:00", "Docs", "Release notes")
	phone := []json.RawMessage{
		change(devB, at(180, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T11:00:00+01:00"}),
		change(devB, at(180, 1), typeActivity, act(2), release),
	}
	vector := mergeVector{
		File: "auto-end-concurrent-starts",
		Name: "two devices each start an activity while a third device's runs, before they read each other's start, and " +
			"both write its auto_end; the later clock wins, but the rule stops it at the start of the first running " +
			"activity after it that starts before that auto_end, and every device writes that end as for any other stop",
		Expect: mergeExpect{
			Activities: []record{
				merged(act(1), with(api, fields{"auto_end": "2026-11-18T11:30:00+01:00", "end": "2026-11-18T11:00:00+01:00"})),
				merged(act(2), with(release, fields{"end": "2026-11-18T11:30:00+01:00"})),
				merged(act(3), notes),
			},
			Writes: map[string][]write{
				devA: {stopWrite(act(1), "2026-11-18T11:00:00+01:00"), autoEndWrite(act(2), "2026-11-18T11:30:00+01:00")},
				devB: {autoEndWrite(act(1), "2026-11-18T11:00:00+01:00"), stopWrite(act(2), "2026-11-18T11:30:00+01:00")},
				devC: {autoEndWrite(act(1), "2026-11-18T11:00:00+01:00"), autoEndWrite(act(2), "2026-11-18T11:30:00+01:00")},
			},
		},
	}
	if stopped {
		phone = append(phone, change(devB, at(200, 0), typeActivity, act(2), fields{"end": "2026-11-18T11:20:00+01:00"}))
		vector.File = "auto-end-concurrent-starts-stopped"
		vector.Name = "when the phone stops its activity before it reads the tablet's start, no running activity starts " +
			"inside the Mac's activity any more: the tablet's later auto_end stands and the two overlap, as before auto_end " +
			"(docs/sync.md, Open questions)"
		vector.Expect.Activities[0] = merged(act(1), with(api, fields{
			"auto_end": "2026-11-18T11:30:00+01:00", "end": "2026-11-18T11:30:00+01:00",
		}))
		vector.Expect.Activities[1] = merged(act(2), with(release, fields{"end": "2026-11-18T11:20:00+01:00"}))
		vector.Expect.Writes = map[string][]write{devA: {}, devB: {}, devC: {}}
	}
	vector.Journals = map[string][]json.RawMessage{
		devA: {change(devA, at(120, 0), typeActivity, act(1), api)},
		devB: phone,
		devC: {
			change(devC, at(210, 0), typeActivity, act(1), fields{"auto_end": "2026-11-18T11:30:00+01:00"}),
			change(devC, at(210, 1), typeActivity, act(3), notes),
		},
	}
	return vector
}
