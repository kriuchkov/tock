package journal

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/go-faster/errors"
)

// RecordType is the kind of record a change applies to.
type RecordType string

const (
	TypeActivity RecordType = "activity"
	TypeReminder RecordType = "reminder"
)

const (
	formatVersion  = 1
	deviceIDLength = 16
	uidLength      = 36
)

// Change is one line of a journal: values of fields of one record.
type Change struct {
	HLC  HLC
	Dev  string
	Type RecordType
	UID  string
	// Set maps field paths to their new values, as JSON.
	Set map[string]json.RawMessage
}

// ParseChange reads one line of a journal, without its "\n". Keys are matched exactly and keys it
// does not know are ignored.
func ParseChange(line []byte) (Change, error) {
	if !utf8.Valid(line) {
		return Change{}, errors.New("line is not UTF-8")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil {
		return Change{}, errors.Wrap(err, "parse line")
	}

	var (
		version float64
		hlc     string
		change  Change
	)
	keys := []struct {
		name string
		dst  any
	}{
		{"v", &version}, {"hlc", &hlc}, {"dev", &change.Dev}, {"type", &change.Type}, {"uid", &change.UID}, {"set", &change.Set},
	}
	for _, key := range keys {
		raw, ok := obj[key.name]
		if !ok {
			return Change{}, errors.Errorf("missing %q", key.name)
		}
		if err := json.Unmarshal(raw, key.dst); err != nil {
			return Change{}, errors.Wrapf(err, "parse %q", key.name)
		}
	}

	var err error
	if change.HLC, err = ParseHLC(hlc); err != nil {
		return Change{}, err
	}
	if err = change.validate(version); err != nil {
		return Change{}, err
	}
	return change, nil
}

func (c Change) validate(version float64) error {
	switch {
	case version != formatVersion:
		return errors.Errorf("unknown version %v", version)
	case !isDeviceID(c.Dev):
		return errors.Errorf("invalid dev %q", c.Dev)
	case c.Type != TypeActivity && c.Type != TypeReminder:
		return errors.Errorf("unknown type %q", c.Type)
	case !isUUIDv7(c.UID):
		return errors.Errorf("invalid uid %q", c.UID)
	case len(c.Set) == 0:
		return errors.New("set is empty")
	}
	if _, ok := c.Set[""]; ok {
		return errors.New("empty field path")
	}
	return nil
}

// isDeviceID reports whether s is 16 lowercase hex characters.
func isDeviceID(s string) bool {
	if len(s) != deviceIDLength {
		return false
	}
	for i := range len(s) {
		if !isLowerHex(s[i]) {
			return false
		}
	}
	return true
}

// isUUIDv7 reports whether s is a lowercase UUID of version 7 and the RFC 9562 variant.
func isUUIDv7(s string) bool {
	if len(s) != uidLength || s[14] != '7' || !strings.ContainsRune("89ab", rune(s[19])) {
		return false
	}
	for i := range len(s) {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return false
			}
		default:
			if !isLowerHex(s[i]) {
				return false
			}
		}
	}
	return true
}

func isLowerHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

// Segment is what one read of a journal segment found.
type Segment struct {
	Changes []Change
	// Read is the number of bytes up to the end of the last complete line.
	Read int
	// Skipped holds an error for every complete line that is not a valid change of the journal's device.
	Skipped []error
}

// ReadSegment parses the complete lines of data, read from a segment of dev's journal. A last line
// without "\n" is left for a later read: its writer may still be writing it.
func ReadSegment(dev string, data []byte) Segment {
	var seg Segment
	for {
		end := bytes.IndexByte(data[seg.Read:], '\n')
		if end < 0 {
			return seg
		}
		offset := seg.Read
		seg.Read += end + 1

		change, err := ParseChange(data[offset : offset+end])
		if err == nil && change.Dev != dev {
			err = errors.Errorf("change of device %s in the journal of %s", change.Dev, dev)
		}
		if err != nil {
			seg.Skipped = append(seg.Skipped, errors.Wrapf(err, "line at byte %d", offset))
			continue
		}
		seg.Changes = append(seg.Changes, change)
	}
}
