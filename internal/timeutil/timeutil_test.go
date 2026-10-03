package timeutil

import (
	"testing"
	"time"
)

func TestNewFormatter(t *testing.T) {
	tests := []struct {
		name      string
		formatStr string
		expected  TimeFormat
	}{
		{"default when empty string", "", Format24Hour},
		{"12 hour when set to 12", "12", Format12Hour},
		{"24 hour when set to 24", "24", Format24Hour},
		{"default for invalid value", "invalid", Format24Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewFormatter(tt.formatStr)
			if f.Format() != tt.expected {
				t.Errorf("NewFormatter(%q).Format() = %v, want %v", tt.formatStr, f.Format(), tt.expected)
			}
		})
	}
}

func TestGetDisplayFormat(t *testing.T) {
	tests := []struct {
		name     string
		format   TimeFormat
		expected string
	}{
		{"24 hour format", Format24Hour, "15:04"},
		{"12 hour format", Format12Hour, "03:04 PM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Formatter{format: tt.format}
			result := f.GetDisplayFormat()
			if result != tt.expected {
				t.Errorf("GetDisplayFormat() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetDisplayFormatWithDate(t *testing.T) {
	tests := []struct {
		name     string
		format   TimeFormat
		expected string
	}{
		{"24 hour format with date", Format24Hour, "2006-01-02 15:04"},
		{"12 hour format with date", Format12Hour, "2006-01-02 03:04 PM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Formatter{format: tt.format}
			result := f.GetDisplayFormatWithDate()
			if result != tt.expected {
				t.Errorf("GetDisplayFormatWithDate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseTime_24HourMode(t *testing.T) {
	f := &Formatter{format: Format24Hour}

	tests := []struct {
		name      string
		input     string
		wantHour  int
		wantMin   int
		wantError bool
	}{
		{"valid 24hr time", "15:04", 15, 4, false},
		{"midnight", "00:00", 0, 0, false},
		{"noon", "12:00", 12, 0, false},
		{"with leading zero", "09:30", 9, 30, false},
		{"12hr format should fail in 24hr mode", "3:04 PM", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := f.ParseTime(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("ParseTime(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTime(%q) unexpected error: %v", tt.input, err)
				return
			}

			if result.Hour() != tt.wantHour {
				t.Errorf("ParseTime(%q) hour = %d, want %d", tt.input, result.Hour(), tt.wantHour)
			}
			if result.Minute() != tt.wantMin {
				t.Errorf("ParseTime(%q) minute = %d, want %d", tt.input, result.Minute(), tt.wantMin)
			}

			// Verify it's set to today
			now := time.Now()
			if result.Year() != now.Year() || result.Month() != now.Month() || result.Day() != now.Day() {
				t.Errorf("ParseTime(%q) not set to today's date", tt.input)
			}
		})
	}
}

func TestParseTime_12HourMode(t *testing.T) {
	f := &Formatter{format: Format12Hour}

	tests := []struct {
		name      string
		input     string
		wantHour  int
		wantMin   int
		wantError bool
	}{
		// 24hr fallback
		{"24hr fallback", "15:04", 15, 4, false},
		{"midnight 24hr", "00:00", 0, 0, false},

		// 12hr formats with minutes
		{"12hr with space uppercase", "3:04 PM", 15, 4, false},
		{"12hr without space uppercase", "3:04PM", 15, 4, false},
		{"12hr with space lowercase", "3:04 pm", 15, 4, false},
		{"12hr without space lowercase", "3:04pm", 15, 4, false},
		{"12hr AM", "9:30 AM", 9, 30, false},
		{"12hr AM lowercase", "9:30am", 9, 30, false},

		// 12hr without minutes
		{"12hr PM no minutes", "3PM", 15, 0, false},
		{"12hr PM no minutes with space", "3 PM", 15, 0, false},
		{"12hr pm no minutes lowercase", "3pm", 15, 0, false},
		{"12hr AM no minutes", "9AM", 9, 0, false},

		// Zero-padded formats
		{"12hr zero-padded with space", "03:04 PM", 15, 4, false},
		{"12hr zero-padded without space", "03:04PM", 15, 4, false},
		{"12hr zero-padded lowercase", "03:04 pm", 15, 4, false},
		{"12hr zero-padded AM", "08:30 AM", 8, 30, false},
		{"12hr zero-padded no minutes", "03PM", 15, 0, false},
		{"12hr zero-padded no minutes with space", "03 PM", 15, 0, false},
		{"single digit hour zero-padded", "01:15 AM", 1, 15, false},

		// Edge cases
		{"noon 12hr", "12:00 PM", 12, 0, false},
		{"midnight 12hr", "12:00 AM", 0, 0, false},

		// Invalid
		{"invalid format", "25:00", 0, 0, true},
		{"invalid text", "not a time", 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := f.ParseTime(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("ParseTime(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTime(%q) unexpected error: %v", tt.input, err)
				return
			}

			if result.Hour() != tt.wantHour {
				t.Errorf("ParseTime(%q) hour = %d, want %d", tt.input, result.Hour(), tt.wantHour)
			}
			if result.Minute() != tt.wantMin {
				t.Errorf("ParseTime(%q) minute = %d, want %d", tt.input, result.Minute(), tt.wantMin)
			}

			// Verify it's set to today
			now := time.Now()
			if result.Year() != now.Year() || result.Month() != now.Month() || result.Day() != now.Day() {
				t.Errorf("ParseTime(%q) not set to today's date", tt.input)
			}
		})
	}
}

// checkDate validates date components of a parsed time result.
func checkDate(t *testing.T, input string, result time.Time, wantYear int, wantMonth time.Month, wantDay int) {
	t.Helper()
	if wantYear == 0 {
		// For time-only input, check it's today
		now := time.Now()
		if result.Year() != now.Year() || result.Month() != now.Month() || result.Day() != now.Day() {
			t.Errorf("ParseTimeWithDate(%q) not set to today's date", input)
		}
		return
	}
	if result.Year() != wantYear {
		t.Errorf("ParseTimeWithDate(%q) year = %d, want %d", input, result.Year(), wantYear)
	}
	if result.Month() != wantMonth {
		t.Errorf("ParseTimeWithDate(%q) month = %v, want %v", input, result.Month(), wantMonth)
	}
	if result.Day() != wantDay {
		t.Errorf("ParseTimeWithDate(%q) day = %d, want %d", input, result.Day(), wantDay)
	}
}

func TestParseTimeWithDate_24HourMode(t *testing.T) {
	f := &Formatter{format: Format24Hour}

	tests := []struct {
		name      string
		input     string
		wantYear  int
		wantMonth time.Month
		wantDay   int
		wantHour  int
		wantMin   int
		wantError bool
	}{
		{"time only", "15:04", 0, 0, 0, 15, 4, false}, // year/month/day will be today
		{"full datetime", "2025-01-05 15:04", 2025, time.January, 5, 15, 4, false},
		{"different date", "2024-12-25 23:59", 2024, time.December, 25, 23, 59, false},
		{"12hr format should fail", "2025-01-05 3:04 PM", 0, 0, 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := f.ParseTimeWithDate(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("ParseTimeWithDate(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTimeWithDate(%q) unexpected error: %v", tt.input, err)
				return
			}

			checkDate(t, tt.input, result, tt.wantYear, tt.wantMonth, tt.wantDay)

			if result.Hour() != tt.wantHour {
				t.Errorf("ParseTimeWithDate(%q) hour = %d, want %d", tt.input, result.Hour(), tt.wantHour)
			}
			if result.Minute() != tt.wantMin {
				t.Errorf("ParseTimeWithDate(%q) minute = %d, want %d", tt.input, result.Minute(), tt.wantMin)
			}
		})
	}
}

func TestParseTimeWithDate_12HourMode(t *testing.T) {
	f := &Formatter{format: Format12Hour}

	tests := []struct {
		name      string
		input     string
		wantYear  int
		wantMonth time.Month
		wantDay   int
		wantHour  int
		wantMin   int
		wantError bool
	}{
		// Time only (uses ParseTime which supports 12hr)
		{"time only 12hr", "3:04 PM", 0, 0, 0, 15, 4, false},
		{"time only 24hr fallback", "15:04", 0, 0, 0, 15, 4, false},

		// Full datetime 12hr
		{"full datetime 12hr", "2025-01-05 3:04 PM", 2025, time.January, 5, 15, 4, false},
		{"full datetime 12hr no space", "2025-01-05 3:04PM", 2025, time.January, 5, 15, 4, false},
		{"full datetime 12hr lowercase", "2025-01-05 3:04 pm", 2025, time.January, 5, 15, 4, false},
		{"full datetime AM", "2025-01-05 9:30 AM", 2025, time.January, 5, 9, 30, false},

		// Full datetime 12hr zero-padded
		{"full datetime 12hr zero-padded", "2025-01-05 03:04 PM", 2025, time.January, 5, 15, 4, false},
		{"full datetime 12hr zero-padded no space", "2025-01-05 03:04PM", 2025, time.January, 5, 15, 4, false},
		{"full datetime 12hr zero-padded lowercase", "2025-01-05 03:04 pm", 2025, time.January, 5, 15, 4, false},
		{"full datetime AM zero-padded", "2025-01-05 08:30 AM", 2025, time.January, 5, 8, 30, false},

		// Full datetime 24hr fallback
		{"full datetime 24hr fallback", "2025-01-05 15:04", 2025, time.January, 5, 15, 4, false},

		// Invalid
		{"invalid format", "not a datetime", 0, 0, 0, 0, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := f.ParseTimeWithDate(tt.input)
			if tt.wantError {
				if err == nil {
					t.Errorf("ParseTimeWithDate(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseTimeWithDate(%q) unexpected error: %v", tt.input, err)
				return
			}

			checkDate(t, tt.input, result, tt.wantYear, tt.wantMonth, tt.wantDay)

			if result.Hour() != tt.wantHour {
				t.Errorf("ParseTimeWithDate(%q) hour = %d, want %d", tt.input, result.Hour(), tt.wantHour)
			}
			if result.Minute() != tt.wantMin {
				t.Errorf("ParseTimeWithDate(%q) minute = %d, want %d", tt.input, result.Minute(), tt.wantMin)
			}
		})
	}
}

func TestLocalWeekBounds(t *testing.T) {
	tests := []struct {
		name      string
		input     time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			"mid-week",
			time.Date(2026, time.March, 11, 14, 30, 0, 0, time.Local),
			time.Date(2026, time.March, 9, 0, 0, 0, 0, time.Local),
			time.Date(2026, time.March, 16, 0, 0, 0, 0, time.Local),
		},
		{
			"on Sunday, the last day of the week",
			time.Date(2026, time.March, 15, 14, 30, 0, 0, time.Local),
			time.Date(2026, time.March, 9, 0, 0, 0, 0, time.Local),
			time.Date(2026, time.March, 16, 0, 0, 0, 0, time.Local),
		},
		{
			"week crossing a month and year boundary",
			time.Date(2025, time.December, 31, 9, 0, 0, 0, time.Local),
			time.Date(2025, time.December, 29, 0, 0, 0, 0, time.Local),
			time.Date(2026, time.January, 5, 0, 0, 0, 0, time.Local),
		},
		{
			"on Monday itself",
			time.Date(2026, time.March, 9, 9, 0, 0, 0, time.Local),
			time.Date(2026, time.March, 9, 0, 0, 0, 0, time.Local),
			time.Date(2026, time.March, 16, 0, 0, 0, 0, time.Local),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := LocalWeekBounds(tt.input)
			if !start.Equal(tt.wantStart) {
				t.Errorf("LocalWeekBounds(%v) start = %v, want %v", tt.input, start, tt.wantStart)
			}
			if !end.Equal(tt.wantEnd) {
				t.Errorf("LocalWeekBounds(%v) end = %v, want %v", tt.input, end, tt.wantEnd)
			}
		})
	}
}

func TestLocalWeekBoundsOnDSTSkippedMidnight(t *testing.T) {
	loc, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Skipf("timezone data unavailable: %v", err)
	}
	previous := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = previous })

	// Clocks jump from 00:00 to 01:00 on Sunday 2026-09-06, so that day has no midnight.
	start, end := LocalWeekBounds(time.Date(2026, time.September, 6, 12, 0, 0, 0, loc))
	wantStart := time.Date(2026, time.August, 31, 0, 0, 0, 0, loc)
	wantEnd := time.Date(2026, time.September, 7, 0, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("LocalWeekBounds() = (%v, %v), want (%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestLocalMonthBounds(t *testing.T) {
	start, end := LocalMonthBounds(time.Date(2026, time.March, 15, 14, 30, 0, 0, time.Local))
	wantStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.Local)
	wantEnd := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("LocalMonthBounds() = (%v, %v), want (%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestLocalQuarterBounds(t *testing.T) {
	tests := []struct {
		name      string
		input     time.Time
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			"Q1",
			time.Date(2026, time.March, 15, 0, 0, 0, 0, time.Local),
			time.Date(2026, time.January, 1, 0, 0, 0, 0, time.Local),
			time.Date(2026, time.April, 1, 0, 0, 0, 0, time.Local),
		},
		{
			"Q4",
			time.Date(2026, time.November, 1, 0, 0, 0, 0, time.Local),
			time.Date(2026, time.October, 1, 0, 0, 0, 0, time.Local),
			time.Date(2027, time.January, 1, 0, 0, 0, 0, time.Local),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end := LocalQuarterBounds(tt.input)
			if !start.Equal(tt.wantStart) {
				t.Errorf("LocalQuarterBounds(%v) start = %v, want %v", tt.input, start, tt.wantStart)
			}
			if !end.Equal(tt.wantEnd) {
				t.Errorf("LocalQuarterBounds(%v) end = %v, want %v", tt.input, end, tt.wantEnd)
			}
		})
	}
}

