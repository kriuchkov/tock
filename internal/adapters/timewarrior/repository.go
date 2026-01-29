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
	"unicode"

	"github.com/go-faster/errors"
	"github.com/samber/lo"

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

func (r *repository) Find(_ context.Context, filter dto.ActivityFilter) ([]models.Activity, error) {
	start, end := determineDateRange(filter)
	var activities []models.Activity

	// Iterate over months from start to end
	current := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	for !current.After(end) {
		monthFile := r.getMonthFilePath(current)
		monthActs, err := r.readActivitiesFromFile(monthFile)
		if err != nil && !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "read file %s", monthFile)
		}

		filtered := lo.Filter(monthActs, func(act models.Activity, _ int) bool {
			return matchesFilter(act, filter)
		})
		activities = append(activities, filtered...)

		current = current.AddDate(0, 1, 0)
	}

	return activities, nil
}

func determineDateRange(filter dto.ActivityFilter) (time.Time, time.Time) {
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if filter.FromDate != nil {
		start = *filter.FromDate
	}
	end := time.Now().AddDate(1, 0, 0) // Future
	if filter.ToDate != nil {
		end = *filter.ToDate
	}
	return start, end
}

func matchesFilter(act models.Activity, filter dto.ActivityFilter) bool {
	if filter.Project != nil && act.Project != *filter.Project {
		return false
	}
	if filter.FromDate != nil && act.StartTime.Before(*filter.FromDate) {
		return false
	}
	if filter.Description != nil && act.Description != *filter.Description {
		return false
	}

	if filter.ToDate != nil {
		actEnd := act.StartTime
		if act.EndTime != nil {
			actEnd = *act.EndTime
		}
		if actEnd.After(*filter.ToDate) {
			return false
		}
	}

	if filter.IsRunning != nil {
		if *filter.IsRunning && act.EndTime != nil {
			return false
		}
		if !*filter.IsRunning && act.EndTime == nil {
			return false
		}
	}

	return true
}

func (r *repository) FindLast(_ context.Context) (*models.Activity, error) {
	// Start from current month and go backwards
	current := time.Now()
	var lastActivity *models.Activity

	// Check up to 12 months back
	for range 12 {
		monthFile := r.getMonthFilePath(current)
		acts, err := r.readActivitiesFromFile(monthFile)
		if err != nil && !os.IsNotExist(err) {
			return nil, errors.Wrap(err, "read file")
		}

		// Find the activity with the latest start time in this month
		for i := range acts {
			if lastActivity == nil || acts[i].StartTime.After(lastActivity.StartTime) {
				lastActivity = &acts[i]
			}
		}

		// If we found any activities in current or later months, we can stop
		// (activities can't be in the future beyond this point)
		if len(acts) > 0 && current.Before(time.Now().AddDate(0, -1, 0)) {
			break
		}

		current = current.AddDate(0, -1, 0)
	}

	if lastActivity == nil {
		return nil, coreErrors.ErrActivityNotFound
	}

	return lastActivity, nil
}

func (r *repository) Save(_ context.Context, activity models.Activity) error {
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
		var act models.Activity
		act, err = fromTWInterval(iv)
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
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var iv twInterval
		var parseErr error

		// TimeWarrior data files typically use JSON Lines format (starting with '{').
		// However, they may also contain lines in TimeWarrior's internal serialization format
		// (starting with 'inc'), especially if the file includes undo logs or was generated
		// by specific commands.
		//
		//nolint:gocritic // ignore else-if complexity for clarity
		if strings.HasPrefix(line, "{") {
			// Standard JSON format
			parseErr = json.Unmarshal([]byte(line), &iv)
		} else if strings.HasPrefix(line, "inc") {
			// Internal serialization format (e.g. "inc 20230101T000000Z ...")
			iv, parseErr = parseIncLine(line)
		} else {
			// Unknown format, skip with warning
			fmt.Fprintf(os.Stderr, "Warning: skipping unknown line format in %s: %s\n", path, line)
			continue
		}

		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "Error parsing line in %s: %v\nLine: %s\n", path, parseErr, line)
			continue
		}
		intervals = append(intervals, iv)
	}
	if err = scanner.Err(); err != nil {
		return nil, err
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
		var b []byte
		if b, err = json.Marshal(iv); err != nil {
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
		var e time.Time
		e, err = time.Parse(timeLayout, iv.End)
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

// parseIncLine parses a line in TimeWarrior's internal serialization format.
// Format: inc <start> [ - <end> ] [ # <tag1> <tag2> ... ] [ # <annotation> ]
// Example: inc 20251201T014528Z - 20251201T041127Z # plan "plan_" |8ba7daab|.
func parseIncLine(line string) (twInterval, error) {
	tokens := tokenize(line)
	if len(tokens) == 0 || tokens[0] != "inc" {
		return twInterval{}, errors.New("invalid inc line")
	}

	var iv twInterval
	idx := 1

	// Start time
	if idx < len(tokens) && len(tokens[idx]) == 16 && tokens[idx][8] == 'T' {
		iv.Start = tokens[idx]
		idx++
	}

	// End time
	if idx+1 < len(tokens) && tokens[idx] == "-" && len(tokens[idx+1]) == 16 {
		iv.End = tokens[idx+1]
		idx += 2
	}

	// Tags
	if idx < len(tokens) && tokens[idx] == "#" {
		idx++
		for idx < len(tokens) && tokens[idx] != "#" {
			iv.Tags = append(iv.Tags, tokens[idx])
			idx++
		}
	}

	// Annotation
	if idx < len(tokens) && tokens[idx] == "#" {
		idx++
		iv.Annotation = strings.Join(tokens[idx:], " ")
	}

	// tokenize splits a string into tokens, respecting quoted strings.
	// This is necessary because tags or annotations might contain spaces within quotes.
	return iv, nil
}

func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for _, r := range line {
		if r == '"' {
			inQuote = !inQuote
			continue
		}

		if unicode.IsSpace(r) && !inQuote {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}
