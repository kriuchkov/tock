package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/doug-martin/goqu/v9/dialect/sqlite3" // register the goqu sqlite3 dialect
	_ "github.com/mattn/go-sqlite3"                    // register the sqlite3 database driver

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
)

func setupTestDB(t *testing.T) *ActivityRepository {
	t.Helper()
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_tock.db")

	repo, err := NewSQLiteActivityRepository(t.Context(), dbPath)
	require.NoError(t, err)
	return repo
}

func TestSQLiteRepository_Save(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	tests := []struct {
		name     string
		activity models.Activity
		setup    func(repo *ActivityRepository)
		verify   func(t *testing.T, repo *ActivityRepository)
	}{
		{
			name: "insert new running activity",
			activity: models.Activity{
				Description: "Fix bugs",
				Project:     "Tock",
				StartTime:   now,
				Tags:        []string{"bug", "golang"},
				Notes:       "Found a few issues in the repo",
			},
			verify: func(t *testing.T, repo *ActivityRepository) {
				last, err := repo.FindLast(ctx)
				require.NoError(t, err)
				require.NotNil(t, last)
				assert.Equal(t, "Fix bugs", last.Description)
				assert.Equal(t, "Tock", last.Project)
				assert.True(t, now.Equal(last.StartTime))
				assert.Nil(t, last.EndTime)
				assert.Equal(t, []string{"bug", "golang"}, last.Tags)
				assert.Equal(t, "Found a few issues in the repo", last.Notes)
			},
		},
		{
			name: "update existing activity",
			activity: models.Activity{
				Description: "Fix bugs (updated)",
				Project:     "Tock",
				StartTime:   now, // Same start time will trigger ON CONFLICT update
				EndTime:     new(now.Add(time.Hour)),
				Tags:        []string{"updated"},
				Notes:       "Updated notes",
			},
			setup: func(repo *ActivityRepository) {
				// Insert the initial record
				_ = repo.Save(ctx, models.Activity{
					Description: "Fix bugs",
					Project:     "Tock",
					StartTime:   now,
					Tags:        []string{"bug"},
				})
			},
			verify: func(t *testing.T, repo *ActivityRepository) {
				last, err := repo.FindLast(ctx)
				require.NoError(t, err)
				require.NotNil(t, last)
				assert.Equal(t, "Fix bugs (updated)", last.Description)
				assert.Equal(t, []string{"updated"}, last.Tags)
				require.NotNil(t, last.EndTime)
				assert.True(t, now.Add(time.Hour).Equal(*last.EndTime))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setupTestDB(t)

			if tt.setup != nil {
				tt.setup(repo)
			}

			err := repo.Save(ctx, tt.activity)
			require.NoError(t, err)

			if tt.verify != nil {
				tt.verify(t, repo)
			}
		})
	}
}

func TestSQLiteRepository_FindLast(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	tests := []struct {
		name         string
		setup        func(repo *ActivityRepository)
		expectNil    bool
		expectedDesc string
	}{
		{
			name:      "empty database",
			setup:     func(_ *ActivityRepository) {},
			expectNil: true,
		},
		{
			name: "multiple activities, returns the most recent one",
			setup: func(repo *ActivityRepository) {
				_ = repo.Save(ctx, models.Activity{Description: "Past", Project: "A", StartTime: now.Add(-2 * time.Hour)})
				_ = repo.Save(ctx, models.Activity{Description: "Present", Project: "B", StartTime: now})
				_ = repo.Save(ctx, models.Activity{Description: "Older", Project: "A", StartTime: now.Add(-4 * time.Hour)})
			},
			expectNil:    false,
			expectedDesc: "Present",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setupTestDB(t)
			tt.setup(repo)

			last, err := repo.FindLast(ctx)

			if tt.expectNil {
				require.ErrorIs(t, err, coreErrors.ErrActivityNotFound)
				assert.Nil(t, last)
			} else {
				require.NoError(t, err)
				require.NotNil(t, last)
				assert.Equal(t, tt.expectedDesc, last.Description)
			}
		})
	}
}

