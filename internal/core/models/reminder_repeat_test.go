package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.Local)
}

// at is a time in November 2026.
func at(d, hour, minute int) time.Time {
	return time.Date(2026, time.November, d, hour, minute, 0, 0, time.Local)
}

func TestParseRepeat(t *testing.T) {
	tests := []struct {
		spec string
		want models.Repeat
		// canonical is what Spec gives back; empty means the spec itself.
		canonical string
	}{
		{spec: "", want: models.Repeat{}},
		{spec: "none", want: models.Repeat{}, canonical: "-"},
		{spec: "daily", want: models.Repeat{Freq: models.RepeatDaily}},
		{spec: "3d", want: models.Repeat{Freq: models.RepeatDaily, Interval: 3}},
		{spec: "1d", want: models.Repeat{Freq: models.RepeatDaily}, canonical: "daily"},
		{spec: "weekdays", want: models.Repeat{Freq: models.RepeatWeekdays}},
		{spec: "weekly", want: models.Repeat{Freq: models.RepeatWeekly}},
		{
			spec:      "Weekly:thu,MON,mon",
			want:      models.Repeat{Freq: models.RepeatWeekly, Weekdays: []time.Weekday{time.Monday, time.Thursday}},
			canonical: "weekly:mon,thu",
		},
		{
			spec: "2w:fri",
			want: models.Repeat{Freq: models.RepeatWeekly, Interval: 2, Weekdays: []time.Weekday{time.Friday}},
		},
		{spec: "monthly", want: models.Repeat{Freq: models.RepeatMonthly}},
		{spec: "monthly:16", want: models.Repeat{Freq: models.RepeatMonthly, MonthDay: 16}},
		{spec: "monthly:last", want: models.Repeat{Freq: models.RepeatMonthly, MonthDay: models.MonthDayLast}},
		{spec: "monthly:2-tue", want: models.Repeat{Freq: models.RepeatMonthly, Week: 2, Weekday: time.Tuesday}},
		{
			spec:      "monthly:last-friday",
			want:      models.Repeat{Freq: models.RepeatMonthly, Week: models.WeekLast, Weekday: time.Friday},
			canonical: "monthly:last-fri",
		},
		{spec: "6m", want: models.Repeat{Freq: models.RepeatMonthly, Interval: 6}},
		{spec: "yearly", want: models.Repeat{Freq: models.RepeatYearly}},
		{spec: "2y", want: models.Repeat{Freq: models.RepeatYearly, Interval: 2}},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := models.ParseRepeat(tt.spec)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			canonical := tt.canonical
			switch canonical {
			case "":
				canonical = tt.spec
			case "-":
				canonical = ""
			}
			assert.Equal(t, canonical, got.Spec())
		})
	}
}

func TestParseRepeatRejectsInvalidSpecs(t *testing.T) {
	for _, spec := range []string{"hourly", "0d", "-1w", "d", "daily:1", "weekly:funday", "monthly:32", "monthly:5-mon", "monthly:x", "yearly:3", "none:1"} {
		t.Run(spec, func(t *testing.T) {
			_, err := models.ParseRepeat(spec)
			assert.Error(t, err)
		})
	}
}

func TestRepeatValidate(t *testing.T) {
	due := day(2026, time.November, 16)
	before := due.AddDate(0, 0, -1)

	require.NoError(t, models.Repeat{Freq: models.RepeatDaily, Until: &due}.Validate(due))
	require.Error(t, models.Repeat{Freq: "hourly"}.Validate(due))
	require.Error(t, models.Repeat{Freq: models.RepeatDaily, Count: -1}.Validate(due))
	require.Error(t, models.Repeat{Freq: models.RepeatDaily, Until: &before}.Validate(due))
	require.Error(t, models.Repeat{Freq: models.RepeatMonthly, MonthDay: 40}.Validate(due))
	assert.Error(t, models.Repeat{Freq: models.RepeatMonthly, Week: 5}.Validate(due))
}

// occurrenceDays lists the days the reminder falls on in [from, to).
func occurrenceDays(reminder models.Reminder, from, to time.Time) []string {
	var days []string
	for _, occurrence := range reminder.Occurrences(from, to, from) {
		days = append(days, occurrence.Key())
	}
	return days
}

