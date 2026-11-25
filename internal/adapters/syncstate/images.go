package syncstate

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

func (s *state) Images(ctx context.Context, typ journal.RecordType) ([]ports.SyncImage, error) {
	return s.images(ctx, typ, "SELECT uid, backend_key, fields FROM images WHERE type = ? ORDER BY uid", string(typ))
}

func (s *state) ImagesByUID(ctx context.Context, typ journal.RecordType, uids []string) ([]ports.SyncImage, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	list, err := jsonList(uids)
	if err != nil {
		return nil, err
	}
	return s.images(ctx, typ,
		"SELECT uid, backend_key, fields FROM images WHERE type = ? AND uid IN (SELECT value FROM json_each(?)) ORDER BY uid",
		string(typ), list)
}

func (s *state) ImagesByKey(ctx context.Context, typ journal.RecordType, keys []string) ([]ports.SyncImage, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	list, err := jsonList(keys)
	if err != nil {
		return nil, err
	}
	// Ordered here rather than in SQL, which would read every image in uid order instead of using the
	// index on keys.
	images, err := s.images(ctx, typ,
		"SELECT uid, backend_key, fields FROM images INDEXED BY images_by_key "+
			"WHERE type = ? AND backend_key IN (SELECT value FROM json_each(?))",
		string(typ), list)
	slices.SortFunc(images, func(a, b ports.SyncImage) int { return cmp.Compare(a.UID, b.UID) })
	return images, err
}

// images runs a query of uid, backend_key and fields.
func (s *state) images(ctx context.Context, typ journal.RecordType, query string, args ...any) ([]ports.SyncImage, error) {
	var images []ports.SyncImage
	err := s.read(ctx, func(ctx context.Context, db *sql.DB) error {
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return errors.Wrap(err, "read sync images")
		}
		defer rows.Close()

		for rows.Next() {
			image := ports.SyncImage{Type: typ}
			var fields sql.RawBytes
			if err = rows.Scan(&image.UID, &image.Key, &fields); err != nil {
				return errors.Wrap(err, "read sync images")
			}
			if err = json.Unmarshal(fields, &image.Fields); err != nil {
				return errors.Wrapf(err, "read sync image %s", image.UID)
			}
			images = append(images, image)
		}
		return wrapRowsErr(rows, "read sync images")
	})
	return images, err
}

// saveImages removes the dropped images, then replaces images.
func saveImages(ctx context.Context, tx *sql.Tx, images []ports.SyncImage, drops []ports.SyncRecordRef) error {
	dropped := make(map[journal.RecordType][]string)
	for _, ref := range drops {
		dropped[ref.Type] = append(dropped[ref.Type], ref.UID)
	}
	for typ, uids := range dropped {
		if err := execList(ctx, tx, "DELETE FROM images WHERE type = ? AND uid IN (SELECT value FROM json_each(?))",
			uids, string(typ)); err != nil {
			return errors.Wrap(err, "drop sync images")
		}
	}
	if len(images) == 0 {
		return nil
	}

	put := newBatch(tx, "INSERT OR REPLACE INTO images (type, uid, backend_key, fields)", 4)
	defer put.close()
	var buf bytes.Buffer
	for _, image := range images {
		if image.Type == "" || image.UID == "" {
			return errors.Errorf("save sync image %q of type %q: no type or uid", image.UID, image.Type)
		}
		buf.Reset()
		if err := encodeFields(&buf, image.Fields); err != nil {
			return errors.Wrapf(err, "save sync image %s", image.UID)
		}
		if err := put.add(ctx, string(image.Type), image.UID, image.Key, buf.String()); err != nil {
			return errors.Wrap(err, "save sync images")
		}
	}
	if err := put.flush(ctx); err != nil {
		return errors.Wrap(err, "save sync images")
	}
	return nil
}

// encodeFields writes fields as a JSON object in path order with each value exactly as given, so
// an image reads back byte for byte: json.Marshal would compact values and escape HTML in them.
func encodeFields(buf *bytes.Buffer, fields map[string]json.RawMessage) error {
	paths := make([]string, 0, len(fields))
	for path := range fields {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	buf.WriteByte('{')
	for i, path := range paths {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := enc.Encode(path); err != nil {
			return errors.Wrap(err, "encode field path")
		}
		buf.Truncate(buf.Len() - 1) // Encode ends with a newline.
		buf.WriteByte(':')
		value := fields[path]
		if !json.Valid(value) {
			return errors.Errorf("field %q holds invalid JSON %q", path, value)
		}
		buf.Write(value)
	}
	buf.WriteByte('}')
	return nil
}

func (s *state) Cursors(ctx context.Context) ([]ports.SyncCursor, error) {
	var cursors []ports.SyncCursor
	err := s.read(ctx, func(ctx context.Context, db *sql.DB) error {
		rows, err := db.QueryContext(ctx,
			"SELECT device, month, cursor_offset, file_size, compressed FROM cursors ORDER BY device, month")
		if err != nil {
			return errors.Wrap(err, "read sync cursors")
		}
		defer rows.Close()

		for rows.Next() {
			var cursor ports.SyncCursor
			if err = rows.Scan(&cursor.Device, &cursor.Month, &cursor.Offset, &cursor.Size, &cursor.Compressed); err != nil {
				return errors.Wrap(err, "read sync cursors")
			}
			cursors = append(cursors, cursor)
		}
		return wrapRowsErr(rows, "read sync cursors")
	})
	return cursors, err
}

func saveCursors(ctx context.Context, tx *sql.Tx, cursors []ports.SyncCursor) error {
	if len(cursors) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx,
		"INSERT OR REPLACE INTO cursors (device, month, cursor_offset, file_size, compressed) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return errors.Wrap(err, "save sync cursors")
	}
	defer stmt.Close()

	for _, cursor := range cursors {
		if _, err = stmt.ExecContext(ctx, cursor.Device, cursor.Month, cursor.Offset, cursor.Size, cursor.Compressed); err != nil {
			return errors.Wrap(err, "save sync cursor")
		}
	}
	return nil
}

func (s *state) ReminderIDs(ctx context.Context) (map[string]int, error) {
	ids := make(map[string]int)
	err := s.read(ctx, func(ctx context.Context, db *sql.DB) error {
		rows, err := db.QueryContext(ctx, "SELECT uid, number FROM reminder_ids")
		if err != nil {
			return errors.Wrap(err, "read reminder numbers")
		}
		defer rows.Close()

		for rows.Next() {
			var (
				uid    string
				number int
			)
			if err = rows.Scan(&uid, &number); err != nil {
				return errors.Wrap(err, "read reminder numbers")
			}
			ids[uid] = number
		}
		return wrapRowsErr(rows, "read reminder numbers")
	})
	return ids, err
}

func saveReminderIDs(ctx context.Context, tx *sql.Tx, ids map[string]int) error {
	if len(ids) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO reminder_ids (uid, number) VALUES (?, ?)")
	if err != nil {
		return errors.Wrap(err, "save reminder numbers")
	}
	defer stmt.Close()

	for uid, number := range ids {
		if _, err = stmt.ExecContext(ctx, uid, number); err != nil {
			return errors.Wrap(err, "save reminder number")
		}
	}
	return nil
}
