package models

import (
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/timeutil"
)

// CurrentPeriod is the ActivityFilterOptions.Month/Quarter value that selects the period containing Now.
const CurrentPeriod = -1

type ActivityFilterOptions struct {
	Now         time.Time
	Today       bool
	Yesterday   bool
	Week        bool
	Month       int // 0 = unset, CurrentPeriod, or 1-12 for the most recent such month
	Quarter     int // 0 = unset, CurrentPeriod, or 1-4 for the most recent such quarter
	Year        bool
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

	filter := ActivityFilter{}

	fromDate, toDate, err := resolveDateRange(opts)
	if err != nil {
		return ActivityFilter{}, err
	}
	filter.FromDate = fromDate
	filter.ToDate = toDate

	if opts.Project != "" {
		filter.Project = &opts.Project
	}
	if opts.Description != "" {
		filter.Description = &opts.Description
	}

	return filter, nil
}

func resolveDateRange(opts ActivityFilterOptions) (*time.Time, *time.Time, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	switch {
	case opts.From != "" || opts.To != "":
		return buildDateRange(opts.From, opts.To)
	case opts.Today:
		start, end := timeutil.LocalDayBounds(now)
		return &start, &end, nil
	case opts.Yesterday:
		todayStart, _ := timeutil.LocalDayBounds(now)
		start := todayStart.AddDate(0, 0, -1)
		return &start, &todayStart, nil
	case opts.Date != "":
		parsedDate, err := time.ParseInLocation("2006-01-02", opts.Date, time.Local)
		if err != nil {
			return nil, nil, errors.Wrap(err, "invalid date format (use YYYY-MM-DD)")
		}
		start, end := timeutil.LocalDayBounds(parsedDate)
		return &start, &end, nil
	case opts.Week:
		start, end := timeutil.LocalWeekBounds(now)
		return &start, &end, nil
	case opts.Month != 0:
		ref, err := monthReference(now, opts.Month)
		if err != nil {
			return nil, nil, err
		}
		start, end := timeutil.LocalMonthBounds(ref)
		return &start, &end, nil
	case opts.Quarter != 0:
		ref, err := quarterReference(now, opts.Quarter)
		if err != nil {
			return nil, nil, err
		}
		start, end := timeutil.LocalQuarterBounds(ref)
		return &start, &end, nil
	case opts.Year:
		start, end := timeutil.LocalYearBounds(now)
		return &start, &end, nil
	}

	return nil, nil, nil
}

func validateDateFilters(opts ActivityFilterOptions) error {
	dateFilters := 0
	if opts.Today {
		dateFilters++
	}
	if opts.Yesterday {
		dateFilters++
	}
	if opts.Week {
		dateFilters++
	}
	if opts.Month != 0 {
		dateFilters++
	}
	if opts.Quarter != 0 {
		dateFilters++
	}
	if opts.Year {
		dateFilters++
	}
	if opts.Date != "" {
		dateFilters++
	}
	if opts.From != "" || opts.To != "" {
		dateFilters++
	}
	if dateFilters > 1 {
		return errors.New(
			"cannot specify multiple date filters (--today, --yesterday, --week, --month, --quarter, --year, " +
				"--date, --from/--to are mutually exclusive)",
		)
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

// monthReference returns a time in the most recent month numbered month that is not in the future.
func monthReference(now time.Time, month int) (time.Time, error) {
	if month == CurrentPeriod {
		return now, nil
	}
	if month < 1 || month > 12 {
		return time.Time{}, errors.New("invalid --month value, use 1-12")
	}
	local := now.In(time.Local)
	year := local.Year()
	if time.Month(month) > local.Month() {
		year--
	}
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local), nil
}

// quarterReference returns a time in the most recent quarter numbered quarter that is not in the future.
func quarterReference(now time.Time, quarter int) (time.Time, error) {
	if quarter == CurrentPeriod {
		return now, nil
	}
	if quarter < 1 || quarter > 4 {
		return time.Time{}, errors.New("invalid --quarter value, use 1-4")
	}
	// A quarter starts in the future exactly when its first month does.
	return monthReference(now, 3*quarter-2)
}
