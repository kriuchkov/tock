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
