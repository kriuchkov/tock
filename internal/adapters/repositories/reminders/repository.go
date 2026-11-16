package reminders

import (
	"context"
	"os"
	"path/filepath"

	"github.com/go-faster/errors"
	"gopkg.in/yaml.v3"

	"github.com/kriuchkov/tock/internal/adapters/repositories/reminderrecord"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	directoryMode = 0750
	fileMode      = 0644
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
	doc, err := r.load()
	if err != nil {
		return nil, err
	}

	if reminder.ID == 0 {
		reminder.ID = doc.nextID()
	}
	doc.NextID = max(doc.NextID, reminder.ID+1)

	record := reminderrecord.NewRecord(reminder)
	replaced := false
	for i := range doc.Reminders {
		if doc.Reminders[i].ID == reminder.ID {
			doc.Reminders[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		doc.Reminders = append(doc.Reminders, record)
	}

	if err = r.store(doc); err != nil {
		return nil, err
	}
	return &reminder, nil
}

func (r *repository) Remove(_ context.Context, id int) error {
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
	return r.store(doc)
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

// store writes the file through a temporary sibling and a rename, so a reader never sees half of it.
func (r *repository) store(doc document) error {
	payload, err := yaml.Marshal(doc)
	if err != nil {
		return errors.Wrap(err, "encode reminders")
	}

	dir := filepath.Dir(r.path)
	if err = os.MkdirAll(dir, directoryMode); err != nil {
		return errors.Wrap(err, "create reminders directory")
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(r.path)+".*.tmp")
	if err != nil {
		return errors.Wrap(err, "create temporary reminders file")
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err = tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return errors.Wrap(err, "write reminders")
	}
	if err = tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return errors.Wrap(err, "chmod reminders")
	}
	if err = tmp.Close(); err != nil {
		return errors.Wrap(err, "close reminders")
	}
	if err = os.Rename(tmp.Name(), r.path); err != nil {
		return errors.Wrap(err, "replace reminders")
	}
	return nil
}
