package monitoring

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"

	"moitoring/internal/models"
)

const maxEmailAttempts = 5

type Collector interface {
	Collect(context.Context) ([]models.QueueObservation, error)
}

type WorkerCollector interface {
	Collect(context.Context, []models.WorkerTarget) ([]models.WorkerObservation, error)
}

type Store interface {
	SaveQueueObservations(context.Context, []models.QueueObservation) error
	KnownQueues(context.Context) ([]models.QueueKey, error)
	MarkQueueUnavailable(context.Context, models.QueueKey, time.Time) error
	RecordSourceSuccess(context.Context, time.Time) error
	RecordSourceFailure(context.Context, time.Time, string) error
	OpenIncident(context.Context, string, string, string, string, string, []string, time.Time) (bool, error)
	ResolveIncident(context.Context, string, string, string, []string, time.Time) (bool, error)
	ActiveIncidentKeys(context.Context) (map[string]struct{}, error)
	QueueDueReminders(context.Context, time.Time, time.Duration, []string, []string) error
	ClaimDueNotifications(context.Context, time.Time, int) ([]models.Notification, error)
	MarkNotificationSent(context.Context, int64, string, time.Time) error
	MarkNotificationFailed(context.Context, int64, string, int, time.Time, string, int) error
	PruneQueueHistory(context.Context, time.Time) error
	Snapshot(context.Context, time.Time, time.Duration, []string) (models.MonitoringSnapshot, error)
	SaveWorkerObservations(context.Context, []models.WorkerObservation) error
	RecordWorkerSourceSuccess(context.Context, time.Time) error
	RecordWorkerSourceFailure(context.Context, time.Time, string) error
}

type EmailSender interface {
	Send(context.Context, models.Notification) error
}

type Settings struct {
	PollInterval     time.Duration
	HistoryRetention time.Duration
	ReminderInterval time.Duration
	Recipients       []string
	ExpectedQueues   []models.QueueKey
	ExcludedQueues   []string
	WorkerCollector  WorkerCollector
	Workers          []models.WorkerTarget
}

type Service struct {
	collector Collector
	store     Store
	sender    EmailSender
	settings  Settings
	now       func() time.Time
}

func NewService(collector Collector, store Store, sender EmailSender, settings Settings) *Service {
	return &Service{collector: collector, store: store, sender: sender, settings: settings, now: time.Now}
}

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.settings.PollInterval)
	defer ticker.Stop()

	if err := s.poll(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.poll(ctx); err != nil {
				return err
			}
		}
	}
}

func (s *Service) Snapshot(ctx context.Context) (models.MonitoringSnapshot, error) {
	snapshot, err := s.store.Snapshot(ctx, s.now().UTC(), 2*s.settings.PollInterval, s.settings.ExcludedQueues)
	if err != nil {
		return models.MonitoringSnapshot{}, err
	}
	return filterConfiguredQueues(snapshot, s.expectedQueueKeys()), nil
}

