// Package journal is the sync format of docs/sync.md: the clock, the lines of a journal, the
// field-by-field merge and the rules applied after it. It does no I/O.
package journal

import (
	"cmp"
	"fmt"
	"time"

	"github.com/go-faster/errors"
)

const (
	wallDigits    = 16
	counterDigits = 6
	// maxCounter is the largest counter; a clock past it moves its wall on by 1 ms instead.
	maxCounter = 999999
	// maxAhead is how far ahead of the device's own time a clock it reads can be and still advance its clock.
	maxAhead = 24 * time.Hour
)

// HLC is a hybrid logical clock: a wall time in milliseconds since the Unix epoch and a counter
// that orders changes within one millisecond.
type HLC struct {
	Wall    int64
	Counter int
}

// ParseHLC reads a clock written as <16-digit wall>.<6-digit counter>.
func ParseHLC(s string) (HLC, error) {
	if len(s) != wallDigits+1+counterDigits || s[wallDigits] != '.' {
		return HLC{}, errors.Errorf("invalid hlc %q", s)
	}
	wall, okWall := parseDigits(s[:wallDigits])
	counter, okCounter := parseDigits(s[wallDigits+1:])
	if !okWall || !okCounter {
		return HLC{}, errors.Errorf("invalid hlc %q", s)
	}
	return HLC{Wall: wall, Counter: int(counter)}, nil
}

func (h HLC) String() string {
	return fmt.Sprintf("%016d.%06d", h.Wall, h.Counter)
}

// Compare orders clocks by wall time, then by counter.
func (h HLC) Compare(other HLC) int {
	if c := cmp.Compare(h.Wall, other.Wall); c != 0 {
		return c
	}
	return cmp.Compare(h.Counter, other.Counter)
}

// parseDigits reads a non-negative decimal number made of ASCII digits only.
func parseDigits(s string) (int64, bool) {
	var n int64
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n, true
}

// Clock issues the clocks of a device's changes.
type Clock struct {
	last HLC
}

// NewClock returns a clock that goes on from last, the latest clock the device wrote or read.
func NewClock(last HLC) *Clock {
	return &Clock{last: last}
}

// Last returns the latest clock the device wrote or read.
func (c *Clock) Last() HLC {
	return c.last
}

// Next returns the clock of a change made at now: never earlier than the device's own time and
// always after every clock it wrote or read.
func (c *Clock) Next(now time.Time) HLC {
	next := HLC{Wall: max(now.UnixMilli(), c.last.Wall)}
	if next.Wall == c.last.Wall {
		next.Counter = c.last.Counter + 1
		if next.Counter > maxCounter {
			next = HLC{Wall: next.Wall + 1}
		}
	}
	c.last = next
	return next
}

// Observe advances the clock to h, a clock read from a journal, so the device's next change comes
// after it. A clock more than 24 hours ahead of now does not advance it: one device with a wrong
// clock must not drag every other device into the future.
func (c *Clock) Observe(h HLC, now time.Time) {
	if h.Wall > now.Add(maxAhead).UnixMilli() {
		return
	}
	if h.Compare(c.last) > 0 {
		c.last = h
	}
}
