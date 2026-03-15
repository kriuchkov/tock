package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/go-faster/errors"
)

type NotesRepository struct {
	DB *sql.DB
}

func NewNotesRepository(db *sql.DB) *NotesRepository {
	return &NotesRepository{DB: db}
}

func (r *NotesRepository) Save(ctx context.Context, _ string, date time.Time, notes string, tags []string) error {
	var tagsString *string

	if len(tags) > 0 {
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return errors.Wrap(err, "serialize tags")
		}
		jsonStr := string(tagsJSON)
		tagsString = &jsonStr
	} else {
		emptyList := "[]"
		tagsString = &emptyList
	}

	query := `UPDATE activities SET notes = ?, tags = ? WHERE start_time = ?;`

	_, err := r.DB.ExecContext(ctx, query, notes, tagsString, date.UTC())
	if err != nil {
		return errors.Wrap(err, "update activity notes and tags")
	}
	return nil
}

func (r *NotesRepository) Get(ctx context.Context, _ string, date time.Time) (string, []string, error) {
	query := `SELECT notes, tags FROM activities WHERE start_time = ? LIMIT 1;`
	row := r.DB.QueryRowContext(ctx, query, date.UTC())

	var notesString sql.NullString
	var tagsString sql.NullString

	err := row.Scan(&notesString, &tagsString)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, nil
		}

		return "", nil, errors.Wrap(err, "scan notes and tags")
	}

	var notes string
	if notesString.Valid {
		notes = notesString.String
	}

	var tags []string
	if tagsString.Valid && tagsString.String != "" {
		if err = json.Unmarshal([]byte(tagsString.String), &tags); err != nil {
			return "", nil, errors.Wrap(err, "unmarshal activity tags")
		}
	}
	return notes, tags, nil
}
