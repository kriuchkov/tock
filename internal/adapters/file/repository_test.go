package file_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/adapters/file"
	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
)

func TestRepository_Find(t *testing.T) {
	// Setup temporary file
	f, createErr := os.CreateTemp(t.TempDir(), "tock_test_*.txt")
	require.NoError(t, createErr)
	defer os.Remove(f.Name())

	// Write some data
	content := `2023-10-26 10:00 - 2023-10-26 11:00 | ProjectA | Task 1
2023-10-26 11:00 - 2023-10-26 12:00 | ProjectB | Task 2
2023-10-27 09:00 | ProjectA | Task 3
`
	_, createErr = f.WriteString(content)
	require.NoError(t, createErr)
	f.Close()

	repo := file.NewRepository(f.Name())

	t3, _ := time.ParseInLocation("2006-01-02 15:04", "2023-10-26 12:00", time.Local)
	t4, _ := time.ParseInLocation("2006-01-02 15:04", "2023-10-27 09:00", time.Local)

	projectA := "ProjectA"
	isRunning := true
	isNotRunning := false

	tests := []struct {
		name    string
		filter  dto.ActivityFilter
		wantLen int
		want    []models.Activity
	}{
		{
			name:    "All",
			filter:  dto.ActivityFilter{},
			wantLen: 3,
		},
		{
			name: "Filter by Project",
			filter: dto.ActivityFilter{
				Project: &projectA,
			},
			wantLen: 2,
		},
		{
			name: "Filter IsRunning",
			filter: dto.ActivityFilter{
				IsRunning: &isRunning,
			},
			wantLen: 1,
			want: []models.Activity{
				{StartTime: t4, Project: "ProjectA", Description: "Task 3"},
			},
		},
		{
			name: "Filter IsNotRunning",
			filter: dto.ActivityFilter{
				IsRunning: &isNotRunning,
			},
			wantLen: 2,
		},
		{
			name: "Filter FromDate",
			filter: dto.ActivityFilter{
				FromDate: &t3, // 12:00
			},
			wantLen: 1, // Only the last one (starts next day)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := repo.Find(context.Background(), tt.filter)
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
			if tt.want != nil {
				// Check specific fields if needed, simplified here
				for i, w := range tt.want {
					assert.Equal(t, w.Project, got[i].Project)
					assert.Equal(t, w.Description, got[i].Description)
					assert.True(t, w.StartTime.Equal(got[i].StartTime))
				}
			}
		})
	}
}

