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
	expectedQueues []models.QueueKey
}

func NewQueryService(store SnapshotRepository, pollInterval time.Duration, excludedQueues []string, expectedQueues []models.QueueKey) QueryService {
	return QueryService{
		store:          store,
		staleAfter:     2 * pollInterval,
		excludedQueues: append([]string(nil), excludedQueues...),
		expectedQueues: append([]models.QueueKey(nil), expectedQueues...),
	}
}

func (s QueryService) Snapshot(ctx context.Context) (models.MonitoringSnapshot, error) {
	snapshot, err := s.store.Snapshot(ctx, time.Now().UTC(), s.staleAfter, s.excludedQueues)
	if err != nil {
		return models.MonitoringSnapshot{}, err
	}
	return filterConfiguredQueues(snapshot, s.expectedQueues), nil
}

func filterConfiguredQueues(snapshot models.MonitoringSnapshot, expectedQueues []models.QueueKey) models.MonitoringSnapshot {
	if len(expectedQueues) == 0 {
		return snapshot
	}

	configured := make(map[string]struct{}, len(expectedQueues))
	for _, key := range expectedQueues {
		configured[queueID(key)] = struct{}{}
	}

	queues := snapshot.Queues[:0]
	for _, queue := range snapshot.Queues {
		if _, ok := configured[queueID(queue.QueueKey)]; ok {
			queues = append(queues, queue)
		}
	}
	snapshot.Queues = queues
	return snapshot
}
