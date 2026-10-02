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
	"github.com/kriuchkov/tock/internal/timeutil"
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

// Update applies a partial change to an existing activity. When the start time
// changes, the entry is re-keyed: the updated activity is written first and the
// original removed afterwards, so an interrupted update never loses the entry.
func (s *service) Update(
	ctx context.Context,
	original models.Activity,
	req models.UpdateActivityRequest,
) (*models.Activity, error) {
	if req.IsEmpty() {
		return &original, nil
	}

	if err := validateUpdateRequest(req); err != nil {
		return nil, err
	}

	current, err := s.hydrateNotes(ctx, original, req)
	if err != nil {
		return nil, err
	}

	updated := req.Apply(current)

	if updated.StartTime.IsZero() {
		return nil, errors.New("start time is required")
	}
	if updated.EndTime != nil && updated.EndTime.Before(updated.StartTime) {
		return nil, coreErrors.ErrInvalidTimeRange
	}

	// Entries are keyed by start time with minute precision, so a sub-minute
	// difference is not a move and must not re-key the entry.
	moved := !updated.StartTime.Truncate(time.Minute).Equal(original.StartTime.Truncate(time.Minute))
	if moved {
		if err = s.ensureStartTimeFree(ctx, original, updated.StartTime); err != nil {
			return nil, err
		}
	} else {
		updated.StartTime = original.StartTime
	}

	if updated.EndTime == nil {
		if err = s.ensureNoOtherRunningActivity(ctx, original); err != nil {
			return nil, err
		}
	}

	if err = s.repo.Save(ctx, updated); err != nil {
		return nil, errors.Wrap(err, "save activity")
	}

	if moved {
		if err = s.repo.Remove(ctx, original); err != nil {
			return nil, errors.Wrap(err, "remove previous activity")
		}
	}

	if err = s.syncNotes(ctx, original, updated, moved); err != nil {
		return nil, err
	}
	return &updated, nil
}

// validateUpdateRequest rejects explicitly blanked mandatory fields; fields the
// request does not mention keep whatever the activity already carries.
func validateUpdateRequest(req models.UpdateActivityRequest) error {
	if req.Project != nil && strings.TrimSpace(*req.Project) == "" {
		return coreErrors.ErrProjectRequired
	}
	if req.Description != nil && strings.TrimSpace(*req.Description) == "" {
		return coreErrors.ErrDescriptionRequired
	}
	return nil
}

// hydrateNotes fills in the stored notes/tags the caller may not carry (GetLast
// and the repositories return activities without them), so an update that does
// not mention notes or tags cannot erase them.
func (s *service) hydrateNotes(
	ctx context.Context,
	activity models.Activity,
	req models.UpdateActivityRequest,
) (models.Activity, error) {
	if s.notesRepo == nil || (req.Notes != nil && req.Tags != nil) {
		return activity, nil
	}

	storedNotes, storedTags, err := s.loadStoredNotes(ctx, activity)
	if err != nil {
		return models.Activity{}, err
	}

	hydrated := activity
	hydrated.Notes = storedNotes
	hydrated.Tags = storedTags
	return hydrated, nil
}

// ensureNoOtherRunningActivity keeps the "only one activity runs at a time"
// invariant when an update drops an end time, because a second open entry can
// no longer be closed by `tock stop`.
func (s *service) ensureNoOtherRunningActivity(ctx context.Context, original models.Activity) error {
	isRunning := true

	running, err := s.repo.Find(ctx, models.ActivityFilter{IsRunning: &isRunning})
	if err != nil {
		return errors.Wrap(err, "find running activities")
	}

	for _, activity := range running {
		if !activity.StartTime.Equal(original.StartTime) {
			return coreErrors.ErrActivityAlreadyStarted
		}
	}
	return nil
}

// ensureStartTimeFree rejects a move onto the start time of another activity,
// because every backend keys an entry by its start time.
func (s *service) ensureStartTimeFree(ctx context.Context, original models.Activity, startTime time.Time) error {
	fromDate, toDate := timeutil.LocalDayBounds(startTime)

	activities, err := s.repo.Find(ctx, models.ActivityFilter{FromDate: &fromDate, ToDate: &toDate})
	if err != nil {
		return errors.Wrap(err, "find activities")
	}

	target := startTime.Truncate(time.Minute)
	for _, activity := range activities {
		if activity.StartTime.Equal(original.StartTime) {
			continue
		}
		if activity.StartTime.Truncate(time.Minute).Equal(target) {
			return coreErrors.ErrStartTimeConflict
		}
	}
	return nil
}

// syncNotes persists the updated notes/tags and clears the sidecar entry left
// behind when the activity moved to a new start time.
func (s *service) syncNotes(ctx context.Context, original, updated models.Activity, moved bool) error {
	if s.notesRepo == nil {
		return nil
	}

	if moved {
		if err := s.notesRepo.Save(ctx, original.ID(), original.StartTime, "", nil); err != nil {
			return errors.Wrap(err, "clear previous notes")
		}
	}

	if err := s.notesRepo.Save(ctx, updated.ID(), updated.StartTime, updated.Notes, updated.Tags); err != nil {
		return errors.Wrap(err, "save notes")
	}
	return nil
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
	if err := s.repo.Remove(ctx, activity); err != nil {
		return err
	}

	if s.notesRepo != nil {
		if err := s.notesRepo.Delete(ctx, activity.ID(), activity.StartTime); err != nil {
			return errors.Wrap(err, "delete notes")
		}
	}
	return nil
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
