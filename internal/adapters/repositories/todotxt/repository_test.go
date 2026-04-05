package todotxt_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/repositories/todotxt"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/timeutil"
)

func TestRepository_SaveAndFind(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tock_todotxt_*.txt")
	require.NoError(t, err)
	f.Close()

	repo := todotxt.NewRepository(f.Name())
	ctx := context.Background()

	start1 := time.Date(2026, 3, 16, 10, 15, 0, 0, time.Local)
	end1 := start1.Add(90 * time.Minute)
	activity1 := models.Activity{
		Project:     "Client Work",
		Description: "Deep work session",
		StartTime:   start1,
		EndTime:     &end1,
		Tags:        []string{"desk", "focus"},
	}

	start2 := time.Date(2026, 3, 17, 9, 0, 0, 0, time.Local)
	activity2 := models.Activity{
		Project:     "Ops",
		Description: "Incident triage",
		StartTime:   start2,
	}

	require.NoError(t, repo.Save(ctx, activity1))
	require.NoError(t, repo.Save(ctx, activity2))

	got, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, activity1.Description, got[0].Description)
	assert.Equal(t, activity1.Project, got[0].Project)
	assert.Equal(t, activity2.Description, got[1].Description)
	assert.Nil(t, got[1].EndTime)

	last, err := repo.FindLast(ctx)
	require.NoError(t, err)
	assert.Equal(t, activity2.StartTime, last.StartTime)
	assert.Equal(t, activity2.Description, last.Description)
}

func TestRepository_SaveUpdatesExistingActivity(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tock_todotxt_update_*.txt")
	require.NoError(t, err)
	f.Close()

	repo := todotxt.NewRepository(f.Name())
	ctx := context.Background()

	start := time.Date(2026, 3, 16, 10, 15, 0, 0, time.Local)
	activity := models.Activity{
		Project:     "Client Work",
		Description: "Deep work session",
		StartTime:   start,
	}
	require.NoError(t, repo.Save(ctx, activity))

	end := start.Add(2 * time.Hour)
	activity.EndTime = &end
	require.NoError(t, repo.Save(ctx, activity))

	got, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].EndTime)
	assert.Equal(t, end, *got[0].EndTime)
}

func TestRepository_Find_DateRangeIncludesOverlappingActivity(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tock_todotxt_overlap_*.txt")
	require.NoError(t, err)
	f.Close()

	repo := todotxt.NewRepository(f.Name())
	ctx := context.Background()

	start := time.Date(2026, 3, 4, 22, 25, 0, 0, time.Local)
	end := time.Date(2026, 3, 5, 1, 21, 0, 0, time.Local)
	require.NoError(t, repo.Save(ctx, models.Activity{
		Project:     "ProjectA",
		Description: "Cross-day activity",
		StartTime:   start,
		EndTime:     &end,
	}))

	from := time.Date(2026, 3, 5, 0, 0, 0, 0, time.Local)
	_, to := timeutil.LocalDayBounds(from)
	got, err := repo.Find(ctx, models.ActivityFilter{FromDate: &from, ToDate: &to})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Cross-day activity", got[0].Description)
}

func TestRepository_Remove(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tock_todotxt_remove_*.txt")
	require.NoError(t, err)
	f.Close()

	repo := todotxt.NewRepository(f.Name())
	ctx := context.Background()

	start := time.Date(2026, 3, 16, 10, 15, 0, 0, time.Local)
	activity := models.Activity{
		Project:     "Client Work",
		Description: "Deep work session",
		StartTime:   start,
	}
	require.NoError(t, repo.Save(ctx, activity))
	require.NoError(t, repo.Remove(ctx, activity))

	got, err := repo.Find(ctx, models.ActivityFilter{})
	require.NoError(t, err)
	assert.Empty(t, got)
}
