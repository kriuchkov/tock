package syncbackend

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/ports"
)

// The parts of a file backend's token.
const (
	// partActivities is the data file of the file and todotxt backends.
	partActivities = "activities"
	// partMonth prefixes a timewarrior month, as in activities/2026-11.
	partMonth = "activities/"
	// partReminders is .tock/reminders.yaml.
	partReminders = "reminders"
	// stampMissing is the value of a part whose file does not exist.
	stampMissing = "none"
)

const (
	// monthLayout names a timewarrior data file, YYYY-MM.data.
	monthLayout = "2006-01"
	monthSuffix = ".data"
	// monthMargin widens the span of a changed timewarrior month. timewarrior files an activity under
	// the UTC month of its start, tock under the month of the start's own zone, so a file can hold
	// starts up to a day past each end of the UTC month.
	monthMargin = 24 * time.Hour
)

// Token returns the size and modification time of the data file (one part per YYYY-MM.data for
// timewarrior) and of reminders.yaml. A file that does not exist is a part too, so its creation or
// removal changes the token; a timewarrior month without a file has no part.
func (f *files) Token(_ context.Context) (ports.SyncToken, error) {
	token := ports.SyncToken{}
	if f.monthly {
		if err := f.addMonths(token); err != nil {
			return nil, err
		}
	} else {
		value, err := stamp(f.identity.Path)
		if err != nil {
			return nil, err
		}
		token[partActivities] = value
	}

	if f.reminders != "" {
		value, err := stamp(f.reminders)
		if err != nil {
			return nil, err
		}
		token[partReminders] = value
	}
	return token, nil
}

// Exists reports whether the data file (the data directory for timewarrior) is there.
func (f *files) Exists(_ context.Context) (bool, error) {
	return exists(f.identity.Path)
}

// exists reports whether a file or directory is at path.
func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case os.IsNotExist(err):
		return false, nil
	default:
		return false, errors.Wrapf(err, "stat %s", path)
	}
}

// Changed compares the parts of two tokens: a changed data file covers every activity, a changed
// timewarrior month the activities that start in it, and a changed reminders.yaml the reminders. A
// part it does not know covers everything.
func (f *files) Changed(_ context.Context, since, now ports.SyncToken) (ports.SyncScope, error) {
	if len(since) == 0 {
		return everything(), nil
	}

	var scope ports.SyncScope
	var spans []ports.SyncSpan
	for _, part := range differingParts(since, now) {
		switch {
		case part == partReminders:
			scope.Reminders = true
		case part == partActivities:
			scope.AllActivities = true
		case strings.HasPrefix(part, partMonth):
			month, ok := parseMonth(strings.TrimPrefix(part, partMonth))
			if !ok {
				return everything(), nil
			}
			spans = append(spans, monthSpan(month))
		default:
			return everything(), nil
		}
	}
	return withSpans(scope, spans), nil
}

// addMonths adds a part for every YYYY-MM.data in the timewarrior data directory. A directory that
// does not exist holds no months.
func (f *files) addMonths(token ports.SyncToken) error {
	entries, err := os.ReadDir(f.identity.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrap(err, "list timewarrior data")
	}

	for _, entry := range entries {
		month, found := strings.CutSuffix(entry.Name(), monthSuffix)
		if _, ok := parseMonth(month); !found || !ok || entry.IsDir() {
			continue
		}
		value, stampErr := stamp(filepath.Join(f.identity.Path, entry.Name()))
		if stampErr != nil {
			return stampErr
		}
		if value != stampMissing {
			token[partMonth+month] = value
		}
	}
	return nil
}

// parseMonth reads a YYYY-MM month; ok is false for anything else.
func parseMonth(text string) (time.Time, bool) {
	if len(text) != len(monthLayout) {
		return time.Time{}, false
	}
	month, err := time.Parse(monthLayout, text)
	return month, err == nil
}

// monthSpan is the start times a timewarrior month file can hold: the UTC month, a day wider on each
// side.
func monthSpan(month time.Time) ports.SyncSpan {
	from := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	return ports.SyncSpan{From: from.Add(-monthMargin), To: from.AddDate(0, 1, 0).Add(monthMargin)}
}

// stamp returns a file's size and modification time as one token value, or stampMissing when the
// file does not exist.
func stamp(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return stampMissing, nil
		}
		return "", errors.Wrapf(err, "stat %s", path)
	}
	return strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10), nil
}
