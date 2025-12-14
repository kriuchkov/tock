package timewarrior

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-faster/errors"
	"github.com/kriuchkov/tock/internal/core/dto"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	timeLayout = "20060102T150405Z"
)

type twInterval struct {
	Start      string   `json:"start"`
	End        string   `json:"end,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Annotation string   `json:"annotation,omitempty"`
}

type repository struct {
	dataDir string
}

func NewRepository(dataDir string) ports.ActivityRepository {
	return &repository{dataDir: dataDir}
}

func (r *repository) Find(ctx context.Context, filter dto.ActivityFilter) ([]models.Activity, error) {
	// Determine date range to scan
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if filter.FromDate != nil {
		start = *filter.FromDate
	}
	end := time.Now().AddDate(1, 0, 0) // Future
	if filter.ToDate != nil {
		end = *filter.ToDate
	}

	var activities []models.Activity

	// Iterate over months from start to end
	current := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !current.After(end) {
		monthFile := r.getMonthFilePath(current)
		monthActs, err := r.readActivitiesFromFile(monthFile)
		if err != nil && !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "read file %s", monthFile)
		}

		for _, act := range monthActs {
			if filter.Project != nil && act.Project != *filter.Project {
				continue
			}
			if filter.FromDate != nil && act.StartTime.Before(*filter.FromDate) {
				continue
			}
			if filter.ToDate != nil {
				actEnd := act.StartTime
				if act.EndTime != nil {
					actEnd = *act.EndTime
				}
				if actEnd.After(*filter.ToDate) {
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
			activities = append(activities, act)
		}

		current = current.AddDate(0, 1, 0)
	}

	return activities, nil
}

func (r *repository) FindLast(ctx context.Context) (*models.Activity, error) {
	// Start from current month and go backwards
	current := time.Now()
	// Check up to 12 months back
	for i := 0; i < 12; i++ {
		monthFile := r.getMonthFilePath(current)
		acts, err := r.readActivitiesFromFile(monthFile)
		if err != nil && !os.IsNotExist(err) {
			return nil, errors.Wrap(err, "read file")
		}

		if len(acts) > 0 {
			return &acts[len(acts)-1], nil
		}
		current = current.AddDate(0, -1, 0)
	}

	return nil, coreErrors.ErrActivityNotFound
}

func (r *repository) Save(ctx context.Context, activity models.Activity) error {
	// TimeWarrior stores data by start time month
	filePath := r.getMonthFilePath(activity.StartTime)

	// Read existing to check if we are updating
	intervals, err := r.readIntervalsFromFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "read intervals")
	}

	newInterval := toTWInterval(activity)

	// Check if we are updating an existing interval (e.g. stopping it)
	updated := false
	for i := len(intervals) - 1; i >= 0; i-- {
		if intervals[i].Start == newInterval.Start {
			intervals[i] = newInterval
			updated = true
			break
		}
	}

	if !updated {
		intervals = append(intervals, newInterval)
	}

	// Sort intervals by Start time to ensure chronological order
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start < intervals[j].Start
	})

	return r.writeIntervalsToFile(filePath, intervals)
}

func (r *repository) getMonthFilePath(t time.Time) string {
	filename := fmt.Sprintf("%04d-%02d.data", t.Year(), t.Month())
	return filepath.Join(r.dataDir, filename)
}

func (r *repository) readActivitiesFromFile(path string) ([]models.Activity, error) {
	intervals, err := r.readIntervalsFromFile(path)
	if err != nil {
		return nil, err
	}

	var activities []models.Activity
	for _, iv := range intervals {
		act, err := fromTWInterval(iv)
		if err != nil {
			continue // Skip invalid
		}
		activities = append(activities, act)
	}
	return activities, nil
}

func (r *repository) readIntervalsFromFile(path string) ([]twInterval, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var intervals []twInterval
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var iv twInterval
		if err := json.Unmarshal([]byte(line), &iv); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing line in %s: %v\nLine: %s\n", path, err, line)
			continue
		}
		intervals = append(intervals, iv)
	}
	return intervals, nil
}

func (r *repository) writeIntervalsToFile(path string, intervals []twInterval) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return errors.Wrap(err, "create dir")
	}

	f, err := os.Create(path)
	if err != nil {
		return errors.Wrap(err, "create file")
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, iv := range intervals {
		b, err := json.Marshal(iv)
		if err != nil {
			continue
		}
		fmt.Fprintln(w, string(b))
	}
	return w.Flush()
}

func toTWInterval(a models.Activity) twInterval {
	iv := twInterval{
		Start:      a.StartTime.UTC().Format(timeLayout),
		Annotation: a.Description,
	}
	if a.EndTime != nil {
		iv.End = a.EndTime.UTC().Format(timeLayout)
	}
	if a.Project != "" {
		iv.Tags = []string{a.Project}
	}
	return iv
}

func fromTWInterval(iv twInterval) (models.Activity, error) {
	start, err := time.Parse(timeLayout, iv.Start)
	if err != nil {
		return models.Activity{}, err
	}

	var end *time.Time
	if iv.End != "" {
		e, err := time.Parse(timeLayout, iv.End)
		if err == nil {
			eLocal := e.Local()
			end = &eLocal
		}
	}

	project := ""
	if len(iv.Tags) > 0 {
		project = iv.Tags[0]
	}

	return models.Activity{
		Project:     project,
		Description: iv.Annotation,
		StartTime:   start.Local(),
		EndTime:     end,
	}, nil
}
