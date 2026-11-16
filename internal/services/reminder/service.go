package reminder

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
	repo       ports.ReminderRepository
	activities ports.ActivityResolver
	now        func() time.Time
}

// NewService returns the reminder API. activities starts the activity when an occurrence is tracked.
func NewService(repo ports.ReminderRepository, activities ports.ActivityResolver) ports.ReminderResolver {
	return &service{repo: repo, activities: activities, now: time.Now}
}

func (s *service) Add(ctx context.Context, req models.AddReminderRequest) (*models.Reminder, error) {
	reminder := models.Reminder{
		Title:   strings.TrimSpace(req.Title),
		Project: strings.TrimSpace(req.Project),
		Due:     req.Due,
		AllDay:  req.AllDay,
		Repeat:  req.Repeat,
		Notify:  req.Notify,
		Tags:    slices.Clone(req.Tags),
		Notes:   req.Notes,
		Created: s.now(),
	}
	reminder, err := normalize(reminder)
	if err != nil {
		return nil, err
	}

	saved, err := s.repo.Save(ctx, reminder)
	if err != nil {
		return nil, errors.Wrap(err, "save reminder")
	}
	return saved, nil
}

func (s *service) Get(ctx context.Context, id int) (*models.Reminder, error) {
	reminders, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range reminders {
		if reminders[i].ID == id {
			return &reminders[i], nil
		}
	}
	return nil, coreErrors.ErrReminderNotFound
}

func (s *service) List(ctx context.Context) ([]models.Reminder, error) {
	reminders, err := s.repo.List(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "list reminders")
	}
	slices.SortFunc(reminders, func(a, b models.Reminder) int { return a.ID - b.ID })
	return reminders, nil
}

func (s *service) Update(ctx context.Context, id int, req models.UpdateReminderRequest) (*models.Reminder, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	updated := req.Apply(*current)
	updated.Title = strings.TrimSpace(updated.Title)
	updated.Project = strings.TrimSpace(updated.Project)
	if updated, err = normalize(updated); err != nil {
		return nil, err
	}

	saved, err := s.repo.Save(ctx, updated)
	if err != nil {
		return nil, errors.Wrap(err, "save reminder")
	}
	return saved, nil
}

func (s *service) Remove(ctx context.Context, id int) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.repo.Remove(ctx, id); err != nil {
		return errors.Wrap(err, "remove reminder")
	}
	return nil
}

func (s *service) Occurrences(ctx context.Context, from, to, now time.Time) ([]models.ReminderOccurrence, error) {
	reminders, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var occurrences []models.ReminderOccurrence
	for _, reminder := range reminders {
		occurrences = append(occurrences, reminder.Occurrences(from, to, now)...)
	}
	models.SortOccurrences(occurrences)
	return occurrences, nil
}

func (s *service) Overdue(ctx context.Context, now time.Time) ([]models.ReminderOccurrence, error) {
	reminders, err := s.List(ctx)
	if err != nil {
		return nil, err
	}

	var overdue []models.ReminderOccurrence
	for _, reminder := range reminders {
		current, ok := reminder.Current(now)
		if !ok {
			continue
		}
		switch status := current.Status(now); {
		case status == models.ReminderOverdue,
			status == models.ReminderSnoozed && current.PastDue(now):
			overdue = append(overdue, current)
		}
	}
	models.SortOccurrences(overdue)
	return overdue, nil
}

func (s *service) Current(ctx context.Context, id int, now time.Time) (*models.ReminderOccurrence, error) {
	reminder, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	occurrence, ok := reminder.Current(now)
	if !ok {
		return nil, coreErrors.ErrNoReminderOccurrence
	}
	return &occurrence, nil
}

func (s *service) SetDone(
	ctx context.Context,
	ref models.OccurrenceRef,
	done bool,
	at time.Time,
) (*models.ReminderOccurrence, error) {
	return s.updateOccurrence(ctx, ref, func(state *models.OccurrenceState) {
		if !done {
			state.DoneAt = nil
			state.ActivityStart = nil
			return
		}
		doneAt := at
		state.DoneAt = &doneAt
		state.SnoozedUntil = nil
	})
}

