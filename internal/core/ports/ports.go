package ports

import (
	"context"

	"github.com/kriuchkov/tock/internal/core/dto"
	"github.com/kriuchkov/tock/internal/core/models"
)

type ActivityResolver interface {
	Start(ctx context.Context, req dto.StartActivityRequest) (*models.Activity, error)
	Stop(ctx context.Context, req dto.StopActivityRequest) (*models.Activity, error)
	List(ctx context.Context, filter dto.ActivityFilter) ([]models.Activity, error)
	GetReport(ctx context.Context, filter dto.ActivityFilter) (*dto.Report, error)
	GetRecent(ctx context.Context, limit int) ([]models.Activity, error)
}

type ActivityRepository interface {
	Save(ctx context.Context, activity models.Activity) error
	FindLast(ctx context.Context) (*models.Activity, error)
	Find(ctx context.Context, filter dto.ActivityFilter) ([]models.Activity, error)
}