func TestRepository_FindLast(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tock_test_last_*.txt")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	repo := file.NewRepository(f.Name())

	// Empty file
	_, err = repo.FindLast(context.Background())
	require.Error(t, err)

	// Write one line
	_, err = f.WriteString("2023-10-27 09:00 | ProjectA | Task 3\n")
	require.NoError(t, err)

	last, err := repo.FindLast(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Task 3", last.Description)
}

func TestRepository_Save(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tock_test_save_*.txt")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	f.Close()

	repo := file.NewRepository(f.Name())

	t1, _ := time.ParseInLocation("2006-01-02 15:04", "2023-10-27 09:00", time.Local)

	// 1. Save new activity (Append)
	act1 := models.Activity{
		StartTime:   t1,
		Project:     "ProjectA",
		Description: "Task 1",
	}
	err = repo.Save(context.Background(), act1)
	require.NoError(t, err)

	last, err := repo.FindLast(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Task 1", last.Description)
	assert.Nil(t, last.EndTime)

	// 2. Update existing activity (Stop it)
	t2, _ := time.ParseInLocation("2006-01-02 15:04", "2023-10-27 10:00", time.Local)
	act1.EndTime = &t2

	err = repo.Save(context.Background(), act1)
	require.NoError(t, err)

	last, err = repo.FindLast(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, last.EndTime)
	assert.True(t, t2.Equal(*last.EndTime))

	// Verify file content has only one line
	content, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	lines := 0
	for _, c := range content {
		if c == '\n' {
			lines++
		}
	}
	// Depending on implementation, might have trailing newline
	// "Start - End | ... \n" -> 1 line
	assert.LessOrEqual(t, lines, 2)

	// 3. Append another activity
	t3, _ := time.ParseInLocation("2006-01-02 15:04", "2023-10-27 11:00", time.Local)
	act2 := models.Activity{
		StartTime:   t3,
		Project:     "ProjectB",
		Description: "Task 2",
	}
	err = repo.Save(context.Background(), act2)
	require.NoError(t, err)

	activities, err := repo.Find(context.Background(), dto.ActivityFilter{})
	require.NoError(t, err)
	assert.Len(t, activities, 2)
}

func TestRepository_FindLast_WithAddedHistoricalActivity(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tock_test_last_hist_*.txt")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	f.Close()

	repo := file.NewRepository(f.Name())

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)

	// 1. Start a task at 10:08
	task1 := models.Activity{
		Project:     "test",
		Description: "test task",
		StartTime:   today.Add(10*time.Hour + 8*time.Minute),
		EndTime:     ptr(today.Add(11*time.Hour + 27*time.Minute)),
	}
	require.NoError(t, repo.Save(context.Background(), task1))

	// 2. Start another task at 11:51 (currently running)
	task2 := models.Activity{
		Project:     "test",
		Description: "test task",
		StartTime:   today.Add(11*time.Hour + 51*time.Minute),
		EndTime:     nil, // Running
	}
	require.NoError(t, repo.Save(context.Background(), task2))

	// 3. Add historical activity at 00:00-00:10 (this gets appended to file)
	task3 := models.Activity{
		Project:     "test",
		Description: "test task",
		StartTime:   today.Add(0*time.Hour + 0*time.Minute),
		EndTime:     ptr(today.Add(0*time.Hour + 10*time.Minute)),
	}
	require.NoError(t, repo.Save(context.Background(), task3))

	// FindLast should return task2 (started at 11:51), not task3 (which is last in file)
	got, err := repo.FindLast(context.Background())
	require.NoError(t, err)

	// We compare start times because other fields might be slightly different due to formatting/parsing
	// But start time is the key identifier here
	assert.True(t, task2.StartTime.Equal(got.StartTime), "Expected last activity to be the one started at 11:51, but got %v", got.StartTime)
	assert.Nil(t, got.EndTime, "Last activity should be the running one")
}

func TestRepository_Save_UpdateMiddle(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "tock_test_save_mid_*.txt")
	require.NoError(t, err)
	defer os.Remove(f.Name())
	f.Close()

	repo := file.NewRepository(f.Name())
	ctx := context.Background()

	now := time.Now().Truncate(time.Minute)
	// 1. Add Activity A (Running)
	actA := models.Activity{
		Project:     "A",
		Description: "Task A",
		StartTime:   now.Add(-2 * time.Hour),
	}
	require.NoError(t, repo.Save(ctx, actA))

	// 2. Add Activity B (Completed, added later but historically earlier or just appended)
	// Let's just append another activity B
	actB := models.Activity{
		Project:     "B",
		Description: "Task B",
		StartTime:   now.Add(-1 * time.Hour),
		EndTime:     ptr(now),
	}
	require.NoError(t, repo.Save(ctx, actB))

	// File content order: A, B
	// Now we want to stop A. A is NOT the last line.

	endTime := now.Add(-1 * time.Hour) // Stopped before B started, for example
	actA.EndTime = &endTime

	require.NoError(t, repo.Save(ctx, actA))

	// Verify A is updated
	activities, err := repo.Find(ctx, dto.ActivityFilter{})
	require.NoError(t, err)
	require.Len(t, activities, 2)

	// Find A
	var foundA *models.Activity
	for _, a := range activities {
		if a.Project == "A" {
			foundA = &a
			break
		}
	}
	require.NotNil(t, foundA)
	require.NotNil(t, foundA.EndTime)
	// Compare using Equal but ensure we are comparing apples to apples (both truncated to minute effectively)
	assert.True(t, endTime.Equal(*foundA.EndTime), "Expected %v, got %v", endTime, *foundA.EndTime)
}

func ptr[T any](v T) *T {
	return &v
}
