package journal_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/timeutil"
)

// The test vectors of docs/sync.md, shared with tock-ios: hlc_vectors_test.go, merge_vectors_test.go and
// repeat_vectors_test.go hold them, the tests below run them, and TestWriteVectors writes them as JSON.

// hlcVector checks how clocks parse and compare, and how a device's clock moves.
type hlcVector struct {
	File    string      `json:"-"`
	Name    string      `json:"name"`
	Valid   []string    `json:"valid,omitempty"`
	Invalid []string    `json:"invalid,omitempty"`
	Order   [][2]string `json:"order,omitempty"`
	Last    string      `json:"last,omitempty"`
	Steps   []hlcStep   `json:"steps,omitempty"`
}

// hlcStep is one step on a clock: "new" makes a change at Now, "read" reads HLC at Now.
type hlcStep struct {
	Op     string `json:"op"`
	Now    int64  `json:"now"`
	HLC    string `json:"hlc,omitempty"`
	Expect string `json:"expect"`
}

// mergeVector merges journals and lists the records left visible after the rules.
type mergeVector struct {
	File     string                       `json:"-"`
	Name     string                       `json:"name"`
	Journals map[string][]json.RawMessage `json:"journals"`
	Expect   mergeExpect                  `json:"expect"`
}

// mergeExpect is the result of a merge: visible records in uid order, the number of skipped lines and,
// for the devices Writes lists, the changes each one writes after the rules.
type mergeExpect struct {
	Activities []record           `json:"activities"`
	Reminders  []record           `json:"reminders"`
	Skipped    int                `json:"skipped"`
	Writes     map[string][]write `json:"writes,omitempty"`
}

// write is a change a device writes after the rules, without its clock and device: those depend on
// the device.
type write struct {
	Type string         `json:"type"`
	UID  string         `json:"uid"`
	Set  map[string]any `json:"set"`
}

type record struct {
	UID    string         `json:"uid"`
	Fields map[string]any `json:"fields"`
}

// repeatVector lists the occurrences of a reminder on the days [From, To) at Now, in time zone TZ.
type repeatVector struct {
	File     string         `json:"-"`
	Name     string         `json:"name"`
	TZ       string         `json:"tz"`
	Reminder map[string]any `json:"reminder"`
	From     string         `json:"from"`
	To       string         `json:"to"`
	Now      string         `json:"now"`
	Expect   []occurrence   `json:"expect"`
}

// occurrence is the due wall time (a date for an all-day reminder) and status of one occurrence.
type occurrence struct {
	Due    string `json:"due"`
	Status string `json:"status"`
}

func TestHLCVectors(t *testing.T) {
	for _, vector := range hlcVectors() {
		t.Run(vector.File, func(t *testing.T) {
			v := roundTrip(t, vector)
			t.Log(v.Name)
			for _, s := range v.Valid {
				h, err := journal.ParseHLC(s)
				require.NoError(t, err, s)
				assert.Equal(t, s, h.String())
			}
			for _, s := range v.Invalid {
				_, err := journal.ParseHLC(s)
				require.Error(t, err, s)
			}
			checkOrder(t, v.Order)
			runClock(t, v.Last, v.Steps)
		})
	}
}

// checkOrder merges every pair of changes in the vector's order, later one first, and expects the
// later one to win; comparing the clocks as strings must give the same order.
func checkOrder(t *testing.T, order [][2]string) {
	t.Helper()
	for i := range order {
		for j := i + 1; j < len(order); j++ {
			state := journal.NewState()
			for _, k := range []int{j, i} {
				state.Apply(journal.Change{
					HLC:  parseHLC(t, order[k][0]),
					Dev:  order[k][1],
					Type: journal.TypeActivity,
					UID:  "019a9b3c-0000-7000-8000-000000000001",
					Set:  map[string]json.RawMessage{"description": json.RawMessage(strconv.Itoa(k))},
				})
			}
			got := state.Records(journal.TypeActivity)[0].Fields["description"].Value
			assert.JSONEq(t, strconv.Itoa(j), string(got), "%v before %v", order[i], order[j])
			assert.LessOrEqual(t, order[i][0], order[j][0])
		}
	}
}