func TestSQLiteRepository_Find(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// We create a standard set of records for filtering out
	standardSetup := func(repo *ActivityRepository) {
		// Act 1: Tock project, ran 2 hours ago for 1 hour
		_ = repo.Save(ctx, models.Activity{
			Description: "Design architecture",
			Project:     "Tock",
			StartTime:   now.Add(-2 * time.Hour),
			EndTime:     new(now.Add(-1 * time.Hour)),
			Tags:        []string{"design"},
		})

		// Act 2: Tock project, running now
		_ = repo.Save(ctx, models.Activity{
			Description: "Implement DB",
			Project:     "Tock",
			StartTime:   now,
			Tags:        []string{"db"},
		})

		// Act 3: SideProject, ran yesterday
		_ = repo.Save(ctx, models.Activity{
			Description: "Initial commit",
			Project:     "SideProject",
			StartTime:   now.Add(-24 * time.Hour),
			EndTime:     new(now.Add(-23 * time.Hour)),
		})
	}

	tests := []struct {
		name          string
		filter        models.ActivityFilter
		expectedCount int
		verify        func(t *testing.T, acts []models.Activity)
	}{
		{
			name:          "no filter, returns all",
			filter:        models.ActivityFilter{},
			expectedCount: 3,
		},
		{
			name: "filter by project Tock",
			filter: models.ActivityFilter{
				Project: new("Tock"),
			},
			expectedCount: 2,
			verify: func(t *testing.T, acts []models.Activity) {
				for _, a := range acts {
					assert.Equal(t, "Tock", a.Project)
				}
			},
		},
		{
			name: "filter by partial description (Like clause)",
			filter: models.ActivityFilter{
				Description: new("DB"),
			},
			expectedCount: 1,
			verify: func(t *testing.T, acts []models.Activity) {
				assert.Equal(t, "Implement DB", acts[0].Description)
			},
		},
		{
			name: "filter by IsRunning (true)",
			filter: models.ActivityFilter{
				IsRunning: new(true),
			},
			expectedCount: 1,
			verify: func(t *testing.T, acts []models.Activity) {
				assert.Nil(t, acts[0].EndTime)
				assert.Equal(t, "Implement DB", acts[0].Description)
			},
		},
		{
			name: "filter by IsRunning (false)",
			filter: models.ActivityFilter{
				IsRunning: new(false),
			},
			expectedCount: 2,
			verify: func(t *testing.T, acts []models.Activity) {
				for _, a := range acts {
					assert.NotNil(t, a.EndTime)
				}
			},
		},
		{
			name: "filter by FromDate",
			filter: models.ActivityFilter{
				FromDate: new(now.Add(-3 * time.Hour)),
			},
			expectedCount: 2, // skips the one from 24h ago
		},
		{
			name: "filter by ToDate",
			filter: models.ActivityFilter{
				ToDate: new(now.Add(-10 * time.Hour)),
			},
			expectedCount: 1, // only the one from 24h ago
			verify: func(t *testing.T, acts []models.Activity) {
				assert.Equal(t, "SideProject", acts[0].Project)
			},
		},
		{
			name: "filter by combination (Project + IsRunning)",
			filter: models.ActivityFilter{
				Project:   new("Tock"),
				IsRunning: new(false),
			},
			expectedCount: 1,
			verify: func(t *testing.T, acts []models.Activity) {
				assert.Equal(t, "Design architecture", acts[0].Description)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setupTestDB(t)
			standardSetup(repo)

			acts, err := repo.Find(ctx, tt.filter)
			require.NoError(t, err)

			assert.Len(t, acts, tt.expectedCount)
			if tt.verify != nil && len(acts) > 0 {
				tt.verify(t, acts)
			}
		})
	}
}

