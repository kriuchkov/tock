package models_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestBuildActivityFilter(t *testing.T) {
	now := time.Date(2026, time.March, 15, 14, 30, 0, 0, time.Local)

	t.Run("builds yesterday filter", func(t *testing.T) {
		filter, err := models.BuildActivityFilter(models.ActivityFilterOptions{
			Now:         now,
			Yesterday:   true,
			Project:     "tock",
			Description: "refactor",
		})
		require.NoError(t, err)

		expectedTo, _ := timeutil.LocalDayBounds(now)
		expectedFrom := expectedTo.AddDate(0, 0, -1)

		require.NotNil(t, filter.FromDate)
		require.NotNil(t, filter.ToDate)
		assert.Equal(t, expectedFrom, *filter.FromDate)
		assert.Equal(t, expectedTo, *filter.ToDate)
		require.NotNil(t, filter.Project)
		require.NotNil(t, filter.Description)
		assert.Equal(t, "tock", *filter.Project)
		assert.Equal(t, "refactor", *filter.Description)
	})

	t.Run("rejects invalid date", func(t *testing.T) {
		_, err := models.BuildActivityFilter(models.ActivityFilterOptions{Date: "15-03-2026"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid date format")
	})
}

func TestBuildActivityFilterDateRange(t *testing.T) {
	tests := []struct {
		name      string
		opts      models.ActivityFilterOptions
		wantFrom  *time.Time
		wantTo    *time.Time
		wantError string
	}{
		{
			name:     "builds inclusive range",
			opts:     models.ActivityFilterOptions{From: "2026-04-01", To: "2026-04-15"},
			wantFrom: datePointer(1),
			wantTo:   datePointer(16),
		},
		{
			name:     "builds open-ended range from date",
			opts:     models.ActivityFilterOptions{From: "2026-04-01"},
			wantFrom: datePointer(1),
		},
		{
			name:   "builds open-ended range through date",
			opts:   models.ActivityFilterOptions{To: "2026-04-15"},
			wantTo: datePointer(16),
		},
		{
			name:      "rejects conflicting date filters",
			opts:      models.ActivityFilterOptions{Today: true, From: "2026-04-01"},
			wantError: "cannot specify multiple date filters",
		},
		{
			name:      "rejects invalid from date",
			opts:      models.ActivityFilterOptions{From: "not-a-date"},
			wantError: "invalid --from date format",
		},
		{
			name:      "rejects invalid to date",
			opts:      models.ActivityFilterOptions{To: "2026-13-01"},
			wantError: "invalid --to date format",
		},
		{
			name:      "rejects reversed range",
			opts:      models.ActivityFilterOptions{From: "2026-04-16", To: "2026-04-15"},
			wantError: "--from date must not be after --to date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := models.BuildActivityFilter(tt.opts)
			if tt.wantError != "" {
				require.ErrorContains(t, err, tt.wantError)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantFrom, filter.FromDate)
			assert.Equal(t, tt.wantTo, filter.ToDate)
		})
	}
}

func datePointer(day int) *time.Time {
	date := time.Date(2026, time.April, day, 0, 0, 0, 0, time.Local)
	return &date
}
