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

func ptr[T any](v T) *T {
	return &v
}
