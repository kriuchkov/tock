package activity

import (
	"context"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/dto"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

type service struct {
	repo ports.ActivityRepository
}

func NewService(repo ports.ActivityRepository) ports.ActivityResolver {
	return &service{repo: repo}
}

func (s *service) Start(ctx context.Context, req dto.StartActivityRequest) (*models.Activity, error) {
	isRunning := true
	running, err := s.repo.Find(ctx, dto.ActivityFilter{IsRunning: &isRunning})
	if err != nil {
		return nil, errors.Wrap(err, "find running activities")
	}

	startTime := req.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}

	for _, act := range running {
		stopTime := startTime
		if stopTime.Before(act.StartTime) {
			stopTime = time.Now()
		}
		act.EndTime = &stopTime
		if saveErr := s.repo.Save(ctx, act); saveErr != nil {
			return nil, errors.Wrap(saveErr, "stop running activity")
		}
	}

	newActivity := models.Activity{
		Description: req.Description,
		Project:     req.Project,
		StartTime:   startTime,
	}

	if saveErr := s.repo.Save(ctx, newActivity); saveErr != nil {
		return nil, errors.Wrap(saveErr, "save activity")
	}
	return &newActivity, nil
}

func (s *service) Stop(ctx context.Context, req dto.StopActivityRequest) (*models.Activity, error) {
	isRunning := true
	running, err := s.repo.Find(ctx, dto.ActivityFilter{IsRunning: &isRunning})
	if err != nil {
		return nil, errors.Wrap(err, "find running activities")
	}

	if len(running) == 0 {
		return nil, coreErrors.ErrNoActiveActivity
	}

	// Find the latest running activity
	var last *models.Activity
	for i := range running {
		if last == nil || running[i].StartTime.After(last.StartTime) {
			last = &running[i]
		}
	}

	endTime := req.EndTime
	if endTime.IsZero() {
		endTime = time.Now()
	}

	if endTime.Before(last.StartTime) {
		return nil, errors.New("end time cannot be before start time")
	}

	last.EndTime = &endTime
	if saveErr := s.repo.Save(ctx, *last); saveErr != nil {
		return nil, errors.Wrap(saveErr, "save activity")
	}

	return last, nil
}

func (s *service) Add(ctx context.Context, req dto.AddActivityRequest) (*models.Activity, error) {
	newActivity := models.Activity{
		Description: req.Description,
		Project:     req.Project,
		StartTime:   req.StartTime,
		EndTime:     &req.EndTime,
	}

	if saveErr := s.repo.Save(ctx, newActivity); saveErr != nil {
		return nil, errors.Wrap(saveErr, "save activity")
	}
	return &newActivity, nil
}

func (s *service) List(ctx context.Context, filter dto.ActivityFilter) ([]models.Activity, error) {
	return s.repo.Find(ctx, filter)
}

func (s *service) GetReport(ctx context.Context, filter dto.ActivityFilter) (*dto.Report, error) {
	activities, err := s.repo.Find(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "find activities")
	}

	report := &dto.Report{
		Activities: []models.Activity{},
		ByProject:  make(map[string]dto.ProjectReport),
	}

	for _, a := range activities {
		report.Activities = append(report.Activities, a)
		duration := a.Duration()
		report.TotalDuration += duration

		// Aggregate by project
		projectReport, exists := report.ByProject[a.Project]
		if !exists {
			projectReport = dto.ProjectReport{
				ProjectName: a.Project,
				Duration:    0,
				Activities:  []models.Activity{},
			}
		}

		projectReport.Duration += duration
		projectReport.Activities = append(projectReport.Activities, a)
		report.ByProject[a.Project] = projectReport
	}
	return report, nil
}

func (s *service) GetRecent(ctx context.Context, limit int) ([]models.Activity, error) {
	all, err := s.repo.Find(ctx, dto.ActivityFilter{})
	if err != nil {
		return nil, err
	}

	var recent []models.Activity
	seen := make(map[string]bool)

	for i := len(all) - 1; i >= 0; i-- {
		a := all[i]
		key := a.Project + "|" + a.Description
		if !seen[key] {
			recent = append(recent, a)
			seen[key] = true
		}
		if len(recent) >= limit {
			break
		}
	}
	return recent, nil
}