func (s *Service) poll(ctx context.Context) error {
	now := s.now().UTC()
	queues, err := s.collector.Collect(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.store.RecordSourceFailure(ctx, now, err.Error()); err != nil {
			return err
		}
		details := "RabbitMQ Management API could not be reached. The last successful queue values are now stale."
		if _, err := s.store.OpenIncident(ctx, "rabbitmq-source", "rabbitmq_source", "RabbitMQ", "RabbitMQ source is unavailable", details, s.settings.Recipients, now); err != nil {
			return err
		}
		if err := s.pollWorkers(ctx, now); err != nil {
			return err
		}
		return s.finishPoll(ctx, now)
	}

	expectedQueues := s.expectedQueueKeys()
	monitorConfiguredQueues := len(expectedQueues) > 0
	var knownQueues []models.QueueKey
	if !monitorConfiguredQueues {
		knownQueues, err = s.store.KnownQueues(ctx)
		if err != nil {
			return err
		}
	}
	excluded := make(map[string]struct{}, len(s.settings.ExcludedQueues))
	for _, name := range s.settings.ExcludedQueues {
		excluded[name] = struct{}{}
	}
	configured := make(map[string]struct{}, len(expectedQueues))
	for _, key := range expectedQueues {
		configured[queueID(key)] = struct{}{}
	}
	filtered := make([]models.QueueObservation, 0, len(queues))
	seen := make(map[string]struct{}, len(queues))
	for index := range queues {
		queue := &queues[index]
		if _, skip := excluded[queue.Name]; skip {
			continue
		}
		key := queueID(queue.QueueKey)
		if monitorConfiguredQueues {
			if _, monitor := configured[key]; !monitor {
				continue
			}
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		queue.ObservedAt = now
		queue.Status = statusFor(*queue)
		filtered = append(filtered, *queue)
	}
	if err := s.store.SaveQueueObservations(ctx, filtered); err != nil {
		return err
	}
	if err := s.store.RecordSourceSuccess(ctx, now); err != nil {
		return err
	}
	if _, err := s.store.ResolveIncident(ctx, "rabbitmq-source", "RabbitMQ source is available", "RabbitMQ Management API responded successfully.", s.settings.Recipients, now); err != nil {
		return err
	}

	current := make(map[string]models.QueueObservation, len(filtered))
	for _, queue := range filtered {
		current[queueID(queue.QueueKey)] = queue
	}
	missingHandled := make(map[string]struct{})
	for _, key := range expectedQueues {
		if _, ok := current[queueID(key)]; !ok {
			if _, skip := excluded[key.Name]; !skip {
				if err := s.markQueueDown(ctx, key, "expected queue was not returned by a successful RabbitMQ listing", now); err != nil {
					return err
				}
				missingHandled[queueID(key)] = struct{}{}
			}
		}
	}
	if !monitorConfiguredQueues {
		for _, key := range knownQueues {
			if _, skip := excluded[key.Name]; skip {
				continue
			}
			if _, ok := current[queueID(key)]; !ok {
				if _, handled := missingHandled[queueID(key)]; handled {
					continue
				}
				if err := s.markQueueDown(ctx, key, "queue was not returned by a successful RabbitMQ listing", now); err != nil {
					return err
				}
			}
		}
	}
	activeIncidents, err := s.store.ActiveIncidentKeys(ctx)
	if err != nil {
		return err
	}
	for _, queue := range filtered {
		if queue.Status == "down" {
			details := fmt.Sprintf("RabbitMQ reported state %q for queue %q in virtual host %q.", queue.State, queue.Name, queue.VHost)
			if _, err := s.store.OpenIncident(ctx, alertKey(queue.QueueKey), "rabbitmq_queue", queueTarget(queue.QueueKey), "RabbitMQ queue is down", details, s.settings.Recipients, now); err != nil {
				return err
			}
			continue
		}
		if queue.Status == "healthy" || queue.Status == "warning" {
			if _, active := activeIncidents[alertKey(queue.QueueKey)]; !active {
				continue
			}
			if _, err := s.store.ResolveIncident(ctx, alertKey(queue.QueueKey), "RabbitMQ queue recovered", fmt.Sprintf("Queue %q in virtual host %q returned to state %q.", queue.Name, queue.VHost, queue.State), s.settings.Recipients, now); err != nil {
				return err
			}
		}
	}
	if err := s.pollWorkers(ctx, now); err != nil {
		return err
	}
	return s.finishPoll(ctx, now)
}

func (s *Service) pollWorkers(ctx context.Context, now time.Time) error {
	if s.settings.WorkerCollector == nil || len(s.settings.Workers) == 0 {
		return nil
	}

	targets := make(map[string]models.WorkerTarget, len(s.settings.Workers))
	configured := make([]models.WorkerTarget, 0, len(s.settings.Workers))
	for _, target := range s.settings.Workers {
		unit := strings.TrimSpace(target.Unit)
		if unit == "" {
			continue
		}
		host := strings.TrimSpace(target.Host)
		if host == "" {
			host = "localhost"
		}
		target.Host = host
		target.Unit = unit
		key := workerTargetKey(host, unit)
		if _, duplicate := targets[key]; duplicate {
			continue
		}
		target.ExpectedState = strings.TrimSpace(target.ExpectedState)
		if target.ExpectedState == "" {
			target.ExpectedState = "active"
		}
		targets[key] = target
		configured = append(configured, target)
	}
	if len(configured) == 0 {
		return nil
	}

	workers, err := s.settings.WorkerCollector.Collect(ctx, configured)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.store.RecordWorkerSourceFailure(ctx, now, err.Error()); err != nil {
			return err
		}
		details := fmt.Sprintf("Could not read systemd worker state. Worker observations will become stale if collection remains unavailable. Reason: %s", err)
		_, err = s.store.OpenIncident(ctx, "systemd-source", "systemd_source", "systemd", "Systemd source is unavailable", details, s.settings.Recipients, now)
		return err
	}

	if len(workers) != len(configured) {
		return fmt.Errorf("systemd collector returned %d observations for %d configured targets", len(workers), len(configured))
	}

	seen := make(map[string]struct{}, len(workers))
	observed := make([]models.WorkerObservation, 0, len(workers))
	errorsByHost := make(map[string][]models.WorkerObservation)
	successesByHost := make(map[string]bool)
	for index := range workers {
		worker := &workers[index]
		host := strings.TrimSpace(worker.Host)
		if host == "" {
			host = configured[index].Host
		}
		key := workerTargetKey(host, worker.Unit)
		target, ok := targets[key]
		if !ok {
			return fmt.Errorf("systemd collector returned an unconfigured target %q on %q", worker.Unit, host)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("systemd collector returned target %q on %q more than once", worker.Unit, host)
		}
		seen[key] = struct{}{}
		worker.Host = target.Host
		worker.Unit = target.Unit
		worker.ExpectedState = target.ExpectedState
		if worker.CollectionError != "" {
			errorsByHost[worker.Host] = append(errorsByHost[worker.Host], *worker)
			continue
		}
		successesByHost[worker.Host] = true
		worker.ActiveState = strings.TrimSpace(worker.ActiveState)
		worker.SubState = strings.TrimSpace(worker.SubState)
		worker.Status = "down"
		if strings.EqualFold(worker.ActiveState, strings.TrimSpace(worker.ExpectedState)) {
			worker.Status = "healthy"
		}
		worker.ObservedAt = now
		observed = append(observed, *worker)
	}

	if err := s.store.SaveWorkerObservations(ctx, observed); err != nil {
		return err
	}
	if len(errorsByHost) == 0 {
		if err := s.store.RecordWorkerSourceSuccess(ctx, now); err != nil {
			return err
		}
	} else {
		failure := "one or more configured systemd targets could not be collected"
		for host, failedWorkers := range errorsByHost {
			if len(failedWorkers) > 0 {
				failure = fmt.Sprintf("systemd collection failed for %s/%s: %s", host, failedWorkers[0].Unit, failedWorkers[0].CollectionError)
				break
			}
		}
		if err := s.store.RecordWorkerSourceFailure(ctx, now, failure); err != nil {
			return err
		}
	}
	if len(errorsByHost) == 0 {
		if _, err := s.store.ResolveIncident(ctx, "systemd-source", "Systemd source is available", "Systemd unit properties were read successfully.", s.settings.Recipients, now); err != nil {
			return err
		}
	}

	activeIncidents, err := s.store.ActiveIncidentKeys(ctx)
	if err != nil {
		return err
	}
	for host, failedWorkers := range errorsByHost {
		if !successesByHost[host] {
			unitNames := make([]string, 0, len(failedWorkers))
			for _, worker := range failedWorkers {
				unitNames = append(unitNames, worker.Unit)
			}
			details := fmt.Sprintf("Could not collect configured systemd units on %s: %s. Previous worker observations remain unchanged and will become stale. Reason: %s", host, strings.Join(unitNames, ", "), failedWorkers[0].CollectionError)
			if _, err := s.store.OpenIncident(ctx, workerHostAlertKey(host), "systemd_source", host, "Systemd host is unavailable", details, s.settings.Recipients, now); err != nil {
				return err
			}
			continue
		}
		for _, worker := range failedWorkers {
			key := workerCollectionAlertKey(host, worker.Unit)
			details := fmt.Sprintf("Could not collect systemd state for %s on %s. Previous observation remains unchanged and will become stale. Reason: %s", worker.Unit, host, worker.CollectionError)
			if _, err := s.store.OpenIncident(ctx, key, "systemd_collection", host+"/"+worker.Unit, "Systemd worker could not be collected", details, s.settings.Recipients, now); err != nil {
				return err
			}
		}
	}
	for host := range successesByHost {
		key := workerHostAlertKey(host)
		if _, active := activeIncidents[key]; active {
			if _, err := s.store.ResolveIncident(ctx, key, "Systemd host is available", "Systemd unit properties were read successfully.", s.settings.Recipients, now); err != nil {
				return err
			}
		}
	}
	for _, worker := range observed {
		key := workerCollectionAlertKey(worker.Host, worker.Unit)
		if _, active := activeIncidents[key]; active {
			if _, err := s.store.ResolveIncident(ctx, key, "Systemd worker collection recovered", "Systemd unit properties were read successfully.", s.settings.Recipients, now); err != nil {
				return err
			}
		}
	}
	for _, worker := range observed {
		key := workerAlertKey(worker.Host, worker.Unit)
		if worker.Status == "healthy" {
			if _, active := activeIncidents[key]; !active {
				continue
			}
			details := workerDetails(worker)
			if _, err := s.store.ResolveIncident(ctx, key, "Systemd worker recovered", details, s.settings.Recipients, now); err != nil {
				return err
			}
			continue
		}
		if _, err := s.store.OpenIncident(ctx, key, "systemd_worker", worker.Host+"/"+worker.Unit,
			"Systemd worker is not in its expected state", workerDetails(worker), s.settings.Recipients, now); err != nil {
			return err
		}
	}
	return nil
}

