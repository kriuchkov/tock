package commands

import (
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/timeutil"
)

func normalizeAddDateTimeInput(tf *timeutil.Formatter, dayStr, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" || dayStr == "" {
		return input, nil
	}

	if _, err := time.ParseInLocation("2006-01-02", dayStr, time.Local); err != nil {
		return "", errors.Wrap(err, "parse day")
	}

	if _, err := tf.ParseTime(input); err == nil {
		return dayStr + " " + input, nil
	}

	return input, nil
}

func calculateEndTime(tf *timeutil.Formatter, startTime time.Time, endStr, durationStr string) (time.Time, error) {
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
