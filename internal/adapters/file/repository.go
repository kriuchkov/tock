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
			lastAct = act
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
	// This is a simplified implementation.
	// Ideally we should read all lines, identify if we are updating the last line or appending.

	lines, err := r.readLines()
	if err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrap(err, "read lines")
		}
		// File doesn't exist, will be created on write
		lines = []string{}
	}

	// Check if we are updating the last activity (e.g. stopping it)
	// Or appending a new one.

	// If the activity passed has an ID or we can identify it by start time...
	// But here we don't have IDs.
	// Logic: If the last activity in file is "running" (no end time) and the new activity has the same start time, update it.
	// Otherwise append.

	if len(lines) > 0 { //nolint:nestif // simple logic
		lastLineIdx := findLastNonEmptyLine(lines)

		if lastLineIdx != -1 {
			lastAct, _ := ParseActivity(lines[lastLineIdx])
			if lastAct != nil && lastAct.EndTime == nil && lastAct.StartTime.Equal(activity.StartTime) {
				// Update last line
				lines[lastLineIdx] = FormatActivity(activity)
				if writeErr := r.writeLines(lines); writeErr != nil {
					return errors.Wrap(writeErr, "write lines")
				}
				return nil
			}
		}
	}

	// Append
	lines = append(lines, FormatActivity(activity))
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
