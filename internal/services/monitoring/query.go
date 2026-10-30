package monitoring

import (
	"context"
	"time"

	"moitoring/internal/models"
)

type SnapshotRepository interface {
	Snapshot(context.Context, time.Time, time.Duration, []string) (models.MonitoringSnapshot, error)
}

type QueryService struct {
	store          SnapshotRepository
	staleAfter     time.Duration
	excludedQueues []string
}

func NewQueryService(store SnapshotRepository, pollInterval time.Duration, excludedQueues []string) QueryService {
	return QueryService{
		store:          store,
		staleAfter:     2 * pollInterval,
		excludedQueues: append([]string(nil), excludedQueues...),
	}
}

func (s QueryService) Snapshot(ctx context.Context) (models.MonitoringSnapshot, error) {
	return s.store.Snapshot(ctx, time.Now().UTC(), s.staleAfter, s.excludedQueues)
}
