package activity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/kriuchkov/tock/internal/core/dto"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	portsmocks "github.com/kriuchkov/tock/internal/core/ports/mocks"
	"github.com/kriuchkov/tock/internal/services/activity"
)

func TestService_Stop(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		setup     func(repo *portsmocks.MockActivityRepository, notesRepo *portsmocks.MockNotesRepository)
		req       dto.StopActivityRequest
		assert    func(t *testing.T, act *models.Activity)
		assertErr func(t *testing.T, err error)
	}{
		{
			name: "stop running activity",
			setup: func(repo *portsmocks.MockActivityRepository, _ *portsmocks.MockNotesRepository) {
				runningAct := models.Activity{
					Project:     "test",
					Description: "running",
					StartTime:   now.Add(-1 * time.Hour),
					EndTime:     nil,
				}
				repo.EXPECT().Find(mock.Anything, mock.MatchedBy(func(f dto.ActivityFilter) bool {
					return f.IsRunning != nil && *f.IsRunning
				})).Return([]models.Activity{runningAct}, nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == runningAct.Project && a.EndTime != nil
				})).Return(nil)
			},
			req: dto.StopActivityRequest{EndTime: now},
			assert: func(t *testing.T, act *models.Activity) {
				assert.NotNil(t, act.EndTime)
			},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "stop with multiple running activities (should pick latest)",
			setup: func(repo *portsmocks.MockActivityRepository, _ *portsmocks.MockNotesRepository) {
				act1 := models.Activity{
					Project:   "old",
					StartTime: now.Add(-5 * time.Hour),
					EndTime:   nil,
				}
				act2 := models.Activity{
					Project:   "new",
					StartTime: now.Add(-1 * time.Hour),
					EndTime:   nil,
				}
				repo.EXPECT().Find(mock.Anything, mock.MatchedBy(func(f dto.ActivityFilter) bool {
					return f.IsRunning != nil && *f.IsRunning
				})).Return([]models.Activity{act1, act2}, nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == "new" && a.EndTime != nil
				})).Return(nil)
			},
			req: dto.StopActivityRequest{EndTime: now},
			assert: func(t *testing.T, act *models.Activity) {
				assert.Equal(t, "new", act.Project)
			},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "no running activity",
			setup: func(repo *portsmocks.MockActivityRepository, _ *portsmocks.MockNotesRepository) {
				repo.EXPECT().Find(mock.Anything, mock.MatchedBy(func(f dto.ActivityFilter) bool {
					return f.IsRunning != nil && *f.IsRunning
				})).Return([]models.Activity{}, nil)
			},
			req:    dto.StopActivityRequest{EndTime: now},
			assert: func(_ *testing.T, _ *models.Activity) {},
			assertErr: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, coreErrors.ErrNoActiveActivity)
			},
		},
		{
			name: "stop with notes and tags updates activity",
			setup: func(repo *portsmocks.MockActivityRepository, notesRepo *portsmocks.MockNotesRepository) {
				runningAct := models.Activity{
					Project:     "test",
					Description: "running",
					StartTime:   now.Add(-1 * time.Hour),
				}
				repo.EXPECT().Find(mock.Anything, mock.MatchedBy(func(f dto.ActivityFilter) bool {
					return f.IsRunning != nil && *f.IsRunning
				})).Return([]models.Activity{runningAct}, nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == "test" &&
						a.EndTime != nil &&
						a.Notes == "closing note" &&
						len(a.Tags) == 2 && a.Tags[0] == "done"
				})).Return(nil)

				notesRepo.On("Save", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time"), "closing note", []string{"done", "success"}).
					Return(nil)
			},
			req: dto.StopActivityRequest{
				EndTime: now,
				Notes:   "closing note",
				Tags:    []string{"done", "success"},
			},
			assert: func(t *testing.T, act *models.Activity) {
				assert.Equal(t, "closing note", act.Notes)
				assert.Equal(t, []string{"done", "success"}, act.Tags)
			},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := portsmocks.NewMockActivityRepository(t)
			notesRepo := new(portsmocks.MockNotesRepository)
			tt.setup(repo, notesRepo)

			svc := activity.NewService(repo, notesRepo)
			got, err := svc.Stop(context.Background(), tt.req)
			tt.assertErr(t, err)
			tt.assert(t, got)
		})
	}
}

