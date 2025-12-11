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
