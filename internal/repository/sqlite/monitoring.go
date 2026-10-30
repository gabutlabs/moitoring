package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"moitoring/internal/models"
)

type MonitoringRepository struct {
	db *sql.DB
}

func NewMonitoringRepository(db *sql.DB) MonitoringRepository {
	return MonitoringRepository{db: db}
}

func (r MonitoringRepository) SaveQueueObservations(ctx context.Context, queues []models.QueueObservation) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin queue observation transaction: %w", err)
	}
	defer tx.Rollback()

	current, err := tx.PrepareContext(ctx, `
		INSERT INTO rabbitmq_queues (
			vhost, name, node, state, status, messages, messages_ready,
			messages_unacknowledged, consumers, publish_rate, delivery_rate, observed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(vhost, name) DO UPDATE SET
			node = excluded.node,
			state = excluded.state,
			status = excluded.status,
			messages = excluded.messages,
			messages_ready = excluded.messages_ready,
			messages_unacknowledged = excluded.messages_unacknowledged,
			consumers = excluded.consumers,
			publish_rate = excluded.publish_rate,
			delivery_rate = excluded.delivery_rate,
			observed_at = excluded.observed_at
	`)
	if err != nil {
		return fmt.Errorf("prepare current queue update: %w", err)
	}
	defer current.Close()

	history, err := tx.PrepareContext(ctx, `
		INSERT INTO queue_observations (
			vhost, name, node, state, status, messages, messages_ready,
			messages_unacknowledged, consumers, publish_rate, delivery_rate, observed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare queue history insert: %w", err)
	}
	defer history.Close()

	for _, queue := range queues {
		observedAt := queue.ObservedAt.UTC().UnixNano()
		if queue.ObservedAt.IsZero() {
			observedAt = time.Now().UTC().UnixNano()
		}
		args := []any{
			queue.VHost, queue.Name, queue.Node, queue.State, queue.Status,
			nullableInt(queue.Messages), nullableInt(queue.MessagesReady), nullableInt(queue.MessagesUnacked), nullableInt(queue.Consumers),
			nullableFloat(queue.PublishRate), nullableFloat(queue.DeliveryRate), observedAt,
		}
		if _, err := current.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("save current state for queue %q: %w", queue.Name, err)
		}
		if _, err := history.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("save history for queue %q: %w", queue.Name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit queue observations: %w", err)
	}
	return nil
}

func (r MonitoringRepository) SaveWorkerObservations(ctx context.Context, workers []models.WorkerObservation) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin worker observation transaction: %w", err)
	}
	defer tx.Rollback()

	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO systemd_workers (
			host, unit, load_state, expected_state, active_state, sub_state, result,
			exec_main_code, exec_main_status, status_text, status, observed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(host, unit) DO UPDATE SET
			load_state = excluded.load_state,
			expected_state = excluded.expected_state,
			active_state = excluded.active_state,
			sub_state = excluded.sub_state,
			result = excluded.result,
			exec_main_code = excluded.exec_main_code,
			exec_main_status = excluded.exec_main_status,
			status_text = excluded.status_text,
			status = excluded.status,
			observed_at = excluded.observed_at
	`)
	if err != nil {
		return fmt.Errorf("prepare worker status update: %w", err)
	}
	defer statement.Close()

	for _, worker := range workers {
		observedAt := worker.ObservedAt.UTC().UnixNano()
		if worker.ObservedAt.IsZero() {
			observedAt = time.Now().UTC().UnixNano()
		}
		if _, err := statement.ExecContext(ctx, worker.Host, worker.Unit, worker.LoadState, worker.ExpectedState, worker.ActiveState,
			worker.SubState, worker.Result, worker.ExecMainCode, worker.ExecMainStatus, worker.StatusText, worker.Status, observedAt); err != nil {
			return fmt.Errorf("save current state for systemd unit %q: %w", worker.Unit, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit worker observations: %w", err)
	}
	return nil
}

func (r MonitoringRepository) KnownQueues(ctx context.Context) ([]models.QueueKey, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT vhost, name FROM rabbitmq_queues ORDER BY vhost, name`)
	if err != nil {
		return nil, fmt.Errorf("list known RabbitMQ queues: %w", err)
	}
	defer rows.Close()

	queues := make([]models.QueueKey, 0)
	for rows.Next() {
		var queue models.QueueKey
		if err := rows.Scan(&queue.VHost, &queue.Name); err != nil {
			return nil, fmt.Errorf("read known RabbitMQ queue: %w", err)
		}
		queues = append(queues, queue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate known RabbitMQ queues: %w", err)
	}
	return queues, nil
}

func (r MonitoringRepository) MarkQueueUnavailable(ctx context.Context, queue models.QueueKey, observedAt time.Time) error {
	observation := models.QueueObservation{
		QueueKey:   queue,
		State:      "missing",
		Status:     "down",
		ObservedAt: observedAt,
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin unavailable queue update: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO rabbitmq_queues (
			vhost, name, state, status, observed_at
		) VALUES (?, ?, 'missing', 'down', ?)
		ON CONFLICT(vhost, name) DO UPDATE SET
			state = 'missing', status = 'down', messages = NULL, messages_ready = NULL,
			messages_unacknowledged = NULL, consumers = NULL, publish_rate = NULL,
			delivery_rate = NULL, observed_at = excluded.observed_at
		WHERE rabbitmq_queues.state != 'missing' OR rabbitmq_queues.status != 'down'
	`, queue.VHost, queue.Name, observedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("mark RabbitMQ queue unavailable: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check unavailable queue update: %w", err)
	}
	if changed == 0 {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO queue_observations (vhost, name, state, status, observed_at)
		VALUES (?, ?, ?, ?, ?)
	`, observation.VHost, observation.Name, observation.State, observation.Status, observedAt.UTC().UnixNano()); err != nil {
		return fmt.Errorf("save unavailable queue history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit unavailable queue update: %w", err)
	}
	return nil
}

func (r MonitoringRepository) RecordSourceSuccess(ctx context.Context, observedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO monitoring_sources (name, status, last_success_at, checked_at, last_error)
		VALUES ('rabbitmq', 'up', ?, ?, '')
		ON CONFLICT(name) DO UPDATE SET
			status = 'up', last_success_at = excluded.last_success_at,
			checked_at = excluded.checked_at, last_error = ''
	`, observedAt.UTC().UnixNano(), observedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("record RabbitMQ source success: %w", err)
	}
	return nil
}

