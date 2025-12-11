package file

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/core/models"
)

var ErrSkip = errors.New("skip line")

const (
	timeLayoutMin = "2006-01-02 15:04"
	timeLayoutSec = "2006-01-02 15:04:05"
)

func ParseActivity(line string) (*models.Activity, error) {
	parts := strings.Split(line, "|")
	if len(parts) < 3 {
		return nil, ErrSkip // Not an activity line
	}

	timePart := strings.TrimSpace(parts[0])
	project := strings.TrimSpace(parts[1])
	description := strings.TrimSpace(parts[2])

	var start, end time.Time
	var err error

	if strings.Contains(timePart, " - ") {
		times := strings.Split(timePart, " - ")
		start, err = parseTime(times[0])
		if err != nil {
			return nil, errors.Wrap(err, "parse start time")
		}
		end, err = parseTime(times[1])
		if err != nil {
			return nil, errors.Wrap(err, "parse end time")
		}
		return &models.Activity{
			StartTime:   start,
			EndTime:     &end,
			Project:     project,
			Description: description,
		}, nil
	}

	start, err = parseTime(timePart)
	if err != nil {
		return nil, errors.Wrap(err, "parse start time")
	}

	return &models.Activity{
		StartTime:   start,
		EndTime:     nil,
		Project:     project,
		Description: description,
	}, nil
}

func parseTime(s string) (time.Time, error) {
	t, err := time.ParseInLocation(timeLayoutMin, s, time.Local)
	if err == nil {
		return t, nil
	}
	return time.ParseInLocation(timeLayoutSec, s, time.Local)
}

func FormatActivity(a models.Activity) string {
	startStr := a.StartTime.Format(timeLayoutMin)

	if a.EndTime != nil {
		endStr := a.EndTime.Format(timeLayoutMin)
		return fmt.Sprintf("%s - %s | %s | %s", startStr, endStr, a.Project, a.Description)
	}
	return fmt.Sprintf("%s | %s | %s", startStr, a.Project, a.Description)
}
