package journal_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/models"
)

func newRecord(typ journal.RecordType, fields map[string]string) journal.Record {
	rec := journal.Record{Type: typ, UID: "019a9b3c-0000-7000-8000-000000000001", Fields: map[string]journal.Field{}}
	for path, value := range fields {
		rec.Fields[path] = journal.Field{Value: json.RawMessage(value)}
	}
	return rec
}

func TestRecordActivity(t *testing.T) {
	rec := newRecord(journal.TypeActivity, map[string]string{
		"start":            `"2026-11-18T10:00:00+03:00"`,
		"end":              `"2026-11-18T08:30:00Z"`,
		"project":          `"Backend"`,
		"description":      `"API"`,
		"notes":            `"Design review"`,
		"tags/review":      `true`,
		"tags/client/acme": `true`,
		"tags/old":         `false`,
		"tags/":            `true`,
		"deleted":          `false`,
	})
	activity, ok := rec.Activity()
	require.True(t, ok)
	assert.True(t, activity.StartTime.Equal(time.Date(2026, time.November, 18, 7, 0, 0, 0, time.UTC)))
	require.NotNil(t, activity.EndTime)
	assert.True(t, activity.EndTime.Equal(time.Date(2026, time.November, 18, 8, 30, 0, 0, time.UTC)))
	assert.Equal(t, "Backend", activity.Project)
	assert.Equal(t, "API", activity.Description)
	assert.Equal(t, "Design review", activity.Notes)
	assert.Equal(t, []string{"client/acme", "review"}, activity.Tags)

	rec.Fields["end"] = journal.Field{Value: json.RawMessage(`null`)}
	activity, ok = rec.Activity()
	require.True(t, ok)
	assert.Nil(t, activity.EndTime)

	_, ok = newRecord(journal.TypeReminder, map[string]string{"start": `"2026-11-18T10:00:00Z"`, "project": `"Backend"`}).Activity()
	assert.False(t, ok, "a reminder is not an activity")
}

func TestRecordActivityShownEnd(t *testing.T) {
	tests := []struct {
		name    string
		end     string
		autoEnd string
		want    string
	}{
		{name: "end without auto_end", end: `"2026-11-18T11:00:00Z"`, want: "2026-11-18T11:00:00Z"},
		{name: "auto_end while end is null", end: `null`, autoEnd: `"2026-11-18T12:00:00+01:00"`, want: "2026-11-18T11:00:00Z"},
		{name: "auto_end without end", autoEnd: `"2026-11-18T11:00:00Z"`, want: "2026-11-18T11:00:00Z"},
		{name: "end over a later auto_end", end: `"2026-11-18T10:30:00Z"`, autoEnd: `"2026-11-18T11:00:00Z"`, want: "2026-11-18T10:30:00Z"},
		{name: "end over an earlier one", end: `"2026-11-18T11:30:00Z"`, autoEnd: `"2026-11-18T11:00:00Z"`, want: "2026-11-18T11:30:00Z"},
		{name: "auto_end when end has the wrong type", end: `"later"`, autoEnd: `"2026-11-18T11:00:00Z"`, want: "2026-11-18T11:00:00Z"},
		{name: "auto_end null", end: `null`, autoEnd: `null`},
		{name: "auto_end with fractional seconds", end: `null`, autoEnd: `"2026-11-18T11:00:00.5Z"`},
		{name: "auto_end a date", autoEnd: `"2026-11-18"`},
		{name: "auto_end a number", autoEnd: `1795000000`},
		{name: "auto_end true", autoEnd: `true`},
		{name: "auto_end before the start", end: `null`, autoEnd: `"2026-11-18T09:59:59Z"`},
		{name: "auto_end before the start in another offset", autoEnd: `"2026-11-18T10:30:00+01:00"`},
		{name: "auto_end at the start", autoEnd: `"2026-11-18T10:00:00Z"`, want: "2026-11-18T10:00:00Z"},
		{name: "auto_end at the start in another offset", autoEnd: `"2026-11-18T11:00:00+01:00"`, want: "2026-11-18T10:00:00Z"},
		{name: "end before the start", end: `"2026-11-18T09:00:00Z"`, autoEnd: `"2026-11-18T11:00:00Z"`, want: "2026-11-18T09:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := map[string]string{"start": `"2026-11-18T10:00:00Z"`, "project": `"Backend"`}
			if tt.end != "" {
				set["end"] = tt.end
			}
			if tt.autoEnd != "" {
				set["auto_end"] = tt.autoEnd
			}
			activity, ok := newRecord(journal.TypeActivity, set).Activity()
			require.True(t, ok)
			if tt.want == "" {
				assert.Nil(t, activity.EndTime, "the activity runs")
				return
			}
			require.NotNil(t, activity.EndTime)
			assert.Equal(t, tt.want, activity.EndTime.UTC().Format(time.RFC3339))
		})
	}
}

