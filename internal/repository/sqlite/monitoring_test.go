package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"moitoring/internal/models"
)

func TestNotificationClaimsAreExclusiveAndRecoverAfterLease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.db")
	dbA, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open first database handle: %v", err)
	}
	defer dbA.Close()
	dbB, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open second database handle: %v", err)
	}
	defer dbB.Close()
	storeA := NewMonitoringRepository(dbA)
	storeB := NewMonitoringRepository(dbB)
	now := time.Date(2026, time.October, 29, 10, 30, 0, 0, time.UTC)
	if _, err := storeA.OpenIncident(context.Background(), "queue:jobs", "rabbitmq_queue", "/jobs", "Queue is down", "reported down", []string{"ops@example.local"}, now); err != nil {
		t.Fatalf("open incident: %v", err)
	}

	start := make(chan struct{})
	results := make(chan []models.Notification, 2)
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for _, store := range []MonitoringRepository{storeA, storeB} {
		wait.Add(1)
		go func(store MonitoringRepository) {
			defer wait.Done()
			<-start
			claimed, err := store.ClaimDueNotifications(context.Background(), now, 1)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- claimed
		}(store)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent ClaimDueNotifications() error = %v", err)
	}

	var firstClaim models.Notification
	claimCount := 0
	for claimed := range results {
		claimCount += len(claimed)
		if len(claimed) == 1 {
			firstClaim = claimed[0]
		}
	}
	if claimCount != 1 || firstClaim.ClaimToken == "" {
		t.Fatalf("concurrent claims = %d, token %q; want one exclusive claim with token", claimCount, firstClaim.ClaimToken)
	}

	secondClaim, err := storeB.ClaimDueNotifications(context.Background(), now.Add(notificationClaimLease+time.Second), 1)
	if err != nil || len(secondClaim) != 1 {
		t.Fatalf("claim after expired lease = (%d notifications, %v), want one recoverable claim", len(secondClaim), err)
	}
	if secondClaim[0].ClaimToken == firstClaim.ClaimToken {
		t.Fatal("reclaimed notification reused the old claim token")
	}
	if err := storeA.MarkNotificationSent(context.Background(), firstClaim.ID, firstClaim.ClaimToken, now.Add(2*time.Minute)); err == nil {
		t.Fatal("MarkNotificationSent() with stale token succeeded")
	}
	if err := storeB.MarkNotificationSent(context.Background(), secondClaim[0].ID, secondClaim[0].ClaimToken, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("MarkNotificationSent() with current token error = %v", err)
	}
	var status string
	if err := dbA.QueryRow(`SELECT status FROM notification_outbox WHERE id = ?`, firstClaim.ID).Scan(&status); err != nil {
		t.Fatalf("read notification status: %v", err)
	}
	if status != "sent" {
		t.Fatalf("notification status = %q, want sent", status)
	}
}

