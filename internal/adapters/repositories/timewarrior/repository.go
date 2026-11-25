package timewarrior

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/go-faster/errors"
	"github.com/samber/lo"

	"github.com/kriuchkov/tock/internal/adapters/repositories/fileio"
	coreErrors "github.com/kriuchkov/tock/internal/core/errors"
	"github.com/kriuchkov/tock/internal/core/models"
	"github.com/kriuchkov/tock/internal/core/ports"
)

const (
	timeLayout = "20060102T150405Z"
	// monthFileLayout and monthFileSuffix name a data file, YYYY-MM.data.
	monthFileLayout = "2006-01"
	monthFileSuffix = ".data"
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

func (r *repository) Find(_ context.Context, filter models.ActivityFilter) ([]models.Activity, error) {
	start, end := r.dateRange(filter)
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

// dateRange is the months Find reads: those of the filter's bounds, widened where the filter has no
// bound to the earliest and the latest month file in the data directory, so an activity filed in any
// month, such as one whose year was mistyped, is found.
func (r *repository) dateRange(filter models.ActivityFilter) (time.Time, time.Time) {
	start, end := determineDateRange(filter)
	if filter.FromDate != nil && filter.ToDate != nil {
		return start, end
	}
	entries, err := os.ReadDir(r.dataDir)
	if err != nil {
		return start, end
	}
	for _, entry := range entries {
		name, found := strings.CutSuffix(entry.Name(), monthFileSuffix)
		month, parseErr := time.Parse(monthFileLayout, name)
		if !found || parseErr != nil || entry.IsDir() {
			continue
		}
		if filter.FromDate == nil && month.Before(start) {
			start = month
		}
		if filter.ToDate == nil && month.After(end) {
			end = month
		}
	}
	return start, end
}

func determineDateRange(filter models.ActivityFilter) (time.Time, time.Time) {
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if filter.FromDate != nil {
		start = filter.FromDate.AddDate(0, 0, -1)
	}
	end := time.Now().AddDate(1, 0, 0) // Future
	if filter.ToDate != nil {
		// A day either side: tock files an interval under the month of its local start, which may be
		// the month after or before that of its UTC start.
		end = filter.ToDate.AddDate(0, 0, 1)
	}
	return start, end
}

func matchesFilter(act models.Activity, filter models.ActivityFilter) bool {
	if filter.Project != nil && act.Project != *filter.Project {
		return false
	}
	if filter.Description != nil && act.Description != *filter.Description {
		return false
	}
	if !overlapsDateRange(act, filter.FromDate, filter.ToDate) {
		return false
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

// Save files activity under the month of its start in the zone the Mac is in now (TimeWarrior stores
// data by start time month). An interval with the same start is the activity saved before: Save
// replaces it there, or moves it there from the file of another month it was filed under in another
// zone (see holdingPaths), and removes it from every other file that holds it, as one a move across
// months left in both files when it stopped between the two renames.
func (r *repository) Save(_ context.Context, activity models.Activity) error {
	newInterval := toTWInterval(activity)
	return r.updateHeld(activity.StartTime, func(paths []string, held map[string][]twInterval) ([]string, error) {
		target := paths[0]
		intervals := held[target]

		// Check if we are updating an existing interval (e.g. stopping it)
		updated := false
		for i, v := range slices.Backward(intervals) {
			if v.Start == newInterval.Start {
				intervals[i] = newInterval
				updated = true
				break
			}
		}

		if !updated {
			intervals = append(intervals, newInterval)
		}
		// The file that gains the interval is written before those that lose it, as in Apply.
		changed := []string{target}
		for _, path := range paths[1:] {
			if dropStart(held, path, newInterval.Start) {
				changed = append(changed, path)
			}
		}

		// Sort intervals by Start time to ensure chronological order
		sort.Slice(intervals, func(i, j int) bool {
			return intervals[i].Start < intervals[j].Start
		})
		held[target] = intervals
		return changed, nil
	})
}

// updateHeld runs change on the intervals of the month files an interval starting at start may be filed
// in (holdingPaths), holding the locks of them all, and writes the files change returns, in that order.
// held has no entry for a file that does not exist.
func (r *repository) updateHeld(
	start time.Time,
	change func(paths []string, held map[string][]twInterval) ([]string, error),
) error {
	paths := r.holdingPaths(start)
	return lockAll(slices.Sorted(slices.Values(paths)), func() error {
		held := make(map[string][]twInterval, len(paths))
		for _, path := range paths {
			intervals, err := r.readIntervalsFromFile(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return errors.Wrap(err, "read intervals")
			}
			held[path] = intervals
		}
		changed, err := change(paths, held)
		if err != nil {
			return err
		}
		files := make([]fileio.File, 0, len(changed))
		for _, path := range changed {
			files = append(files, fileio.File{Path: path, Data: formatIntervals(held[path])})
		}
		return fileio.WriteAtomicAll(files)
	})
}

var _ ports.ActivityBatchWriter = (*repository)(nil)

// monthWrite is a write of Apply to one month file: it replaces or removes old, or adds an interval.
type monthWrite struct {
	old *models.Activity
	new *twInterval
}

// Apply applies writes in order, reading and writing each month file once; see
// ports.ActivityBatchWriter. An interval matches Old when it reads back as Old. tock files an interval
// under the month of its local start in the zone the Mac was in when it saved it, so Apply looks for Old
// in that month's file in the zone it is in now, then in the files of the months a day before and after
// its start. A write that moves an activity to another month file removes it from one and adds it to the
// other. It is all or nothing: it locks every month file it may touch, in name order, reads them all and
// checks every Old before it writes any, and returns coreErrors.ErrActivityNotFound, writing nothing,
// when an Old is not there (timew changed the file meanwhile). The files are then written in one pass of
// temporary files and renames (fileio.WriteAtomicAll), those that gain intervals before those that lose
// some, so a crash between two renames leaves an activity moved across months in both files, never in
// neither.
func (r *repository) Apply(_ context.Context, writes []ports.ActivityWrite) error {
	paths := r.batchPaths(writes)
	return lockAll(paths, func() error {
		held := make(map[string][]twInterval, len(paths))
		for _, path := range paths {
			intervals, err := r.readIntervalsFromFile(path)
			if err != nil && !os.IsNotExist(err) {
				return errors.Wrap(err, "read intervals")
			}
			held[path] = intervals
		}
		byFile, err := r.applyWrites(held, writes)
		if err != nil {
			return err
		}
		files := make([]fileio.File, 0, len(byFile))
		for _, path := range slices.Sorted(maps.Keys(byFile)) {
			intervals := held[path]
			sort.SliceStable(intervals, func(i, j int) bool { return intervals[i].Start < intervals[j].Start })
			files = append(files, fileio.File{Path: path, Data: formatIntervals(intervals)})
		}
		slices.SortStableFunc(files, func(a, b fileio.File) int {
			return cmp.Compare(writeRank(byFile[a.Path]), writeRank(byFile[b.Path]))
		})
		return fileio.WriteAtomicAll(files)
	})
}

// batchPaths returns, in name order, the month files a batch may touch: the file of each New, and
// every file an Old may be filed in.
func (r *repository) batchPaths(writes []ports.ActivityWrite) []string {
	set := make(map[string]bool)
	for _, w := range writes {
		if w.New != nil {
			set[r.getMonthFilePath(w.New.StartTime)] = true
		}
		if w.Old != nil {
			for _, path := range r.holdingPaths(w.Old.StartTime) {
				set[path] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// holdingPaths returns the month files an interval starting at start may be filed in: that of its
// month in the zone the Mac is in now, then, when they exist, those of the months a day before and after
// it, which cover its month in any other zone.
func (r *repository) holdingPaths(start time.Time) []string {
	paths := []string{r.getMonthFilePath(start)}
	for _, t := range []time.Time{start.UTC().AddDate(0, 0, -1), start.UTC(), start.UTC().AddDate(0, 0, 1)} {
		path := r.getMonthFilePath(t)
		if slices.Contains(paths, path) {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			paths = append(paths, path)
		}
	}
	return paths
}

// applyWrites applies writes in order to the intervals held of each month file, and returns the writes
// each changed file takes, or an error when an interval a write addresses is not there.
func (r *repository) applyWrites(held map[string][]twInterval, writes []ports.ActivityWrite) (map[string][]monthWrite, error) {
	byFile := make(map[string][]monthWrite)
	for _, w := range writes {
		var (
			iv      *twInterval
			newPath string
		)
		if w.New != nil {
			converted := toTWInterval(*w.New)
			iv, newPath = &converted, r.getMonthFilePath(w.New.StartTime)
		}
		if w.Old != nil {
			path, at := r.findHeld(held, *w.Old)
			if at < 0 {
				return nil, errors.Wrapf(coreErrors.ErrActivityNotFound, "the activity at %s changed", toTWInterval(*w.Old).Start)
			}
			if iv != nil && newPath == path {
				held[path][at] = *iv
				byFile[path] = append(byFile[path], monthWrite{old: w.Old, new: iv})
				continue
			}
			held[path] = slices.Delete(held[path], at, at+1)
			byFile[path] = append(byFile[path], monthWrite{old: w.Old})
		}
		if iv != nil {
			held[newPath] = append(held[newPath], *iv)
			byFile[newPath] = append(byFile[newPath], monthWrite{new: iv})
		}
	}
	return byFile, nil
}

// findHeld returns the month file that holds an interval reading back as old, and its index there, or
// -1 when none does.
func (r *repository) findHeld(held map[string][]twInterval, old models.Activity) (string, int) {
	for _, path := range r.holdingPaths(old.StartTime) {
		at := slices.IndexFunc(held[path], func(iv twInterval) bool {
			a, parseErr := fromTWInterval(iv)
			return parseErr == nil && sameInterval(a, old)
		})
		if at >= 0 {
			return path, at
		}
	}
	return "", -1
}

// lockAll runs fn while holding the locks of every path, taken in the order given.
func lockAll(paths []string, fn func() error) error {
	if len(paths) == 0 {
		return fn()
	}
	return fileio.Update(paths[0], func() error { return lockAll(paths[1:], fn) })
}

// writeRank orders the month files of a batch for writing: those that lose no interval first, then
// those that gain some and lose some, then those that only lose some.
func writeRank(writes []monthWrite) int {
	adds, drops := 0, 0
	for _, w := range writes {
		switch {
		case w.old == nil:
			adds++
		case w.new == nil:
			drops++
		}
	}
	switch {
	case drops == 0:
		return 0
	case adds > 0:
		return 1
	default:
		return 2 //nolint:mnd // the last of three ranks
	}
}

// sameInterval reports whether an interval read back as a is the activity b: the same start, end,
// project and description.
func sameInterval(a, b models.Activity) bool {
	sameEnd := (a.EndTime == nil) == (b.EndTime == nil) && (a.EndTime == nil || a.EndTime.Equal(*b.EndTime))
	return a.StartTime.Equal(b.StartTime) && sameEnd && a.Project == b.Project && a.Description == b.Description
}

// Normalize returns an activity as the data files give it back once saved: its times at the second,
// and its project and description as an interval line keeps them. Notes, tags and the uid pass
// through: the sync bridge keeps tags in the notes sidecar, which wins over the line.
func Normalize(a models.Activity) models.Activity {
	iv, err := parseIncLine(strings.TrimSpace(formatIncLine(toTWInterval(a))))
	if err != nil {
		return a
	}
	parsed, err := fromTWInterval(iv)
	if err != nil {
		return a
	}
	parsed.Notes, parsed.Tags, parsed.UID = a.Notes, a.Tags, a.UID
	return parsed
}

// Remove removes the intervals that start where activity does from every month file they may be filed
// under (see holdingPaths).
func (r *repository) Remove(_ context.Context, activity models.Activity) error {
	targetStart := toTWInterval(activity).Start
	return r.updateHeld(activity.StartTime, func(paths []string, held map[string][]twInterval) ([]string, error) {
		var changed []string
		for _, path := range paths {
			if dropStart(held, path, targetStart) {
				changed = append(changed, path)
			}
		}
		if _, exists := held[paths[0]]; len(changed) == 0 && exists {
			return nil, errors.New("activity not found")
		}
		return changed, nil
	})
}

// dropStart removes the intervals that start at start from those held of path, and reports whether
// there were any.
func dropStart(held map[string][]twInterval, path, start string) bool {
	intervals := held[path]
	kept := slices.DeleteFunc(slices.Clone(intervals), func(iv twInterval) bool { return iv.Start == start })
	if len(kept) == len(intervals) {
		return false
	}
	held[path] = kept
	return true
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

// formatIntervals writes intervals as the lines of a data file.
func formatIntervals(intervals []twInterval) []byte {
	var sb strings.Builder
	for _, iv := range intervals {
		sb.WriteString(formatIncLine(iv))
		sb.WriteByte('\n')
	}
	return []byte(sb.String())
}

func formatIncLine(iv twInterval) string {
	var sb strings.Builder
	sb.WriteString("inc ")
	sb.WriteString(iv.Start)
	if iv.End != "" {
		sb.WriteString(" - ")
		sb.WriteString(iv.End)
	}

	if len(iv.Tags) > 0 || iv.Annotation != "" {
		sb.WriteString(" #")
		for _, tag := range iv.Tags {
			sb.WriteString(" ")
			sb.WriteString(quoteToken(tag))
		}
	}

	if iv.Annotation != "" {
		sb.WriteString(" # ")
		sb.WriteString(quoteToken(iv.Annotation))
	}

	return sb.String()
}

func quoteToken(s string) string {
	if strings.ContainsAny(s, " \"") || s == "" {
		return fmt.Sprintf("%q", s)
	}
	return s
}

// toTWInterval converts an activity to an interval. A line break in the project, a tag or the
// description becomes a space: it would split the interval's line in two.
func toTWInterval(a models.Activity) twInterval {
	iv := twInterval{
		Start:      a.StartTime.UTC().Format(timeLayout),
		Annotation: oneLine(a.Description),
	}
	if a.EndTime != nil {
		iv.End = a.EndTime.UTC().Format(timeLayout)
	}
	if a.Project != "" {
		iv.Tags = append([]string{a.Project}, a.Tags...)
	} else if len(a.Tags) > 0 {
		iv.Tags = append([]string(nil), a.Tags...)
	}
	for i, tag := range iv.Tags {
		iv.Tags[i] = oneLine(tag)
	}
	return iv
}

func oneLine(s string) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
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
	var tags []string
	if len(iv.Tags) > 0 {
		project = iv.Tags[0]
		if len(iv.Tags) > 1 {
			tags = append([]string(nil), iv.Tags[1:]...)
		}
	}

	return models.Activity{
		Project:     project,
		Description: iv.Annotation,
		StartTime:   start.Local(),
		EndTime:     end,
		Tags:        tags,
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
