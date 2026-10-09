package models

import (
	"slices"
	"time"
)

type StartActivityRequest struct {
	Description string
	Project     string
	StartTime   time.Time
	Notes       string
	Tags        []string
}

type StopActivityRequest struct {
	EndTime time.Time
	Notes   string
	Tags    []string
}

type AddActivityRequest struct {
	Description string
	Project     string
	StartTime   time.Time
	EndTime     time.Time
	Notes       string
	Tags        []string
}

type ActivityFilter struct {
	FromDate    *time.Time
	ToDate      *time.Time
	Project     *string
	Description *string
	IsRunning   *bool
}

type Report struct {
	Activities    []Activity
	TotalDuration time.Duration
	ByProject     map[string]ProjectReport
}

type ProjectReport struct {
	ProjectName string
	Duration    time.Duration
	Activities  []Activity
}

// UpdateActivityRequest describes a partial update of an existing activity.
// Nil fields keep the current value.
type UpdateActivityRequest struct {
	Description  *string
	Project      *string
	StartTime    *time.Time
	EndTime      *time.Time
	ClearEndTime bool
	Notes        *string
	Tags         *[]string
}

// IsEmpty reports whether the request carries no change at all.
func (r UpdateActivityRequest) IsEmpty() bool {
	return r.Description == nil && r.Project == nil && r.StartTime == nil &&
		r.EndTime == nil && !r.ClearEndTime && r.Notes == nil && r.Tags == nil
}

// Apply returns a copy of activity with the requested fields replaced.
func (r UpdateActivityRequest) Apply(activity Activity) Activity {
	updated := activity

	if r.Description != nil {
		updated.Description = *r.Description
	}
	if r.Project != nil {
		updated.Project = *r.Project
	}
	if r.StartTime != nil {
		updated.StartTime = *r.StartTime
	}

	switch {
	case r.ClearEndTime:
		updated.EndTime = nil
	case r.EndTime != nil:
		endTime := *r.EndTime
		updated.EndTime = &endTime
	}

	if r.Notes != nil {
		updated.Notes = *r.Notes
	}
	if r.Tags != nil {
		updated.Tags = slices.Clone(*r.Tags)
	}
	return updated
}

// MoveActivityToDay moves an activity to another day, keeping its time of day
// and its duration.
func MoveActivityToDay(activity Activity, day time.Time) Activity {
	startTime := time.Date(
		day.Year(), day.Month(), day.Day(),
		activity.StartTime.Hour(), activity.StartTime.Minute(), activity.StartTime.Second(), 0,
		activity.StartTime.Location(),
	)
	return RescheduleActivity(activity, startTime)
}

// RescheduleActivity moves an activity to a new start time, keeping its duration.
func RescheduleActivity(activity Activity, startTime time.Time) Activity {
	rescheduled := activity
	rescheduled.StartTime = startTime

	if activity.EndTime != nil {
		endTime := activity.EndTime.Add(startTime.Sub(activity.StartTime))
		rescheduled.EndTime = &endTime
	}
	return rescheduled
}
