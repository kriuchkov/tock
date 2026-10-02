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
	ErrProjectRequired        = errors.New("project name is required")
	ErrDescriptionRequired    = errors.New("description is required")
)
