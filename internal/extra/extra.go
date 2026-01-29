package extra

import (
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/timeutil"
)

func CalculateEndTime(tf *timeutil.Formatter, startTime time.Time, endStr, durationStr string) (time.Time, error) {
	if endStr != "" {
		endTime, err := tf.ParseTimeWithDate(endStr)
		if err != nil {
			return time.Time{}, errors.Wrap(err, "parse end time")
		}

		if endTime.Before(startTime) {
			return time.Time{}, errors.New("end time cannot be before start time")
		}
		return endTime, nil
	}

	if durationStr == "" {
		return time.Time{}, errors.New("end time or duration is required")
	}

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		return time.Time{}, errors.Wrap(err, "parse duration")
	}
	return startTime.Add(duration), nil
}