func TestSQLiteRepository_FindOverlapsDateRange(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	yesterday := now.AddDate(0, 0, -1)
	from := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.Local)
	to := from.AddDate(0, 0, 1)
	day := models.ActivityFilter{FromDate: &from, ToDate: &to}
	future := models.ActivityFilter{FromDate: new(now.Add(time.Hour)), ToDate: new(now.Add(2 * time.Hour))}

	tests := []struct {
		name   string
		start  time.Time
		end    *time.Time
		filter models.ActivityFilter
		want   bool
	}{
		{name: "crosses range start", start: from.Add(-2 * time.Hour), end: new(from.Add(time.Hour)), filter: day, want: true},
		{name: "crosses range end", start: to.Add(-time.Hour), end: new(to.Add(time.Hour)), filter: day, want: true},
		{name: "fully inside", start: from.Add(time.Hour), end: new(from.Add(2 * time.Hour)), filter: day, want: true},
		{name: "fully before", start: from.Add(-3 * time.Hour), end: new(from.Add(-time.Hour)), filter: day},
		{name: "fully after", start: to.Add(time.Hour), end: new(to.Add(2 * time.Hour)), filter: day},
		{name: "ends at range start", start: from.Add(-time.Hour), end: new(from), filter: day},
		{name: "starts at range end", start: to, end: new(to.Add(time.Hour)), filter: day},
		{name: "running since before range", start: from.Add(-time.Hour), filter: day, want: true},
		{name: "running, range starts in the future", start: now.Add(-time.Hour), filter: future},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setupTestDB(t)
			require.NoError(t, repo.Save(ctx, models.Activity{Description: "D", Project: "P", StartTime: tt.start, EndTime: tt.end}))

			acts, err := repo.Find(ctx, tt.filter)
			require.NoError(t, err)

			if !tt.want {
				assert.Empty(t, acts)
				return
			}
			require.Len(t, acts, 1)
			assert.True(t, tt.start.Equal(acts[0].StartTime))
		})
	}
}

func TestSQLiteRepository_FindEndTimeWithOffset(t *testing.T) {
	ctx := context.Background()
	repo := setupTestDB(t)

	// Rows as Save writes them: start_time in UTC, end_time with the writer's local offset.
	rows := []struct{ desc, start, end string }{
		{desc: "moscow overnight", start: "2026-11-06 19:00:00+00:00", end: "2026-11-07 01:00:00+03:00"},    // ends 22:00 UTC
		{desc: "new york evening", start: "2026-11-06 20:00:00+00:00", end: "2026-11-06 17:00:00-05:00"},    // ends 22:00 UTC
		{desc: "tokyo, before range", start: "2026-11-06 18:00:00+00:00", end: "2026-11-07 05:00:00+09:00"}, // ends 20:00 UTC
	}
	for _, row := range rows {
		_, err := repo.DB.ExecContext(ctx,
			`INSERT INTO activities (description, project, start_time, end_time) VALUES (?, 'P', ?, ?)`,
			row.desc, row.start, row.end)
		require.NoError(t, err)
	}

	from := time.Date(2026, 11, 6, 21, 0, 0, 0, time.UTC) // 2026-11-07 00:00 in Moscow
	to := from.AddDate(0, 0, 1)

	acts, err := repo.Find(ctx, models.ActivityFilter{FromDate: &from, ToDate: &to})
	require.NoError(t, err)

	require.Len(t, acts, 2)
	assert.Equal(t, "moscow overnight", acts[0].Description)
	assert.Equal(t, "new york evening", acts[1].Description)
	for _, a := range acts {
		require.NotNil(t, a.EndTime)
		assert.True(t, time.Date(2026, 11, 6, 22, 0, 0, 0, time.UTC).Equal(*a.EndTime))
	}
}

func TestSQLiteRepository_Remove(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	tests := []struct {
		name       string
		setup      func(repo *ActivityRepository)
		toRemove   models.Activity
		finalCount int
	}{
		{
			name: "remove existing activity",
			setup: func(repo *ActivityRepository) {
				_ = repo.Save(ctx, models.Activity{Description: "A", Project: "1", StartTime: now})
				_ = repo.Save(ctx, models.Activity{Description: "B", Project: "2", StartTime: now.Add(-time.Hour)})
			},
			toRemove:   models.Activity{StartTime: now}, // Start time is the unique key
			finalCount: 1,
		},
		{
			name: "remove non-existing activity (no-op)",
			setup: func(repo *ActivityRepository) {
				_ = repo.Save(ctx, models.Activity{Description: "A", Project: "1", StartTime: now})
			},
			toRemove:   models.Activity{StartTime: now.Add(-time.Hour)},
			finalCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setupTestDB(t)
			tt.setup(repo)

			err := repo.Remove(ctx, tt.toRemove)
			require.NoError(t, err)

			acts, err := repo.Find(ctx, models.ActivityFilter{})
			require.NoError(t, err)
			assert.Len(t, acts, tt.finalCount)
		})
	}
}
