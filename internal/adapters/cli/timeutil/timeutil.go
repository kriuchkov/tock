package timeutil

import (
	"os"
	"strings"
	"time"

	"github.com/go-faster/errors"
)

// TimeFormat represents the configured time display format
type TimeFormat int

const (
	Format24Hour TimeFormat = iota // default
	Format12Hour
)

// Config holds time formatting configuration
type Config struct {
	format TimeFormat
}

// Global config instance
var config *Config

// Initialize reads TOCK_TIME_FORMAT env var and sets up configuration
func Initialize() {
	format := Format24Hour // default

	envValue := os.Getenv("TOCK_TIME_FORMAT")
	if envValue == "12" {
		format = Format12Hour
	}

	config = &Config{format: format}
}

// GetDisplayFormat returns the Go time format string for display
func GetDisplayFormat() string {
	if config == nil {
		Initialize()
	}

	if config.format == Format12Hour {
		return "03:04 PM"
	}
	return "15:04"
}

// GetDisplayFormatWithDate returns format string with date
func GetDisplayFormatWithDate() string {
	if config == nil {
		Initialize()
	}

	if config.format == Format12Hour {
		return "2006-01-02 03:04 PM"
	}
	return "2006-01-02 15:04"
}

// ParseTime parses user input supporting both 12hr and 24hr formats
// Returns time.Time for today at the specified time
func ParseTime(input string) (time.Time, error) {
	if config == nil {
		Initialize()
	}

	input = strings.TrimSpace(input)

	// Try 24-hour format first (always supported as fallback)
	parsed, err := time.ParseInLocation("15:04", input, time.Local)
	if err == nil {
		now := time.Now()
		return time.Date(now.Year(), now.Month(), now.Day(),
			parsed.Hour(), parsed.Minute(), 0, 0, time.Local), nil
	}

	// If in 12-hour mode, try 12-hour formats
	if config.format == Format12Hour {
		// Try various 12-hour formats (both zero-padded and non-padded)
		formats := []string{
			"3:04 PM", "3:04PM",     // with minutes, no padding
			"03:04 PM", "03:04PM",   // with minutes, zero-padded
			"3 PM", "3PM",           // without minutes, no padding
			"03 PM", "03PM",         // without minutes, zero-padded
		}

		for _, layout := range formats {
			// Try original case
			parsed, err := time.ParseInLocation(layout, input, time.Local)
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
// Supports: "HH:MM", "YYYY-MM-DD HH:MM" (and 12hr equivalents)
func ParseTimeWithDate(input string) (time.Time, error) {
	if config == nil {
		Initialize()
	}

	input = strings.TrimSpace(input)

	// Try time-only format first (HH:MM for today)
	result, err := ParseTime(input)
	if err == nil {
		return result, nil
	}

	// Try 24-hour with date: "2006-01-02 15:04"
	parsed, err := time.ParseInLocation("2006-01-02 15:04", input, time.Local)
	if err == nil {
		return parsed, nil
	}

	// Try 12-hour with date if in 12hr mode
	if config.format == Format12Hour {
		formats := []string{
			"2006-01-02 3:04 PM",
			"2006-01-02 3:04PM",
			"2006-01-02 03:04 PM",
			"2006-01-02 03:04PM",
		}

		for _, layout := range formats {
			// Try original case
			parsed, err := time.ParseInLocation(layout, input, time.Local)
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
