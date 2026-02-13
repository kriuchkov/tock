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
	repo      ports.ActivityRepository
	notesRepo ports.NotesRepository
}

func NewService(repo ports.ActivityRepository, notesRepo ports.NotesRepository) ports.ActivityResolver {
	return &service{repo: repo, notesRepo: notesRepo}
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
		Notes:       req.Notes,
		Tags:        req.Tags,
	}

	if saveErr := s.repo.Save(ctx, newActivity); saveErr != nil {
		return nil, errors.Wrap(saveErr, "save activity")
	}

	if s.notesRepo != nil && (req.Notes != "" || len(req.Tags) > 0) {
		if err = s.notesRepo.Save(ctx, newActivity.ID(), newActivity.StartTime, req.Notes, req.Tags); err != nil {
			return nil, errors.Wrap(err, "save notes")
		}
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
	// Update notes/tags if provided
	if req.Notes != "" {
		last.Notes = req.Notes
	}
	if len(req.Tags) > 0 {
		last.Tags = req.Tags
	}

	if saveErr := s.repo.Save(ctx, *last); saveErr != nil {
		return nil, errors.Wrap(saveErr, "save activity")
	}

	if s.notesRepo != nil && (req.Notes != "" || len(req.Tags) > 0) {
		if err = s.notesRepo.Save(ctx, last.ID(), last.StartTime, last.Notes, last.Tags); err != nil {
			return nil, errors.Wrap(err, "save notes")
		}
	}

	return last, nil
}

func (s *service) Add(ctx context.Context, req dto.AddActivityRequest) (*models.Activity, error) {
	newActivity := models.Activity{
		Description: req.Description,
		Project:     req.Project,
		StartTime:   req.StartTime,
		EndTime:     &req.EndTime,
		Notes:       req.Notes,
		Tags:        req.Tags,
	}

	if saveErr := s.repo.Save(ctx, newActivity); saveErr != nil {
		return nil, errors.Wrap(saveErr, "save activity")
	}

	if s.notesRepo != nil && (req.Notes != "" || len(req.Tags) > 0) {
		if err := s.notesRepo.Save(ctx, newActivity.ID(), newActivity.StartTime, req.Notes, req.Tags); err != nil {
			return nil, errors.Wrap(err, "save notes")
		}
	}

	return &newActivity, nil
}

func (s *service) List(ctx context.Context, filter dto.ActivityFilter) ([]models.Activity, error) {
	activites, err := s.repo.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	return s.enrichActivities(ctx, activites)
}

func (s *service) GetReport(ctx context.Context, filter dto.ActivityFilter) (*dto.Report, error) {
	activities, err := s.repo.Find(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "find activities")
	}

	if s.notesRepo != nil {
		activities, _ = s.enrichActivities(ctx, activities)
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

	// Enrich recent activities if needed?
	// GetRecent is used for completion. Notes don't matter?
	// User said list/calendar display them.
	// But it might be nice to see them.
	// Let's enrich recent - it's a small list.
	return s.enrichActivities(ctx, recent)
}

func (s *service) enrichActivities(ctx context.Context, activities []models.Activity) ([]models.Activity, error) {
	if s.notesRepo == nil {
		return activities, nil
	}
	for i := range activities {
		notes, tags, err := s.notesRepo.Get(ctx, activities[i].ID(), activities[i].StartTime)
		if err == nil {
			activities[i].Notes = notes
			activities[i].Tags = tags
		}
	}
	return activities, nil
}
