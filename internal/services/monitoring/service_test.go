package monitoring

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"moitoring/internal/models"
	"moitoring/internal/repository/sqlite"
)

type testCollector struct {
	queues []models.QueueObservation
	err    error
}

func (c *testCollector) Collect(context.Context) ([]models.QueueObservation, error) {
	return append([]models.QueueObservation(nil), c.queues...), c.err
}

type recordingSender struct {
	notifications []models.Notification
}

type resolveCountingStore struct {
	Store
	resolvedKeys []string
}

func (s *resolveCountingStore) ResolveIncident(ctx context.Context, key, title, details string, recipients []string, now time.Time) (bool, error) {
	s.resolvedKeys = append(s.resolvedKeys, key)
	return s.Store.ResolveIncident(ctx, key, title, details, recipients, now)
}

type firstSendBlockingSender struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (s *firstSendBlockingSender) Send(ctx context.Context, _ models.Notification) error {
	if s.calls.Add(1) == 1 {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type simulatedWorkerCollector struct {
	workers []models.WorkerObservation
	err     error
}

func (c *simulatedWorkerCollector) Collect(_ context.Context, _ []models.WorkerTarget) ([]models.WorkerObservation, error) {
	return append([]models.WorkerObservation(nil), c.workers...), c.err
}

func (s *recordingSender) Send(_ context.Context, notification models.Notification) error {
	s.notifications = append(s.notifications, notification)
	return nil
}

func TestDownReminderRecoveryAndNewDownTransition(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	store := sqlite.NewMonitoringRepository(db)
	collector := &testCollector{}
	sender := &recordingSender{}
	queueKey := models.QueueKey{VHost: "/", Name: "jobs"}
	service := NewService(collector, store, sender, Settings{
		PollInterval:     time.Minute,
		HistoryRetention: 48 * time.Hour,
		ReminderInterval: 24 * time.Hour,
		Recipients:       []string{"ops@example.local"},
		ExpectedQueues:   []models.QueueKey{queueKey},
		ExcludedQueues:   []string{},
	})
	now := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	zeroConsumers := int64(0)
	collector.queues = []models.QueueObservation{{QueueKey: queueKey, State: "down", Consumers: &zeroConsumers}}

	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("first poll error = %v", err)
	}
	if len(sender.notifications) != 1 || sender.notifications[0].Subject != "RabbitMQ queue is down" {
		t.Fatalf("first down notifications = %+v, want one immediate alert", sender.notifications)
	}

	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("repeated down poll error = %v", err)
	}
	if len(sender.notifications) != 1 {
		t.Fatalf("notifications after one minute = %d, want no duplicate", len(sender.notifications))
	}

	now = now.Add(24*time.Hour - time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("daily reminder poll error = %v", err)
	}
	if len(sender.notifications) != 2 || sender.notifications[1].Subject != "Still down: RabbitMQ queue is down" {
		t.Fatalf("daily reminder notifications = %+v, want one reminder", sender.notifications)
	}

	consumers := int64(0)
	collector.queues = []models.QueueObservation{{QueueKey: queueKey, State: "idle", Consumers: &consumers}}
	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("recovery poll error = %v", err)
	}
	if len(sender.notifications) != 3 || sender.notifications[2].Subject != "Recovered: RabbitMQ queue recovered" {
		t.Fatalf("recovery notifications = %+v, want recovery email", sender.notifications)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Queues[0].Status != "warning" {
		t.Fatalf("queue status after recovery with zero consumers = %q, want warning", snapshot.Queues[0].Status)
	}

	collector.queues = []models.QueueObservation{{QueueKey: queueKey, State: "down"}}
	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("second outage poll error = %v", err)
	}
	if len(sender.notifications) != 4 || sender.notifications[3].Subject != "RabbitMQ queue is down" {
		t.Fatalf("notifications after a new outage = %+v, want a fresh immediate alert", sender.notifications)
	}
}

func TestConcurrentWorkersSendOneClaimedNotification(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitoring.db")
	dbA, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open first database handle: %v", err)
	}
	defer dbA.Close()
	dbB, err := sqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open second database handle: %v", err)
	}
	defer dbB.Close()
	now := time.Date(2026, time.October, 29, 10, 30, 0, 0, time.UTC)
	storeA := sqlite.NewMonitoringRepository(dbA)
	storeB := sqlite.NewMonitoringRepository(dbB)
	if _, err := storeA.OpenIncident(context.Background(), "queue:jobs", "rabbitmq_queue", "/jobs", "Queue is down", "reported down", []string{"ops@example.local"}, now); err != nil {
		t.Fatalf("open incident: %v", err)
	}
	sender := &firstSendBlockingSender{entered: make(chan struct{}), release: make(chan struct{})}
	workerA := NewService(nil, storeA, sender, Settings{})
	workerB := NewService(nil, storeB, sender, Settings{})
	workerResult := make(chan error, 1)
	go func() { workerResult <- workerA.deliverQueued(context.Background(), now) }()
	select {
	case <-sender.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first worker did not start sending")
	}
	if err := workerB.deliverQueued(context.Background(), now); err != nil {
		t.Fatalf("second worker delivery error = %v", err)
	}
	close(sender.release)
	select {
	case err := <-workerResult:
		if err != nil {
			t.Fatalf("first worker delivery error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first worker did not finish sending")
	}
	if got := sender.calls.Load(); got != 1 {
		t.Fatalf("email sends = %d, want exactly one across workers", got)
	}
}

