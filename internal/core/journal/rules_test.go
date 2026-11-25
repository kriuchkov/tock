package journal_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
)

func setActivity(t *testing.T, state *journal.State, hlc, dev, uid string, set map[string]string) {
	t.Helper()
	change := journal.Change{HLC: parseHLC(t, hlc), Dev: dev, Type: journal.TypeActivity, UID: uid, Set: map[string]json.RawMessage{}}
	for path, value := range set {
		change.Set[path] = json.RawMessage(value)
	}
	state.Apply(change)
}

func TestOwner(t *testing.T) {
	state := journal.NewState()
	setActivity(t, state, at(10, 0), devA, act(1), map[string]string{"start": `"2026-11-18T09:00:00Z"`, "project": `"Ops"`})
	setActivity(t, state, at(20, 0), devB, act(1), map[string]string{"description": `"Deploy"`})
	setActivity(t, state, at(5, 0), devC, act(1), map[string]string{"start": `"2026-11-18T08:00:00Z"`})
	setActivity(t, state, at(30, 0), devB, act(2), map[string]string{"project": `"Ops"`})

	records := state.Records(journal.TypeActivity)
	require.Len(t, records, 2)
	assert.Equal(t, devA, records[0].Owner(), "the device whose change holds the start owns the activity")
	assert.Empty(t, records[1].Owner(), "a record without a start has no owner")

	setActivity(t, state, at(40, 0), devC, act(1), map[string]string{"start": `"2026-11-18T08:00:00Z"`})
	assert.Equal(t, devC, state.Records(journal.TypeActivity)[0].Owner(), "editing the start moves ownership")
}

func TestApplyRules(t *testing.T) {
	state := journal.NewState()
	setActivity(t, state, at(10, 0), devA, act(1), map[string]string{"start": `"2026-11-18T09:00:00Z"`, "end": `null`, "project": `"Ops"`})
	setActivity(t, state, at(20, 0), devB, act(2), map[string]string{"start": `"2026-11-18T11:00:00+01:00"`, "project": `"Docs"`})

	stop := journal.Stop{UID: act(1), End: json.RawMessage(`"2026-11-18T11:00:00+01:00"`), Owner: devA}
	end := journal.Write{UID: act(1), Field: journal.FieldEnd, Value: stop.End}
	autoEnd := journal.Write{UID: act(1), Field: journal.FieldAutoEnd, Value: stop.End}
	assert.Equal(t, journal.Rules{Stops: []journal.Stop{stop}, Writes: []journal.Write{end}}, state.ApplyRules(devA))
	assert.Equal(t, journal.Rules{Stops: []journal.Stop{stop}, Writes: []journal.Write{autoEnd}}, state.ApplyRules(devB))
	assert.Equal(t, map[string]json.RawMessage{"auto_end": stop.End}, autoEnd.Set())

	rules := state.ApplyRules(devB)
	records := state.Records(journal.TypeActivity)
	shown := rules.Show(records[0])
	activity, ok := shown.Activity()
	require.True(t, ok)
	require.NotNil(t, activity.EndTime)
	assert.Equal(t, "2026-11-18T10:00:00Z", activity.EndTime.UTC().Format(time.RFC3339))
	assert.JSONEq(t, `null`, string(state.Records(journal.TypeActivity)[0].Fields["end"].Value), "showing does not change the state")
	assert.Equal(t, records[1], rules.Show(records[1]), "the last running activity keeps running")

	reminder := journal.Record{Type: journal.TypeReminder, UID: act(1), Fields: map[string]journal.Field{}}
	assert.Equal(t, reminder, rules.Show(reminder), "a reminder with the uid of a stopped activity is not an activity")
}

func TestAutoEnd(t *testing.T) {
	state := journal.NewState()
	setActivity(t, state, at(10, 0), devA, act(1), map[string]string{"start": `"2026-11-18T09:00:00Z"`, "end": `null`, "project": `"Ops"`})
	setActivity(t, state, at(20, 0), devB, act(2), map[string]string{"start": `"2026-11-18T10:00:00Z"`, "project": `"Docs"`})
	for _, w := range state.ApplyRules(devB).Writes {
		state.Apply(journal.Change{HLC: parseHLC(t, at(30, 0)), Dev: devB, Type: journal.TypeActivity, UID: w.UID, Set: w.Set()})
	}
	assert.Empty(t, state.Stops(), "an activity with an auto_end no longer runs")

	setActivity(t, state, at(40, 0), devB, act(2), map[string]string{"end": `"2026-11-18T10:30:00Z"`})
	assert.Empty(t, state.Stops(), "its auto_end keeps it stopped once the next one stops")
	shown := state.ApplyRules("").Show(state.Records(journal.TypeActivity)[0])
	assert.JSONEq(t, `"2026-11-18T10:00:00Z"`, string(shown.Fields["end"].Value), "the end shows the auto_end")
	assert.Equal(t, journal.Field{Value: shown.Fields["end"].Value}, shown.Fields["end"], "a shown auto_end has no clock")
	assert.JSONEq(t, `"2026-11-18T10:00:00Z"`, string(shown.Fields["auto_end"].Value))
	assert.Equal(t, devB, shown.Fields["auto_end"].Dev, "auto_end stays as merged")

	setActivity(t, state, at(50, 0), devA, act(1), map[string]string{"end": `"2026-11-18T09:45:00Z"`})
	shown = state.ApplyRules("").Show(state.Records(journal.TypeActivity)[0])
	assert.JSONEq(t, `"2026-11-18T09:45:00Z"`, string(shown.Fields["end"].Value), "an end shows over an auto_end")
	assert.Equal(t, devA, shown.Fields["end"].Dev, "an end that shows is not computed")

	reopen := journal.Change{HLC: parseHLC(t, at(60, 0)), Dev: devA, Type: journal.TypeActivity, UID: act(1), Set: journal.Reopen()}
	state.Apply(reopen)
	activity, ok := state.Records(journal.TypeActivity)[0].Activity()
	require.True(t, ok)
	assert.Nil(t, activity.EndTime, "a reopen clears end and auto_end")
}

