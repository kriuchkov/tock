package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/adapters/repositories/reminderrecord"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
)

// ReminderRepository keeps reminders in their own table of the activity database.
type ReminderRepository struct {
	DB *sql.DB
}

func NewReminderRepository(ctx context.Context, db *sql.DB) (*ReminderRepository, error) {
	repo := &ReminderRepository{DB: db}
	if err := repo.initSchema(ctx); err != nil {
		return nil, errors.Wrap(err, "initialize reminders schema")
	}
	return repo, nil
}

// AUTOINCREMENT keeps the ID of a removed reminder from being given out again.
func (r *ReminderRepository) initSchema(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS reminders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		project TEXT NOT NULL DEFAULT '',
		due TEXT NOT NULL,
		all_day INTEGER NOT NULL DEFAULT 0,
		every TEXT NOT NULL DEFAULT '',
		"until" TEXT NOT NULL DEFAULT '',
		"count" INTEGER NOT NULL DEFAULT 0,
		notify TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '[]',
		notes TEXT NOT NULL DEFAULT '',
		created TEXT NOT NULL DEFAULT '',
		occurrences TEXT NOT NULL DEFAULT '{}'
	);
	`
	_, err := r.DB.ExecContext(ctx, query)
	return err
}

func (r *ReminderRepository) List(ctx context.Context) ([]models.Reminder, error) {
	query := `
	SELECT id, title, project, due, all_day, every, "until", "count", notify, tags, notes, created, occurrences
	FROM reminders
	ORDER BY id
	`
	rows, err := r.DB.QueryContext(ctx, query)
	if err != nil {
		return nil, errors.Wrap(err, "query reminders")
	}
	defer rows.Close()

	var result []models.Reminder
	for rows.Next() {
		reminder, scanErr := scanReminder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, reminder)
	}
	if err = rows.Err(); err != nil {
		return nil, errors.Wrap(err, "iterate reminders")
	}
	return result, nil
}

func (r *ReminderRepository) Save(ctx context.Context, reminder models.Reminder) (*models.Reminder, error) {
	record := reminderrecord.NewRecord(reminder)

	tags, err := json.Marshal(record.Tags)
	if err != nil {
		return nil, errors.Wrap(err, "serialize reminder tags")
	}
	occurrences, err := json.Marshal(record.Occurrences)
	if err != nil {
		return nil, errors.Wrap(err, "serialize reminder occurrences")
	}

	args := []any{
		record.Title, record.Project, record.Due, record.AllDay, record.Every, record.Until, record.Count,
		record.Notify, string(tags), record.Notes, record.Created.Format(time.RFC3339Nano), string(occurrences),
	}

	if reminder.ID == 0 {
		query := `
		INSERT INTO reminders (title, project, due, all_day, every, "until", "count", notify, tags, notes, created, occurrences)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		res, execErr := r.DB.ExecContext(ctx, query, args...)
		if execErr != nil {
			return nil, errors.Wrap(execErr, "insert reminder")
		}
		id, idErr := res.LastInsertId()
		if idErr != nil {
			return nil, errors.Wrap(idErr, "read reminder id")
		}
		reminder.ID = int(id)
		return &reminder, nil
	}

	query := `
	INSERT INTO reminders (id, title, project, due, all_day, every, "until", "count", notify, tags, notes, created, occurrences)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		title=excluded.title, project=excluded.project, due=excluded.due, all_day=excluded.all_day,
		every=excluded.every, "until"=excluded."until", "count"=excluded."count", notify=excluded.notify,
		tags=excluded.tags, notes=excluded.notes, created=excluded.created, occurrences=excluded.occurrences
	`
	if _, err = r.DB.ExecContext(ctx, query, append([]any{reminder.ID}, args...)...); err != nil {
		return nil, errors.Wrap(err, "save reminder")
	}
	return &reminder, nil
}

func (r *ReminderRepository) Remove(ctx context.Context, id int) error {
	res, err := r.DB.ExecContext(ctx, `DELETE FROM reminders WHERE id = ?`, id)
	if err != nil {
		return errors.Wrap(err, "delete reminder")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return errors.Wrap(err, "read deleted rows")
	}
	if affected == 0 {
		return coreErrors.ErrReminderNotFound
	}
	return nil
}

func scanReminder(rows *sql.Rows) (models.Reminder, error) {
	var (
		record      reminderrecord.Record
		tags        string
		created     string
		occurrences string
	)
	err := rows.Scan(
		&record.ID, &record.Title, &record.Project, &record.Due, &record.AllDay, &record.Every, &record.Until,
		&record.Count, &record.Notify, &tags, &record.Notes, &created, &occurrences,
	)
	if err != nil {
		return models.Reminder{}, errors.Wrap(err, "scan reminder")
	}

	if tags != "" {
		if err = json.Unmarshal([]byte(tags), &record.Tags); err != nil {
			return models.Reminder{}, errors.Wrap(err, "unmarshal reminder tags")
		}
	}
	if occurrences != "" {
		if err = json.Unmarshal([]byte(occurrences), &record.Occurrences); err != nil {
			return models.Reminder{}, errors.Wrap(err, "unmarshal reminder occurrences")
		}
	}
	if created != "" {
		if record.Created, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return models.Reminder{}, errors.Wrap(err, "parse reminder creation time")
		}
	}
	return record.Reminder()
}