func TestHealthyQueuePollingSkipsPerQueueIncidentTransactions(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "healthy-queues.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	repository := sqlite.NewMonitoringRepository(db)
	store := &resolveCountingStore{Store: repository}
	queues := make([]models.QueueObservation, 250)
	for index := range queues {
		queues[index] = models.QueueObservation{
			QueueKey: models.QueueKey{VHost: "/", Name: fmt.Sprintf("queue-%03d", index)},
			State:    "idle",
		}
	}
	service := NewService(&testCollector{queues: queues}, store, &recordingSender{}, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
	})
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("poll healthy queues error = %v", err)
	}
	if len(store.resolvedKeys) != 1 || store.resolvedKeys[0] != "rabbitmq-source" {
		t.Fatalf("ResolveIncident() calls = %v, want only one source recovery call", store.resolvedKeys)
	}
}

func TestZeroConsumersIsWarningWithoutCriticalEmail(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	store := sqlite.NewMonitoringRepository(db)
	consumers := int64(0)
	collector := &testCollector{queues: []models.QueueObservation{{
		QueueKey: models.QueueKey{VHost: "/", Name: "jobs"}, State: "unknown", Consumers: &consumers,
	}}}
	sender := &recordingSender{}
	service := NewService(collector, store, sender, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
		Recipients: []string{"ops@example.local"},
	})
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("poll error = %v", err)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Queues[0].Status != "warning" {
		t.Fatalf("queue status = %q, want warning", snapshot.Queues[0].Status)
	}
	if len(sender.notifications) != 0 {
		t.Fatalf("emails = %d, want zero critical emails for zero consumers", len(sender.notifications))
	}
}

func TestRabbitMQSourceFailureAlertsAndRecovers(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	collector := &testCollector{err: errors.New("RabbitMQ management API returned HTTP 503")}
	sender := &recordingSender{}
	service := NewService(collector, sqlite.NewMonitoringRepository(db), sender, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
		Recipients: []string{"ops@example.local"},
	})
	now := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("failure poll error = %v", err)
	}
	if len(sender.notifications) != 1 || sender.notifications[0].Subject != "RabbitMQ source is unavailable" {
		t.Fatalf("source failure notifications = %+v", sender.notifications)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Source.Status != "down" {
		t.Fatalf("source status = %q, want down", snapshot.Source.Status)
	}

	collector.err = nil
	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("recovery poll error = %v", err)
	}
	if len(sender.notifications) != 2 || sender.notifications[1].Subject != "Recovered: RabbitMQ source is available" {
		t.Fatalf("source recovery notifications = %+v", sender.notifications)
	}
}

func TestWorkerFailureReminderRecoveryAndNewFailure(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	collector := &simulatedWorkerCollector{workers: []models.WorkerObservation{{
		Host: "worker-a", Unit: "jobs.service", LoadState: "loaded", ActiveState: "failed", SubState: "failed",
		Result: "exit-code", ExecMainCode: "1", ExecMainStatus: "1", StatusText: "process exited with status 1",
	}}}
	sender := &recordingSender{}
	service := NewService(&testCollector{}, sqlite.NewMonitoringRepository(db), sender, Settings{
		PollInterval:     time.Minute,
		HistoryRetention: 24 * time.Hour,
		ReminderInterval: 24 * time.Hour,
		Recipients:       []string{"ops@example.local"},
		WorkerCollector:  collector,
		Workers:          []models.WorkerTarget{{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"}},
	})
	now := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("first failed poll error = %v", err)
	}
	if len(sender.notifications) != 1 || sender.notifications[0].Subject != "Systemd worker is not in its expected state" {
		t.Fatalf("first worker alert = %+v, want one immediate failure email", sender.notifications)
	}
	if !strings.Contains(sender.notifications[0].Body, "LoadState: loaded") || !strings.Contains(sender.notifications[0].Body, "Result: exit-code") || !strings.Contains(sender.notifications[0].Body, "ExecMainStatus: 1") {
		t.Fatalf("worker alert body = %q, want systemd failure details", sender.notifications[0].Body)
	}

	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("repeated failed poll error = %v", err)
	}
	if len(sender.notifications) != 1 {
		t.Fatalf("notifications before reminder = %d, want no duplicate", len(sender.notifications))
	}

	now = now.Add(24*time.Hour - time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("daily reminder poll error = %v", err)
	}
	if len(sender.notifications) != 2 || sender.notifications[1].Subject != "Still down: Systemd worker is not in its expected state" {
		t.Fatalf("daily worker reminder = %+v, want one reminder", sender.notifications)
	}

	collector.workers[0].ActiveState = "active"
	collector.workers[0].SubState = "running"
	collector.workers[0].Result = "success"
	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("recovery poll error = %v", err)
	}
	if len(sender.notifications) != 3 || sender.notifications[2].Subject != "Recovered: Systemd worker recovered" {
		t.Fatalf("worker recovery email = %+v, want one recovery notification", sender.notifications)
	}

	collector.workers[0].ActiveState = "inactive"
	collector.workers[0].SubState = "dead"
	collector.workers[0].Result = "success"
	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("second failure poll error = %v", err)
	}
	if len(sender.notifications) != 4 || sender.notifications[3].Subject != "Systemd worker is not in its expected state" {
		t.Fatalf("new worker failure email = %+v, want immediate alert after recovery", sender.notifications)
	}

	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].ActiveState != "inactive" || snapshot.Workers[0].SubState != "dead" || snapshot.Workers[0].Status != "down" {
		t.Fatalf("worker snapshot = %+v, want latest inactive/dead observation", snapshot.Workers)
	}
}