func (r MonitoringRepository) RecordSourceFailure(ctx context.Context, checkedAt time.Time, safeError string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO monitoring_sources (name, status, last_success_at, checked_at, last_error)
		VALUES ('rabbitmq', 'down', NULL, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			status = 'down', checked_at = excluded.checked_at, last_error = excluded.last_error
	`, checkedAt.UTC().UnixNano(), safeError)
	if err != nil {
		return fmt.Errorf("record RabbitMQ source failure: %w", err)
	}
	return nil
}

func (r MonitoringRepository) RecordWorkerSourceSuccess(ctx context.Context, observedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO monitoring_sources (name, status, last_success_at, checked_at, last_error)
		VALUES ('systemd', 'up', ?, ?, '')
		ON CONFLICT(name) DO UPDATE SET
			status = 'up', last_success_at = excluded.last_success_at,
			checked_at = excluded.checked_at, last_error = ''
	`, observedAt.UTC().UnixNano(), observedAt.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("record systemd source success: %w", err)
	}
	return nil
}

func (r MonitoringRepository) RecordWorkerSourceFailure(ctx context.Context, checkedAt time.Time, safeError string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO monitoring_sources (name, status, last_success_at, checked_at, last_error)
		VALUES ('systemd', 'down', NULL, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			status = 'down', checked_at = excluded.checked_at, last_error = excluded.last_error
	`, checkedAt.UTC().UnixNano(), safeError)
	if err != nil {
		return fmt.Errorf("record systemd source failure: %w", err)
	}
	return nil
}

func (r MonitoringRepository) OpenIncident(ctx context.Context, key, kind, target, title, details string, recipients []string, now time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin alert incident transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO alert_incidents (
			alert_key, kind, target, active, title, details, opened_at, last_notified_at, resolved_at
		) VALUES (?, ?, ?, 1, ?, ?, ?, ?, NULL)
		ON CONFLICT(alert_key) DO UPDATE SET
			kind = excluded.kind, target = excluded.target, active = 1,
			title = excluded.title, details = excluded.details,
			opened_at = excluded.opened_at, last_notified_at = excluded.last_notified_at,
			resolved_at = NULL
		WHERE alert_incidents.active = 0
	`, key, kind, target, title, details, now.UTC().UnixNano(), now.UTC().UnixNano())
	if err != nil {
		return false, fmt.Errorf("open alert incident: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check alert incident state: %w", err)
	}
	if changed == 0 {
		return false, tx.Commit()
	}
	if err := insertNotification(ctx, tx, key, "opened", recipients, title, details, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit alert incident: %w", err)
	}
	return true, nil
}

