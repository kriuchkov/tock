package models

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-faster/errors"

	"github.com/kriuchkov/tock/internal/timeutil"
)

// RepeatFreq is how often a reminder comes back.
type RepeatFreq string

const (
	RepeatNone     RepeatFreq = ""
	RepeatDaily    RepeatFreq = "daily"
	RepeatWeekdays RepeatFreq = "weekdays"
	RepeatWeekly   RepeatFreq = "weekly"
	RepeatMonthly  RepeatFreq = "monthly"
	RepeatYearly   RepeatFreq = "yearly"
)

const (
	// MonthDayLast makes a monthly reminder fall on the last day of the month.
	MonthDayLast = -1
	// WeekLast makes a monthly reminder fall on the last given weekday of the month.
	WeekLast = -1

	maxWeekOfMonth = 4
	maxMonthDay    = 31
	daysPerWeek    = 7
	monthsPerYear  = 12
	// maxOccurrenceScanDays bounds how far ahead an occurrence search looks.
	maxOccurrenceScanDays = 366 * 50
)

// Repeat describes when a reminder comes back. The zero value is a one-off reminder.
type Repeat struct {
	Freq RepeatFreq
	// Interval repeats every N days, weeks, months or years; 0 and 1 both mean every one.
	Interval int
	// Weekdays are the days a weekly reminder falls on; empty means the weekday of its first day.
	Weekdays []time.Weekday
	// MonthDay is the day a monthly reminder falls on, or MonthDayLast; 0 means the day of its first occurrence.
	MonthDay int
	// Week and Weekday make a monthly reminder fall on the Nth weekday of the month (1..4 or WeekLast).
	Week    int
	Weekday time.Weekday
	// Until is the last day an occurrence can fall on.
	Until *time.Time
	// Count stops the reminder after this many occurrences; 0 means no limit.
	Count int
}

func (r Repeat) every() int {
	if r.Interval < 1 {
		return 1
	}
	return r.Interval
}

// Validate reports a repeat rule that cannot produce sensible occurrences for a reminder first due at due.
func (r Repeat) Validate(due time.Time) error {
	switch r.Freq {
	case RepeatNone, RepeatDaily, RepeatWeekdays, RepeatWeekly, RepeatMonthly, RepeatYearly:
	default:
		return errors.Errorf("unknown repeat %q", r.Freq)
	}
	if r.Interval < 0 || r.Count < 0 {
		return errors.New("repeat interval and count cannot be negative")
	}
	for _, wd := range r.Weekdays {
		if wd < time.Sunday || wd > time.Saturday {
			return errors.Errorf("invalid weekday %d", wd)
		}
	}
	if r.MonthDay != MonthDayLast && (r.MonthDay < 0 || r.MonthDay > maxMonthDay) {
		return errors.Errorf("invalid day of month %d", r.MonthDay)
	}
	if r.Week != WeekLast && (r.Week < 0 || r.Week > maxWeekOfMonth) {
		return errors.Errorf("invalid week of month %d", r.Week)
	}
	if r.Until != nil && dayStart(*r.Until).Before(dayStart(due)) {
		return errors.New("the repeat cannot end before the first occurrence")
	}
	return nil
}

// ParseRepeat parses a repeat rule such as "daily", "weekdays", "weekly:mon,thu", "monthly:16",
// "monthly:last-fri", "yearly" or an interval like "3d", "2w", "6m", "1y". Until and Count are
// set separately.
func ParseRepeat(spec string) (Repeat, error) {
	spec = strings.ToLower(strings.TrimSpace(spec))
	base, arg, hasArg := strings.Cut(spec, ":")

	switch base {
	case "", "none", "once", "never":
		if hasArg {
			return Repeat{}, errors.Errorf("invalid repeat %q", spec)
		}
		return Repeat{}, nil
	case "daily", "day":
		return noArgRepeat(spec, RepeatDaily, 1, hasArg)
	case "weekdays", "workdays":
		return noArgRepeat(spec, RepeatWeekdays, 1, hasArg)
	case "yearly", "annually":
		return noArgRepeat(spec, RepeatYearly, 1, hasArg)
	case "weekly":
		return parseWeekly(1, arg)
	case "monthly":
		return parseMonthly(1, arg)
	}

	return parseIntervalRepeat(spec, base, arg, hasArg)
}

func noArgRepeat(spec string, freq RepeatFreq, interval int, hasArg bool) (Repeat, error) {
	if hasArg {
		return Repeat{}, errors.Errorf("invalid repeat %q", spec)
	}
	return Repeat{Freq: freq, Interval: normalizeInterval(interval)}, nil
}

