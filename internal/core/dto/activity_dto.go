package dto

import (
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
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
	Activities    []models.Activity
	TotalDuration time.Duration
	ByProject     map[string]ProjectReport
}

type ProjectReport struct {
	ProjectName string
	Duration    time.Duration
	Activities  []models.Activity
}