func (r MonitoringRepository) ResolveIncident(ctx context.Context, key, title, details string, recipients []string, now time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin alert recovery transaction: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		UPDATE alert_incidents SET active = 0, resolved_at = ?
		WHERE alert_key = ? AND active = 1
	`, now.UTC().UnixNano(), key)
	if err != nil {
		return false, fmt.Errorf("resolve alert incident: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check alert recovery state: %w", err)
	}
	if changed == 0 {
		return false, tx.Commit()
	}
	if err := insertNotification(ctx, tx, key, "recovered", recipients, "Recovered: "+title, details, now); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit alert recovery: %w", err)
	}
	return true, nil
}

func (r MonitoringRepository) ActiveIncidentKeys(ctx context.Context) (map[string]struct{}, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT alert_key FROM alert_incidents WHERE active = 1`)
	if err != nil {
		return nil, fmt.Errorf("list active alert incidents: %w", err)
	}
	defer rows.Close()

	keys := make(map[string]struct{})
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("read active alert incident: %w", err)
		}
		keys[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active alert incidents: %w", err)
	}
	return keys, nil
}

func (r MonitoringRepository) QueueDueReminders(ctx context.Context, now time.Time, interval time.Duration, recipients []string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin alert reminder transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT alert_key, title, details, opened_at, last_notified_at
		FROM alert_incidents
		WHERE active = 1 AND last_notified_at <= ?
		ORDER BY last_notified_at
	`, now.Add(-interval).UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("find due alert reminders: %w", err)
	}
	type dueIncident struct {
		key, title, details  string
		openedAt, notifiedAt int64
	}
	due := make([]dueIncident, 0)
	for rows.Next() {
		var item dueIncident
		if err := rows.Scan(&item.key, &item.title, &item.details, &item.openedAt, &item.notifiedAt); err != nil {
			rows.Close()
			return fmt.Errorf("read due alert reminder: %w", err)
		}
		due = append(due, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate due alert reminders: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close due alert reminders: %w", err)
	}

	for _, incident := range due {
		result, err := tx.ExecContext(ctx, `
			UPDATE alert_incidents SET last_notified_at = ?
			WHERE alert_key = ? AND active = 1 AND last_notified_at = ?
		`, now.UTC().UnixNano(), incident.key, incident.notifiedAt)
		if err != nil {
			return fmt.Errorf("claim alert reminder: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("check alert reminder claim: %w", err)
		}
		if changed == 0 {
			continue
		}
		details := fmt.Sprintf("%s\n\nThis incident has remained open since %s.", incident.details, time.Unix(0, incident.openedAt).UTC().Format(time.RFC3339))
		if err := insertNotification(ctx, tx, incident.key, "reminder", recipients, "Still down: "+incident.title, details, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit alert reminders: %w", err)
	}
	return nil
}

func insertNotification(ctx context.Context, tx *sql.Tx, key, event string, recipients []string, subject, body string, now time.Time) error {
	recipientJSON, err := json.Marshal(recipients)
	if err != nil {
		return fmt.Errorf("encode alert recipients: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO notification_outbox (alert_key, event, recipients, subject, body, next_attempt_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, key, event, string(recipientJSON), subject, body, now.UTC().UnixNano(), now.UTC().UnixNano())
	if err != nil {
		return fmt.Errorf("queue alert email: %w", err)
	}
	return nil
}

const notificationClaimLease = time.Minute

func (r MonitoringRepository) ClaimDueNotifications(ctx context.Context, now time.Time, limit int) ([]models.Notification, error) {
	if limit <= 0 {
		return []models.Notification{}, nil
	}
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, fmt.Errorf("generate alert email claim token: %w", err)
	}
	claimToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
	claimTime := now.UTC().UnixNano()
	rows, err := r.db.QueryContext(ctx, `
		UPDATE notification_outbox
		SET status = 'sending', claimed_at = ?, claim_token = ?
		WHERE id IN (
			SELECT id FROM notification_outbox
			WHERE (status = 'queued' AND next_attempt_at <= ?)
			   OR (status = 'sending' AND claimed_at <= ?)
			ORDER BY id LIMIT ?
		)
		RETURNING id, recipients, subject, body, attempts, claim_token
	`, claimTime, claimToken, claimTime, now.Add(-notificationClaimLease).UTC().UnixNano(), limit)
	if err != nil {
		return nil, fmt.Errorf("claim pending alert emails: %w", err)
	}
	defer rows.Close()

	notifications := make([]models.Notification, 0)
	for rows.Next() {
		var notification models.Notification
		var recipients string
		if err := rows.Scan(&notification.ID, &recipients, &notification.Subject, &notification.Body, &notification.Attempts, &notification.ClaimToken); err != nil {
			return nil, fmt.Errorf("read claimed alert email: %w", err)
		}
		if err := json.Unmarshal([]byte(recipients), &notification.Recipients); err != nil {
			return nil, fmt.Errorf("decode alert email recipients: %w", err)
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed alert emails: %w", err)
	}
	return notifications, nil
}

func (r MonitoringRepository) MarkNotificationSent(ctx context.Context, id int64, claimToken string, sentAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE notification_outbox SET status = 'sent', sent_at = ?, last_error = '', claimed_at = 0, claim_token = ''
		WHERE id = ? AND status = 'sending' AND claim_token = ?
	`, sentAt.UTC().UnixNano(), id, claimToken)
	if err != nil {
		return fmt.Errorf("mark alert email sent: %w", err)
	}
	if changed, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("check alert email claim: %w", err)
	} else if changed != 1 {
		return fmt.Errorf("alert email claim was lost")
	}
	return nil
}