func parseIntervalRepeat(spec, base, arg string, hasArg bool) (Repeat, error) {
	const minIntervalLen = 2
	if len(base) < minIntervalLen {
		return Repeat{}, errors.Errorf("invalid repeat %q", spec)
	}

	interval, err := strconv.Atoi(base[:len(base)-1])
	if err != nil || interval < 1 {
		return Repeat{}, errors.Errorf("invalid repeat %q", spec)
	}

	switch base[len(base)-1] {
	case 'd':
		return noArgRepeat(spec, RepeatDaily, interval, hasArg)
	case 'w':
		return parseWeekly(interval, arg)
	case 'm':
		return parseMonthly(interval, arg)
	case 'y':
		return noArgRepeat(spec, RepeatYearly, interval, hasArg)
	}
	return Repeat{}, errors.Errorf("invalid repeat %q", spec)
}

func parseWeekly(interval int, arg string) (Repeat, error) {
	rep := Repeat{Freq: RepeatWeekly, Interval: normalizeInterval(interval)}
	if arg == "" {
		return rep, nil
	}

	for name := range strings.SplitSeq(arg, ",") {
		wd, ok := timeutil.ParseWeekday(name)
		if !ok {
			return Repeat{}, errors.Errorf("invalid weekday %q", name)
		}
		if !slices.Contains(rep.Weekdays, wd) {
			rep.Weekdays = append(rep.Weekdays, wd)
		}
	}
	sortWeekdays(rep.Weekdays)
	return rep, nil
}

func parseMonthly(interval int, arg string) (Repeat, error) {
	rep := Repeat{Freq: RepeatMonthly, Interval: normalizeInterval(interval)}
	switch arg {
	case "":
		return rep, nil
	case "last":
		rep.MonthDay = MonthDayLast
		return rep, nil
	}

	if day, err := strconv.Atoi(arg); err == nil {
		if day < 1 || day > maxMonthDay {
			return Repeat{}, errors.Errorf("invalid day of month %q", arg)
		}
		rep.MonthDay = day
		return rep, nil
	}

	weekToken, dayToken, ok := strings.Cut(arg, "-")
	if !ok {
		return Repeat{}, errors.Errorf("invalid monthly repeat %q", arg)
	}
	week, ok := parseWeekOfMonth(weekToken)
	if !ok {
		return Repeat{}, errors.Errorf("invalid week of month %q", weekToken)
	}
	wd, ok := timeutil.ParseWeekday(dayToken)
	if !ok {
		return Repeat{}, errors.Errorf("invalid weekday %q", dayToken)
	}
	rep.Week = week
	rep.Weekday = wd
	return rep, nil
}

func normalizeInterval(interval int) int {
	if interval <= 1 {
		return 0
	}
	return interval
}

func parseWeekOfMonth(token string) (int, bool) {
	switch token {
	case "1", "first":
		return 1, true
	case "2", "second":
		return 2, true //nolint:mnd // second week
	case "3", "third":
		return 3, true //nolint:mnd // third week
	case "4", "fourth":
		return 4, true //nolint:mnd // fourth week
	case "last":
		return WeekLast, true
	}
	return 0, false
}

// sortWeekdays orders weekdays Monday first.
func sortWeekdays(days []time.Weekday) {
	slices.SortFunc(days, func(a, b time.Weekday) int {
		return mondayIndex(a) - mondayIndex(b)
	})
}

func mondayIndex(wd time.Weekday) int {
	return (int(wd) + daysPerWeek - 1) % daysPerWeek
}

// Spec formats the rule the way ParseRepeat reads it, without Until and Count.
func (r Repeat) Spec() string {
	interval := r.every()
	switch r.Freq {
	case RepeatNone:
		return ""
	case RepeatDaily:
		return intervalSpec("daily", interval, "d")
	case RepeatWeekdays:
		return "weekdays"
	case RepeatWeekly:
		spec := intervalSpec("weekly", interval, "w")
		if len(r.Weekdays) == 0 {
			return spec
		}
		names := make([]string, 0, len(r.Weekdays))
		for _, wd := range r.Weekdays {
			names = append(names, weekdayToken(wd))
		}
		return spec + ":" + strings.Join(names, ",")
	case RepeatMonthly:
		return intervalSpec("monthly", interval, "m") + r.monthlySpecSuffix()
	case RepeatYearly:
		return intervalSpec("yearly", interval, "y")
	}
	return string(r.Freq)
}

func (r Repeat) monthlySpecSuffix() string {
	switch {
	case r.Week == WeekLast:
		return ":last-" + weekdayToken(r.Weekday)
	case r.Week > 0:
		return fmt.Sprintf(":%d-%s", r.Week, weekdayToken(r.Weekday))
	case r.MonthDay == MonthDayLast:
		return ":last"
	case r.MonthDay > 0:
		return ":" + strconv.Itoa(r.MonthDay)
	}
	return ""
}

