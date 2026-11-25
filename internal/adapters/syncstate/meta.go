package syncstate

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/journal"
	"github.com/kriuchkov/tock/internal/core/ports"
)

// The names of the meta rows. Every layout keeps a meta table of name and value with dir, device,
// clock, backend_name and backend_path as they are here, so a rebuild can carry them over.
const (
	metaDir          = "dir"
	metaDevice       = "device"
	metaClock        = "clock"
	metaBackendName  = "backend_name"
	metaBackendPath  = "backend_path"
	metaMatchBackend = "match_backend"
	metaToken        = "token"
	metaLastSync     = "last_sync"
	metaSkipped      = "skipped"
	metaAutoStops    = "auto_stops"
)

// autoStopJSON is a ports.SyncAutoStop as the auto_stops row keeps it.
type autoStopJSON struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Project     string    `json:"project"`
	Description string    `json:"description"`
	Next        time.Time `json:"next"`
}

func (s *state) Meta(ctx context.Context) (ports.SyncMeta, error) {
	var meta ports.SyncMeta
	err := s.read(ctx, func(ctx context.Context, db *sql.DB) error {
		values, err := readMeta(ctx, db)
		if err != nil {
			return err
		}
		meta, err = decodeMeta(values)
		return err
	})
	return meta, err
}

// readMeta returns the meta rows by name.
func readMeta(ctx context.Context, q queryer) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT name, value FROM meta")
	if err != nil {
		return nil, errors.Wrap(err, "read sync meta")
	}
	defer rows.Close()

	values := make(map[string]string)
	for rows.Next() {
		var name, value string
		if err = rows.Scan(&name, &value); err != nil {
			return nil, errors.Wrap(err, "read sync meta")
		}
		values[name] = value
	}
	if err = rows.Err(); err != nil {
		return nil, errors.Wrap(err, "read sync meta")
	}
	return values, nil
}

// putMeta writes meta rows, replacing those of the same names.
func putMeta(ctx context.Context, tx *sql.Tx, values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO meta (name, value) VALUES (?, ?)")
	if err != nil {
		return errors.Wrap(err, "write sync meta")
	}
	defer stmt.Close()

	for name, value := range values {
		if _, err = stmt.ExecContext(ctx, name, value); err != nil {
			return errors.Wrap(err, "write sync meta")
		}
	}
	return nil
}

// encodeMeta returns the meta rows of meta; a zero value is written as an empty string.
func encodeMeta(meta ports.SyncMeta) (map[string]string, error) {
	values := map[string]string{
		metaDir:          meta.Dir,
		metaDevice:       meta.Device,
		metaClock:        "",
		metaBackendName:  meta.Backend.Name,
		metaBackendPath:  meta.Backend.Path,
		metaMatchBackend: "",
		metaToken:        "",
		metaLastSync:     "",
		metaSkipped:      strconv.Itoa(meta.Skipped),
		metaAutoStops:    "",
	}
	if len(meta.AutoStops) > 0 {
		stops := make([]autoStopJSON, 0, len(meta.AutoStops))
		for _, stop := range meta.AutoStops {
			stops = append(stops, autoStopJSON(stop))
		}
		encoded, err := json.Marshal(stops)
		if err != nil {
			return nil, errors.Wrap(err, "encode auto-stops")
		}
		values[metaAutoStops] = string(encoded)
	}
	if meta.Clock != (journal.HLC{}) {
		values[metaClock] = meta.Clock.String()
	}
	if meta.MatchBackend {
		values[metaMatchBackend] = "1"
	}
	if meta.Token != nil {
		token, err := json.Marshal(map[string]string(meta.Token))
		if err != nil {
			return nil, errors.Wrap(err, "encode backend token")
		}
		values[metaToken] = string(token)
	}
	if !meta.LastSync.IsZero() {
		values[metaLastSync] = meta.LastSync.UTC().Format(time.RFC3339Nano)
	}
	return values, nil
}

// decodeMeta reads the meta rows; a missing or empty one is the zero value.
func decodeMeta(values map[string]string) (ports.SyncMeta, error) {
	meta := ports.SyncMeta{
		Dir:          values[metaDir],
		Device:       values[metaDevice],
		Backend:      ports.SyncBackendIdentity{Name: values[metaBackendName], Path: values[metaBackendPath]},
		MatchBackend: values[metaMatchBackend] != "",
	}
	var err error
	if clock := values[metaClock]; clock != "" {
		if meta.Clock, err = journal.ParseHLC(clock); err != nil {
			return ports.SyncMeta{}, errors.Wrap(err, "read sync clock")
		}
	}
	if token := values[metaToken]; token != "" {
		if err = json.Unmarshal([]byte(token), &meta.Token); err != nil {
			return ports.SyncMeta{}, errors.Wrap(err, "read backend token")
		}
	}
	if lastSync := values[metaLastSync]; lastSync != "" {
		if meta.LastSync, err = time.Parse(time.RFC3339Nano, lastSync); err != nil {
			return ports.SyncMeta{}, errors.Wrap(err, "read last sync time")
		}
	}
	if skipped := values[metaSkipped]; skipped != "" {
		if meta.Skipped, err = strconv.Atoi(skipped); err != nil {
			return ports.SyncMeta{}, errors.Wrap(err, "read skipped line count")
		}
	}
	if stops := values[metaAutoStops]; stops != "" {
		var decoded []autoStopJSON
		if err = json.Unmarshal([]byte(stops), &decoded); err != nil {
			return ports.SyncMeta{}, errors.Wrap(err, "read auto-stops")
		}
		for _, stop := range decoded {
			meta.AutoStops = append(meta.AutoStops, ports.SyncAutoStop(stop))
		}
	}
	return meta, nil
}
