package journal_test

import "slices"

// repeatVectors returns the repeat rule vectors.
func repeatVectors() []repeatVector {
	return slices.Concat(statusVectors(), ruleVectors(), dayVectors())
}

func statusVectors() []repeatVector {
	return []repeatVector{
		{
			File: "daily",
			Name: "daily: only the latest past occurrence is overdue, older ones not done are missed",
			TZ:   berlin,
			Reminder: fields{
				"title": "Stand-up", "due": "2026-11-16T10:00", "all_day": false, "every": "daily", "until": "", "count": 0, "notify": 0,
				"occ/2026-11-16/done": "2026-11-16T10:05:00+01:00",
			},
			From: "2026-11-16", To: "2026-11-21", Now: "2026-11-18T12:00",
			Expect: []occurrence{
				{"2026-11-16T10:00", "done"}, {"2026-11-17T10:00", "missed"}, {"2026-11-18T10:00", "overdue"},
				{"2026-11-19T10:00", statusPending}, {"2026-11-20T10:00", statusPending},
			},
		},
		{
			File: "all-day",
			Name: "an all-day occurrence is overdue once its day has ended; the time of an all-day due date is ignored",
			TZ:   berlin,
			Reminder: fields{
				"title": "Expenses", "due": "2026-11-16T15:00", "all_day": true, "every": "daily",
			},
			From: "2026-11-16", To: "2026-11-20", Now: "2026-11-18T23:59",
			Expect: []occurrence{
				{"2026-11-16", "missed"}, {"2026-11-17", "overdue"}, {"2026-11-18", statusPending}, {"2026-11-19", statusPending},
			},
		},
		{
			File: "snoozed",
			Name: "an occurrence is snoozed while its snooze is in the future",
			TZ:   berlin,
			Reminder: fields{
				"title": "Call the bank", "due": "2026-11-17T09:00", "all_day": false, "every": "daily",
				"occ/2026-11-17/snoozed": "2026-11-17T10:00:00+01:00", "occ/2026-11-18/snoozed": "2026-11-18T13:00:00+01:00",
			},
			From: "2026-11-17", To: "2026-11-20", Now: "2026-11-18T12:00",
			Expect: []occurrence{
				{"2026-11-17T09:00", "missed"}, {"2026-11-18T09:00", "snoozed"}, {"2026-11-19T09:00", statusPending},
			},
		},
		{
			File: "wall-time",
			Name: "due is wall time: 17:30 is 17:30 in any time zone; an occurrence is overdue from its due minute",
			TZ:   "Asia/Tokyo",
			Reminder: fields{
				"title": "Leave the office", "due": "2026-11-17T17:30", "all_day": false, "every": "daily",
			},
			From: "2026-11-17", To: "2026-11-20", Now: "2026-11-18T17:30",
			Expect: []occurrence{
				{"2026-11-17T17:30", "missed"}, {"2026-11-18T17:30", "overdue"}, {"2026-11-19T17:30", statusPending},
			},
		},
		{
			File: "dst-gap",
			Name: "a wall time skipped by a daylight saving change moves forward by the gap",
			TZ:   "America/New_York",
			Reminder: fields{
				"title": "Backup", "due": "2027-03-13T02:30", "all_day": false, "every": "daily",
			},
			From: "2027-03-13", To: "2027-03-16", Now: "2027-03-12T00:00",
			Expect: pending("2027-03-13T02:30", "2027-03-14T03:30", "2027-03-15T02:30"),
		},
	}
}