func (r MonitoringRepository) MarkNotificationFailed(ctx context.Context, id int64, claimToken string, attempts int, retryAt time.Time, safeError string, maxAttempts int) error {
	status := "queued"
	if attempts+1 >= maxAttempts {
		status = "failed"
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE notification_outbox
		SET status = ?, attempts = attempts + 1, next_attempt_at = ?, last_error = ?, claimed_at = 0, claim_token = ''
		WHERE id = ? AND status = 'sending' AND claim_token = ?
	`, status, retryAt.UTC().UnixNano(), safeError, id, claimToken)
	if err != nil {
		return fmt.Errorf("record alert email failure: %w", err)
	}
	if changed, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("check failed alert email claim: %w", err)
	} else if changed != 1 {
		return fmt.Errorf("alert email claim was lost")
	}
	return nil
}

func (r MonitoringRepository) PruneQueueHistory(ctx context.Context, before time.Time) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM queue_observations WHERE observed_at < ?`, before.UTC().UnixNano()); err != nil {
		return fmt.Errorf("prune queue history: %w", err)
	}
	return nil
}

func (r MonitoringRepository) Snapshot(ctx context.Context, now time.Time, staleAfter time.Duration, excludedQueues []string) (models.MonitoringSnapshot, error) {
	snapshot := models.MonitoringSnapshot{
		Source:       models.SourceStatus{Name: "rabbitmq", Status: "not_configured"},
		WorkerSource: models.SourceStatus{Name: "systemd", Status: "not_configured"},
		Queues:       make([]models.QueueStatus, 0),
		Workers:      make([]models.WorkerStatus, 0),
	}
	var sourceStatus, lastError string
	var lastSuccess, checkedAt sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT status, last_success_at, checked_at, last_error
		FROM monitoring_sources WHERE name = 'rabbitmq'
	`).Scan(&sourceStatus, &lastSuccess, &checkedAt, &lastError)
	if err != nil && err != sql.ErrNoRows {
		return models.MonitoringSnapshot{}, fmt.Errorf("read RabbitMQ source status: %w", err)
	}
	if err == nil {
		snapshot.Source.Status = sourceStatus
		snapshot.Source.LastError = lastError
		if lastSuccess.Valid {
			value := time.Unix(0, lastSuccess.Int64).UTC()
			snapshot.Source.LastSuccessAt = &value
		}
		if checkedAt.Valid {
			value := time.Unix(0, checkedAt.Int64).UTC()
			snapshot.Source.CheckedAt = &value
		}
	}
	var workerSourceStatus, workerLastError string
	var workerLastSuccess, workerCheckedAt sql.NullInt64
	err = r.db.QueryRowContext(ctx, `
		SELECT status, last_success_at, checked_at, last_error
		FROM monitoring_sources WHERE name = 'systemd'
	`).Scan(&workerSourceStatus, &workerLastSuccess, &workerCheckedAt, &workerLastError)
	if err != nil && err != sql.ErrNoRows {
		return models.MonitoringSnapshot{}, fmt.Errorf("read systemd source status: %w", err)
	}
	if err == nil {
		snapshot.WorkerSource.Status = workerSourceStatus
		snapshot.WorkerSource.LastError = workerLastError
		if workerLastSuccess.Valid {
			value := time.Unix(0, workerLastSuccess.Int64).UTC()
			snapshot.WorkerSource.LastSuccessAt = &value
		}
		if workerCheckedAt.Valid {
			value := time.Unix(0, workerCheckedAt.Int64).UTC()
			snapshot.WorkerSource.CheckedAt = &value
		}
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_outbox WHERE status = 'failed'`).Scan(&snapshot.Source.FailedEmails); err != nil {
		return models.MonitoringSnapshot{}, fmt.Errorf("count failed alert emails: %w", err)
	}
	_ = r.db.QueryRowContext(ctx, `
		SELECT last_error FROM notification_outbox WHERE status = 'failed' ORDER BY id DESC LIMIT 1
	`).Scan(&snapshot.Source.LastEmailError)

	excluded := make(map[string]struct{}, len(excludedQueues))
	for _, name := range excludedQueues {
		excluded[name] = struct{}{}
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT vhost, name, node, state, status, messages, messages_ready,
			messages_unacknowledged, consumers, publish_rate, delivery_rate, observed_at
		FROM rabbitmq_queues ORDER BY vhost, name
	`)
	if err != nil {
		return models.MonitoringSnapshot{}, fmt.Errorf("list RabbitMQ queue status: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var queue models.QueueStatus
		var messages, ready, unacked, consumers sql.NullInt64
		var publishRate, deliveryRate sql.NullFloat64
		var observedAt int64
		if err := rows.Scan(&queue.VHost, &queue.Name, &queue.Node, &queue.State, &queue.Status, &messages, &ready, &unacked, &consumers, &publishRate, &deliveryRate, &observedAt); err != nil {
			return models.MonitoringSnapshot{}, fmt.Errorf("read RabbitMQ queue status: %w", err)
		}
		if _, skip := excluded[queue.Name]; skip {
			continue
		}
		queue.Messages = intPointer(messages)
		queue.MessagesReady = intPointer(ready)
		queue.MessagesUnacked = intPointer(unacked)
		queue.Consumers = intPointer(consumers)
		queue.PublishRate = floatPointer(publishRate)
		queue.DeliveryRate = floatPointer(deliveryRate)
		queue.ObservedAt = time.Unix(0, observedAt).UTC()
		queue.IsStale = now.Sub(queue.ObservedAt) > staleAfter || snapshot.Source.Status == "down"
		snapshot.Queues = append(snapshot.Queues, queue)
	}
	if err := rows.Err(); err != nil {
		return models.MonitoringSnapshot{}, fmt.Errorf("iterate RabbitMQ queue status: %w", err)
	}
	workerRows, err := r.db.QueryContext(ctx, `
		SELECT host, unit, load_state, expected_state, active_state, sub_state, result,
			exec_main_code, exec_main_status, status_text, status, observed_at
		FROM systemd_workers ORDER BY host, unit
	`)
	if err != nil {
		return models.MonitoringSnapshot{}, fmt.Errorf("list systemd worker status: %w", err)
	}
	defer workerRows.Close()
	for workerRows.Next() {
		var worker models.WorkerStatus
		var observedAt int64
		if err := workerRows.Scan(&worker.Host, &worker.Unit, &worker.LoadState, &worker.ExpectedState, &worker.ActiveState, &worker.SubState,
			&worker.Result, &worker.ExecMainCode, &worker.ExecMainStatus, &worker.StatusText, &worker.Status, &observedAt); err != nil {
			return models.MonitoringSnapshot{}, fmt.Errorf("read systemd worker status: %w", err)
		}
		worker.ObservedAt = time.Unix(0, observedAt).UTC()
		worker.IsStale = now.Sub(worker.ObservedAt) > staleAfter
		snapshot.Workers = append(snapshot.Workers, worker)
	}
	if err := workerRows.Err(); err != nil {
		return models.MonitoringSnapshot{}, fmt.Errorf("iterate systemd worker status: %w", err)
	}
	return snapshot, nil
}

func nullableInt(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func intPointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

func floatPointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}
