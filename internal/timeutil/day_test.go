package timeutil_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestParseDay(t *testing.T) {
	now := time.Date(2026, time.November, 18, 15, 4, 0, 0, time.Local) // a Wednesday
	date := func(month time.Month, d int) time.Time { return time.Date(2026, month, d, 0, 0, 0, 0, time.Local) }

	tests := []struct {
		input string
		want  time.Time
	}{
		{"", date(time.November, 18)},
		{"today", date(time.November, 18)},
		{" Tomorrow ", date(time.November, 19)},
		{"wed", date(time.November, 18)},
		{"thursday", date(time.November, 19)},
		{"mon", date(time.November, 23)},
		{"2026-12-31", date(time.December, 31)},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := timeutil.ParseDay(tt.input, now)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	for _, input := range []string{"someday", "2026-13-01", "31.12.2026"} {
		t.Run(input, func(t *testing.T) {
			_, err := timeutil.ParseDay(input, now)
			assert.Error(t, err)
		})
	}
}

func TestParseWeekday(t *testing.T) {
	valid := []struct {
		input string
		want  time.Weekday
	}{
		{"mo", time.Monday}, {"mon", time.Monday}, {"Tue", time.Tuesday}, {"sunday", time.Sunday}, {" fri ", time.Friday},
	}
	for _, tt := range valid {
		got, ok := timeutil.ParseWeekday(tt.input)
		assert.True(t, ok, tt.input)
		assert.Equal(t, tt.want, got, tt.input)
	}
	for _, input := range []string{"m", "mond", "funday"} {
		_, ok := timeutil.ParseWeekday(input)
		assert.False(t, ok, input)
	}
}
