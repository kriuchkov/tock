package todotxt_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/todotxt"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestParseActivity(t *testing.T) {
	localTime := func(year int, month time.Month, day, hour, minute, second int) time.Time {
		return time.Date(year, month, day, hour, minute, second, 0, time.Local)
	}

	tests := []struct {
		name      string
		line      string
		want      *models.Activity
		wantErr   bool
		errTarget error
	}{
		{
			name: "valid tock extensions round trip line",
			line: "x 2026-03-16 2026-03-16 Deep work +focus @desk tock_start:2026-03-16T10:15:00Z tock_end:2026-03-16T11:45:00Z tock_project:Client+Work tock_desc:Deep+work+session tock_tags:desk%1Ffocus",
			want: &models.Activity{
				StartTime:   time.Date(2026, 3, 16, 10, 15, 0, 0, time.UTC).Local(),
				EndTime:     new(time.Date(2026, 3, 16, 11, 45, 0, 0, time.UTC).Local()),
				Project:     "Client Work",
				Description: "Deep work session",
				Tags:        []string{"desk", "focus"},
			},
		},
		{
			name: "valid plain todotxt completed task fallback",
			line: "x 2026-03-16 2026-03-15 Review PR +Work @github",
			want: &models.Activity{
				StartTime:   localTime(2026, 3, 15, 0, 0, 0),
				EndTime:     new(localTime(2026, 3, 16, 23, 59, 59)),
				Project:     "Work",
				Description: "Review PR",
				Tags:        []string{"github"},
			},
		},
		{
			name:      "running task without exact time is skipped",
			line:      "(A) Call Mom +Family @phone",
			wantErr:   true,
			errTarget: todotxt.ErrSkip,
		},
		{
			name: "running task with creation date uses date fallback",
			line: "2026-03-15 Plan sprint +Work @desk",
			want: &models.Activity{
				StartTime:   localTime(2026, 3, 15, 0, 0, 0),
				Project:     "Work",
				Description: "Plan sprint",
				Tags:        []string{"desk"},
			},
		},
		{
			name: "priority token is ignored for plain todotxt line",
			line: "(A) 2026-03-15 Plan sprint +Work @desk",
			want: &models.Activity{
				StartTime:   localTime(2026, 3, 15, 0, 0, 0),
				Project:     "Work",
				Description: "Plan sprint",
				Tags:        []string{"desk"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := todotxt.ParseActivity(tt.line)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errTarget != nil {
					require.ErrorIs(t, err, tt.errTarget)
				}
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFormatActivity(t *testing.T) {
	start := time.Date(2026, 3, 16, 10, 15, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	end := start.Add(90 * time.Minute)

	formatted := todotxt.FormatActivity(models.Activity{
		Description: "Deep work session",
		Project:     "Client Work",
		StartTime:   start,
		EndTime:     &end,
		Tags:        []string{"desk", "focus"},
	})

	assert.Contains(t, formatted, "x 2026-03-16 2026-03-16")
	assert.Contains(t, formatted, "tock_start:2026-03-16T10:15:00+02:00")
	assert.Contains(t, formatted, "tock_end:2026-03-16T11:45:00+02:00")
	assert.Contains(t, formatted, "tock_project:Client+Work")
	assert.Contains(t, formatted, "tock_desc:Deep+work+session")
	assert.Contains(t, formatted, "tock_tags:desk%1Ffocus")
}
