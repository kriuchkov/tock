package file_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/file"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestParseActivity(t *testing.T) {
	localTime := func(year int, month time.Month, day, hour, minute, sec int) time.Time {
		return time.Date(year, month, day, hour, minute, sec, 0, time.Local)
	}

	tests := []struct {
		name      string
		line      string
		want      *models.Activity
		wantErr   bool
		errTarget error
	}{
		{
			name: "valid activity with start and end time",
			line: "2023-10-27 10:00 - 2023-10-27 11:00 | Project A | Working on feature X",
			want: &models.Activity{
				StartTime:   localTime(2023, 10, 27, 10, 0, 0),
				EndTime:     func() *time.Time { t := localTime(2023, 10, 27, 11, 0, 0); return &t }(),
				Project:     "Project A",
				Description: "Working on feature X",
			},
			wantErr: false,
		},
		{
			name: "valid activity with start time only",
			line: "2023-10-27 12:00 | Project B | Meeting",
			want: &models.Activity{
				StartTime:   localTime(2023, 10, 27, 12, 0, 0),
				EndTime:     nil,
				Project:     "Project B",
				Description: "Meeting",
			},
			wantErr: false,
		},
		{
			name:      "invalid line format (not enough parts)",
			line:      "2023-10-27 10:00 | Project A",
			want:      nil,
			wantErr:   true,
			errTarget: file.ErrSkip,
		},
		{
			name:    "invalid start time format",
			line:    "invalid-time | Project A | Description",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid end time format",
			line:    "2023-10-27 10:00 - invalid-time | Project A | Description",
			want:    nil,
			wantErr: true,
		},
		{
			name: "valid activity with seconds",
			line: "2023-10-27 10:00:30 | Project C | Seconds test",
			want: &models.Activity{
				StartTime:   localTime(2023, 10, 27, 10, 0, 30),
				EndTime:     nil,
				Project:     "Project C",
				Description: "Seconds test",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := file.ParseActivity(tt.line)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errTarget != nil {
					require.ErrorIs(t, err, tt.errTarget)
				}
				return
			}
			if assert.NoError(t, err) {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestFormatActivity(t *testing.T) {
	localTime := func(year int, month time.Month, day, hour, minute, sec int) time.Time {
		return time.Date(year, month, day, hour, minute, sec, 0, time.Local)
	}

	tests := []struct {
		name     string
		activity models.Activity
		want     string
	}{
		{
			name: "activity with start and end time",
			activity: models.Activity{
				StartTime:   localTime(2023, 10, 27, 10, 0, 0),
				EndTime:     func() *time.Time { t := localTime(2023, 10, 27, 11, 0, 0); return &t }(),
				Project:     "Project A",
				Description: "Description",
			},
			want: "2023-10-27 10:00 - 2023-10-27 11:00 | Project A | Description",
		},
		{
			name: "activity with start time only",
			activity: models.Activity{
				StartTime:   localTime(2023, 10, 27, 12, 0, 0),
				EndTime:     nil,
				Project:     "Project B",
				Description: "Meeting",
			},
			want: "2023-10-27 12:00 | Project B | Meeting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := file.FormatActivity(tt.activity)
			assert.Equal(t, tt.want, got)
		})
	}
}