func TestLocalYearBounds(t *testing.T) {
	start, end := LocalYearBounds(time.Date(2026, time.March, 15, 14, 30, 0, 0, time.Local))
	wantStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.Local)
	wantEnd := time.Date(2027, time.January, 1, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Errorf("LocalYearBounds() = (%v, %v), want (%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name   string
		d      time.Duration
		format string
		want   string
	}{
		// decimal format — defaults to 2 decimal places
		{"decimal whole hours", 2*time.Hour + 0*time.Minute, "decimal", "2.00"},
		{"decimal quarter hour", 2*time.Hour + 15*time.Minute, "decimal", "2.25"},
		{"decimal half hour", 5*time.Hour + 45*time.Minute, "decimal", "5.75"},
		{"decimal third hour", 1*time.Hour + 30*time.Minute, "decimal", "1.50"},
		{"decimal sub-hour", 45 * time.Minute, "decimal", "0.75"},
		{"decimal repeating", 1*time.Hour + 20*time.Minute, "decimal", "1.33"},
		{"decimal zero", 0, "decimal", "0.00"},
		// decimal:N — configurable precision
		{"decimal:0 rounds down", 7*time.Hour + 20*time.Minute, "decimal:0", "7"},
		{"decimal:1", 7*time.Hour + 20*time.Minute, "decimal:1", "7.3"},
		{"decimal:4", 7*time.Hour + 20*time.Minute, "decimal:4", "7.3333"},
		{"decimal:2 explicit", 2*time.Hour + 0*time.Minute, "decimal:2", "2.00"},
		// empty format falls back to Go's Duration.String()
		{"empty format", 2*time.Hour + 15*time.Minute, "", "2h15m0s"},
		// Go time layout format
		{"HH:MM layout", 2*time.Hour + 15*time.Minute, "15:04", "02:15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.d, tt.format)
			if got != tt.want {
				t.Errorf("FormatDuration(%v, %q) = %q, want %q", tt.d, tt.format, got, tt.want)
			}
		})
	}
}
