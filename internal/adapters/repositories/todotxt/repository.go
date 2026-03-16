package todotxt

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/dto"
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

func (r *repository) Find(_ context.Context, filter dto.ActivityFilter) ([]models.Activity, error) {
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
	lines, err := r.readLines()
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrap(err, "read lines")
		}
		lines = []string{}
	}

	formatted := FormatActivity(activity)
	updated := false
	for index := len(lines) - 1; index >= 0; index-- {
		parsed, parseErr := ParseActivity(lines[index])
		if parseErr != nil || parsed == nil {
			continue
		}
		if parsed.StartTime.Equal(activity.StartTime) {
			lines[index] = formatted
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
	dir := filepath.Dir(r.filePath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return errors.Wrap(err, "create directory")
	}

	f, err := os.Create(r.filePath)
	if err != nil {
		return errors.Wrap(err, "create file")
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fmt.Fprintln(writer, line)
	}
	if err = writer.Flush(); err != nil {
		return errors.Wrap(err, "flush writer")
	}
	return nil
}
