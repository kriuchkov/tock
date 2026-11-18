package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/go-faster/errors"
	"github.com/google/uuid"
)

// newUID returns a new record uid, a lowercase UUIDv7 (docs/sync.md).
func newUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// ensureUIDs adds the uid column to a table made before uids and gives every row without a uid a new
// one, in one transaction. table is one of this package's table names, never user input. Another
// process may open the same old database at once: a column it added and rows it filled are kept.
func ensureUIDs(ctx context.Context, db *sql.DB, table string) error {
	has, err := hasUIDColumn(ctx, db, table)
	if err != nil {
		return err
	}
	if !has {
		alter := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN uid TEXT NOT NULL DEFAULT ''`, table)
		if _, alterErr := db.ExecContext(ctx, alter); alterErr != nil {
			if has, err = hasUIDColumn(ctx, db, table); err != nil || !has {
				return errors.Wrapf(alterErr, "add uid to %s", table)
			}
		}
	}

	ids, err := rowsWithoutUID(ctx, db, table)
	if err != nil || len(ids) == 0 {
		return err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrapf(err, "begin filling uids in %s", table)
	}
	defer func() { _ = tx.Rollback() }()

	update := fmt.Sprintf(`UPDATE %s SET uid = ? WHERE id = ? AND uid = ''`, table) //nolint:gosec // table is a constant
	for _, id := range ids {
		uid, uidErr := newUID()
		if uidErr != nil {
			return errors.Wrap(uidErr, "make uid")
		}
		if _, err = tx.ExecContext(ctx, update, uid, id); err != nil {
			return errors.Wrapf(err, "set uid in %s", table)
		}
	}
	if err = tx.Commit(); err != nil {
		return errors.Wrapf(err, "commit uids in %s", table)
	}
	return nil
}

func hasUIDColumn(ctx context.Context, db *sql.DB, table string) (bool, error) {
	var columns int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'uid'`, table).Scan(&columns)
	if err != nil {
		return false, errors.Wrapf(err, "inspect %s", table)
	}
	return columns > 0, nil
}

func rowsWithoutUID(ctx context.Context, db *sql.DB, table string) ([]int64, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT id FROM %s WHERE uid = ''`, table))
	if err != nil {
		return nil, errors.Wrapf(err, "find rows without uid in %s", table)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, errors.Wrap(err, "scan row id")
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, errors.Wrap(err, "iterate rows without uid")
	}
	return ids, nil
}