func runClock(t *testing.T, last string, steps []hlcStep) {
	t.Helper()
	var start journal.HLC
	if last != "" {
		start = parseHLC(t, last)
	}
	clock := journal.NewClock(start)
	for i, step := range steps {
		now := time.UnixMilli(step.Now)
		switch step.Op {
		case "new":
			assert.Equal(t, step.Expect, clock.Next(now).String(), "step %d", i)
		case "read":
			clock.Observe(parseHLC(t, step.HLC), now)
			assert.Equal(t, step.Expect, clock.Last().String(), "step %d", i)
		default:
			t.Fatalf("step %d: unknown op %q", i, step.Op)
		}
	}
}

func TestMergeVectors(t *testing.T) {
	for _, vector := range mergeVectors() {
		t.Run(vector.File, func(t *testing.T) {
			v := roundTrip(t, vector)
			t.Log(v.Name)
			journals := make(map[string][]byte, len(v.Journals))
			for dev, lines := range v.Journals {
				journals[dev] = journalBytes(t, lines)
			}
			devs := slices.Sorted(maps.Keys(journals))
			reversed := slices.Clone(devs)
			slices.Reverse(reversed)

			// The result must not depend on the order journals are read in, on reading them again,
			// or on reading them a few bytes at a time.
			runs := map[string]*journal.State{}
			skipped := map[string]int{}
			runs["in order"], skipped["in order"] = readJournals(journals, devs, 1)
			runs["reversed"], skipped["reversed"] = readJournals(journals, reversed, 1)
			runs["twice"], skipped["twice"] = readJournals(journals, devs, 2)
			runs["in steps"], skipped["in steps"] = readInSteps(journals, devs, 7)

			for name, state := range runs {
				assert.Equal(t, v.Expect.Skipped, skipped[name], "%s: skipped lines", name)
				checkVisible(t, state, v.Expect, name)
				for dev, want := range v.Expect.Writes {
					assert.Equal(t, want, writes(t, state.ApplyRules(dev)), "%s: writes of %s", name, dev)
				}

				// Once every owner has written its ends, devices show the same records and the rule
				// gives no more ends.
				writeOwnEnds(state, devs)
				require.Empty(t, state.Stops(), "%s: the rule holds once the owners write their ends", name)
				checkVisible(t, state, v.Expect, name+", after the owners write")
			}
		})
	}
}