func workerDetails(worker models.WorkerObservation) string {
	return fmt.Sprintf("Worker: %s on %s\nExpected state: %s\nLoadState: %s\nActiveState: %s\nSubState: %s\nResult: %s\nExecMainCode: %s\nExecMainStatus: %s\nStatusText: %s\nObserved at: %s",
		worker.Unit, worker.Host, worker.ExpectedState, worker.LoadState, worker.ActiveState, worker.SubState, worker.Result,
		worker.ExecMainCode, worker.ExecMainStatus, worker.StatusText, worker.ObservedAt.UTC().Format(time.RFC3339))
}

func workerAlertKey(host, unit string) string {
	encode := base64.RawURLEncoding.EncodeToString
	return "systemd-worker:" + encode([]byte(host)) + ":" + encode([]byte(unit))
}

func workerTargetKey(host, unit string) string {
	return host + "\x00" + unit
}

func workerHostAlertKey(host string) string {
	return "systemd-host:" + base64.RawURLEncoding.EncodeToString([]byte(host))
}

func workerCollectionAlertKey(host, unit string) string {
	encode := base64.RawURLEncoding.EncodeToString
	return "systemd-collection:" + encode([]byte(host)) + ":" + encode([]byte(unit))
}

func (s *Service) markQueueDown(ctx context.Context, key models.QueueKey, reason string, now time.Time) error {
	if err := s.store.MarkQueueUnavailable(ctx, key, now); err != nil {
		return err
	}
	details := fmt.Sprintf("Queue %q in virtual host %q is unavailable: %s.", key.Name, key.VHost, reason)
	if _, err := s.store.OpenIncident(ctx, alertKey(key), "rabbitmq_queue", queueTarget(key), "RabbitMQ queue is down", details, s.settings.Recipients, now); err != nil {
		return err
	}
	return nil
}

