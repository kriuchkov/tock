package syncstate

import (
	"context"
	"database/sql"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/ports"
)

// largeSave is the number of records and images from which a save empties the WAL after it.
const largeSave = 1000

// Save applies delta in one transaction, in this order: the reset, the meta, the records, the
// cursors, the dropped images, the images and the reminder numbers. Nothing of it is kept when any
// part fails. After a reset or a large save it empties the WAL, which then holds a copy of most of
// the database, so the next small save does not pay for cutting it back.
func (s *state) Save(ctx context.Context, delta ports.SyncDelta) error {
	if err := s.write(ctx, func(ctx context.Context, tx *sql.Tx) error {
		return saveDelta(ctx, tx, delta)
	}); err != nil {
		return err
	}
	if delta.Reset || len(delta.Records)+len(delta.Images) >= largeSave {
		s.checkpoint(ctx)
	}
	return nil
}

// checkpoint copies the WAL into the database and empties it, as far as readers in other processes
// let it. The save is done either way, so it reports nothing.
func (s *state) checkpoint(ctx context.Context) {
	_ = s.read(ctx, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		return err
	})
}

func saveDelta(ctx context.Context, tx *sql.Tx, delta ports.SyncDelta) error {
	if delta.Reset {
		if _, err := tx.ExecContext(ctx, "DELETE FROM fields; DELETE FROM running; DELETE FROM starts; DELETE FROM cursors"); err != nil {
			return errors.Wrap(err, "reset sync records")
		}
	}
	if delta.Meta != nil {
		values, err := encodeMeta(*delta.Meta)
		if err != nil {
			return err
		}
		if err = putMeta(ctx, tx, values); err != nil {
			return err
		}
	}
	if err := saveRecords(ctx, tx, delta.Records, delta.Reset); err != nil {
		return err
	}
	if err := saveCursors(ctx, tx, delta.Cursors); err != nil {
		return err
	}
	if err := saveImages(ctx, tx, delta.Images, delta.DropImages); err != nil {
		return err
	}
	return saveReminderIDs(ctx, tx, delta.ReminderIDs)
}