func (s *service) Snooze(ctx context.Context, ref models.OccurrenceRef, until time.Time) (*models.ReminderOccurrence, error) {
	return s.updateOccurrence(ctx, ref, func(state *models.OccurrenceState) {
		if until.IsZero() {
			state.SnoozedUntil = nil
			return
		}
		snoozedUntil := until
		state.SnoozedUntil = &snoozedUntil
		state.DoneAt = nil
	})
}

func (s *service) Skip(ctx context.Context, ref models.OccurrenceRef) error {
	reminder, err := s.Get(ctx, ref.ID)
	if err != nil {
		return err
	}
	if !reminder.IsRepeating() {
		return s.Remove(ctx, ref.ID)
	}

	_, err = s.updateOccurrence(ctx, ref, func(state *models.OccurrenceState) {
		*state = models.OccurrenceState{Skipped: true}
	})
	return err
}

func (s *service) Track(ctx context.Context, ref models.OccurrenceRef, req models.TrackReminderRequest) (*models.Activity, error) {
	if s.activities == nil {
		return nil, errors.New("activity service is not configured")
	}

	reminder, err := s.Get(ctx, ref.ID)
	if err != nil {
		return nil, err
	}
	if _, ok := reminder.OccurrenceOn(ref.Day, s.now()); !ok {
		return nil, coreErrors.ErrNoReminderOccurrence
	}

	project := strings.TrimSpace(req.Project)
	if project == "" {
		project = reminder.Project
	}
	if project == "" {
		return nil, coreErrors.ErrProjectRequired
	}

	startTime := req.StartTime
	if startTime.IsZero() {
		startTime = s.now()
	}

	activity, err := s.activities.Start(ctx, models.StartActivityRequest{
		Description: reminder.Title,
		Project:     project,
		StartTime:   startTime,
		Tags:        slices.Clone(reminder.Tags),
	})
	if err != nil {
		return nil, errors.Wrap(err, "start activity")
	}

	_, err = s.updateOccurrence(ctx, ref, func(state *models.OccurrenceState) {
		doneAt := activity.StartTime
		state.DoneAt = &doneAt
		state.ActivityStart = &doneAt
		state.SnoozedUntil = nil
	})
	if err != nil {
		return nil, err
	}
	return activity, nil
}

// updateOccurrence changes the recorded state of one existing occurrence and saves the reminder.
func (s *service) updateOccurrence(
	ctx context.Context,
	ref models.OccurrenceRef,
	change func(state *models.OccurrenceState),
) (*models.ReminderOccurrence, error) {
	reminder, err := s.Get(ctx, ref.ID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	occurrence, ok := reminder.OccurrenceOn(ref.Day, now)
	if !ok {
		return nil, coreErrors.ErrNoReminderOccurrence
	}

	state := occurrence.State
	change(&state)

	saved, err := s.repo.Save(ctx, reminder.WithState(occurrence.Due, state))
	if err != nil {
		return nil, errors.Wrap(err, "save reminder")
	}

	occurrence.Reminder = *saved
	occurrence.State = state
	if updated, found := saved.OccurrenceOn(ref.Day, now); found {
		occurrence = updated
	}
	return &occurrence, nil
}

func validate(reminder models.Reminder) error {
	if reminder.Title == "" {
		return coreErrors.ErrReminderTitleRequired
	}
	if reminder.Due.IsZero() {
		return coreErrors.ErrReminderDueRequired
	}
	if reminder.Notify < 0 {
		return errors.New("notify lead time cannot be negative")
	}
	if reminder.Notify > models.MaxNotifyLead {
		return errors.New("notify lead time cannot be longer than 7 days")
	}
	if err := reminder.Repeat.Validate(reminder.Due); err != nil {
		return errors.Wrap(err, "invalid repeat")
	}
	return nil
}

// normalize validates the reminder, stores an all-day one at the start of its day, drops seconds
// from a timed one, and moves Due to the first day the repeat rule really falls on.
func normalize(reminder models.Reminder) (models.Reminder, error) {
	if err := validate(reminder); err != nil {
		return models.Reminder{}, err
	}

	local := reminder.Due.In(time.Local)
	if reminder.AllDay {
		reminder.Due = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	} else {
		reminder.Due = time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, time.Local)
	}

	first, ok := reminder.FirstDue()
	if !ok {
		return models.Reminder{}, coreErrors.ErrReminderNeverDue
	}
	reminder.Due = first
	return reminder, nil
}
