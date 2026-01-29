package models

import (
	"time"
)

// Activity represents a time tracking entry.
type Activity struct {
	Description string     `json:"description"`
	Project     string     `json:"project"`
	StartTime   time.Time  `json:"start_time"`
	EndTime     *time.Time `json:"end_time,omitempty"` // nil if active
}

// Duration returns the duration of the activity.
// If the activity is active, it returns the duration from StartTime to now.
func (a Activity) Duration() time.Duration {
	if a.EndTime != nil {
		return a.EndTime.Sub(a.StartTime)
	}
	return time.Since(a.StartTime)
}