func TestWorkerSourceFailureIsDistinctFromWorkerFailure(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	collector := &simulatedWorkerCollector{err: errors.New("systemctl is unavailable")}
	sender := &recordingSender{}
	service := NewService(&testCollector{}, sqlite.NewMonitoringRepository(db), sender, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
		Recipients: []string{"ops@example.local"}, WorkerCollector: collector,
		Workers: []models.WorkerTarget{{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"}},
	})
	now := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("source failure poll error = %v", err)
	}
	if len(sender.notifications) != 1 || sender.notifications[0].Subject != "Systemd source is unavailable" {
		t.Fatalf("source failure email = %+v, want a distinct source alert", sender.notifications)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.WorkerSource.Status != "down" || len(snapshot.Workers) != 0 {
		t.Fatalf("snapshot source/workers = (%q, %d), want source down and no false worker state", snapshot.WorkerSource.Status, len(snapshot.Workers))
	}

	collector.err = nil
	collector.workers = []models.WorkerObservation{{Host: "worker-a", Unit: "jobs.service", ActiveState: "active"}}
	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("source recovery poll error = %v", err)
	}
	if len(sender.notifications) != 2 || sender.notifications[1].Subject != "Recovered: Systemd source is available" {
		t.Fatalf("source recovery email = %+v, want one recovery notification", sender.notifications)
	}
	snapshot, err = service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() after recovery error = %v", err)
	}
	if snapshot.WorkerSource.Status != "up" || len(snapshot.Workers) != 1 || snapshot.Workers[0].Status != "healthy" {
		t.Fatalf("recovered source/workers = (%q, %+v), want up with one healthy worker", snapshot.WorkerSource.Status, snapshot.Workers)
	}
}

func TestHealthyWorkerDoesNotSendFailureEmail(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	collector := &simulatedWorkerCollector{workers: []models.WorkerObservation{{
		Host: "worker-a", Unit: "jobs.service", ActiveState: "active", SubState: "running",
	}}}
	sender := &recordingSender{}
	service := NewService(&testCollector{}, sqlite.NewMonitoringRepository(db), sender, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
		Recipients: []string{"ops@example.local"}, WorkerCollector: collector,
		Workers: []models.WorkerTarget{{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"}},
	})
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("healthy poll error = %v", err)
	}
	if len(sender.notifications) != 0 {
		t.Fatalf("emails = %+v, want no failure email for a healthy worker", sender.notifications)
	}
}

func TestUnknownSystemdStateRemainsVisibleAndUnhealthy(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	collector := &simulatedWorkerCollector{workers: []models.WorkerObservation{{
		Host: "worker-a", Unit: "jobs.service", LoadState: "loaded", ActiveState: "new-systemd-state", SubState: "custom",
	}}}
	sender := &recordingSender{}
	service := NewService(&testCollector{}, sqlite.NewMonitoringRepository(db), sender, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
		Recipients: []string{"ops@example.local"}, WorkerCollector: collector,
		Workers: []models.WorkerTarget{{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"}},
	})
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("unknown-state poll error = %v", err)
	}
	if len(sender.notifications) != 1 || !strings.Contains(sender.notifications[0].Body, "ActiveState: new-systemd-state") {
		t.Fatalf("unknown-state notification = %+v, want alert with raw state", sender.notifications)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].ActiveState != "new-systemd-state" || snapshot.Workers[0].Status != "down" {
		t.Fatalf("worker status = %+v, want raw unknown state marked down", snapshot.Workers)
	}
}