func TestActiveIncidentKeysContainsOnlyUnresolvedIncidents(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "active-incidents.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	repository := NewMonitoringRepository(db)
	now := time.Date(2026, time.October, 29, 10, 30, 0, 0, time.UTC)
	if _, err := repository.OpenIncident(context.Background(), "queue:jobs", "rabbitmq_queue", "/jobs", "Queue is down", "reported down", nil, now); err != nil {
		t.Fatalf("OpenIncident() error = %v", err)
	}
	keys, err := repository.ActiveIncidentKeys(context.Background())
	if err != nil {
		t.Fatalf("ActiveIncidentKeys() error = %v", err)
	}
	if _, ok := keys["queue:jobs"]; !ok || len(keys) != 1 {
		t.Fatalf("active incident keys = %v, want only queue:jobs", keys)
	}
	if _, err := repository.ResolveIncident(context.Background(), "queue:jobs", "Recovered", "healthy", nil, now.Add(time.Minute)); err != nil {
		t.Fatalf("ResolveIncident() error = %v", err)
	}
	keys, err = repository.ActiveIncidentKeys(context.Background())
	if err != nil {
		t.Fatalf("ActiveIncidentKeys() after resolution error = %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("active incident keys after resolution = %v, want empty", keys)
	}
}

func TestAlertIncidentDeduplicatesUntilRecoveryAndRemindsDaily(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	repository := NewMonitoringRepository(db)
	start := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	recipients := []string{"ops@example.local"}

	opened, err := repository.OpenIncident(context.Background(), "queue:jobs", "rabbitmq_queue", "/jobs", "Queue is down", "RabbitMQ reported down", recipients, start)
	if err != nil || !opened {
		t.Fatalf("first OpenIncident() = (%t, %v), want opened", opened, err)
	}
	opened, err = repository.OpenIncident(context.Background(), "queue:jobs", "rabbitmq_queue", "/jobs", "Queue is down", "Still down", recipients, start.Add(time.Minute))
	if err != nil || opened {
		t.Fatalf("repeated OpenIncident() = (%t, %v), want no duplicate", opened, err)
	}

	if err := repository.QueueDueReminders(context.Background(), start.Add(time.Minute), 24*time.Hour, recipients, nil); err != nil {
		t.Fatalf("QueueDueReminders() before 24h error = %v", err)
	}
	if count := notificationCount(t, db); count != 1 {
		t.Fatalf("notifications before reminder = %d, want 1", count)
	}
	if err := repository.QueueDueReminders(context.Background(), start.Add(24*time.Hour), 24*time.Hour, recipients, nil); err != nil {
		t.Fatalf("QueueDueReminders() after 24h error = %v", err)
	}
	if count := notificationCount(t, db); count != 2 {
		t.Fatalf("notifications after reminder = %d, want 2", count)
	}

	resolvedAt := start.Add(24*time.Hour + time.Minute)
	resolved, err := repository.ResolveIncident(context.Background(), "queue:jobs", "Queue recovered", "Queue returned to idle", recipients, resolvedAt)
	if err != nil || !resolved {
		t.Fatalf("ResolveIncident() = (%t, %v), want resolved", resolved, err)
	}
	opened, err = repository.OpenIncident(context.Background(), "queue:jobs", "rabbitmq_queue", "/jobs", "Queue is down", "RabbitMQ reported down again", recipients, resolvedAt.Add(time.Minute))
	if err != nil || !opened {
		t.Fatalf("reopened OpenIncident() = (%t, %v), want a fresh incident", opened, err)
	}
	if count := notificationCount(t, db); count != 4 {
		t.Fatalf("notifications after recovery and re-open = %d, want 4", count)
	}
}

func TestNotificationsBatchMatchingEventsAndDailyReminders(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "notification-batches.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	repository := NewMonitoringRepository(db)
	start := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	recipients := []string{"ops@example.local"}

	for _, incident := range []struct {
		key, kind, target, title, details string
	}{
		{"queue:jobs", "rabbitmq_queue", "/jobs", "Queue jobs is down", "Reason: RabbitMQ returned down."},
		{"worker:jobs", "systemd_worker", "worker-a/jobs.service", "Worker jobs.service is down", "Reason: systemd reports failed."},
	} {
		opened, err := repository.OpenIncident(context.Background(), incident.key, incident.kind, incident.target, incident.title, incident.details, recipients, start)
		if err != nil || !opened {
			t.Fatalf("OpenIncident(%q) = (%t, %v), want opened", incident.key, opened, err)
		}
	}
	if count := notificationCount(t, db); count != 1 {
		t.Fatalf("notifications for two outages = %d, want one grouped email", count)
	}
	claimed, err := repository.ClaimDueNotifications(context.Background(), start, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimDueNotifications() = (%d notifications, %v), want one grouped email", len(claimed), err)
	}
	if claimed[0].Subject != "Monitoring alert: 2 issues" || !strings.Contains(claimed[0].Body, "Queue jobs is down") || !strings.Contains(claimed[0].Body, "Worker jobs.service is down") || !strings.Contains(claimed[0].Body, "systemd reports failed") {
		t.Fatalf("grouped outage email = %+v, want both incident reasons", claimed[0])
	}
	if err := repository.MarkNotificationSent(context.Background(), claimed[0].ID, claimed[0].ClaimToken, start); err != nil {
		t.Fatalf("MarkNotificationSent() error = %v", err)
	}

	reminderAt := start.Add(24 * time.Hour)
	if err := repository.QueueDueReminders(context.Background(), reminderAt, 24*time.Hour, recipients, nil); err != nil {
		t.Fatalf("QueueDueReminders() error = %v", err)
	}
	claimed, err = repository.ClaimDueNotifications(context.Background(), reminderAt, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("ClaimDueNotifications() for reminders = (%d notifications, %v), want one grouped reminder", len(claimed), err)
	}
	if claimed[0].Subject != "Still down: 2 issues" || !strings.Contains(claimed[0].Body, "RabbitMQ returned down") || !strings.Contains(claimed[0].Body, "systemd reports failed") {
		t.Fatalf("grouped reminder email = %+v, want both active incident reasons", claimed[0])
	}
}

func TestQueueRemindersRespectConfiguredTargets(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "filtered-reminders.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	repository := NewMonitoringRepository(db)
	start := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	recipients := []string{"ops@example.local"}

	for _, incident := range []struct {
		key, kind, target, title, details string
	}{
		{"queue:jobs", "rabbitmq_queue", "/jobs", "Queue jobs is down", "jobs reason"},
		{"queue:orders", "rabbitmq_queue", "/orders", "Queue orders is down", "orders reason"},
		{"worker:jobs", "systemd_worker", "worker-a/jobs.service", "Worker jobs.service is down", "worker reason"},
	} {
		if _, err := repository.OpenIncident(context.Background(), incident.key, incident.kind, incident.target, incident.title, incident.details, recipients, start); err != nil {
			t.Fatalf("OpenIncident(%q): %v", incident.key, err)
		}
	}
	opened, err := repository.ClaimDueNotifications(context.Background(), start, 10)
	if err != nil || len(opened) != 1 {
		t.Fatalf("ClaimDueNotifications() for opened incidents = (%d notifications, %v), want one batch", len(opened), err)
	}
	if err := repository.MarkNotificationSent(context.Background(), opened[0].ID, opened[0].ClaimToken, start); err != nil {
		t.Fatalf("MarkNotificationSent() for opened incidents: %v", err)
	}

	reminderAt := start.Add(24 * time.Hour)
	if err := repository.QueueDueReminders(context.Background(), reminderAt, 24*time.Hour, recipients, []string{"queue:jobs"}); err != nil {
		t.Fatalf("QueueDueReminders() error = %v", err)
	}
	reminders, err := repository.ClaimDueNotifications(context.Background(), reminderAt, 10)
	if err != nil || len(reminders) != 1 {
		t.Fatalf("ClaimDueNotifications() for reminders = (%d notifications, %v), want one batch", len(reminders), err)
	}
	reminder := reminders[0]
	if reminder.Subject != "Still down: 2 issues" || !strings.Contains(reminder.Body, "jobs reason") || strings.Contains(reminder.Body, "orders reason") || !strings.Contains(reminder.Body, "worker reason") {
		t.Fatalf("filtered reminder = %+v, want the selected queue and worker only", reminder)
	}
}

func TestSystemdMigrationPreservesExistingQueueData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitoring.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	repository := NewMonitoringRepository(db)
	now := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	if err := repository.SaveQueueObservations(context.Background(), []models.QueueObservation{{
		QueueKey: models.QueueKey{VHost: "/", Name: "jobs"}, State: "idle", Status: "healthy", ObservedAt: now,
	}}); err != nil {
		t.Fatalf("SaveQueueObservations() error = %v", err)
	}
	if _, err := db.Exec(`DROP TABLE systemd_workers`); err != nil {
		t.Fatalf("drop new systemd table: %v", err)
	}
	dropNotificationClaimSchema(t, db)
	if _, err := db.Exec(`PRAGMA user_version = 5`); err != nil {
		t.Fatalf("set old schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	db, err = Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() after v5 migration error = %v", err)
	}
	defer db.Close()
	repository = NewMonitoringRepository(db)
	if err := repository.SaveWorkerObservations(context.Background(), []models.WorkerObservation{{
		Host: "worker-a", Unit: "jobs.service", LoadState: "loaded", ExpectedState: "active", ActiveState: "failed", Status: "down", ObservedAt: now,
	}}); err != nil {
		t.Fatalf("SaveWorkerObservations() after migration error = %v", err)
	}
	snapshot, err := repository.Snapshot(context.Background(), now, time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot() after migration error = %v", err)
	}
	if len(snapshot.Queues) != 1 || snapshot.Queues[0].Name != "jobs" {
		t.Fatalf("queue data after migration = %+v, want existing jobs queue", snapshot.Queues)
	}
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].Unit != "jobs.service" || snapshot.Workers[0].LoadState != "loaded" {
		t.Fatalf("worker data after migration = %+v, want saved systemd unit", snapshot.Workers)
	}
}

