package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/doug-martin/goqu/v9"
	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/dto"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
)

type Repository struct {
	db *sql.DB
}

func NewSQLiteActivityRepository(ctx context.Context, dataSourceName string) (*Repository, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, errors.Wrap(err, "open database")
	}

	if pingErr := db.PingContext(ctx); pingErr != nil {
		return nil, errors.Wrap(pingErr, "ping database")
	}

	repo := &Repository{db: db}
	if initErr := repo.initSchema(ctx); initErr != nil {
		return nil, errors.Wrap(initErr, "initialize schema")
	}

	return repo, nil
}

func (r *Repository) initSchema(ctx context.Context) error {
	query := `
	CREATE TABLE IF NOT EXISTS activities (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		description TEXT NOT NULL,
		project TEXT NOT NULL,
		start_time DATETIME NOT NULL UNIQUE,
		end_time DATETIME,
		notes TEXT,
		tags TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_start_time ON activities(start_time);
	`
	_, err := r.db.ExecContext(ctx, query)
	return err
}

func (r *Repository) Save(ctx context.Context, activity models.Activity) error {
	tagsJSON, err := json.Marshal(activity.Tags)
	if err != nil {
		return errors.Wrap(err, "serialize tags")
	}

	query := `
	INSERT INTO activities (description, project, start_time, end_time, notes, tags)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(start_time) DO UPDATE SET
		description=excluded.description,
		project=excluded.project,
		end_time=excluded.end_time,
		notes=excluded.notes,
		tags=excluded.tags;
	`
	_, err = r.db.ExecContext(ctx, query,
		activity.Description,
		activity.Project,
		activity.StartTime.UTC(),
		activity.EndTime,
		activity.Notes,
		string(tagsJSON),
	)
	if err != nil {
		return errors.Wrap(err, "save activity")
	}
	return nil
}

func (r *Repository) FindLast(ctx context.Context) (*models.Activity, error) {
	query := `
	SELECT description, project, start_time, end_time, notes, tags
	FROM activities
	ORDER BY start_time DESC
	LIMIT 1
	`
	row := r.db.QueryRowContext(ctx, query)
	return scanActivity(row)
}

func (r *Repository) Find(ctx context.Context, filter dto.ActivityFilter) ([]models.Activity, error) {
	dialect := goqu.Dialect("sqlite3")
	dataset := dialect.From("activities").
		Select("description", "project", "start_time", "end_time", "notes", "tags").
		Order(goqu.I("start_time").Asc())

	if filter.FromDate != nil {
		dataset = dataset.Where(goqu.I("start_time").Gte(filter.FromDate.UTC()))
	}
	if filter.ToDate != nil {
		dataset = dataset.Where(goqu.I("start_time").Lte(filter.ToDate.UTC()))
	}
	if filter.Project != nil && *filter.Project != "" {
		dataset = dataset.Where(goqu.Ex{"project": *filter.Project})
	}
	if filter.Description != nil && *filter.Description != "" {
		dataset = dataset.Where(goqu.I("description").Like("%" + *filter.Description + "%"))
	}
	if filter.IsRunning != nil {
		if *filter.IsRunning {
			dataset = dataset.Where(goqu.I("end_time").IsNull())
		} else {
			dataset = dataset.Where(goqu.I("end_time").IsNotNull())
		}
	}

	query, args, err := dataset.Prepared(true).ToSQL()
	if err != nil {
		return nil, errors.Wrap(err, "build query")
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "find activities")
	}
	defer rows.Close()

	var activities []models.Activity
	for rows.Next() {
		activity, scanErr := scanActivity(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if activity != nil {
			activities = append(activities, *activity)
		}
	}

	if iterErr := rows.Err(); iterErr != nil {
		return nil, errors.Wrap(iterErr, "error iterating over activities")
	}

	return activities, nil
}

func (r *Repository) Remove(ctx context.Context, activity models.Activity) error {
	query := `DELETE FROM activities WHERE start_time = ?`
	_, err := r.db.ExecContext(ctx, query, activity.StartTime.UTC())
	if err != nil {
		return errors.Wrap(err, "remove activity")
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanActivity(s scanner) (*models.Activity, error) {
	var act models.Activity
	var endTime sql.NullTime
	var tagsString sql.NullString
	var notesString sql.NullString

	err := s.Scan(
		&act.Description,
		&act.Project,
		&act.StartTime,
		&endTime,
		&notesString,
		&tagsString,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, coreErrors.ErrActivityNotFound
		}
		return nil, errors.Wrap(err, "scan activity")
	}

	act.StartTime = act.StartTime.Local()

	if endTime.Valid {
		localEndTime := endTime.Time.Local()
		act.EndTime = &localEndTime
	}

	if notesString.Valid {
		act.Notes = notesString.String
	}

	if tagsString.Valid && tagsString.String != "" {
		if unmarshalErr := json.Unmarshal([]byte(tagsString.String), &act.Tags); unmarshalErr != nil {
			return nil, errors.Wrap(unmarshalErr, "failed to unmarshal tags")
		}
	}

	return &act, nil
}
