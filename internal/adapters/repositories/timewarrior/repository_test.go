package timewarrior

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestRepository_Save(t *testing.T) {
	tests := []struct {
		name     string
		initial  []models.Activity
		activity models.Activity
		wantFile string // Expected filename (YYYY-MM.data)
		want     string // Expected content in file
	}{
		{
			name: "save new activity",
			activity: models.Activity{
				Project:     "ProjectA",
				Description: "Task 1",
				StartTime:   time.Date(2023, 10, 1, 10, 0, 0, 0, time.UTC),
			},
			wantFile: "2023-10.data",
			want:     `{"start":"20231001T100000Z","tags":["ProjectA"],"annotation":"Task 1"}`,
		},
		{
			name: "save activity with end time",
			activity: models.Activity{
				Project:     "ProjectB",
				Description: "Task 2",
				StartTime:   time.Date(2023, 10, 1, 12, 0, 0, 0, time.UTC),
				EndTime:     ptr(time.Date(2023, 10, 1, 13, 0, 0, 0, time.UTC)),
			},
			wantFile: "2023-10.data",
			want:     `{"start":"20231001T120000Z","end":"20231001T130000Z","tags":["ProjectB"],"annotation":"Task 2"}`,
		},
		{
			name: "update existing activity",
			initial: []models.Activity{
				{
					Project:     "ProjectC",
					Description: "Task 3",
					StartTime:   time.Date(2023, 11, 1, 9, 0, 0, 0, time.UTC),
				},
			},
			activity: models.Activity{
				Project:     "ProjectC",
				Description: "Task 3",
				StartTime:   time.Date(2023, 11, 1, 9, 0, 0, 0, time.UTC),
				EndTime:     ptr(time.Date(2023, 11, 1, 10, 0, 0, 0, time.UTC)),
			},
			wantFile: "2023-11.data",
			want:     `{"start":"20231101T090000Z","end":"20231101T100000Z","tags":["ProjectC"],"annotation":"Task 3"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			repo := NewRepository(tmpDir)
			ctx := context.Background()

			// Setup initial state
			for _, act := range tt.initial {
				require.NoError(t, repo.Save(ctx, act))
			}

			// Execute
			err := repo.Save(ctx, tt.activity)
			require.NoError(t, err)

			// Verify
			content, err := os.ReadFile(filepath.Join(tmpDir, tt.wantFile))
			require.NoError(t, err)
			assert.Contains(t, string(content), tt.want)
		})
	}
}

func TestRepository_Find(t *testing.T) {
	baseTime := time.Date(2023, 10, 15, 12, 0, 0, 0, time.UTC)
	activities := []models.Activity{
		{
			Project:     "Work",
			Description: "Meeting",
			StartTime:   baseTime.Add(-24 * time.Hour), // Oct 14
			EndTime:     ptr(baseTime.Add(-23 * time.Hour)),
		},
		{
			Project:     "Personal",
			Description: "Gym",
			StartTime:   baseTime, // Oct 15
			EndTime:     ptr(baseTime.Add(1 * time.Hour)),
		},
		{
			Project:     "Work",
			Description: "Coding",
			StartTime:   baseTime.Add(24 * time.Hour), // Oct 16
			EndTime:     nil,                          // Running
		},
	}

	tests := []struct {
		name      string
		filter    dto.ActivityFilter
		wantCount int
	}{
		{
			name:      "all activities",
			filter:    dto.ActivityFilter{},
			wantCount: 3,
		},
		{
			name: "filter by project Work",
			filter: dto.ActivityFilter{
				Project: ptr("Work"),
			},
			wantCount: 2,
		},
		{
			name: "filter by date range (Oct 15 only)",
			filter: dto.ActivityFilter{
				FromDate: ptr(time.Date(2023, 10, 15, 0, 0, 0, 0, time.UTC)),
				ToDate:   ptr(time.Date(2023, 10, 15, 23, 59, 59, 0, time.UTC)),
			},
			wantCount: 1,
		},
		{
			name: "filter running",
			filter: dto.ActivityFilter{
				IsRunning: ptr(true),
			},
			wantCount: 1,
		},
		{
			name: "filter stopped",
			filter: dto.ActivityFilter{
				IsRunning: ptr(false),
			},
			wantCount: 2,
		},
		{
			name: "filter by description",
			filter: dto.ActivityFilter{
				Description: ptr("Meeting"),
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			repo := NewRepository(tmpDir)
			ctx := context.Background()

			for _, act := range activities {
				require.NoError(t, repo.Save(ctx, act))
			}

			got, err := repo.Find(ctx, tt.filter)
			require.NoError(t, err)
			assert.Len(t, got, tt.wantCount)
		})
	}
}

func TestRepository_FindLast(t *testing.T) {
	t.Run("find last activity", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo := NewRepository(tmpDir)
		ctx := context.Background()

		// Add some activities in past months
		past := models.Activity{
			Project:   "Old",
			StartTime: time.Now().AddDate(0, -2, 0),
			EndTime:   ptr(time.Now().AddDate(0, -2, 0).Add(time.Hour)),
		}
		require.NoError(t, repo.Save(ctx, past))

		// Add recent activity
		recent := models.Activity{
			Project:   "Recent",
			StartTime: time.Now().Add(-time.Hour),
		}
		require.NoError(t, repo.Save(ctx, recent))

		got, err := repo.FindLast(ctx)
		require.NoError(t, err)
		assert.Equal(t, recent.Project, got.Project)
	})

	t.Run("not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo := NewRepository(tmpDir)
		ctx := context.Background()

		_, err := repo.FindLast(ctx)
		assert.Error(t, err)
	})
}

func TestRepository_FindLast_WithAddedHistoricalActivity(t *testing.T) {
	// This test reproduces the bug where adding a historical activity with early time (00:00-00:10)
	// makes it the last activity in the file, breaking the stop command
	t.Run("find last by start time not file position", func(t *testing.T) {
		tmpDir := t.TempDir()
		repo := NewRepository(tmpDir)
		ctx := context.Background()

		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

		// Simulate the scenario:
		// 1. Start a task at 10:08
		task1 := models.Activity{
			Project:     "test",
			Description: "test task",
			StartTime:   today.Add(10*time.Hour + 8*time.Minute),
			EndTime:     ptr(today.Add(11*time.Hour + 27*time.Minute)),
		}
		require.NoError(t, repo.Save(ctx, task1))

		// 2. Start another task at 11:51 (currently running)
		task2 := models.Activity{
			Project:     "test",
			Description: "test task",
			StartTime:   today.Add(11*time.Hour + 51*time.Minute),
			EndTime:     nil, // Running
		}
		require.NoError(t, repo.Save(ctx, task2))

		// 3. Add historical activity at 00:00-00:10 (this gets sorted and becomes last in file)
		task3 := models.Activity{
			Project:     "test",
			Description: "test task",
			StartTime:   today.Add(0*time.Hour + 0*time.Minute),
			EndTime:     ptr(today.Add(0*time.Hour + 10*time.Minute)),
		}
		require.NoError(t, repo.Save(ctx, task3))

		// FindLast should return task2 (started at 11:51), not task3 (which is last in file)
		got, err := repo.FindLast(ctx)
		require.NoError(t, err)
		assert.Equal(t, task2.StartTime, got.StartTime)
		assert.Nil(t, got.EndTime, "Last activity should be the running one")
	})
}

func TestRepository_Find_IsRunning_WithHistoricalData(t *testing.T) {
	// Verify that Find with IsRunning=true works correctly even when historical data is added
	// This supports the fix in service.Stop()
	tmpDir := t.TempDir()
	repo := NewRepository(tmpDir)
	ctx := context.Background()

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	// 1. Start a task (Running)
	runningTask := models.Activity{
		Project:     "running",
		Description: "running task",
		StartTime:   today.Add(12 * time.Hour),
		EndTime:     nil,
	}
	require.NoError(t, repo.Save(ctx, runningTask))

	// 2. Add historical task (Completed, earlier)
	historicalTask := models.Activity{
		Project:     "historical",
		Description: "historical task",
		StartTime:   today.Add(9 * time.Hour),
		EndTime:     ptr(today.Add(10 * time.Hour)),
	}
	require.NoError(t, repo.Save(ctx, historicalTask))

	// 3. Find running
	isRunning := true
	results, err := repo.Find(ctx, dto.ActivityFilter{IsRunning: &isRunning})
	require.NoError(t, err)

	require.Len(t, results, 1)
	assert.Equal(t, runningTask.StartTime, results[0].StartTime)
	assert.Equal(t, runningTask.Project, results[0].Project)
}

func ptr[T any](v T) *T {
	return &v
}

func TestRepository_ReadIncFormat(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewRepository(tmpDir)

	// Create a data file with legacy "inc" lines
	// Format: inc <start> [ - <end> ] [ # <tag1> <tag2> ... ] [ # <annotation> ]
	content := `inc 20230101T100000Z - 20230101T110000Z # ProjectA # Task 1
inc 20230101T120000Z # ProjectB # Task 2
`
	// 2023-01.data
	dataPath := filepath.Join(tmpDir, "2023-01.data")
	err := os.WriteFile(dataPath, []byte(content), 0600)
	require.NoError(t, err)

	// Use Find to retrieve
	ctx := context.Background()
	start := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, 1, 1, 23, 59, 59, 0, time.UTC)

	acts, err := repo.Find(ctx, dto.ActivityFilter{
		FromDate: &start,
		ToDate:   &end,
	})
	require.NoError(t, err)
	require.Len(t, acts, 2)

	// Verify Task 1 (Completed)
	assert.Equal(t, "Task 1", acts[0].Description)
	assert.Equal(t, "ProjectA", acts[0].Project)
	assert.NotNil(t, acts[0].EndTime)

	// Verify Task 2 (Running)
	assert.Equal(t, "Task 2", acts[1].Description)
	assert.Equal(t, "ProjectB", acts[1].Project)
	assert.Nil(t, acts[1].EndTime)
}

func TestRepository_Find_CrossMonth(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewRepository(tmpDir)
	ctx := context.Background()

	// Activity in October
	actOct := models.Activity{
		Project:   "OctProject",
		StartTime: time.Date(2023, 10, 31, 23, 0, 0, 0, time.UTC),
		EndTime:   ptr(time.Date(2023, 10, 31, 23, 30, 0, 0, time.UTC)),
	}
	require.NoError(t, repo.Save(ctx, actOct))

	// Activity in November
	actNov := models.Activity{
		Project:   "NovProject",
		StartTime: time.Date(2023, 11, 1, 0, 30, 0, 0, time.UTC),
		EndTime:   ptr(time.Date(2023, 11, 1, 1, 0, 0, 0, time.UTC)),
	}
	require.NoError(t, repo.Save(ctx, actNov))

	// Find covering both months
	start := time.Date(2023, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2023, 11, 30, 23, 59, 59, 0, time.UTC)

	acts, err := repo.Find(ctx, dto.ActivityFilter{
		FromDate: &start,
		ToDate:   &end,
	})
	require.NoError(t, err)
	require.Len(t, acts, 2)
}

func TestRepository_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	repo := NewRepository(tmpDir)
	ctx := context.Background()

	now := time.Now().UTC()
	actA := models.Activity{
		Project:     "A",
		Description: "Task A",
		StartTime:   now.Add(-2 * time.Hour),
		EndTime:     ptr(now.Add(-1 * time.Hour)),
	}
	require.NoError(t, repo.Save(ctx, actA))

	actB := models.Activity{
		Project:     "B",
		Description: "Task B",
		StartTime:   now.Add(-1 * time.Hour),
	}
	require.NoError(t, repo.Save(ctx, actB))

	// Verify both exist
	activities, err := repo.Find(ctx, dto.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, activities, 2)

	// Remove A
	require.NoError(t, repo.Remove(ctx, actA))

	// Verify only B remains
	activities, err = repo.Find(ctx, dto.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, activities, 1)
	assert.Equal(t, "B", activities[0].Project)

	// Try removing A again (should fail)
	err = repo.Remove(ctx, actA)
	require.Error(t, err)
}
