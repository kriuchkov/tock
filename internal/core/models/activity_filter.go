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
	From        string
	To          string
	Project     string
	Description string
}

func BuildActivityFilter(opts ActivityFilterOptions) (ActivityFilter, error) {
	if err := validateDateFilters(opts); err != nil {
		return ActivityFilter{}, err
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	filter := ActivityFilter{}

	switch {
	case opts.From != "" || opts.To != "":
		fromDate, toDate, err := buildDateRange(opts.From, opts.To)
		if err != nil {
			return ActivityFilter{}, err
		}
		filter.FromDate = fromDate
		filter.ToDate = toDate
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

	if opts.Project != "" {
		filter.Project = &opts.Project
	}
	if opts.Description != "" {
		filter.Description = &opts.Description
	}

	return filter, nil
}

func validateDateFilters(opts ActivityFilterOptions) error {
	dateFilters := 0
	if opts.Today {
		dateFilters++
	}
	if opts.Yesterday {
		dateFilters++
	}
	if opts.Date != "" {
		dateFilters++
	}
	if opts.From != "" || opts.To != "" {
		dateFilters++
	}
	if dateFilters > 1 {
		return errors.New("cannot specify multiple date filters (--today, --yesterday, --date, --from/--to are mutually exclusive)")
	}

	return nil
}

func buildDateRange(from, to string) (*time.Time, *time.Time, error) {
	var fromDate, toDate *time.Time

	if from != "" {
		parsed, err := time.ParseInLocation("2006-01-02", from, time.Local)
		if err != nil {
			return nil, nil, errors.Wrap(err, "invalid --from date format, use YYYY-MM-DD")
		}
		fromDate = &parsed
	}

	if to != "" {
		parsed, err := time.ParseInLocation("2006-01-02", to, time.Local)
		if err != nil {
			return nil, nil, errors.Wrap(err, "invalid --to date format, use YYYY-MM-DD")
		}
		_, end := timeutil.LocalDayBounds(parsed)
		toDate = &end
	}

	if fromDate != nil && toDate != nil && !fromDate.Before(*toDate) {
		return nil, nil, errors.New("--from date must not be after --to date")
	}

	return fromDate, toDate, nil
}