func TestWorkerCollectionFailureKeepsLastObservationStaleAndRecovers(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	collector := &simulatedWorkerCollector{workers: []models.WorkerObservation{{
		Host: "worker-a", Unit: "jobs.service", LoadState: "loaded", ActiveState: "active", SubState: "running",
	}}}
	sender := &recordingSender{}
	service := NewService(&testCollector{}, sqlite.NewMonitoringRepository(db), sender, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
		Recipients: []string{"ops@example.local"}, WorkerCollector: collector,
		Workers: []models.WorkerTarget{{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"}},
	})
	now := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("healthy poll error = %v", err)
	}

	collector.workers[0] = models.WorkerObservation{
		Host: "worker-a", Unit: "jobs.service", CollectionError: "ssh exited with status 255",
	}
	now = now.Add(3 * time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("unreachable-host poll error = %v", err)
	}
	if len(sender.notifications) != 1 || sender.notifications[0].Subject != "Systemd host is unavailable" {
		t.Fatalf("host outage notifications = %+v, want one host-level alert", sender.notifications)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() during outage error = %v", err)
	}
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].Status != "healthy" || !snapshot.Workers[0].IsStale {
		t.Fatalf("worker during host outage = %+v, want last healthy value marked stale", snapshot.Workers)
	}

	collector.workers[0] = models.WorkerObservation{
		Host: "worker-a", Unit: "jobs.service", LoadState: "loaded", ActiveState: "active", SubState: "running",
	}
	now = now.Add(time.Minute)
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("host recovery poll error = %v", err)
	}
	if len(sender.notifications) != 2 || sender.notifications[1].Subject != "Recovered: Systemd host is available" {
		t.Fatalf("host recovery notifications = %+v, want one recovery email", sender.notifications)
	}
	snapshot, err = service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() after recovery error = %v", err)
	}
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].Status != "healthy" || snapshot.Workers[0].IsStale {
		t.Fatalf("worker after host recovery = %+v, want a fresh healthy observation", snapshot.Workers)
	}
}

func TestSameUnitOnDifferentHostsUsesEachExpectedState(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	collector := &simulatedWorkerCollector{workers: []models.WorkerObservation{
		{Host: "worker-a", Unit: "jobs.service", ActiveState: "active"},
		{Host: "worker-b", Unit: "jobs.service", ActiveState: "inactive"},
	}}
	sender := &recordingSender{}
	service := NewService(&testCollector{}, sqlite.NewMonitoringRepository(db), sender, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
		Recipients: []string{"ops@example.local"}, WorkerCollector: collector,
		Workers: []models.WorkerTarget{
			{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"},
			{Host: "worker-b", Unit: "jobs.service", ExpectedState: "inactive"},
		},
	})
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("poll error = %v", err)
	}
	if len(sender.notifications) != 0 {
		t.Fatalf("notifications = %+v, want both target-specific expected states healthy", sender.notifications)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Workers) != 2 || snapshot.Workers[0].Host != "worker-a" || snapshot.Workers[0].ExpectedState != "active" || snapshot.Workers[1].Host != "worker-b" || snapshot.Workers[1].ExpectedState != "inactive" {
		t.Fatalf("workers = %+v, want same unit kept separate with per-host expected states", snapshot.Workers)
	}
}

func TestPartialHostFailureAlertsOnlyFailedTarget(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	collector := &simulatedWorkerCollector{workers: []models.WorkerObservation{
		{Host: "worker-a", Unit: "jobs.service", ActiveState: "active"},
		{Host: "worker-a", Unit: "billing.service", CollectionError: "permission denied"},
	}}
	sender := &recordingSender{}
	service := NewService(&testCollector{}, sqlite.NewMonitoringRepository(db), sender, Settings{
		PollInterval: time.Minute, HistoryRetention: time.Hour, ReminderInterval: 24 * time.Hour,
		Recipients: []string{"ops@example.local"}, WorkerCollector: collector,
		Workers: []models.WorkerTarget{
			{Host: "worker-a", Unit: "jobs.service", ExpectedState: "active"},
			{Host: "worker-a", Unit: "billing.service", ExpectedState: "active"},
		},
	})
	if err := service.poll(context.Background()); err != nil {
		t.Fatalf("poll error = %v", err)
	}
	if len(sender.notifications) != 1 || sender.notifications[0].Subject != "Systemd worker could not be collected" {
		t.Fatalf("partial-failure notifications = %+v, want one target-level collection alert", sender.notifications)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].Unit != "jobs.service" || snapshot.Workers[0].Status != "healthy" {
		t.Fatalf("workers = %+v, want successful unit only and healthy", snapshot.Workers)
	}
}
