package models

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"
)

type ActivityReference struct {
	Date        time.Time
	Sequence    int
	HasSequence bool
}

func ParseActivityReference(value string) (ActivityReference, error) {
	parts := strings.Split(value, "-")

	if len(parts) == 3 {
		date, err := time.ParseInLocation(time.DateOnly, value, time.Local)
		if err != nil {
			return ActivityReference{}, errors.Wrap(err, "invalid date format (expected YYYY-MM-DD)")
		}
		return ActivityReference{Date: date}, nil
	}

	if len(parts) < 4 {
		return ActivityReference{}, errors.New("invalid key or date format")
	}

	seqStr := parts[len(parts)-1]
	seq, err := strconv.Atoi(seqStr)
	if err != nil {
		return ActivityReference{}, errors.Wrap(err, "invalid sequence number")
	}

	dateStr := strings.Join(parts[:len(parts)-1], "-")
	date, err := time.ParseInLocation(time.DateOnly, dateStr, time.Local)
	if err != nil {
		return ActivityReference{}, errors.Wrap(err, "invalid date format")
	}

	return ActivityReference{Date: date, Sequence: seq, HasSequence: true}, nil
}

func ParseActivityKey(value string) (time.Time, int, error) {
	ref, err := ParseActivityReference(value)
	if err != nil {
		return time.Time{}, 0, err
	}
	if !ref.HasSequence {
		return time.Time{}, 0, errors.New("invalid format: expected YYYY-MM-DD-NN")
	}
	return ref.Date, ref.Sequence, nil
}

func SortActivitiesByStart(activities []Activity) []Activity {
	sorted := make([]Activity, len(activities))
	copy(sorted, activities)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartTime.Before(sorted[j].StartTime)
	})
	return sorted
}

func ActivityForSequence(activities []Activity, sequence int) (Activity, error) {
	sorted := SortActivitiesByStart(activities)
	if sequence < 1 || sequence > len(sorted) {
		return Activity{}, errors.Errorf("activity sequence %d out of range 1-%d", sequence, len(sorted))
	}
	return sorted[sequence-1], nil
}

func ActivitySequenceIDs(activities []Activity) map[int64]string {
	sorted := SortActivitiesByStart(activities)
	ids := make(map[int64]string, len(sorted))
	dayCounts := make(map[string]int)

	for _, activity := range sorted {
		day := activity.StartTime.Format(time.DateOnly)
		dayCounts[day]++
		ids[activity.StartTime.UnixNano()] = fmt.Sprintf("%s-%02d", day, dayCounts[day])
	}

	return ids
}
