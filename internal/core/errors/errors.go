package errors

import "errors"

var (
	ErrActivityNotFound       = errors.New("activity not found")
	ErrNoActiveActivity       = errors.New("no active activity found")
	ErrActivityAlreadyStarted = errors.New("activity already started")
)
