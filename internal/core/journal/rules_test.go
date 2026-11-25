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
	assert.Equal(t, journal.Rules{Stops: []journal.Stop{stop}, Writes: []journal.Stop{stop}}, state.ApplyRules(devA))
	assert.Equal(t, journal.Rules{Stops: []journal.Stop{stop}}, state.ApplyRules(devB))

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

func TestRulesForRecords(t *testing.T) {
	state := journal.NewState()
	setActivity(t, state, at(10, 0), devA, act(1), map[string]string{"start": `"2026-11-18T09:00:00Z"`, "project": `"Ops"`})
	setActivity(t, state, at(20, 0), devB, act(2), map[string]string{"start": `"2026-11-18T10:00:00Z"`, "project": `"Docs"`})
	records := state.Records(journal.TypeActivity)

	reversed := []journal.Record{records[1], records[0]}
	assert.Equal(t, state.ApplyRules(devA), journal.RulesFor(reversed, devA), "the order of the records does not matter")
	assert.Empty(t, journal.RulesFor(records[:1], devA).Stops, "one running activity runs on")
}