func (s *Service) finishPoll(ctx context.Context, now time.Time) error {
	if err := s.store.QueueDueReminders(ctx, now, s.settings.ReminderInterval, s.settings.Recipients, s.monitoredQueueAlertKeys()); err != nil {
		return err
	}
	if err := s.deliverQueued(ctx, now); err != nil {
		return err
	}
	if err := s.store.PruneQueueHistory(ctx, now.Add(-s.settings.HistoryRetention)); err != nil {
		return err
	}
	return nil
}

func (s *Service) monitoredQueueAlertKeys() []string {
	if len(s.settings.ExpectedQueues) == 0 {
		return nil
	}

	excluded := make(map[string]struct{}, len(s.settings.ExcludedQueues))
	for _, name := range s.settings.ExcludedQueues {
		excluded[name] = struct{}{}
	}
	keys := make([]string, 0, len(s.settings.ExpectedQueues))
	for _, key := range s.settings.ExpectedQueues {
		if _, skip := excluded[key.Name]; skip {
			continue
		}
		keys = append(keys, alertKey(key))
	}
	return keys
}

func (s *Service) deliverQueued(ctx context.Context, now time.Time) error {
	notifications, err := s.store.ClaimDueNotifications(ctx, now, 25)
	if err != nil {
		return err
	}
	for _, notification := range notifications {
		if err := s.sender.Send(ctx, notification); err != nil {
			delay := retryDelay(notification.Attempts)
			if markErr := s.store.MarkNotificationFailed(ctx, notification.ID, notification.ClaimToken, notification.Attempts, now.Add(delay), err.Error(), maxEmailAttempts); markErr != nil {
				return markErr
			}
			log.Printf("alert email %d delivery failed; retry in %s", notification.ID, delay)
			continue
		}
		if err := s.store.MarkNotificationSent(ctx, notification.ID, notification.ClaimToken, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) expectedQueueKeys() []models.QueueKey {
	return append([]models.QueueKey(nil), s.settings.ExpectedQueues...)
}

func statusFor(queue models.QueueObservation) string {
	state := strings.ToLower(strings.TrimSpace(queue.State))
	if state == "down" {
		return "down"
	}
	if queue.Consumers != nil && *queue.Consumers == 0 {
		return "warning"
	}
	switch state {
	case "running", "idle":
		return "healthy"
	default:
		return "unknown"
	}
}

func alertKey(queue models.QueueKey) string {
	encode := base64.RawURLEncoding.EncodeToString
	return "rabbitmq-queue:" + encode([]byte(queue.VHost)) + ":" + encode([]byte(queue.Name))
}

func queueID(queue models.QueueKey) string {
	return queue.VHost + "\x00" + queue.Name
}

func queueTarget(queue models.QueueKey) string {
	return queue.VHost + "/" + queue.Name
}

func retryDelay(attempts int) time.Duration {
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
	if attempts >= len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempts]
}