func TestOccurrencesFollowTheRepeatRule(t *testing.T) {
	until := day(2026, time.December, 2)
	tests := []struct {
		name     string
		reminder models.Reminder
		from, to time.Time
		want     []string
	}{
		{
			name:     "one-off",
			reminder: models.Reminder{Due: at(18, 11, 0)},
			from:     day(2026, time.November, 1), to: day(2026, time.December, 1),
			want: []string{"2026-11-18"},
		},
		{
			name: "every three days with a count",
			reminder: models.Reminder{
				Due:    day(2026, time.November, 16),
				AllDay: true,
				Repeat: models.Repeat{Freq: models.RepeatDaily, Interval: 3, Count: 3},
			},
			from: day(2026, time.November, 1),
			to:   day(2026, time.December, 31),
			want: []string{"2026-11-16", "2026-11-19", "2026-11-22"},
		},
		{
			name:     "weekdays skip the weekend",
			reminder: models.Reminder{Due: at(19, 9, 30), Repeat: models.Repeat{Freq: models.RepeatWeekdays}},
			from:     day(2026, time.November, 19), to: day(2026, time.November, 25),
			want: []string{"2026-11-19", "2026-11-20", "2026-11-23", "2026-11-24"},
		},
		{
			name: "weekly on two days until a date",
			reminder: models.Reminder{
				Due:    at(16, 17, 30),
				Repeat: models.Repeat{Freq: models.RepeatWeekly, Weekdays: []time.Weekday{time.Monday, time.Thursday}, Until: &until},
			},
			from: day(2026, time.November, 1), to: day(2026, time.December, 31),
			want: []string{"2026-11-16", "2026-11-19", "2026-11-23", "2026-11-26", "2026-11-30"},
		},
		{
			name: "every other week",
			reminder: models.Reminder{
				Due:    day(2026, time.November, 20),
				AllDay: true,
				Repeat: models.Repeat{Freq: models.RepeatWeekly, Interval: 2},
			},
			from: day(2026, time.November, 1),
			to:   day(2026, time.December, 31),
			want: []string{"2026-11-20", "2026-12-04", "2026-12-18"},
		},
		{
			name:     "monthly on the 31st clamps to short months",
			reminder: models.Reminder{Due: day(2027, time.January, 31), AllDay: true, Repeat: models.Repeat{Freq: models.RepeatMonthly}},
			from:     day(2027, time.January, 1), to: day(2027, time.May, 1),
			want: []string{"2027-01-31", "2027-02-28", "2027-03-31", "2027-04-30"},
		},
		{
			name: "monthly on the last day",
			reminder: models.Reminder{
				Due:    day(2028, time.January, 31),
				AllDay: true,
				Repeat: models.Repeat{Freq: models.RepeatMonthly, MonthDay: models.MonthDayLast},
			},
			from: day(2028, time.January, 1),
			to:   day(2028, time.April, 1),
			want: []string{"2028-01-31", "2028-02-29", "2028-03-31"},
		},
		{
			name: "monthly on the second Tuesday",
			reminder: models.Reminder{
				Due: day(2026, time.November, 10), AllDay: true,
				Repeat: models.Repeat{Freq: models.RepeatMonthly, Week: 2, Weekday: time.Tuesday},
			},
			from: day(2026, time.November, 1), to: day(2027, time.February, 1),
			want: []string{"2026-11-10", "2026-12-08", "2027-01-12"},
		},
		{
			name: "monthly on the last Friday",
			reminder: models.Reminder{
				Due: day(2026, time.November, 27), AllDay: true,
				Repeat: models.Repeat{Freq: models.RepeatMonthly, Week: models.WeekLast, Weekday: time.Friday},
			},
			from: day(2026, time.November, 1), to: day(2027, time.February, 1),
			want: []string{"2026-11-27", "2026-12-25", "2027-01-29"},
		},
		{
			name: "every six months",
			reminder: models.Reminder{
				Due:    day(2026, time.March, 15),
				AllDay: true,
				Repeat: models.Repeat{Freq: models.RepeatMonthly, Interval: 6},
			},
			from: day(2026, time.January, 1),
			to:   day(2028, time.January, 1),
			want: []string{"2026-03-15", "2026-09-15", "2027-03-15", "2027-09-15"},
		},
		{
			name:     "yearly on Feb 29 falls on Feb 28 in common years",
			reminder: models.Reminder{Due: day(2028, time.February, 29), AllDay: true, Repeat: models.Repeat{Freq: models.RepeatYearly}},
			from:     day(2028, time.January, 1), to: day(2031, time.January, 1),
			want: []string{"2028-02-29", "2029-02-28", "2030-02-28"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, occurrenceDays(tt.reminder, tt.from, tt.to))
		})
	}
}

