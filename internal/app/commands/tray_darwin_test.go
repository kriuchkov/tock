//go:build darwin

package commands

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCompactDuration(t *testing.T) {
	cases := map[string]struct {
		in   time.Duration
		want string
	}{
		"zero":            {0, "0m"},
		"seconds only":    {45 * time.Second, "0m"},
		"minutes":         {5*time.Minute + 30*time.Second, "5m"},
		"just under hour": {59 * time.Minute, "59m"},
		"one hour":        {time.Hour, "1:00"},
		"hour and change": {time.Hour + 5*time.Minute, "1:05"},
		"many hours":      {12*time.Hour + 34*time.Minute, "12:34"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, compactDuration(tc.in))
		})
	}
}