func TestService_Start(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		setup     func(repo *portsmocks.MockActivityRepository, notesRepo *portsmocks.MockNotesRepository)
		req       dto.StartActivityRequest
		assert    func(t *testing.T, act *models.Activity)
		assertErr func(t *testing.T, err error)
	}{
		{
			name: "start stops currently running",
			setup: func(repo *portsmocks.MockActivityRepository, _ *portsmocks.MockNotesRepository) {
				runningAct := models.Activity{
					Project:   "prev",
					StartTime: now.Add(-1 * time.Hour),
				}
				repo.EXPECT().Find(mock.Anything, mock.MatchedBy(func(f dto.ActivityFilter) bool {
					return f.IsRunning != nil && *f.IsRunning
				})).Return([]models.Activity{runningAct}, nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == "prev" && a.EndTime != nil
				})).Return(nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == "new" && a.EndTime == nil
				})).Return(nil)
			},
			req: dto.StartActivityRequest{Project: "new", Description: "task"},
			assert: func(t *testing.T, act *models.Activity) {
				assert.Equal(t, "new", act.Project)
			},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "start with specific time stops running activity at that time",
			setup: func(repo *portsmocks.MockActivityRepository, _ *portsmocks.MockNotesRepository) {
				newStartTime := now.Add(-10 * time.Minute)
				runningAct := models.Activity{
					Project:   "prev",
					StartTime: now.Add(-1 * time.Hour),
				}
				repo.EXPECT().Find(mock.Anything, mock.MatchedBy(func(f dto.ActivityFilter) bool {
					return f.IsRunning != nil && *f.IsRunning
				})).Return([]models.Activity{runningAct}, nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					if a.Project != "prev" {
						return false
					}
					if a.EndTime == nil {
						return false
					}
					return a.EndTime.Equal(newStartTime)
				})).Return(nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == "new" && a.StartTime.Equal(newStartTime)
				})).Return(nil)
			},
			req: dto.StartActivityRequest{
				Project:     "new",
				Description: "task",
				StartTime:   now.Add(-10 * time.Minute),
			},
			assert: func(t *testing.T, act *models.Activity) {
				assert.Equal(t, "new", act.Project)
				assert.Equal(t, now.Add(-10*time.Minute), act.StartTime)
			},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "start time before running start time falls back to now",
			setup: func(repo *portsmocks.MockActivityRepository, _ *portsmocks.MockNotesRepository) {
				newStartTime := now.Add(-2 * time.Hour)
				runningAct := models.Activity{
					Project:   "prev",
					StartTime: now.Add(-1 * time.Hour), // Started 1 hour ago
				}
				repo.EXPECT().Find(mock.Anything, mock.MatchedBy(func(f dto.ActivityFilter) bool {
					return f.IsRunning != nil && *f.IsRunning
				})).Return([]models.Activity{runningAct}, nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					if a.Project != "prev" {
						return false
					}
					if a.EndTime == nil {
						return false
					}
					return a.EndTime.After(runningAct.StartTime)
				})).Return(nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == "new" && a.StartTime.Equal(newStartTime)
				})).Return(nil)
			},
			req: dto.StartActivityRequest{
				Project:     "new",
				Description: "task",
				StartTime:   now.Add(-2 * time.Hour),
			},
			assert: func(t *testing.T, act *models.Activity) {
				assert.Equal(t, "new", act.Project)
			},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "start with notes and tags",
			setup: func(repo *portsmocks.MockActivityRepository, notesRepo *portsmocks.MockNotesRepository) {
				repo.EXPECT().Find(mock.Anything, mock.MatchedBy(func(f dto.ActivityFilter) bool {
					return f.IsRunning != nil && *f.IsRunning
				})).Return([]models.Activity{}, nil)

				repo.EXPECT().Save(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == "project" && a.Notes == "note" && len(a.Tags) == 2
				})).Return(nil)

				notesRepo.On("Save", mock.Anything, mock.AnythingOfType("string"), mock.AnythingOfType("time.Time"), "note", []string{"tag1", "tag2"}).
					Return(nil)
			},
			req: dto.StartActivityRequest{
				Project:     "project",
				Description: "desc",
				StartTime:   now,
				Notes:       "note",
				Tags:        []string{"tag1", "tag2"},
			},
			assert: func(t *testing.T, act *models.Activity) {
				assert.Equal(t, "note", act.Notes)
				assert.Equal(t, []string{"tag1", "tag2"}, act.Tags)
			},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := portsmocks.NewMockActivityRepository(t)
			notesRepo := new(portsmocks.MockNotesRepository)
			tt.setup(repo, notesRepo)

			svc := activity.NewService(repo, notesRepo)
			got, err := svc.Start(context.Background(), tt.req)
			tt.assertErr(t, err)
			tt.assert(t, got)
		})
	}
}

func TestService_Remove(t *testing.T) {
	tests := []struct {
		name      string
		activity  models.Activity
		setup     func(repo *portsmocks.MockActivityRepository)
		assertErr func(t *testing.T, err error)
	}{
		{
			name:     "success",
			activity: models.Activity{Project: "A", Description: "Task A"},
			setup: func(repo *portsmocks.MockActivityRepository) {
				repo.EXPECT().Remove(mock.Anything, mock.MatchedBy(func(a models.Activity) bool {
					return a.Project == "A"
				})).Return(nil)
			},
			assertErr: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:     "failure",
			activity: models.Activity{Project: "NonExistent"},
			setup: func(repo *portsmocks.MockActivityRepository) {
				repo.EXPECT().Remove(mock.Anything, mock.Anything).Return(errors.New("not found"))
			},
			assertErr: func(t *testing.T, err error) {
				assert.Error(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := portsmocks.NewMockActivityRepository(t)
			tt.setup(repo)

			svc := activity.NewService(repo, nil)
			err := svc.Remove(context.Background(), tt.activity)
			if tt.assertErr != nil {
				tt.assertErr(t, err)
			}
		})
	}
}
