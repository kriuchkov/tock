package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kriuchkov/tock/internal/core/models"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "zero", d: 0, want: "00:00:00"},
		{name: "under an hour", d: 45 * time.Minute, want: "00:45:00"},
		{name: "over a day", d: 25*time.Hour + 5*time.Minute, want: "25:05:00"},
		{name: "rounds sub-second remainder", d: 90*time.Second + 700*time.Millisecond, want: "00:01:31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, models.FormatDuration(tt.d))
		})
	}
}
