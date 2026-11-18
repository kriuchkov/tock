package journal_test

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/journal/journaltest"
)

// rulesDevice writes the ends the one-running rule gives.
const rulesDevice = "0123456789abcdef"

type hlcVector struct {
	Name    string      `json:"name"`
	Valid   []string    `json:"valid"`
	Invalid []string    `json:"invalid"`
	Order   [][2]string `json:"order"`
	Last    string      `json:"last"`
	Steps   []hlcStep   `json:"steps"`
}

type hlcStep struct {
	Op     string `json:"op"`
	Now    int64  `json:"now"`
	HLC    string `json:"hlc"`
	Expect string `json:"expect"`
}

type mergeVector struct {
	Name     string                       `json:"name"`
	Journals map[string][]json.RawMessage `json:"journals"`
	Expect   struct {
		Activities []record `json:"activities"`
		Reminders  []record `json:"reminders"`
		Skipped    int      `json:"skipped"`
	} `json:"expect"`
}

type record struct {
	UID    string         `json:"uid"`
	Fields map[string]any `json:"fields"`
}

type repeatVector struct {
	Name     string                     `json:"name"`
	TZ       string                     `json:"tz"`
	Reminder map[string]json.RawMessage `json:"reminder"`
	From     string                     `json:"from"`
	To       string                     `json:"to"`
	Now      string                     `json:"now"`
	Expect   []occurrence               `json:"expect"`
}

type occurrence struct {
	Due    string `json:"due"`
	Status string `json:"status"`
}

func TestHLCVectors(t *testing.T) {
	for _, vector := range journaltest.HLCVectors() {
		t.Run(vector.File, func(t *testing.T) {
			v := wire[hlcVector](t, vector)
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
	for _, vector := range journaltest.MergeVectors() {
		t.Run(vector.File, func(t *testing.T) {
			v := wire[mergeVector](t, vector)
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
				applyStops(t, state)
				assert.Equal(t, v.Expect.Skipped, skipped[name], "%s: skipped lines", name)
				assert.Equal(t, v.Expect.Activities, visible(t, state, journal.TypeActivity), "%s: activities", name)
				assert.Equal(t, v.Expect.Reminders, visible(t, state, journal.TypeReminder), "%s: reminders", name)
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

// applyStops writes the ends the one-running rule gives, as a device would, after every change read.
func applyStops(t *testing.T, state *journal.State) {
	t.Helper()
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

	clock := journal.NewClock(latest)
	for _, stop := range state.Stops() {
		state.Apply(journal.Change{
			HLC:  clock.Next(time.UnixMilli(0)),
			Dev:  rulesDevice,
			Type: journal.TypeActivity,
			UID:  stop.UID,
			Set:  map[string]json.RawMessage{"end": stop.End},
		})
	}
	require.Empty(t, state.Stops(), "the rule holds once its ends are written")
}

// visible returns the records of one type that are not hidden, in uid order, with their merged fields.
func visible(t *testing.T, state *journal.State, typ journal.RecordType) []record {
	t.Helper()
	records := []record{}
	for _, rec := range state.Records(typ) {
		var shown bool
		if typ == journal.TypeActivity {
			_, shown = rec.Activity()
		} else {
			_, shown = rec.Reminder()
		}
		if !shown {
			continue
		}
		fields := make(map[string]any, len(rec.Fields))
		for path, field := range rec.Fields {
			var value any
			require.NoError(t, json.Unmarshal(field.Value, &value))
			fields[path] = value
		}
		records = append(records, record{UID: rec.UID, Fields: fields})
	}
	return records
}

func TestRepeatVectors(t *testing.T) {
	for _, vector := range journaltest.RepeatVectors() {
		t.Run(vector.File, func(t *testing.T) {
			v := wire[repeatVector](t, vector)
			t.Log(v.Name)
			loc, err := time.LoadLocation(v.TZ)
			if err != nil {
				t.Skipf("timezone data unavailable: %v", err)
			}
			setLocal(t, loc)

			fields := make(map[string]journal.Field, len(v.Reminder))
			for name, value := range v.Reminder {
				fields[name] = journal.Field{Value: value}
			}
			reminder, ok := journal.Record{
				Type:   journal.TypeReminder,
				UID:    "019a9b3c-0000-7000-8000-000000000001",
				Fields: fields,
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

func localTime(t *testing.T, layout, value string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation(layout, value, time.Local)
	require.NoError(t, err)
	return parsed
}

func parseHLC(t *testing.T, s string) journal.HLC {
	t.Helper()
	h, err := journal.ParseHLC(s)
	require.NoError(t, err)
	return h
}

// wire returns a vector as tock-ios reads it: encoded by `make sync-vectors` and decoded by keys
// the format defines, so a key the tests do not read cannot slip into the files.
func wire[T any](t *testing.T, vector any) T {
	t.Helper()
	data, err := json.Marshal(vector)
	require.NoError(t, err)
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var v T
	require.NoError(t, dec.Decode(&v))
	return v
}
