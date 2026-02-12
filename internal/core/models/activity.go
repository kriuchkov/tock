package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// Activity represents a time tracking entry.
type Activity struct {
	Description string     `json:"description"`
	Project     string     `json:"project"`
	StartTime   time.Time  `json:"start_time"`
	EndTime     *time.Time `json:"end_time,omitempty"` // nil if active
}

func (a Activity) Duration() time.Duration {
	if a.EndTime != nil {
		return a.EndTime.Sub(a.StartTime)
	}
	return time.Since(a.StartTime)
}

// DurationString returns the duration formatted as "HH:MM:SS".
func (a Activity) DurationString() string {
	d := a.Duration().Round(time.Second)
	h := d / time.Hour
	d %= time.Hour
	m := d / time.Minute
	d %= time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func (a Activity) MarshalJSON() ([]byte, error) {
	type Alias Activity
	return json.Marshal(&struct {
		Alias

		Duration string `json:"duration"`
	}{
		Alias:    (Alias)(a),
		Duration: a.DurationString(),
	})
}
