package models

import (
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/timeutil"
)

type ActivityFilterOptions struct {
	Now         time.Time
	Today       bool
	Yesterday   bool
	Date        string
	FromDate    *time.Time
	ToDate      *time.Time
	Project     string
	Description string
}

func BuildActivityFilter(opts ActivityFilterOptions) (ActivityFilter, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	filter := ActivityFilter{}

	if opts.FromDate != nil || opts.ToDate != nil {
		if opts.FromDate != nil {
			filter.FromDate = opts.FromDate
		}
		if opts.ToDate != nil {
			filter.ToDate = opts.ToDate
		}
	} else {
		switch {
		case opts.Today:
			start, end := timeutil.LocalDayBounds(now)
			filter.FromDate = &start
			filter.ToDate = &end
		case opts.Yesterday:
			todayStart, _ := timeutil.LocalDayBounds(now)
			start := todayStart.AddDate(0, 0, -1)
			end := todayStart
			filter.FromDate = &start
			filter.ToDate = &end
		case opts.Date != "":
			parsedDate, err := time.ParseInLocation("2006-01-02", opts.Date, time.Local)
			if err != nil {
				return ActivityFilter{}, errors.Wrap(err, "invalid date format (use YYYY-MM-DD)")
			}
			start, end := timeutil.LocalDayBounds(parsedDate)
			filter.FromDate = &start
			filter.ToDate = &end
		}
	}

	if opts.Project != "" {
		filter.Project = &opts.Project
	}
	if opts.Description != "" {
		filter.Description = &opts.Description
	}

	return filter, nil
}
