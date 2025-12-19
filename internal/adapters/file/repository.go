package file

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
			// Log warning? Skip?
			continue
		}

		if act == nil {
			continue
		}

		if filter.Project != nil && act.Project != *filter.Project {
			continue
		}
		if filter.FromDate != nil && act.StartTime.Before(*filter.FromDate) {
			continue
		}
		if filter.ToDate != nil {
			activityEnd := act.StartTime
			if act.EndTime != nil {
				activityEnd = *act.EndTime
			}
			if activityEnd.After(*filter.ToDate) {
				continue
			}
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
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		act, _ := ParseActivity(lines[i])
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
	// Ensure directory exists
	dir := filepath.Dir(r.filePath)
	if dirErr := os.MkdirAll(dir, 0750); dirErr != nil {
		return errors.Wrap(dirErr, "create directory")
	}

	f, err := os.Create(r.filePath)
	if err != nil {
		return errors.Wrap(err, "create file")
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	if flushErr := w.Flush(); flushErr != nil {
		return errors.Wrap(flushErr, "flush writer")
	}
	return nil
}

func findLastNonEmptyLine(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}
