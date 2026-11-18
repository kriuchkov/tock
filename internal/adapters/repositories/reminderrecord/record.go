package reminderrecord

import (
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
)

// Due times are stored as local wall-clock time: a reminder at 17:30 stays at 17:30 in any zone.
const (
	dueLayout    = "2006-01-02 15:04"
	allDayLayout = time.DateOnly
)

// Record is the storage form of a reminder, shared by the reminders (YAML) and sqlite backends.
type Record struct {
	ID          int                    `yaml:"id"                    json:"id"`
	UID         string                 `yaml:"uid,omitempty"         json:"uid,omitempty"`
	Title       string                 `yaml:"title"                 json:"title"`
	Project     string                 `yaml:"project,omitempty"     json:"project,omitempty"`
	Due         string                 `yaml:"due"                   json:"due"`
	AllDay      bool                   `yaml:"all_day,omitempty"     json:"all_day,omitempty"`
	Every       string                 `yaml:"every,omitempty"       json:"every,omitempty"`
	Until       string                 `yaml:"until,omitempty"       json:"until,omitempty"`
	Count       int                    `yaml:"count,omitempty"       json:"count,omitempty"`
	Notify      string                 `yaml:"notify,omitempty"      json:"notify,omitempty"`
	Tags        []string               `yaml:"tags,omitempty"        json:"tags,omitempty"`
	Notes       string                 `yaml:"notes,omitempty"       json:"notes,omitempty"`
	Created     time.Time              `yaml:"created"               json:"created"`
	Occurrences map[string]StateRecord `yaml:"occurrences,omitempty" json:"occurrences,omitempty"`
}

// StateRecord is the storage form of one occurrence's state.
type StateRecord struct {
	Done     *time.Time `yaml:"done,omitempty"     json:"done,omitempty"`
	Snoozed  *time.Time `yaml:"snoozed,omitempty"  json:"snoozed,omitempty"`
	Activity *time.Time `yaml:"activity,omitempty" json:"activity,omitempty"`
	Skipped  bool       `yaml:"skipped,omitempty"  json:"skipped,omitempty"`
}

// NewRecord converts a reminder to its storage form.
func NewRecord(reminder models.Reminder) Record {
	record := Record{
		ID:      reminder.ID,
		UID:     reminder.UID,
		Title:   reminder.Title,
		Project: reminder.Project,
		AllDay:  reminder.AllDay,
		Every:   reminder.Repeat.Spec(),
		Count:   reminder.Repeat.Count,
		Tags:    reminder.Tags,
		Notes:   reminder.Notes,
		Created: reminder.Created,
	}

	due := reminder.Due.In(time.Local)
	if reminder.AllDay {
		record.Due = due.Format(allDayLayout)
	} else {
		record.Due = due.Format(dueLayout)
	}
	if reminder.Repeat.Until != nil {
		record.Until = reminder.Repeat.Until.In(time.Local).Format(time.DateOnly)
	}
	if reminder.Notify > 0 {
		record.Notify = reminder.Notify.String()
	}

	if len(reminder.States) > 0 {
		record.Occurrences = make(map[string]StateRecord, len(reminder.States))
		for key, state := range reminder.States {
			record.Occurrences[key] = StateRecord{
				Done:     state.DoneAt,
				Snoozed:  state.SnoozedUntil,
				Activity: state.ActivityStart,
				Skipped:  state.Skipped,
			}
		}
	}
	return record
}

// Reminder converts the record back to a reminder.
func (r Record) Reminder() (models.Reminder, error) {
	layout := dueLayout
	if r.AllDay {
		layout = allDayLayout
	}
	due, err := time.ParseInLocation(layout, r.Due, time.Local)
	if err != nil {
		return models.Reminder{}, errors.Wrapf(err, "parse due of reminder %d", r.ID)
	}

	repeat, err := models.ParseRepeat(r.Every)
	if err != nil {
		return models.Reminder{}, errors.Wrapf(err, "parse repeat of reminder %d", r.ID)
	}
	repeat.Count = r.Count
	if r.Until != "" {
		until, parseErr := time.ParseInLocation(time.DateOnly, r.Until, time.Local)
		if parseErr != nil {
			return models.Reminder{}, errors.Wrapf(parseErr, "parse until of reminder %d", r.ID)
		}
		repeat.Until = &until
	}

	var notify time.Duration
	if r.Notify != "" {
		if notify, err = time.ParseDuration(r.Notify); err != nil {
			return models.Reminder{}, errors.Wrapf(err, "parse notify of reminder %d", r.ID)
		}
	}

	reminder := models.Reminder{
		ID:      r.ID,
		UID:     r.UID,
		Title:   r.Title,
		Project: r.Project,
		Due:     due,
		AllDay:  r.AllDay,
		Repeat:  repeat,
		Notify:  notify,
		Tags:    r.Tags,
		Notes:   r.Notes,
		Created: r.Created,
	}
	if len(r.Occurrences) > 0 {
		reminder.States = make(map[string]models.OccurrenceState, len(r.Occurrences))
		for key, state := range r.Occurrences {
			reminder.States[key] = models.OccurrenceState{
				DoneAt:        state.Done,
				SnoozedUntil:  state.Snoozed,
				ActivityStart: state.Activity,
				Skipped:       state.Skipped,
			}
		}
	}
	return reminder, nil
}
