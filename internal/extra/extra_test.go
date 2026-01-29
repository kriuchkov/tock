package extra

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestCalculateEndTime(t *testing.T) {
	baseTime := time.Date(2026, 1, 29, 10, 0, 0, 0, time.Local)

	tests := []struct {
		name        string
		formatStr   string // "12" or "24"
		startTime   time.Time
		endStr      string
		durationStr string
		want        time.Time
		wantErr     string
	}{
		{
			name:        "End time provided (24h format)",
			formatStr:   "24",
			startTime:   baseTime,
			endStr:      "2026-01-29 11:30",
			durationStr: "",
			want:        time.Date(2026, 1, 29, 11, 30, 0, 0, time.Local),
		},
		{
			name:        "End time provided (12h format)",
			formatStr:   "12",
			startTime:   baseTime,
			endStr:      "2026-01-29 11:30 AM",
			durationStr: "",
			want:        time.Date(2026, 1, 29, 11, 30, 0, 0, time.Local),
		},
		{
			name:        "End time provided (12h format PM)",
			formatStr:   "12",
			startTime:   baseTime,
			endStr:      "2026-01-29 02:00 PM",
			durationStr: "",
			want:        time.Date(2026, 1, 29, 14, 0, 0, 0, time.Local),
		},
		{
			name:        "End time parsing error",
			formatStr:   "24",
			startTime:   baseTime,
			endStr:      "invalid-time",
			durationStr: "",
			wantErr:     "parse end time: invalid time format",
		},
		{
			name:        "End time before start time",
			formatStr:   "24",
			startTime:   baseTime,
			endStr:      "2026-01-29 09:00",
			durationStr: "",
			wantErr:     "end time cannot be before start time",
		},
		{
			name:        "Duration provided (minutes)",
			formatStr:   "24",
			startTime:   baseTime,
			endStr:      "",
			durationStr: "30m",
			want:        baseTime.Add(30 * time.Minute),
		},
		{
			name:        "Duration provided (hours)",
			formatStr:   "24",
			startTime:   baseTime,
			endStr:      "",
			durationStr: "1h30m",
			want:        baseTime.Add(90 * time.Minute),
		},
		{
			name:        "Duration parsing error",
			formatStr:   "24",
			startTime:   baseTime,
			endStr:      "",
			durationStr: "invalid",
			wantErr:     "parse duration: time: invalid duration",
		},
		{
			name:        "Both empty",
			formatStr:   "24",
			startTime:   baseTime,
			endStr:      "",
			durationStr: "",
			wantErr:     "end time or duration is required",
		},
		{
			name:        "End time (HH:MM only) - assumes today",
			formatStr:   "24",
			startTime:   time.Now().Add(-2 * time.Hour), // Ensure start is before "now"
			endStr:      time.Now().Format("15:04"),
			durationStr: "",
			want: time.Date(
				time.Now().Year(),
				time.Now().Month(),
				time.Now().Day(),
				time.Now().Hour(),
				time.Now().Minute(),
				0,
				0,
				time.Local,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tf := timeutil.NewFormatter(tt.formatStr)
			got, err := CalculateEndTime(tf, tt.startTime, tt.endStr, tt.durationStr)

			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				require.Equal(t, time.Time{}, got)
			} else {
				require.NoError(t, err)
				require.WithinDuration(t, tt.want, got, time.Second)
			}
		})
	}
}
