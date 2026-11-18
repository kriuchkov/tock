package journal

import (
	"cmp"
	"encoding/json"
	"slices"
)

// Field is the merged value of one field and the clock of the change that set it.
type Field struct {
	Value json.RawMessage
	HLC   HLC
	Dev   string
}

// Record is the merged state of one record, hidden or not.
type Record struct {
	Type   RecordType
	UID    string
	Fields map[string]Field
}

type recordKey struct {
	typ RecordType
	uid string
}

// State is the merge of the changes read so far. Every field holds the value of the latest change
// that set it, by clock and then by device, so the order changes are applied in does not matter.
// The one exception is two changes of one device with the same clock, which only a cloned device
// writes: the one applied later wins, and a journal is always applied in file order.
type State struct {
	records map[recordKey]*Record
}

func NewState() *State {
	return &State{records: make(map[recordKey]*Record)}
}

// Apply merges a change into the state.
func (s *State) Apply(change Change) {
	key := recordKey{typ: change.Type, uid: change.UID}
	record, ok := s.records[key]
	if !ok {
		record = &Record{Type: change.Type, UID: change.UID, Fields: make(map[string]Field, len(change.Set))}
		s.records[key] = record
	}
	for path, value := range change.Set {
		if old, set := record.Fields[path]; set && compareChanges(change.HLC, change.Dev, old.HLC, old.Dev) < 0 {
			continue
		}
		record.Fields[path] = Field{Value: value, HLC: change.HLC, Dev: change.Dev}
	}
}

// Records returns the records of one type in uid order, hidden ones included.
func (s *State) Records(typ RecordType) []Record {
	var records []Record
	for key, record := range s.records {
		if key.typ == typ {
			records = append(records, *record)
		}
	}
	slices.SortFunc(records, func(a, b Record) int { return cmp.Compare(a.UID, b.UID) })
	return records
}

// compareChanges orders changes by clock, then by device.
func compareChanges(hlc HLC, dev string, otherHLC HLC, otherDev string) int {
	if c := hlc.Compare(otherHLC); c != 0 {
		return c
	}
	return cmp.Compare(dev, otherDev)
}