func TestRecordReminder(t *testing.T) {
	rec := newRecord(journal.TypeReminder, map[string]string{
		"title":                      `"Stand-up"`,
		"project":                    `"Team"`,
		"due":                        `"2026-11-16T10:00"`,
		"all_day":                    `false`,
		"every":                      `"weekly:mon,thu"`,
		"until":                      `"2026-12-31"`,
		"count":                      `10`,
		"notify":                     `900`,
		"notes":                      `"Room 4"`,
		"tags/meeting":               `true`,
		"created":                    `"2026-11-15T12:00:00Z"`,
		"occ/2026-11-16/done":        `"2026-11-16T10:05:00Z"`,
		"occ/2026-11-19/snoozed":     `"2026-11-19T11:00:00Z"`,
		"occ/2026-11-19/activity":    `"2026-11-19T11:02:00Z"`,
		"occ/2026-11-23/skipped":     `true`,
		"occ/2026-11-26/skipped":     `false`,
		"occ/2026-11-31/skipped":     `true`,
		"occ/2026-11-30/done/extra":  `"2026-11-30T10:00:00Z"`,
		"occ/2026-11-30/postponed":   `true`,
		"occ/2026-12-03/done":        `"not a time"`,
		"occ/2026-12-07/snoozed":     `null`,
		"occ/2026-12-10/activity":    `"2026-12-10T10:00:00.5Z"`,
		"occ/2026-12-14/skipped":     `"yes"`,
		"occ/2026-12-17/done":        `true`,
		"occ/2026-12-21/snoozed":     `"2026-12-21"`,
		"occ/2026-12-24/activity":    `42`,
		"occ/2026-12-28/skipped":     `1`,
		"occ/2026-12-31/done":        `""`,
		"occ/2026-12-31/unknown":     `true`,
		"occ/not-a-date/done":        `"2026-11-16T10:05:00Z"`,
		"occ/2026-11-16":             `true`,
		"occ/":                       `true`,
		"occ/2026-11-16/done/nested": `true`,
	})
	reminder, ok := rec.Reminder()
	require.True(t, ok)
	assert.Equal(t, "Stand-up", reminder.Title)
	assert.Equal(t, "Team", reminder.Project)
	assert.True(t, reminder.Due.Equal(time.Date(2026, time.November, 16, 10, 0, 0, 0, time.Local)))
	assert.False(t, reminder.AllDay)
	assert.Equal(t, "weekly:mon,thu", reminder.Repeat.Spec())
	require.NotNil(t, reminder.Repeat.Until)
	assert.True(t, reminder.Repeat.Until.Equal(time.Date(2026, time.December, 31, 0, 0, 0, 0, time.Local)))
	assert.Equal(t, 10, reminder.Repeat.Count)
	assert.Equal(t, 15*time.Minute, reminder.Notify)
	assert.Equal(t, "Room 4", reminder.Notes)
	assert.Equal(t, []string{"meeting"}, reminder.Tags)
	assert.True(t, reminder.Created.Equal(time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)))
	assert.Zero(t, reminder.ID, "reminder numbers are local")

	utc := func(day, hour, minute int) *time.Time {
		v := time.Date(2026, time.November, day, hour, minute, 0, 0, time.UTC)
		return &v
	}
	want := map[string]models.OccurrenceState{
		"2026-11-16": {DoneAt: utc(16, 10, 5)},
		"2026-11-19": {SnoozedUntil: utc(19, 11, 0), ActivityStart: utc(19, 11, 2)},
		"2026-11-23": {Skipped: true},
	}
	assert.Len(t, reminder.States, len(want))
	for day, state := range want {
		got := reminder.States[day]
		assert.Equal(t, state.Skipped, got.Skipped, day)
		assertSameTime(t, state.DoneAt, got.DoneAt, day+" done")
		assertSameTime(t, state.SnoozedUntil, got.SnoozedUntil, day+" snoozed")
		assertSameTime(t, state.ActivityStart, got.ActivityStart, day+" activity")
	}
}

func TestRecordReminderValues(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]string
		check  func(t *testing.T, reminder models.Reminder)
	}{
		{
			name:   "a date without a time is all day",
			fields: map[string]string{"due": `"2026-11-20"`, "all_day": `false`},
			check: func(t *testing.T, reminder models.Reminder) {
				assert.True(t, reminder.AllDay)
				assert.True(t, reminder.Due.Equal(time.Date(2026, time.November, 20, 0, 0, 0, 0, time.Local)))
			},
		},
		{
			name:   "an all-day reminder ignores the time",
			fields: map[string]string{"due": `"2026-11-20T15:00"`, "all_day": `true`},
			check: func(t *testing.T, reminder models.Reminder) {
				assert.True(t, reminder.AllDay)
				assert.True(t, reminder.Due.Equal(time.Date(2026, time.November, 20, 0, 0, 0, 0, time.Local)))
			},
		},
		{
			name:   "numbers that are not whole and non-negative count as 0",
			fields: map[string]string{"due": `"2026-11-20T15:00"`, "every": `"daily"`, "count": `2.5`, "notify": `-60`},
			check: func(t *testing.T, reminder models.Reminder) {
				assert.Zero(t, reminder.Repeat.Count)
				assert.Zero(t, reminder.Notify)
			},
		},
		{
			name: "values of the wrong type count as missing",
			fields: map[string]string{
				"due":     `"2026-11-20T15:00"`,
				"every":   `7`,
				"until":   `"December"`,
				"project": `true`,
				"created": `"yesterday"`,
			},
			check: func(t *testing.T, reminder models.Reminder) {
				assert.False(t, reminder.IsRepeating())
				assert.Nil(t, reminder.Repeat.Until)
				assert.Empty(t, reminder.Project)
				assert.True(t, reminder.Created.IsZero())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fields["title"] = `"Title"`
			reminder, ok := newRecord(journal.TypeReminder, tt.fields).Reminder()
			require.True(t, ok)
			tt.check(t, reminder)
		})
	}
}

func assertSameTime(t *testing.T, want, got *time.Time, msg string) {
	t.Helper()
	if want == nil {
		assert.Nil(t, got, msg)
		return
	}
	if assert.NotNil(t, got, msg) {
		assert.True(t, want.Equal(*got), "%s: got %v, want %v", msg, *got, *want)
	}
}
