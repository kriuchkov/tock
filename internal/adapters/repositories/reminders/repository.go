package reminders

import (
	"context"
	"os"
	"slices"

	"github.com/go-faster/errors"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/kriuchkov/tock/internal/adapters/repositories/fileio"
	"github.com/kriuchkov/tock/internal/adapters/repositories/reminderrecord"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// repository keeps reminders in one YAML file next to the activity log.
type repository struct {
	path string
}

// NewRepository returns a reminder store backed by the YAML file at path.
func NewRepository(path string) ports.ReminderRepository {
	return &repository{path: path}
}

type document struct {
	// NextID is never lowered, so the ID of a removed reminder is not given out again.
	NextID    int                     `yaml:"next_id"`
	Reminders []reminderrecord.Record `yaml:"reminders"`
}

func (r *repository) List(_ context.Context) ([]models.Reminder, error) {
	doc, err := r.load()
	if err != nil {
		return nil, err
	}

	reminders := make([]models.Reminder, 0, len(doc.Reminders))
	for _, record := range doc.Reminders {
		reminder, convErr := record.Reminder()
		if convErr != nil {
			return nil, convErr
		}
		reminders = append(reminders, reminder)
	}
	return reminders, nil
}

func (r *repository) Save(_ context.Context, reminder models.Reminder) (*models.Reminder, error) {
	var saved *models.Reminder
	err := fileio.Update(r.path, func() error {
		var saveErr error
		saved, saveErr = r.save(reminder)
		return saveErr
	})
	return saved, err
}

func (r *repository) save(reminder models.Reminder) (*models.Reminder, error) {
	doc, err := r.load()
	if err != nil {
		return nil, err
	}

	if reminder.ID == 0 {
		reminder.ID = doc.nextID()
	}
	doc.NextID = max(doc.NextID, reminder.ID+1)

	index := slices.IndexFunc(doc.Reminders, func(record reminderrecord.Record) bool { return record.ID == reminder.ID })
	if index >= 0 && reminder.UID == "" {
		reminder.UID = doc.Reminders[index].UID
	}
	record := reminderrecord.NewRecord(reminder)
	if index < 0 {
		index = len(doc.Reminders)
		doc.Reminders = append(doc.Reminders, record)
	} else {
		doc.Reminders[index] = record
	}

	if err = r.store(&doc); err != nil {
		return nil, err
	}
	reminder.UID = doc.Reminders[index].UID
	return &reminder, nil
}

func (r *repository) Remove(_ context.Context, id int) error {
	return fileio.Update(r.path, func() error { return r.remove(id) })
}

func (r *repository) remove(id int) error {
	doc, err := r.load()
	if err != nil {
		return err
	}

	kept := doc.Reminders[:0]
	for _, record := range doc.Reminders {
		if record.ID != id {
			kept = append(kept, record)
		}
	}
	if len(kept) == len(doc.Reminders) {
		return coreErrors.ErrReminderNotFound
	}
	doc.Reminders = kept
	return r.store(&doc)
}

func (d document) nextID() int {
	next := max(d.NextID, 1)
	for _, record := range d.Reminders {
		next = max(next, record.ID+1)
	}
	return next
}

func (r *repository) load() (document, error) {
	payload, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return document{}, nil
		}
		return document{}, errors.Wrap(err, "read reminders")
	}

	var doc document
	if err = yaml.Unmarshal(payload, &doc); err != nil {
		return document{}, errors.Wrap(err, "decode reminders")
	}
	return doc, nil
}

// store writes the file atomically, so a reader never sees half of it. It first gives a uid to every
// reminder without one: reminders from before uids get theirs on the first write.
func (r *repository) store(doc *document) error {
	for i := range doc.Reminders {
		if doc.Reminders[i].UID != "" {
			continue
		}
		uid, err := uuid.NewV7()
		if err != nil {
			return errors.Wrap(err, "make reminder uid")
		}
		doc.Reminders[i].UID = uid.String()
	}

	payload, err := yaml.Marshal(doc)
	if err != nil {
		return errors.Wrap(err, "encode reminders")
	}
	if err = fileio.WriteAtomic(r.path, payload); err != nil {
		return errors.Wrap(err, "write reminders")
	}
	return nil
}
