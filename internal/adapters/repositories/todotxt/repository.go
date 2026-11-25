package todotxt

import (
	"bufio"
	"context"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/adapters/repositories/fileio"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

type repository struct {
	filePath string
}

func NewRepository(filePath string) ports.ActivityRepository {
	return &repository{filePath: filePath}
}

func (r *repository) Find(_ context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
	lines, err := r.readLines()
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Activity{}, nil
		}
		return nil, errors.Wrap(err, "read lines")
	}

	activities := make([]models.Activity, 0, len(lines))
	for _, line := range lines {
		activity, parseErr := ParseActivity(line)
		if parseErr != nil {
			continue
		}
		if activity == nil {
			continue
		}
		if filter.Project != nil && activity.Project != *filter.Project {
			continue
		}
		if filter.Description != nil && activity.Description != *filter.Description {
			continue
		}
		if !overlapsDateRange(*activity, filter.FromDate, filter.ToDate) {
			continue
		}
		if filter.IsRunning != nil {
			if *filter.IsRunning && activity.EndTime != nil {
				continue
			}
			if !*filter.IsRunning && activity.EndTime == nil {
				continue
			}
		}
		activities = append(activities, *activity)
	}

	return activities, nil
}

func (r *repository) FindLast(_ context.Context) (*models.Activity, error) {
	lines, err := r.readLines()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, coreErrors.ErrActivityNotFound
		}
		return nil, errors.Wrap(err, "read lines")
	}

	var lastActivity *models.Activity
	for _, line := range lines {
		activity, parseErr := ParseActivity(line)
		if parseErr != nil || activity == nil {
			continue
		}
		if lastActivity == nil || activity.StartTime.After(lastActivity.StartTime) {
			lastActivity = activity
		}
	}

	if lastActivity == nil {
		return nil, coreErrors.ErrActivityNotFound
	}
	return lastActivity, nil
}

func (r *repository) Save(_ context.Context, activity models.Activity) error {
	return fileio.Update(r.filePath, func() error { return r.save(activity) })
}

func (r *repository) save(activity models.Activity) error {
	lines, err := r.readLines()
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrap(err, "read lines")
		}
		lines = []string{}
	}

	formatted := FormatActivity(activity)
	updated := false
	for i, v := range slices.Backward(lines) {
		parsed, parseErr := ParseActivity(v)
		if parseErr != nil || parsed == nil {
			continue
		}
		if parsed.StartTime.Equal(activity.StartTime) {
			lines[i] = formatted
			updated = true
			break
		}
	}

	if !updated {
		lines = append(lines, formatted)
	}

	if err = r.writeLines(lines); err != nil {
		return errors.Wrap(err, "write lines")
	}
	return nil
}

func (r *repository) Remove(_ context.Context, activity models.Activity) error {
	return fileio.Update(r.filePath, func() error { return r.remove(activity) })
}

func (r *repository) remove(activity models.Activity) error {
	lines, err := r.readLines()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrap(err, "read lines")
	}

	newLines := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		parsed, parseErr := ParseActivity(line)
		if parseErr != nil || parsed == nil {
			newLines = append(newLines, line)
			continue
		}
		if parsed.StartTime.Equal(activity.StartTime) {
			removed = true
			continue
		}
		newLines = append(newLines, line)
	}

	if !removed {
		return errors.New("activity not found")
	}

	if err = r.writeLines(newLines); err != nil {
		return errors.Wrap(err, "write lines")
	}
	return nil
}

var _ ports.ActivityBatchWriter = (*repository)(nil)

// Apply applies writes in order under one lock, with one read and one write of the file; see
// ports.ActivityBatchWriter. A line matches Old when it reads back as Old. A new line goes to the end
// of the file.
func (r *repository) Apply(_ context.Context, writes []ports.ActivityWrite) error {
	if len(writes) == 0 {
		return nil
	}
	return fileio.Update(r.filePath, func() error {
		lines, err := r.readLines()
		if err != nil {
			if !os.IsNotExist(err) {
				return errors.Wrap(err, "read lines")
			}
			lines = []string{}
		}
		parsed := make([]*models.Activity, len(lines))
		for i, line := range lines {
			parsed[i], _ = ParseActivity(line)
		}
		for _, w := range writes {
			if lines, parsed, err = applyWrite(lines, parsed, w); err != nil {
				return err
			}
		}
		if err = r.writeLines(lines); err != nil {
			return errors.Wrap(err, "write lines")
		}
		return nil
	})
}

// applyWrite applies one write to the lines of a file and to what they read as. A removed line
// becomes blank, which writeLines leaves out.
func applyWrite(lines []string, parsed []*models.Activity, w ports.ActivityWrite) ([]string, []*models.Activity, error) {
	if w.Old == nil {
		if w.New == nil {
			return lines, parsed, nil
		}
		line := FormatActivity(*w.New)
		activity, _ := ParseActivity(line)
		return append(lines, line), append(parsed, activity), nil
	}
	at := slices.IndexFunc(parsed, func(a *models.Activity) bool { return a != nil && sameLine(*a, *w.Old) })
	if at < 0 {
		return nil, nil, errors.Wrapf(coreErrors.ErrActivityNotFound, "the activity at %s changed", w.Old.StartTime.Format(time.RFC3339))
	}
	if w.New == nil {
		lines[at], parsed[at] = "", nil
		return lines, parsed, nil
	}
	lines[at] = FormatActivity(*w.New)
	parsed[at], _ = ParseActivity(lines[at])
	return lines, parsed, nil
}

// sameLine reports whether a line read back as a is the activity b: the same start, end, project and
// description.
func sameLine(a, b models.Activity) bool {
	sameEnd := (a.EndTime == nil) == (b.EndTime == nil) && (a.EndTime == nil || a.EndTime.Equal(*b.EndTime))
	return a.StartTime.Equal(b.StartTime) && sameEnd && a.Project == b.Project && a.Description == b.Description
}

// Normalize returns an activity as the file gives it back once saved: its times at the second, and its
// project and description as a line keeps them. Notes, tags and the uid pass through: the sync bridge
// keeps notes and tags in the sidecar too, which wins over the line.
func Normalize(a models.Activity) models.Activity {
	parsed, err := ParseActivity(FormatActivity(a))
	if err != nil || parsed == nil {
		return a
	}
	parsed.Notes, parsed.Tags, parsed.UID = a.Notes, a.Tags, a.UID
	return *parsed
}

func overlapsDateRange(act models.Activity, fromDate, toDate *time.Time) bool {
	actEnd := time.Now()
	if act.EndTime != nil {
		actEnd = *act.EndTime
	}

	if fromDate != nil && !actEnd.After(*fromDate) {
		return false
	}
	if toDate != nil && !act.StartTime.Before(*toDate) {
		return false
	}
	return true
}

func (r *repository) readLines() ([]string, error) {
	f, err := os.Open(r.filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	lines := make([]string, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "scan file")
	}
	return lines, nil
}

func (r *repository) writeLines(lines []string) error {
	var sb strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return fileio.WriteAtomic(r.filePath, []byte(sb.String()))
}