func TestOccurrencesKeepTheTimeOfDay(t *testing.T) {
	reminder := models.Reminder{Due: at(16, 17, 30), Repeat: models.Repeat{Freq: models.RepeatDaily}}

	occurrences := reminder.Occurrences(day(2026, time.November, 17), day(2026, time.November, 18), day(2026, time.November, 16))
	require.Len(t, occurrences, 1)
	assert.Equal(t, at(17, 17, 30), occurrences[0].Due)
}

func TestFirstDueMovesToTheFirstMatchingDay(t *testing.T) {
	reminder := models.Reminder{
		Due:    at(18, 9, 0), // a Wednesday
		Repeat: models.Repeat{Freq: models.RepeatMonthly, Week: models.WeekLast, Weekday: time.Wednesday},
	}
	first, ok := reminder.FirstDue()
	require.True(t, ok)
	assert.Equal(t, at(25, 9, 0), first)

	until := day(2026, time.November, 20)
	reminder.Repeat.Until = &until
	_, ok = reminder.FirstDue()
	assert.False(t, ok, "the rule never falls before until")
}

func TestWeeklyDaysDefaultsToTheFirstDay(t *testing.T) {
	reminder := models.Reminder{Due: day(2026, time.November, 19), Repeat: models.Repeat{Freq: models.RepeatWeekly}}
	assert.Equal(t, []time.Weekday{time.Thursday}, reminder.WeeklyDays())
}

// inZone runs the rest of the test with time.Local set to zone: reminders count days in time.Local.
func inZone(t *testing.T, zone string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(zone)
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}
	saved := time.Local
	time.Local = loc //nolint:reassign // the models read the zone from time.Local
	t.Cleanup(func() {
		time.Local = saved //nolint:reassign // restores the zone set above
	})
	return loc
}

// wall is a local wall time; one that a daylight saving change skips moves forward by the gap.
func wall(year int, month time.Month, d, hour, minute int) time.Time {
	return timeutil.WallTime(year, month, d, hour, minute, time.Local)
}

