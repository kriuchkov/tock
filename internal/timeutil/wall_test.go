package timeutil

import (
	"testing"
	"time"
)

func TestWallTime(t *testing.T) {
	tests := []struct {
		zone   string
		at     time.Time
		hour   int
		minute int
		want   string
	}{
		{
			zone:   "Europe/Berlin",
			at:     time.Date(2027, time.March, 28, 0, 0, 0, 0, time.UTC),
			hour:   2,
			minute: 30,
			want:   "2027-03-28T03:30:00+02:00",
		},
		{
			zone:   "America/New_York",
			at:     time.Date(2027, time.March, 14, 0, 0, 0, 0, time.UTC),
			hour:   2,
			minute: 30,
			want:   "2027-03-14T03:30:00-04:00",
		},
		// Clocks jump from 00:00 to 01:00, so the gap starts at midnight.
		{
			zone:   "America/Santiago",
			at:     time.Date(2026, time.September, 6, 0, 0, 0, 0, time.UTC),
			hour:   0,
			minute: 30,
			want:   "2026-09-06T01:30:00-03:00",
		},
		{
			zone:   "America/New_York",
			at:     time.Date(2027, time.March, 14, 0, 0, 0, 0, time.UTC),
			hour:   3,
			minute: 0,
			want:   "2027-03-14T03:00:00-04:00",
		},
		{
			zone:   "Europe/Berlin",
			at:     time.Date(2026, time.November, 18, 0, 0, 0, 0, time.UTC),
			hour:   17,
			minute: 30,
			want:   "2026-11-18T17:30:00+01:00",
		},
	}
	for _, tt := range tests {
		t.Run(tt.zone+" "+tt.want, func(t *testing.T) {
			loc, err := time.LoadLocation(tt.zone)
			if err != nil {
				t.Skipf("timezone data unavailable: %v", err)
			}
			got := WallTime(tt.at.Year(), tt.at.Month(), tt.at.Day(), tt.hour, tt.minute, loc)
			if got.Format(time.RFC3339) != tt.want {
				t.Errorf("WallTime() = %s, want %s", got.Format(time.RFC3339), tt.want)
			}
		})
	}
}
