package file

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
	f, err := os.Open(r.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.Activity{}, nil
		}
		return nil, errors.Wrap(err, "open file")
	}
	defer f.Close()

	var activities []models.Activity
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.TrimSpace(line) == "" {
			continue
		}

		act, parseErr := ParseActivity(line)
		if parseErr != nil {
			continue
		}

		if act == nil {
			continue
		}

		if filter.Project != nil && act.Project != *filter.Project {
			continue
		}
		if filter.Description != nil && act.Description != *filter.Description {
			continue
		}

		if !overlapsDateRange(*act, filter.FromDate, filter.ToDate) {
			continue
		}

		if filter.IsRunning != nil {
			if *filter.IsRunning && act.EndTime != nil {
				continue
			}
			if !*filter.IsRunning && act.EndTime == nil {
				continue
			}
		}

		activities = append(activities, *act)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return activities, errors.Wrap(scanErr, "scan file")
	}
	return activities, nil
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

func (r *repository) FindLast(_ context.Context) (*models.Activity, error) {
	f, err := os.Open(r.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, coreErrors.ErrActivityNotFound
		}
		return nil, errors.Wrap(err, "open file")
	}
	defer f.Close()

	var lastAct *models.Activity
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		act, parseErr := ParseActivity(line)
		if parseErr != nil {
			continue
		}
		if act != nil {
			// Keep the activity with the latest start time
			if lastAct == nil || act.StartTime.After(lastAct.StartTime) {
				lastAct = act
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, errors.Wrap(scanErr, "scan file")
	}
	if lastAct == nil {
		return nil, coreErrors.ErrActivityNotFound
	}
	return lastAct, nil
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
		// File doesn't exist, will be created on write
		lines = []string{}
	}

	// Check if we are updating an existing activity
	updated := false
	// Iterate backwards to find the most recent entry (though StartTime should be unique)
	for i, v := range slices.Backward(lines) {
		if strings.TrimSpace(v) == "" {
			continue
		}
		act, _ := ParseActivity(v)
		// We identify the activity by StartTime.
		// Since file format might have lower precision (minutes), we compare formatted strings or truncated times.
		// Using Unix() comparison for simplicity, assuming minute precision is enough for uniqueness in this context.
		if act != nil && act.StartTime.Unix()/60 == activity.StartTime.Unix()/60 {
			lines[i] = FormatActivity(activity)
			updated = true
			break
		}
	}

	if !updated {
		lines = append(lines, FormatActivity(activity))
	}

	if writeErr := r.writeLines(lines); writeErr != nil {
		return errors.Wrap(writeErr, "write lines")
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

	var newLines []string
	removed := false

	// Iterate over lines.
	// We want to avoid consecutive empty lines if removing an item causes it.
	// We implement a simple cleanup: remove consecutive empty lines.

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// If current line is empty, check if previous was also empty in our new list.
		if trimmed == "" {
			if len(newLines) == 0 {
				continue // Skip leading empty lines
			}
			if strings.TrimSpace(newLines[len(newLines)-1]) == "" {
				continue // Skip duplicate empty line
			}
			newLines = append(newLines, line)
			continue
		}

		act, parseErr := ParseActivity(line)
		if parseErr != nil {
			newLines = append(newLines, line)
			continue
		}

		if act.StartTime.Equal(activity.StartTime) {
			removed = true
			continue
		}
		newLines = append(newLines, line)
	}

	// Remove trailing empty line if it exists to avoid double newlines at EOF
	if len(newLines) > 0 {
		if strings.TrimSpace(newLines[len(newLines)-1]) == "" {
			newLines = newLines[:len(newLines)-1]
		}
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
// ports.ActivityBatchWriter. A line matches Old when it reads back as Old, so of two lines in one
// minute only the one meant is replaced or removed. A new line goes to the end of the file.
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
		if lines, err = applyWrites(lines, writes); err != nil {
			return err
		}
		if err = r.writeLines(lines); err != nil {
			return errors.Wrap(err, "write lines")
		}
		return nil
	})
}

// applyWrites applies writes to the lines of a file. A removed line leaves no run of blank lines
// behind.
func applyWrites(lines []string, writes []ports.ActivityWrite) ([]string, error) {
	parsed := make([]*models.Activity, 0, len(lines))
	for _, line := range lines {
		activity, _ := ParseActivity(line)
		parsed = append(parsed, activity)
	}
	removed := make([]bool, 0, len(lines))
	for range lines {
		removed = append(removed, false)
	}
	for _, w := range writes {
		at := -1
		if w.Old != nil {
			at = slices.IndexFunc(parsed, func(a *models.Activity) bool { return a != nil && sameLine(*a, *w.Old) })
			if at < 0 {
				return nil, errors.Wrapf(
					coreErrors.ErrActivityNotFound,
					"the activity at %s changed",
					w.Old.StartTime.Format(timeLayoutMin),
				)
			}
		}
		switch {
		case w.New == nil && at >= 0:
			parsed[at], removed[at] = nil, true
		case at >= 0:
			lines[at] = FormatActivity(*w.New)
			parsed[at], _ = ParseActivity(lines[at])
		case w.New != nil:
			lines = append(lines, FormatActivity(*w.New))
			activity, _ := ParseActivity(lines[len(lines)-1])
			parsed = append(parsed, activity)
			removed = append(removed, false)
		}
	}
	if !slices.Contains(removed, true) {
		return lines, nil
	}
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		blank := strings.TrimSpace(line) == ""
		switch {
		case removed[i]:
		case blank && (len(kept) == 0 || strings.TrimSpace(kept[len(kept)-1]) == ""):
		default:
			kept = append(kept, line)
		}
	}
	if len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	return kept, nil
}

// sameLine reports whether a line read back as a is the activity b: the same start, end, project and
// description.
func sameLine(a, b models.Activity) bool {
	sameEnd := (a.EndTime == nil) == (b.EndTime == nil) && (a.EndTime == nil || a.EndTime.Equal(*b.EndTime))
	return a.StartTime.Equal(b.StartTime) && sameEnd && a.Project == b.Project && a.Description == b.Description
}

func (r *repository) readLines() ([]string, error) {
	f, err := os.Open(r.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
		return nil, errors.Wrap(err, "open file")
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, errors.Wrap(scanErr, "scan file")
	}
	return lines, nil
}

func (r *repository) writeLines(lines []string) error {
	var sb strings.Builder
	for _, line := range lines {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	return fileio.WriteAtomic(r.filePath, []byte(sb.String()))
}
