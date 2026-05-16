package timeutil

import (
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
)

// TimeFormat represents the configured time display format.
type TimeFormat int

const (
	Format24Hour TimeFormat = iota // default
	Format12Hour
)

// Time layout string constants.
const (
	layout24Hour         = "15:04"
	layout12Hour         = "03:04 PM"
	layout12HourNoZero   = "3:04 PM"
	layout24HourDate     = "2006-01-02 15:04"
	layout12HourDate     = "2006-01-02 03:04 PM"
	formatDecimalKeyword = "decimal"
)

// Formatter handles time formatting and parsing.
type Formatter struct {
	format TimeFormat
}

// NewFormatter creates a new Formatter with the given format string.
// Valid values are "12" for 12-hour format and anything else for 24-hour format.
func NewFormatter(formatStr string) *Formatter {
	format := Format24Hour // default
	if formatStr == "12" {
		format = Format12Hour
	}
	return &Formatter{format: format}
}

// Format returns the TimeFormat configured for this Formatter.
func (f *Formatter) Format() TimeFormat {
	return f.format
}

// GetDisplayFormat returns the Go time format string for display.
func (f *Formatter) GetDisplayFormat() string {
	if f.format == Format12Hour {
		return layout12Hour
	}
	return layout24Hour
}

// GetDisplayFormatWithDate returns format string with date.
func (f *Formatter) GetDisplayFormatWithDate() string {
	if f.format == Format12Hour {
		return layout12HourDate
	}
	return layout24HourDate
}

// ParseTime parses user input supporting both 12hr and 24hr formats
// Returns time.Time for today at the specified time.
func (f *Formatter) ParseTime(input string) (time.Time, error) {
	input = strings.TrimSpace(input)

	// Try 24-hour format first (always supported as fallback)
	parsed, err := time.ParseInLocation(layout24Hour, input, time.Local)
	if err == nil {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(),
			parsed.Hour(), parsed.Minute(), 0, 0, time.Local), nil
	}

	// If in 12-hour mode, try 12-hour formats
	if f.format == Format12Hour {
		// Try various 12-hour formats (both zero-padded and non-padded)
		formats := []string{
			layout12HourNoZero, "3:04PM", // with minutes, no padding
			layout12Hour, "03:04PM", // with minutes, zero-padded
			"3 PM", "3PM", // without minutes, no padding
			"03 PM", "03PM", // without minutes, zero-padded
		}

		for _, layout := range formats {
			// Try original case
			parsed, err = time.ParseInLocation(layout, input, time.Local)
			if err == nil {
				now := time.Now()
				return time.Date(now.Year(), now.Month(), now.Day(),
					parsed.Hour(), parsed.Minute(), 0, 0, time.Local), nil
			}

			// Try uppercase version for case-insensitive matching
			upperInput := strings.ToUpper(input)
			upperLayout := strings.ToUpper(layout)
			parsed, err = time.ParseInLocation(upperLayout, upperInput, time.Local)
			if err == nil {
				now := time.Now()
				return time.Date(now.Year(), now.Month(), now.Day(),
					parsed.Hour(), parsed.Minute(), 0, 0, time.Local), nil
			}
		}
	}

	return time.Time{}, errors.New("invalid time format (use HH:MM or, in 12hr mode, formats like '3:04 PM' or '3PM')")
}

// ParseTimeWithDate parses time that may include a date
// Supports: "HH:MM", "YYYY-MM-DD HH:MM" (and 12hr equivalents).
func (f *Formatter) ParseTimeWithDate(input string) (time.Time, error) {
	input = strings.TrimSpace(input)

	// Try time-only format first (HH:MM for today)
	result, err := f.ParseTime(input)
	if err == nil {
		return result, nil
	}

	// Try 24-hour with date: "2006-01-02 15:04"
	parsed, err := time.ParseInLocation(layout24HourDate, input, time.Local)
	if err == nil {
		return parsed, nil
	}

	// Try 12-hour with date if in 12hr mode
	if f.format == Format12Hour {
		formats := []string{
			"2006-01-02 3:04 PM",
			"2006-01-02 3:04PM",
			layout12HourDate,
			"2006-01-02 03:04PM",
		}

		for _, layout := range formats {
			// Try original case
			parsed, err = time.ParseInLocation(layout, input, time.Local)
			if err == nil {
				return parsed, nil
			}

			// Try case-insensitive
			// We need to uppercase only the AM/PM part
			upperInput := input
			for _, meridiem := range []string{"am", "pm", "AM", "PM", "Am", "Pm", "aM", "pM"} {
				if strings.Contains(input, meridiem) {
					upperInput = strings.ReplaceAll(input, meridiem, strings.ToUpper(meridiem))
					break
				}
			}

			parsed, err = time.ParseInLocation(layout, upperInput, time.Local)
			if err == nil {
				return parsed, nil
			}
		}
	}

	return time.Time{}, errors.New("invalid time format (use HH:MM or YYYY-MM-DD HH:MM)")
}

// FormatDuration formats a duration using Go's time layout constants.
// The special format "decimal" returns decimal hours rounded to 2 decimal places,
// e.g. 2h15m → "2.25", 7h20m → "7.33". Use "decimal:N" for N decimal places,
// e.g. "decimal:0" → "7", "decimal:4" → "7.3333".
func FormatDuration(d time.Duration, format string) string {
	if format == formatDecimalKeyword || strings.HasPrefix(format, formatDecimalKeyword+":") {
		prec := 2
		if after, ok := strings.CutPrefix(format, formatDecimalKeyword+":"); ok {
			if n, err := strconv.Atoi(after); err == nil && n >= 0 {
				prec = n
			}
		}
		hours := d.Round(time.Minute).Minutes() / 60
		return strconv.FormatFloat(hours, 'f', prec, 64)
	}

	if format == "" {
		return d.Round(time.Minute).String()
	}

	t := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC).Add(d)
	return t.Format(format)
}

// LocalDayBounds returns the start of the day and the start of the next day in local time.
func LocalDayBounds(t time.Time) (time.Time, time.Time) {
	local := t.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	return start, start.AddDate(0, 0, 1)
}