// In these zones the clocks jump from 00:00 to 01:00, so the day starts at 01:00. time.Date at
// midnight lands on 23:00 the evening before in the zones west of UTC.
func TestOccurrencesWhereDaylightSavingSkipsMidnight(t *testing.T) {
	tests := []struct {
		name     string
		zone     string
		reminder func() models.Reminder
		from, to [3]int
		want     []string
	}{
		{
			name: "daily",
			zone: "America/Santiago",
			reminder: func() models.Reminder {
				return models.Reminder{Due: wall(2026, time.September, 4, 0, 30), Repeat: models.Repeat{Freq: models.RepeatDaily}}
			},
			from: [3]int{2026, 9, 4}, to: [3]int{2026, 9, 9},
			want: []string{
				"2026-09-04T00:30:00-04:00", "2026-09-05T00:30:00-04:00", "2026-09-06T01:30:00-03:00",
				"2026-09-07T00:30:00-03:00", "2026-09-08T00:30:00-03:00",
			},
		},
		{
			name: "weekly from the day of the change",
			zone: "America/Havana",
			reminder: func() models.Reminder {
				return models.Reminder{Due: wall(2026, time.March, 8, 10, 0), Repeat: models.Repeat{Freq: models.RepeatWeekly}}
			},
			from: [3]int{2026, 3, 1}, to: [3]int{2026, 3, 23},
			want: []string{"2026-03-08T10:00:00-04:00", "2026-03-15T10:00:00-04:00", "2026-03-22T10:00:00-04:00"},
		},
		{
			name: "monthly on the day of the change",
			zone: "America/Santiago",
			reminder: func() models.Reminder {
				return models.Reminder{Due: wall(2026, time.August, 6, 18, 0), Repeat: models.Repeat{Freq: models.RepeatMonthly}}
			},
			from: [3]int{2026, 8, 1}, to: [3]int{2026, 11, 1},
			want: []string{"2026-08-06T18:00:00-04:00", "2026-09-06T18:00:00-03:00", "2026-10-06T18:00:00-03:00"},
		},
		{
			name: "all day, first due on the day of the change, until a date",
			zone: "America/Santiago",
			reminder: func() models.Reminder {
				until := wall(2026, time.September, 13, 0, 0)
				return models.Reminder{
					Due:    wall(2026, time.September, 6, 0, 0),
					AllDay: true,
					Repeat: models.Repeat{Freq: models.RepeatWeekly, Weekdays: []time.Weekday{time.Saturday, time.Sunday}, Until: &until},
				}
			},
			from: [3]int{2026, 9, 1}, to: [3]int{2026, 9, 20},
			want: []string{"2026-09-06T01:00:00-03:00", "2026-09-12T00:00:00-03:00", "2026-09-13T00:00:00-03:00"},
		},
		{
			name: "monthly on the last Friday",
			zone: "Africa/Cairo",
			reminder: func() models.Reminder {
				return models.Reminder{
					Due:    wall(2026, time.March, 27, 0, 0),
					AllDay: true,
					Repeat: models.Repeat{Freq: models.RepeatMonthly, Week: models.WeekLast, Weekday: time.Friday},
				}
			},
			from: [3]int{2026, 3, 1}, to: [3]int{2026, 6, 1},
			want: []string{"2026-03-27T00:00:00+02:00", "2026-04-24T01:00:00+03:00", "2026-05-29T00:00:00+03:00"},
		},
		{
			name: "all day on the day of the change",
			zone: "Asia/Beirut",
			reminder: func() models.Reminder {
				return models.Reminder{Due: wall(2026, time.March, 28, 0, 0), AllDay: true, Repeat: models.Repeat{Freq: models.RepeatDaily}}
			},
			from: [3]int{2026, 3, 28}, to: [3]int{2026, 3, 31},
			want: []string{"2026-03-28T00:00:00+02:00", "2026-03-29T01:00:00+03:00", "2026-03-30T00:00:00+03:00"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.zone+" "+tt.name, func(t *testing.T) {
			inZone(t, tt.zone)
			from := wall(tt.from[0], time.Month(tt.from[1]), tt.from[2], 0, 0)
			to := wall(tt.to[0], time.Month(tt.to[1]), tt.to[2], 0, 0)
			var got []string
			for _, occurrence := range tt.reminder().Occurrences(from, to, from) {
				got = append(got, occurrence.Due.Format(time.RFC3339))
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFirstDueWhereDaylightSavingSkipsMidnight(t *testing.T) {
	inZone(t, "America/Santiago")
	reminder := models.Reminder{
		Due:    wall(2026, time.September, 6, 9, 0), // a Sunday
		Repeat: models.Repeat{Freq: models.RepeatWeekly},
	}
	assert.Equal(t, []time.Weekday{time.Sunday}, reminder.WeeklyDays())
	first, ok := reminder.FirstDue()
	require.True(t, ok)
	assert.Equal(t, "2026-09-06T09:00:00-03:00", first.Format(time.RFC3339))

	until := wall(2026, time.September, 6, 0, 0)
	require.NoError(t, models.Repeat{Freq: models.RepeatDaily, Until: &until}.Validate(reminder.Due))
}

// The day before a midnight gap ends when the next one starts, at 01:00 after the jump: not an hour
// early, which is where midnight would be. A day with no occurrence of its own does not borrow the
// next day's.
func TestDayEndsWhereTheNextDayStartsOverAMidnightGap(t *testing.T) {
	inZone(t, "America/Santiago")
	weekdays := models.Reminder{
		Due:    wall(2026, time.September, 4, 0, 0),
		AllDay: true,
		Repeat: models.Repeat{Freq: models.RepeatWeekdays},
	}
	daily := weekdays
	daily.Repeat = models.Repeat{Freq: models.RepeatDaily}

	saturday, ok := daily.OccurrenceOn(wall(2026, time.September, 5, 12, 0), wall(2026, time.September, 5, 12, 0))
	require.True(t, ok)
	assert.Equal(t, "2026-09-06T01:00:00-03:00", saturday.DueBy().Format(time.RFC3339), "Saturday ends when Sunday starts")
	assert.Equal(t, models.ReminderPending, saturday.Status(wall(2026, time.September, 5, 23, 30)))

	sunday, ok := daily.OccurrenceOn(wall(2026, time.September, 6, 12, 0), wall(2026, time.September, 6, 12, 0))
	require.True(t, ok)
	assert.Equal(t, "2026-09-07T00:00:00-03:00", sunday.DueBy().Format(time.RFC3339), "Sunday ends at Monday's midnight")

	_, ok = weekdays.OccurrenceOn(wall(2026, time.September, 6, 12, 0), wall(2026, time.September, 6, 12, 0))
	assert.False(t, ok, "Sunday has no weekday occurrence, and Monday's is not Sunday's")
}
