package models

import (
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/timeutil"
)

// WorkingHours is when the working day ends; an activity left running past it is stopped there.
type WorkingHours struct {
	StopHour   int
	StopMinute int
	Weekdays   map[time.Weekday]bool
}

// StopTime returns the latest end of a working day between start and now, if the activity ran past one.
func (w WorkingHours) StopTime(start, now time.Time) (time.Time, bool) {
	startLocal := start.In(time.Local)
	nowLocal := now.In(time.Local)
	if nowLocal.Before(startLocal) {
		return time.Time{}, false
	}

	firstDay := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, time.Local)
	for day := time.Date(nowLocal.Year(), nowLocal.Month(), nowLocal.Day(), 0, 0, 0, 0, time.Local); !day.Before(firstDay); day = day.AddDate(0, 0, -1) {
		if !w.Weekdays[day.Weekday()] {
			continue
		}

		cutoff := time.Date(day.Year(), day.Month(), day.Day(), w.StopHour, w.StopMinute, 0, 0, time.Local)
		if cutoff.After(nowLocal) || cutoff.Before(startLocal) {
			continue
		}
		return cutoff, true
	}
	return time.Time{}, false
}

// ParseWorkingWeekdays reads a comma-separated list of weekdays ("mon,tue", "all"); empty means Monday to Friday.
func ParseWorkingWeekdays(raw string) (map[time.Weekday]bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "mon,tue,wed,thu,fri"
	}

	weekdays := make(map[time.Weekday]bool, 7)
	for token := range strings.SplitSeq(trimmed, ",") {
		name := strings.ToLower(strings.TrimSpace(token))
		switch name {
		case "":
			continue
		case "all", "*":
			for wd := time.Sunday; wd <= time.Saturday; wd++ {
				weekdays[wd] = true
			}
			return weekdays, nil
		}

		wd, ok := parseWorkingWeekday(name)
		if !ok {
			return nil, errors.Errorf("invalid working hours weekday: %q", token)
		}
		weekdays[wd] = true
	}

	if len(weekdays) == 0 {
		return nil, errors.New("working hours weekdays cannot be empty")
	}
	return weekdays, nil
}

func parseWorkingWeekday(name string) (time.Weekday, bool) {
	switch name {
	case "tues":
		return time.Tuesday, true
	case "thur", "thurs":
		return time.Thursday, true
	}
	return timeutil.ParseWeekday(name)
}