func TestSystemdLoadStateMigrationPreservesWorkerStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "monitoring.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := db.Exec(`DROP TABLE systemd_workers`); err != nil {
		t.Fatalf("drop systemd worker table: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE systemd_workers (
		host TEXT NOT NULL,
		unit TEXT NOT NULL,
		expected_state TEXT NOT NULL,
		active_state TEXT NOT NULL,
		sub_state TEXT NOT NULL DEFAULT '',
		result TEXT NOT NULL DEFAULT '',
		exec_main_code TEXT NOT NULL DEFAULT '',
		exec_main_status TEXT NOT NULL DEFAULT '',
		status_text TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		observed_at INTEGER NOT NULL,
		PRIMARY KEY (host, unit)
	)`); err != nil {
		t.Fatalf("create v6 systemd worker table: %v", err)
	}
	now := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	if _, err := db.Exec(`INSERT INTO systemd_workers (host, unit, expected_state, active_state, sub_state, result, status, observed_at)
		VALUES ('worker-a', 'jobs.service', 'active', 'failed', 'failed', 'exit-code', 'down', ?)`, now.UnixNano()); err != nil {
		t.Fatalf("insert v6 systemd worker: %v", err)
	}
	dropNotificationClaimSchema(t, db)
	if _, err := db.Exec(`PRAGMA user_version = 6`); err != nil {
		t.Fatalf("set v6 schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	db, err = Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() after v7 migration error = %v", err)
	}
	defer db.Close()
	snapshot, err := NewMonitoringRepository(db).Snapshot(context.Background(), now, time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot() after v7 migration error = %v", err)
	}
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].Unit != "jobs.service" || snapshot.Workers[0].ActiveState != "failed" || snapshot.Workers[0].LoadState != "" {
		t.Fatalf("worker status after migration = %+v, want preserved state and blank new field", snapshot.Workers)
	}
}

func TestQueueObservationsPreserveMissingMetricsAndUnavailableTransition(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "monitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	repository := NewMonitoringRepository(db)
	now := time.Date(2026, time.October, 28, 10, 30, 0, 0, time.UTC)
	zero := int64(0)
	queues := []models.QueueObservation{{
		QueueKey:   models.QueueKey{VHost: "/", Name: "jobs"},
		Node:       "rabbit@node-a",
		State:      "idle",
		Status:     "warning",
		Consumers:  &zero,
		ObservedAt: now,
	}}
	if err := repository.SaveQueueObservations(context.Background(), queues); err != nil {
		t.Fatalf("SaveQueueObservations() error = %v", err)
	}
	if err := repository.RecordSourceSuccess(context.Background(), now); err != nil {
		t.Fatalf("RecordSourceSuccess() error = %v", err)
	}
	snapshot, err := repository.Snapshot(context.Background(), now.Add(time.Second), time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Source.Status != "up" || len(snapshot.Queues) != 1 {
		t.Fatalf("snapshot = (%q, %d queues), want up and one queue", snapshot.Source.Status, len(snapshot.Queues))
	}
	if snapshot.Queues[0].Consumers == nil || *snapshot.Queues[0].Consumers != 0 || snapshot.Queues[0].Messages != nil {
		t.Fatalf("queue metrics = (consumers %v, messages %v), want zero and unavailable", snapshot.Queues[0].Consumers, snapshot.Queues[0].Messages)
	}
	if snapshot.Queues[0].Node != "rabbit@node-a" {
		t.Fatalf("queue node = %q, want rabbit@node-a", snapshot.Queues[0].Node)
	}

	key := models.QueueKey{VHost: "/", Name: "jobs"}
	if err := repository.MarkQueueUnavailable(context.Background(), key, now.Add(2*time.Second)); err != nil {
		t.Fatalf("MarkQueueUnavailable() error = %v", err)
	}
	if err := repository.MarkQueueUnavailable(context.Background(), key, now.Add(3*time.Second)); err != nil {
		t.Fatalf("repeated MarkQueueUnavailable() error = %v", err)
	}
	snapshot, err = repository.Snapshot(context.Background(), now.Add(4*time.Second), time.Minute, nil)
	if err != nil {
		t.Fatalf("Snapshot() after missing queue error = %v", err)
	}
	if snapshot.Queues[0].Status != "down" || snapshot.Queues[0].State != "missing" || snapshot.Queues[0].Messages != nil {
		t.Fatalf("unavailable queue = %+v, want down/missing with unavailable metrics", snapshot.Queues[0])
	}
	var historyCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM queue_observations WHERE status = 'down'`).Scan(&historyCount); err != nil {
		t.Fatalf("count unavailable observations: %v", err)
	}
	if historyCount != 1 {
		t.Fatalf("unavailable history rows = %d, want one transition row", historyCount)
	}
}

func notificationCount(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notification_outbox`).Scan(&count); err != nil {
		t.Fatalf("count notification outbox: %v", err)
	}
	return count
}

func dropNotificationClaimSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`DROP INDEX notification_outbox_claim_idx`,
		`ALTER TABLE notification_outbox DROP COLUMN claim_token`,
		`ALTER TABLE notification_outbox DROP COLUMN claimed_at`,
		`DROP INDEX notification_outbox_batch_idx`,
		`ALTER TABLE notification_outbox DROP COLUMN item_count`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("restore pre-claim notification schema with %q: %v", statement, err)
		}
	}
}