// journalBytes writes a vector's journal: an object as one line of compact JSON, a string as is,
// so it can hold a malformed line or one cut off before its "\n".
func journalBytes(t *testing.T, lines []json.RawMessage) []byte {
	t.Helper()
	var buf bytes.Buffer
	for _, line := range lines {
		if line[0] == '"' {
			var raw string
			require.NoError(t, json.Unmarshal(line, &raw))
			buf.WriteString(raw)
			continue
		}
		require.NoError(t, json.Compact(&buf, line))
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// readJournals reads every journal whole, times times over, and counts the lines skipped in the first pass.
func readJournals(journals map[string][]byte, devs []string, times int) (*journal.State, int) {
	state := journal.NewState()
	skipped := 0
	for pass := range times {
		for _, dev := range devs {
			seg := journal.ReadSegment(dev, journals[dev])
			for _, change := range seg.Changes {
				state.Apply(change)
			}
			if pass == 0 {
				skipped += len(seg.Skipped)
			}
		}
	}
	return state, skipped
}

// readInSteps reads the journals as if they arrived step bytes at a time, keeping a cursor per journal.
func readInSteps(journals map[string][]byte, devs []string, step int) (*journal.State, int) {
	state := journal.NewState()
	cursors := make(map[string]int, len(devs))
	skipped := 0
	for size := step; ; size += step {
		done := true
		for _, dev := range devs {
			data := journals[dev][:min(size, len(journals[dev]))]
			done = done && len(data) == len(journals[dev])
			seg := journal.ReadSegment(dev, data[cursors[dev]:])
			cursors[dev] += seg.Read
			skipped += len(seg.Skipped)
			for _, change := range seg.Changes {
				state.Apply(change)
			}
		}
		if done {
			return state, skipped
		}
	}
}

// writeOwnEnds has every device write the ends of the activities it owns, as each one would after
// reading every change: all of them from the same state, on a clock that goes on from the latest
// clock in the state.
func writeOwnEnds(state *journal.State, devs []string) {
	var latest journal.HLC
	for _, typ := range []journal.RecordType{journal.TypeActivity, journal.TypeReminder} {
		for _, rec := range state.Records(typ) {
			for _, field := range rec.Fields {
				if field.HLC.Compare(latest) > 0 {
					latest = field.HLC
				}
			}
		}
	}

	var changes []journal.Change
	clock := journal.NewClock(latest)
	for _, dev := range devs {
		for _, stop := range state.ApplyRules(dev).Writes {
			changes = append(changes, journal.Change{
				HLC:  clock.Next(time.UnixMilli(0)),
				Dev:  dev,
				Type: journal.TypeActivity,
				UID:  stop.UID,
				Set:  map[string]json.RawMessage{"end": stop.End},
			})
		}
	}
	for _, change := range changes {
		state.Apply(change)
	}
}

// writes returns the changes a device writes after the rules, in uid order.
func writes(t *testing.T, rules journal.Rules) []write {
	t.Helper()
	out := []write{}
	for _, stop := range rules.Writes {
		var end any
		require.NoError(t, json.Unmarshal(stop.End, &end))
		out = append(out, write{Type: string(journal.TypeActivity), UID: stop.UID, Set: map[string]any{"end": end}})
	}
	slices.SortFunc(out, func(a, b write) int { return strings.Compare(a.UID, b.UID) })
	return out
}

// checkVisible compares the records every device shows after the rules with the expected ones.
func checkVisible(t *testing.T, state *journal.State, expect mergeExpect, name string) {
	t.Helper()
	rules := state.ApplyRules("")
	assert.Equal(t, expect.Activities, visible(t, state, rules, journal.TypeActivity), "%s: activities", name)
	assert.Equal(t, expect.Reminders, visible(t, state, rules, journal.TypeReminder), "%s: reminders", name)
}

// visible returns the records of one type that are not hidden, in uid order, with their merged fields
// as devices show them after the rules.
func visible(t *testing.T, state *journal.State, rules journal.Rules, typ journal.RecordType) []record {
	t.Helper()
	records := []record{}
	for _, stored := range state.Records(typ) {
		rec := rules.Show(stored)
		var shown bool
		if typ == journal.TypeActivity {
			_, shown = rec.Activity()
		} else {
			_, shown = rec.Reminder()
		}
		if !shown {
			continue
		}
		values := make(map[string]any, len(rec.Fields))
		for path, field := range rec.Fields {
			var value any
			require.NoError(t, json.Unmarshal(field.Value, &value))
			values[path] = value
		}
		records = append(records, record{UID: rec.UID, Fields: values})
	}
	return records
}

func TestRepeatVectors(t *testing.T) {
	for _, vector := range repeatVectors() {
		t.Run(vector.File, func(t *testing.T) {
			v := roundTrip(t, vector)
			t.Log(v.Name)
			loc, err := time.LoadLocation(v.TZ)
			if err != nil {
				t.Skipf("timezone data unavailable: %v", err)
			}
			setLocal(t, loc)

			reminderFields := make(map[string]journal.Field, len(v.Reminder))
			for name, value := range v.Reminder {
				raw, marshalErr := json.Marshal(value)
				require.NoError(t, marshalErr)
				reminderFields[name] = journal.Field{Value: raw}
			}
			reminder, ok := journal.Record{
				Type:   journal.TypeReminder,
				UID:    "019a9b3c-0000-7000-8000-000000000001",
				Fields: reminderFields,
			}.Reminder()
			require.True(t, ok, "the reminder is hidden")

			now := localTime(t, "2006-01-02T15:04", v.Now)
			got := []occurrence{}
			for _, o := range reminder.Occurrences(localTime(t, time.DateOnly, v.From), localTime(t, time.DateOnly, v.To), now) {
				layout := "2006-01-02T15:04"
				if reminder.AllDay {
					layout = time.DateOnly
				}
				got = append(got, occurrence{Due: o.Due.In(time.Local).Format(layout), Status: string(o.Status(now))})
			}
			assert.Equal(t, v.Expect, got)
		})
	}
}

// setLocal runs the rest of the test in loc: reminders compute their occurrences in time.Local.
func setLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	saved := time.Local
	time.Local = loc //nolint:reassign // the models read the zone from time.Local
	t.Cleanup(func() {
		time.Local = saved //nolint:reassign // restores the zone set above
	})
}

// localTime reads a local wall time, or a date as the first instant of that day. A wall time that a
// daylight saving change skips moves forward by the gap, so a day whose midnight is skipped starts
// at the end of the gap; time.ParseInLocation would put it on the day before in some zones.
func localTime(t *testing.T, layout, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(layout, value)
	require.NoError(t, err)
	return timeutil.WallTime(parsed.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), time.Local)
}

