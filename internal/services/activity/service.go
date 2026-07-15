package activity

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/go-faster/errors"

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

func (s *service) Start(ctx context.Context, req models.StartActivityRequest) (*models.Activity, error) {
	isRunning := true
	running, err := s.repo.Find(ctx, models.ActivityFilter{IsRunning: &isRunning})
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

func (s *service) Stop(ctx context.Context, req models.StopActivityRequest) (*models.Activity, error) {
	isRunning := true
	running, err := s.repo.Find(ctx, models.ActivityFilter{IsRunning: &isRunning})
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

func (s *service) Add(ctx context.Context, req models.AddActivityRequest) (*models.Activity, error) {
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

func (s *service) List(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
	activites, err := s.repo.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	return s.enrichActivities(ctx, activites)
}

func (s *service) GetReport(ctx context.Context, filter models.ActivityFilter) (*models.Report, error) {
	activities, err := s.repo.Find(ctx, filter)
	if err != nil {
		return nil, errors.Wrap(err, "find activities")
	}

	if s.notesRepo != nil {
		activities, _ = s.enrichActivities(ctx, activities)
	}

	report := &models.Report{
		Activities: []models.Activity{},
		ByProject:  make(map[string]models.ProjectReport),
	}

	now := time.Now()
	for _, a := range activities {
		clipped, ok := clipActivityByRange(a, filter, now)
		if !ok {
			continue
		}

		report.Activities = append(report.Activities, clipped)
		duration := clipped.Duration()
		report.TotalDuration += duration

		projectReport, exists := report.ByProject[clipped.Project]
		if !exists {
			projectReport = models.ProjectReport{
				ProjectName: clipped.Project,
				Duration:    0,
				Activities:  []models.Activity{},
			}
		}

		projectReport.Duration += duration
		projectReport.Activities = append(projectReport.Activities, clipped)
		report.ByProject[clipped.Project] = projectReport
	}
	return report, nil
}

func clipActivityByRange(
	activity models.Activity,
	filter models.ActivityFilter,
	now time.Time,
) (models.Activity, bool) {
	start := activity.StartTime
	end := now
	if activity.EndTime != nil {
		end = *activity.EndTime
	}

	if filter.FromDate != nil && start.Before(*filter.FromDate) {
		start = *filter.FromDate
	}
	if filter.ToDate != nil && end.After(*filter.ToDate) {
		end = *filter.ToDate
	}
	if !end.After(start) {
		return models.Activity{}, false
	}

	clipped := activity
	clipped.StartTime = start
	if activity.EndTime == nil && (filter.ToDate == nil || !filter.ToDate.Before(now)) && end.Equal(now) {
		clipped.EndTime = nil
		return clipped, true
	}

	clippedEnd := end
	clipped.EndTime = &clippedEnd
	return clipped, true
}

func (s *service) GetRecent(ctx context.Context, limit int) ([]models.Activity, error) {
	all, err := s.repo.Find(ctx, models.ActivityFilter{})
	if err != nil {
		return nil, err
	}

	var recent []models.Activity
	seen := make(map[string]bool)

	for _, v := range slices.Backward(all) {
		a := v
		key := a.Project + "|" + a.Description
		if !seen[key] {
			recent = append(recent, a)
			seen[key] = true
		}
		if len(recent) >= limit {
			break
		}
	}

	return s.enrichActivities(ctx, recent)
}

func (s *service) GetLast(ctx context.Context) (*models.Activity, error) {
	return s.repo.FindLast(ctx)
}

// AddNote appends note text to an activity, keeping its existing tags. The
// authoritative notes/tags come from the notes repository when present, so the
// caller may pass a non-enriched activity (e.g. from GetLast).
func (s *service) AddNote(ctx context.Context, activity models.Activity, note string) (*models.Activity, error) {
	if s.notesRepo == nil {
		return nil, coreErrors.ErrNotesUnavailable
	}

	existingNotes, existingTags, err := s.loadStoredNotes(ctx, activity)
	if err != nil {
		return nil, err
	}

	updated := activity
	updated.Notes = joinNotes(existingNotes, note)
	updated.Tags = existingTags

	if err = s.notesRepo.Save(ctx, updated.ID(), updated.StartTime, updated.Notes, updated.Tags); err != nil {
		return nil, errors.Wrap(err, "save note")
	}
	return &updated, nil
}

// AddTags merges new tags into an activity, keeping its existing notes and
// de-duplicating while preserving order.
func (s *service) AddTags(ctx context.Context, activity models.Activity, tags []string) (*models.Activity, error) {
	if s.notesRepo == nil {
		return nil, coreErrors.ErrNotesUnavailable
	}

	existingNotes, existingTags, err := s.loadStoredNotes(ctx, activity)
	if err != nil {
		return nil, err
	}

	updated := activity
	updated.Notes = existingNotes
	updated.Tags = mergeTags(existingTags, tags)

	if err = s.notesRepo.Save(ctx, updated.ID(), updated.StartTime, updated.Notes, updated.Tags); err != nil {
		return nil, errors.Wrap(err, "save tags")
	}
	return &updated, nil
}

func (s *service) Remove(ctx context.Context, activity models.Activity) error {
	return s.repo.Remove(ctx, activity)
}

// loadStoredNotes returns the authoritative notes/tags for an activity,
// preferring values persisted in the notes repository over whatever the
// passed-in activity carries.
func (s *service) loadStoredNotes(ctx context.Context, activity models.Activity) (string, []string, error) {
	existingNotes := strings.TrimSpace(activity.Notes)
	existingTags := append([]string(nil), activity.Tags...)

	storedNotes, storedTags, err := s.notesRepo.Get(ctx, activity.ID(), activity.StartTime)
	if err != nil {
		return "", nil, errors.Wrap(err, "get notes")
	}

	if trimmed := strings.TrimSpace(storedNotes); trimmed != "" {
		existingNotes = trimmed
	}
	if len(storedTags) > 0 {
		existingTags = append([]string(nil), storedTags...)
	}
	return existingNotes, existingTags, nil
}

func mergeTags(existingTags, newTags []string) []string {
	seen := make(map[string]struct{}, len(existingTags)+len(newTags))
	merged := make([]string, 0, len(existingTags)+len(newTags))

	for _, tag := range append(append([]string(nil), existingTags...), newTags...) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		merged = append(merged, tag)
	}

	return merged
}

func joinNotes(existingNotes, noteText string) string {
	existingNotes = strings.TrimSpace(existingNotes)
	noteText = strings.TrimSpace(noteText)

	if existingNotes == "" {
		return noteText
	}
	if noteText == "" {
		return existingNotes
	}
	return existingNotes + "\n\n" + noteText
}

func (s *service) enrichActivities(ctx context.Context, activities []models.Activity) ([]models.Activity, error) {
	if s.notesRepo == nil {
		return activities, nil
	}

	for i := range activities {
		notes, tags, err := s.notesRepo.Get(ctx, activities[i].ID(), activities[i].StartTime)
		if err == nil {
			if notes != "" {
				activities[i].Notes = notes
			}
			if len(tags) > 0 {
				activities[i].Tags = tags
			}
		}
	}

	return activities, nil
}
