package errors

import "errors"

var (
	ErrActivityNotFound       = errors.New("activity not found")
	ErrNoActiveActivity       = errors.New("no active activity found")
	ErrActivityAlreadyStarted = errors.New("activity already started")
	ErrCancelled              = errors.New("operation cancelled")
	ErrNotesUnavailable       = errors.New("notes repository is not configured")
	ErrInvalidTimeRange       = errors.New("end time cannot be before start time")
	ErrStartTimeConflict      = errors.New("another activity already starts at this time")
	ErrAmbiguousStartTime     = errors.New("more than one activity starts at this time, so they cannot be told apart")
	ErrProjectRequired        = errors.New("project name is required")
	ErrDescriptionRequired    = errors.New("description is required")
	ErrReminderNotFound       = errors.New("reminder not found")
	ErrReminderTitleRequired  = errors.New("reminder title is required")
	ErrReminderDueRequired    = errors.New("reminder date is required")
	ErrNoReminderOccurrence   = errors.New("the reminder has no such occurrence")
	ErrReminderNeverDue       = errors.New("the reminder never comes due")
)
