package timeutil

import (
	"strings"
	"time"

	"github.com/go-faster/errors"
)

const daysPerWeek = 7

// ParseWeekday parses a weekday name such as "mo", "mon" or "monday", in any case.
func ParseWeekday(name string) (time.Weekday, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		full := strings.ToLower(wd.String())
		if name == full || name == full[:3] || name == full[:2] {
			return wd, true
		}
	}
	return 0, false
}

// ParseDay parses a day given as YYYY-MM-DD, "today", "tomorrow" or a weekday name, which means
// its next occurrence from today on. It returns the start of that day in local time.
func ParseDay(input string, now time.Time) (time.Time, error) {
	input = strings.ToLower(strings.TrimSpace(input))
	today, _ := LocalDayBounds(now)

	switch input {
	case "", "today":
		return today, nil
	case "tomorrow":
		return today.AddDate(0, 0, 1), nil
	}

	if wd, ok := ParseWeekday(input); ok {
		ahead := (int(wd) - int(today.Weekday()) + daysPerWeek) % daysPerWeek
		return today.AddDate(0, 0, ahead), nil
	}

	day, err := time.ParseInLocation(time.DateOnly, input, time.Local)
	if err != nil {
		return time.Time{}, errors.New("invalid day (use YYYY-MM-DD, today, tomorrow or a weekday like mon)")
	}
	return day, nil
}
