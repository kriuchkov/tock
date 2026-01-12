package timeutil

import (
	"testing"
	"time"
)

func TestInitialize(t *testing.T) {
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
			// Reset config
			config = nil

			Initialize(tt.formatStr)

			if config.format != tt.expected {
				t.Errorf("Initialize(%q) format = %v, want %v", tt.formatStr, config.format, tt.expected)
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
			config = &Config{format: tt.format}
			result := GetDisplayFormat()
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
			config = &Config{format: tt.format}
			result := GetDisplayFormatWithDate()
			if result != tt.expected {
				t.Errorf("GetDisplayFormatWithDate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseTime_24HourMode(t *testing.T) {
	config = &Config{format: Format24Hour}

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
			result, err := ParseTime(tt.input)
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
	config = &Config{format: Format12Hour}

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
			result, err := ParseTime(tt.input)
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

func TestParseTimeWithDate_24HourMode(t *testing.T) {
	config = &Config{format: Format24Hour}

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
			result, err := ParseTimeWithDate(tt.input)
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

			// For time-only, check it's today
			if tt.wantYear == 0 {
				now := time.Now()
				if result.Year() != now.Year() || result.Month() != now.Month() || result.Day() != now.Day() {
					t.Errorf("ParseTimeWithDate(%q) not set to today's date", tt.input)
				}
			} else {
				if result.Year() != tt.wantYear {
					t.Errorf("ParseTimeWithDate(%q) year = %d, want %d", tt.input, result.Year(), tt.wantYear)
				}
				if result.Month() != tt.wantMonth {
					t.Errorf("ParseTimeWithDate(%q) month = %v, want %v", tt.input, result.Month(), tt.wantMonth)
				}
				if result.Day() != tt.wantDay {
					t.Errorf("ParseTimeWithDate(%q) day = %d, want %d", tt.input, result.Day(), tt.wantDay)
				}
			}

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
	config = &Config{format: Format12Hour}

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
			result, err := ParseTimeWithDate(tt.input)
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

			// For time-only, check it's today
			if tt.wantYear == 0 {
				now := time.Now()
				if result.Year() != now.Year() || result.Month() != now.Month() || result.Day() != now.Day() {
					t.Errorf("ParseTimeWithDate(%q) not set to today's date", tt.input)
				}
			} else {
				if result.Year() != tt.wantYear {
					t.Errorf("ParseTimeWithDate(%q) year = %d, want %d", tt.input, result.Year(), tt.wantYear)
				}
				if result.Month() != tt.wantMonth {
					t.Errorf("ParseTimeWithDate(%q) month = %v, want %v", tt.input, result.Month(), tt.wantMonth)
				}
				if result.Day() != tt.wantDay {
					t.Errorf("ParseTimeWithDate(%q) day = %d, want %d", tt.input, result.Day(), tt.wantDay)
				}
			}

			if result.Hour() != tt.wantHour {
				t.Errorf("ParseTimeWithDate(%q) hour = %d, want %d", tt.input, result.Hour(), tt.wantHour)
			}
			if result.Minute() != tt.wantMin {
				t.Errorf("ParseTimeWithDate(%q) minute = %d, want %d", tt.input, result.Minute(), tt.wantMin)
			}
		})
	}
}
