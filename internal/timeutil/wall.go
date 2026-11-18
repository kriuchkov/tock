package timeutil

import "time"

// WallTime returns the moment a clock in loc shows the given date and time. A time that a daylight
// saving change skips moves forward by the length of the gap: on a day the clocks jump from 02:00
// to 03:00, 02:30 becomes 03:30. time.Date alone resolves such a time either way, depending on the
// zone.
func WallTime(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	t := time.Date(year, month, day, hour, minute, 0, 0, loc)
	want := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	got := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	if !got.Before(want) {
		return t
	}
	// t is before the gap, so its offset is the one in force before the change.
	_, offset := t.Zone()
	return want.Add(-time.Duration(offset) * time.Second).In(loc)
}
