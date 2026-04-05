package commands

import (
	"fmt"
	"time"

	"github.com/kriuchkov/tock/internal/app/localization"
)

func localizedWeekdayShortNames(loc *localization.Localizer) []string {
	return []string{
		loc.Text("date.weekday_short.mon"),
		loc.Text("date.weekday_short.tue"),
		loc.Text("date.weekday_short.wed"),
		loc.Text("date.weekday_short.thu"),
		loc.Text("date.weekday_short.fri"),
		loc.Text("date.weekday_short.sat"),
		loc.Text("date.weekday_short.sun"),
	}
}

func localizedWeekdayShort(loc *localization.Localizer, weekday time.Weekday) string {
	switch weekday {
	case time.Sunday:
		return loc.Text("date.weekday_short.sun")
	case time.Monday:
		return loc.Text("date.weekday_short.mon")
	case time.Tuesday:
		return loc.Text("date.weekday_short.tue")
	case time.Wednesday:
		return loc.Text("date.weekday_short.wed")
	case time.Thursday:
		return loc.Text("date.weekday_short.thu")
	case time.Friday:
		return loc.Text("date.weekday_short.fri")
	case time.Saturday:
		return loc.Text("date.weekday_short.sat")
	}

	return loc.Text("date.weekday_short.sun")
}

func localizedWeekdayLong(loc *localization.Localizer, weekday time.Weekday) string {
	switch weekday {
	case time.Sunday:
		return loc.Text("date.weekday_long.sunday")
	case time.Monday:
		return loc.Text("date.weekday_long.monday")
	case time.Tuesday:
		return loc.Text("date.weekday_long.tuesday")
	case time.Wednesday:
		return loc.Text("date.weekday_long.wednesday")
	case time.Thursday:
		return loc.Text("date.weekday_long.thursday")
	case time.Friday:
		return loc.Text("date.weekday_long.friday")
	case time.Saturday:
		return loc.Text("date.weekday_long.saturday")
	}

	return loc.Text("date.weekday_long.sunday")
}

func localizedMonthName(loc *localization.Localizer, month time.Month) string {
	switch month {
	case time.January:
		return loc.Text("date.month.january")
	case time.February:
		return loc.Text("date.month.february")
	case time.March:
		return loc.Text("date.month.march")
	case time.April:
		return loc.Text("date.month.april")
	case time.May:
		return loc.Text("date.month.may")
	case time.June:
		return loc.Text("date.month.june")
	case time.July:
		return loc.Text("date.month.july")
	case time.August:
		return loc.Text("date.month.august")
	case time.September:
		return loc.Text("date.month.september")
	case time.October:
		return loc.Text("date.month.october")
	case time.November:
		return loc.Text("date.month.november")
	case time.December:
		return loc.Text("date.month.december")
	}

	return loc.Text("date.month.december")
}

func localizedMonthShortName(loc *localization.Localizer, month time.Month) string {
	switch month {
	case time.January:
		return loc.Text("date.month_short.january")
	case time.February:
		return loc.Text("date.month_short.february")
	case time.March:
		return loc.Text("date.month_short.march")
	case time.April:
		return loc.Text("date.month_short.april")
	case time.May:
		return loc.Text("date.month_short.may")
	case time.June:
		return loc.Text("date.month_short.june")
	case time.July:
		return loc.Text("date.month_short.july")
	case time.August:
		return loc.Text("date.month_short.august")
	case time.September:
		return loc.Text("date.month_short.september")
	case time.October:
		return loc.Text("date.month_short.october")
	case time.November:
		return loc.Text("date.month_short.november")
	case time.December:
		return loc.Text("date.month_short.december")
	}

	return loc.Text("date.month_short.december")
}

func formatLocalizedMonthYear(loc *localization.Localizer, date time.Time) string {
	return fmt.Sprintf("%s %d", localizedMonthName(loc, date.Month()), date.Year())
}

func formatLocalizedLongDate(loc *localization.Localizer, date time.Time) string {
	return fmt.Sprintf(
		"%s, %02d %s %d",
		localizedWeekdayLong(loc, date.Weekday()),
		date.Day(),
		localizedMonthName(loc, date.Month()),
		date.Year(),
	)
}

func formatLocalizedLongDateShortMonth(loc *localization.Localizer, date time.Time) string {
	return fmt.Sprintf(
		"%s, %02d %s %d",
		localizedWeekdayLong(loc, date.Weekday()),
		date.Day(),
		localizedMonthShortName(loc, date.Month()),
		date.Year(),
	)
}