func ruleVectors() []repeatVector {
	return []repeatVector{
		{
			File:     "interval-days",
			Name:     "every third day, counted from the first day",
			TZ:       berlin,
			Reminder: fields{"title": "Water the plants", "due": "2026-11-01T08:00", "all_day": false, "every": "3d"},
			From:     "2026-11-01", To: "2026-11-15", Now: "2026-11-01T07:00",
			Expect: pending("2026-11-01T08:00", "2026-11-04T08:00", "2026-11-07T08:00", "2026-11-10T08:00", "2026-11-13T08:00"),
		},
		{
			File:     "weekdays",
			Name:     "weekdays: Monday to Friday",
			TZ:       berlin,
			Reminder: fields{"title": "Check the inbox", "due": "2026-11-20T09:00", "all_day": false, "every": "weekdays"},
			From:     "2026-11-20", To: "2026-11-27", Now: "2026-11-19T00:00",
			Expect: pending("2026-11-20T09:00", "2026-11-23T09:00", "2026-11-24T09:00", "2026-11-25T09:00", "2026-11-26T09:00"),
		},
		{
			File:     "weekly-interval",
			Name:     "every second week on Monday and Thursday, counting weeks from the Monday of the first day's week",
			TZ:       berlin,
			Reminder: fields{"title": "Planning", "due": "2026-11-19T18:00", "all_day": false, "every": "2w:mon,thu"},
			From:     "2026-11-16", To: "2026-12-18", Now: "2026-11-16T00:00",
			Expect: pending("2026-11-19T18:00", "2026-11-30T18:00", "2026-12-03T18:00", "2026-12-14T18:00", "2026-12-17T18:00"),
		},
		{
			File:     "monthly-clamp",
			Name:     "monthly on the 31st falls on the last day of shorter months",
			TZ:       berlin,
			Reminder: fields{"title": "Close the books", "due": "2027-01-31", "all_day": true, "every": "monthly"},
			From:     "2027-01-01", To: "2027-05-01", Now: "2027-01-01T00:00",
			Expect: pending("2027-01-31", "2027-02-28", "2027-03-31", "2027-04-30"),
		},
		{
			File:     "monthly-last-weekday",
			Name:     "monthly on the last Friday",
			TZ:       berlin,
			Reminder: fields{"title": "Demo day", "due": "2026-11-27T16:00", "all_day": false, "every": "monthly:last-fri"},
			From:     "2026-11-01", To: "2027-03-01", Now: "2026-11-01T00:00",
			Expect: pending("2026-11-27T16:00", "2026-12-25T16:00", "2027-01-29T16:00", "2027-02-26T16:00"),
		},
		{
			File:     "monthly-nth-weekday",
			Name:     "monthly on the second Tuesday",
			TZ:       berlin,
			Reminder: fields{"title": "Patch day", "due": "2026-11-10T19:00", "all_day": false, "every": "monthly:2-tue"},
			From:     "2026-11-01", To: "2027-03-01", Now: "2026-11-01T00:00",
			Expect: pending("2026-11-10T19:00", "2026-12-08T19:00", "2027-01-12T19:00", "2027-02-09T19:00"),
		},
		{
			File:     "yearly-leap-day",
			Name:     "yearly on 29 February falls on 28 February in other years",
			TZ:       berlin,
			Reminder: fields{"title": "Leap day", "due": "2028-02-29", "all_day": true, "every": "yearly"},
			From:     "2028-01-01", To: "2033-01-01", Now: "2028-01-01T00:00",
			Expect: pending("2028-02-29", "2029-02-28", "2030-02-28", "2031-02-28", "2032-02-29"),
		},
	}
}

func dayVectors() []repeatVector {
	return []repeatVector{
		{
			File: "count",
			Name: "count limits the occurrences, skipped ones included",
			TZ:   berlin,
			Reminder: fields{
				"title": "Take the pills", "due": "2026-11-16T09:00", "all_day": false, "every": "daily", "count": 4,
				"occ/2026-11-17/skipped": true,
			},
			From: "2026-11-01", To: "2026-12-01", Now: "2026-11-16T08:00",
			Expect: pending("2026-11-16T09:00", "2026-11-18T09:00", "2026-11-19T09:00"),
		},
		{
			File: "until",
			Name: "no occurrence falls after until; until itself can have one",
			TZ:   berlin,
			Reminder: fields{
				"title": "Weekly report", "due": "2026-11-16T09:00", "all_day": false, "every": "weekly", "until": "2026-12-07",
			},
			From: "2026-11-01", To: "2027-01-01", Now: "2026-11-16T08:00",
			Expect: pending("2026-11-16T09:00", "2026-11-23T09:00", "2026-11-30T09:00", "2026-12-07T09:00"),
		},
		{
			File: "non-canonical-rule",
			Name: "a repeat rule not in its canonical form counts as missing, so the reminder is one-off",
			TZ:   berlin,
			Reminder: fields{
				"title": "Rule not in canonical form", "due": "2026-11-20T10:00", "all_day": false, "every": "Daily",
			},
			From: "2026-11-16", To: "2026-11-27", Now: "2026-11-16T00:00",
			Expect: pending("2026-11-20T10:00"),
		},
		{
			File:     "date-only-due",
			Name:     "a due date without a time makes the reminder all day",
			TZ:       berlin,
			Reminder: fields{"title": "Renew the passport", "due": "2026-11-20", "all_day": false, "every": "weekly"},
			From:     "2026-11-16", To: "2026-12-01", Now: "2026-11-21T00:00",
			Expect: []occurrence{{"2026-11-20", "overdue"}, {"2026-11-27", statusPending}},
		},
	}
}

// pending returns occurrences due at the given times, all pending.
func pending(dues ...string) []occurrence {
	occurrences := make([]occurrence, 0, len(dues))
	for _, due := range dues {
		occurrences = append(occurrences, occurrence{Due: due, Status: statusPending})
	}
	return occurrences
}