func parseHLC(t *testing.T, s string) journal.HLC {
	t.Helper()
	h, err := journal.ParseHLC(s)
	require.NoError(t, err)
	return h
}

// roundTrip returns a vector as tock-ios reads it, encoded to JSON and decoded again, so the tests
// run what TestWriteVectors writes.
func roundTrip[T any](t *testing.T, vector T) T {
	t.Helper()
	data, err := json.Marshal(vector)
	require.NoError(t, err)
	var v T
	require.NoError(t, json.Unmarshal(data, &v))
	return v
}

// TestWriteVectors writes the vectors as JSON files, one per vector, into <dir>/hlc, <dir>/merge and
// <dir>/repeat for tock-ios when SYNC_VECTORS_OUT is an absolute path <dir>: make sync-vectors OUT=<dir>.
// It first removes the *.json files there, so a renamed vector does not linger.
func TestWriteVectors(t *testing.T) {
	out := os.Getenv("SYNC_VECTORS_OUT")
	if out == "" {
		t.Skip("set SYNC_VECTORS_OUT to write the vectors")
	}
	require.True(t, filepath.IsAbs(out), "SYNC_VECTORS_OUT must be an absolute path")

	files := map[string]map[string]any{"hlc": {}, "merge": {}, "repeat": {}}
	for _, v := range hlcVectors() {
		files["hlc"][v.File] = v
	}
	for _, v := range mergeVectors() {
		files["merge"][v.File] = v
	}
	for _, v := range repeatVectors() {
		files["repeat"][v.File] = v
	}

	for kind, vectors := range files {
		dir := filepath.Join(out, kind)
		require.NoError(t, os.MkdirAll(dir, 0o750))
		stale, err := filepath.Glob(filepath.Join(dir, "*.json"))
		require.NoError(t, err)
		for _, path := range stale {
			require.NoError(t, os.Remove(path))
		}
		for name, vector := range vectors {
			data, marshalErr := json.MarshalIndent(vector, "", "  ")
			require.NoError(t, marshalErr)
			require.NoError(t, os.WriteFile(filepath.Join(dir, name+".json"), append(data, '\n'), 0o600))
		}
		t.Logf("wrote %d vectors to %s", len(vectors), dir)
	}
}
