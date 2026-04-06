package ports

import (
	"context"
	"time"

	"github.com/kriuchkov/tock/internal/core/models"
)

type ActivityResolver interface {
	Start(ctx context.Context, req models.StartActivityRequest) (*models.Activity, error)
	Stop(ctx context.Context, req models.StopActivityRequest) (*models.Activity, error)
	Add(ctx context.Context, req models.AddActivityRequest) (*models.Activity, error)
	List(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error)
	GetReport(ctx context.Context, filter models.ActivityFilter) (*models.Report, error)
	GetRecent(ctx context.Context, limit int) ([]models.Activity, error)
	GetLast(ctx context.Context) (*models.Activity, error)
	Remove(ctx context.Context, activity models.Activity) error
}

type ActivityRepository interface {
	Save(ctx context.Context, activity models.Activity) error
	FindLast(ctx context.Context) (*models.Activity, error)
	Find(ctx context.Context, filter models.ActivityFilter) ([]models.Activity, error)
	Remove(ctx context.Context, activity models.Activity) error
}

type NotesRepository interface {
	Save(ctx context.Context, activityID string, date time.Time, notes string, tags []string) error
	Get(ctx context.Context, activityID string, date time.Time) (string, []string, error)
}