func intervalSpec(name string, interval int, unit string) string {
	if interval > 1 {
		return strconv.Itoa(interval) + unit
	}
	return name
}

func weekdayToken(wd time.Weekday) string {
	return strings.ToLower(wd.String()[:3])
}

// WeeklyDays returns the weekdays a weekly reminder falls on, Monday first.
func (r Reminder) WeeklyDays() []time.Weekday {
	if len(r.Repeat.Weekdays) > 0 {
		days := slices.Clone(r.Repeat.Weekdays)
		sortWeekdays(days)
		return days
	}
	return []time.Weekday{r.firstDay().Weekday()}
}

// firstDay is the start of the day the reminder was first due.
func (r Reminder) firstDay() time.Time {
	return dayStart(r.Due)
}

// dueOn returns the occurrence time on day.
func (r Reminder) dueOn(day time.Time) time.Time {
	if r.AllDay {
		return day
	}
	clock := r.Due.In(time.Local)
	return timeutil.WallTime(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), time.Local)
}

// eachOccurrence calls fn with the due time of every occurrence on a day before limit, in order,
// including skipped ones, until fn returns false.
func (r Reminder) eachOccurrence(limit time.Time, fn func(due time.Time) bool) {
	first := r.firstDay()
	var until time.Time
	if r.Repeat.Until != nil {
		until = dayStart(*r.Repeat.Until)
	}

	count := 0
	for offset := 0; offset <= maxOccurrenceScanDays; offset++ {
		day := time.Date(first.Year(), first.Month(), first.Day()+offset, 0, 0, 0, 0, time.Local)
		if !day.Before(limit) || (!until.IsZero() && day.After(until)) {
			return
		}
		if !r.fallsOn(day, offset) {
			continue
		}
		count++
		if r.Repeat.Count > 0 && count > r.Repeat.Count {
			return
		}
		if !fn(r.dueOn(day)) || !r.IsRepeating() {
			return
		}
	}
}

// fallsOn reports whether an occurrence falls on day, offset days after the first one.
func (r Reminder) fallsOn(day time.Time, offset int) bool {
	rep := r.Repeat
	first := r.firstDay()

	switch rep.Freq {
	case RepeatNone:
		return offset == 0
	case RepeatDaily:
		return offset%rep.every() == 0
	case RepeatWeekdays:
		return day.Weekday() != time.Saturday && day.Weekday() != time.Sunday
	case RepeatWeekly:
		week := (offset + mondayIndex(first.Weekday())) / daysPerWeek
		return week%rep.every() == 0 && slices.Contains(r.WeeklyDays(), day.Weekday())
	case RepeatMonthly:
		months := (day.Year()-first.Year())*monthsPerYear + int(day.Month()) - int(first.Month())
		return months%rep.every() == 0 && day.Day() == r.monthlyDay(day.Year(), day.Month())
	case RepeatYearly:
		years := day.Year() - first.Year()
		return years%rep.every() == 0 && day.Month() == first.Month() &&
			day.Day() == clampDay(day.Year(), day.Month(), first.Day())
	}
	return false
}

// monthlyDay returns the day a monthly reminder falls on in the given month, or 0 when it skips it.
func (r Reminder) monthlyDay(year int, month time.Month) int {
	rep := r.Repeat
	if rep.Week != 0 {
		return nthWeekday(year, month, rep.Week, rep.Weekday)
	}

	switch rep.MonthDay {
	case 0:
		return clampDay(year, month, r.firstDay().Day())
	case MonthDayLast:
		return daysIn(year, month)
	}
	return clampDay(year, month, rep.MonthDay)
}

// nthWeekday returns the day of the nth (or last) weekday of the month, or 0 when there is none.
func nthWeekday(year int, month time.Month, n int, wd time.Weekday) int {
	last := daysIn(year, month)
	if n == WeekLast {
		lastWeekday := time.Date(year, month, last, 0, 0, 0, 0, time.Local).Weekday()
		return last - (int(lastWeekday)-int(wd)+daysPerWeek)%daysPerWeek
	}

	firstWeekday := time.Date(year, month, 1, 0, 0, 0, 0, time.Local).Weekday()
	day := 1 + (int(wd)-int(firstWeekday)+daysPerWeek)%daysPerWeek + (n-1)*daysPerWeek
	if day > last {
		return 0
	}
	return day
}

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.Local).Day()
}

// clampDay keeps day within the month, so the 31st falls on the 30th in a 30-day month.
func clampDay(year int, month time.Month, day int) int {
	return min(day, daysIn(year, month))
}

func dayStart(t time.Time) time.Time {
	local := t.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}