func TestStopField(t *testing.T) {
	assert.Equal(t, journal.FieldEnd, journal.StopField(devA, devA))
	assert.Equal(t, journal.FieldAutoEnd, journal.StopField(devA, devB))
}

func TestRulesForRecords(t *testing.T) {
	state := journal.NewState()
	setActivity(t, state, at(10, 0), devA, act(1), map[string]string{"start": `"2026-11-18T09:00:00Z"`, "project": `"Ops"`})
	setActivity(t, state, at(20, 0), devB, act(2), map[string]string{"start": `"2026-11-18T10:00:00Z"`, "project": `"Docs"`})
	records := state.Records(journal.TypeActivity)

	reversed := []journal.Record{records[1], records[0]}
	assert.Equal(t, state.ApplyRules(devA), journal.RulesFor(reversed, devA), "the order of the records does not matter")
	assert.Empty(t, journal.RulesFor(records[:1], devA).Stops, "one running activity runs on")
}

// Two devices each start an activity while a third device's runs and write its auto_end; the later
// clock wins, but the rule stops it at the start of the first running activity after it.
func TestAutoEndStopsAtAnEarlierStart(t *testing.T) {
	state := journal.NewState()
	setActivity(t, state, at(10, 0), devA, act(1), map[string]string{"start": `"2026-11-18T10:00:00Z"`, "project": `"Ops"`})
	setActivity(t, state, at(20, 0), devB, act(1), map[string]string{"auto_end": `"2026-11-18T11:00:00Z"`})
	setActivity(t, state, at(20, 1), devB, act(2), map[string]string{"start": `"2026-11-18T11:00:00Z"`, "project": `"Docs"`})
	setActivity(t, state, at(30, 0), devC, act(1), map[string]string{"auto_end": `"2026-11-18T11:30:00Z"`})
	setActivity(t, state, at(30, 1), devC, act(3), map[string]string{"start": `"2026-11-18T11:30:00Z"`, "project": `"QA"`})

	rules := state.ApplyRules(devC)
	assert.Equal(t, []journal.Stop{
		{UID: act(1), End: json.RawMessage(`"2026-11-18T11:00:00Z"`), Owner: devA},
		{UID: act(2), End: json.RawMessage(`"2026-11-18T11:30:00Z"`), Owner: devB},
	}, rules.Stops, "in the order of the starts")
	shown := rules.Show(state.Records(journal.TypeActivity)[0])
	assert.JSONEq(t, `"2026-11-18T11:00:00Z"`, string(shown.Fields["end"].Value), "the rule's end shows over the later auto_end")
	assert.JSONEq(t, `"2026-11-18T11:30:00Z"`, string(shown.Fields["auto_end"].Value), "auto_end stays as merged")

	for _, w := range rules.Writes {
		state.Apply(journal.Change{HLC: parseHLC(t, at(40, 0)), Dev: devC, Type: journal.TypeActivity, UID: w.UID, Set: w.Set()})
	}
	assert.Empty(t, state.Stops(), "the earlier auto_end holds")

	setActivity(t, state, at(50, 0), devA, act(1), map[string]string{"end": `"2026-11-18T11:45:00Z"`})
	assert.Empty(t, state.Stops(), "an end is never cut")
	activity, ok := state.Records(journal.TypeActivity)[0].Activity()
	require.True(t, ok)
	assert.Equal(t, "2026-11-18T11:45:00Z", activity.EndTime.UTC().Format(time.RFC3339))
}

// A running activity that starts at or after an auto_end does not cut it.
func TestAutoEndIsNotCutByALaterStart(t *testing.T) {
	state := journal.NewState()
	setActivity(t, state, at(10, 0), devA, act(1), map[string]string{
		"start": `"2026-11-18T10:00:00Z"`, "project": `"Ops"`, "auto_end": `"2026-11-18T11:00:00Z"`,
	})
	setActivity(t, state, at(20, 0), devB, act(2), map[string]string{"start": `"2026-11-18T11:00:00Z"`, "project": `"Docs"`})
	setActivity(t, state, at(20, 1), devB, act(3), map[string]string{"start": `"2026-11-18T09:00:00Z"`, "project": `"QA"`})
	assert.Equal(t, []journal.Stop{{UID: act(3), End: json.RawMessage(`"2026-11-18T11:00:00Z"`), Owner: devB}}, state.Stops(),
		"only the running activity before it stops, at the next running one")
}

func TestStartStopTime(t *testing.T) {
	start := time.Date(2026, time.November, 18, 9, 30, 0, 0, time.UTC)
	now := time.Date(2026, time.November, 18, 9, 31, 25, 750_000_000, time.UTC)
	assert.Equal(t, start.Add(time.Hour), journal.StartStopTime(start, start.Add(time.Hour), now), "a later start")
	assert.Equal(t, start, journal.StartStopTime(start, start, now), "the same instant")
	assert.Equal(t, now.Truncate(time.Second), journal.StartStopTime(start, start.Add(-time.Hour), now), "an earlier start: now")
	assert.Equal(t, start, journal.StartStopTime(start, start.Add(-time.Hour), start.Add(-time.Minute)), "never before the start")
}
