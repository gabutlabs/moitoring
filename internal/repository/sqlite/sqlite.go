package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const currentSchemaVersion = 9

func Open(ctx context.Context, path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("database path cannot be empty")
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	dsnURL := &url.URL{Scheme: "file", Path: absolutePath}
	query := dsnURL.Query()
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	dsnURL.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", dsnURL.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to sqlite database: %w", err)
	}
	if err := migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite database: %w", err)
	}

	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) (retErr error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("get migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if _, err := conn.ExecContext(context.Background(), "ROLLBACK"); err != nil && !errors.Is(err, sql.ErrConnDone) {
				retErr = errors.Join(retErr, fmt.Errorf("rollback migration: %w", err))
			}
		}
	}()

	var version int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, currentSchemaVersion)
	}
	if version < 1 {
		if _, err := conn.ExecContext(ctx, `
			CREATE TABLE users (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				username TEXT NOT NULL UNIQUE,
				password TEXT NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
				deleted_at DATETIME NULL
			)
		`); err != nil {
			return fmt.Errorf("create users table: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
		version = 1
	}
	if version < 2 {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE users ADD COLUMN email TEXT`); err != nil {
			return fmt.Errorf("add admin email: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `CREATE UNIQUE INDEX users_email_unique ON users(email) WHERE email IS NOT NULL`); err != nil {
			return fmt.Errorf("index admin email: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
		version = 2
	}
	if version < 3 {
		if _, err := conn.ExecContext(ctx, `
			CREATE TABLE auth_sessions (
				token_hash TEXT PRIMARY KEY,
				user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				expires_at INTEGER NOT NULL,
				created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)
		`); err != nil {
			return fmt.Errorf("create auth sessions table: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `CREATE INDEX auth_sessions_user_id_idx ON auth_sessions(user_id)`); err != nil {
			return fmt.Errorf("index auth sessions: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 3"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}
	if version < 4 {
		statements := []string{
			`CREATE TABLE monitoring_sources (
				name TEXT PRIMARY KEY,
				status TEXT NOT NULL,
				last_success_at INTEGER,
				checked_at INTEGER NOT NULL,
				last_error TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE TABLE rabbitmq_queues (
				vhost TEXT NOT NULL,
				name TEXT NOT NULL,
				state TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL,
				messages INTEGER,
				messages_ready INTEGER,
				messages_unacknowledged INTEGER,
				consumers INTEGER,
				publish_rate REAL,
				delivery_rate REAL,
				observed_at INTEGER NOT NULL,
				PRIMARY KEY (vhost, name)
			)`,
			`CREATE TABLE queue_observations (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				vhost TEXT NOT NULL,
				name TEXT NOT NULL,
				state TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL,
				messages INTEGER,
				messages_ready INTEGER,
				messages_unacknowledged INTEGER,
				consumers INTEGER,
				publish_rate REAL,
				delivery_rate REAL,
				observed_at INTEGER NOT NULL
			)`,
			`CREATE INDEX queue_observations_lookup_idx ON queue_observations(vhost, name, observed_at DESC)`,
			`CREATE TABLE alert_incidents (
				alert_key TEXT PRIMARY KEY,
				kind TEXT NOT NULL,
				target TEXT NOT NULL,
				active INTEGER NOT NULL,
				title TEXT NOT NULL,
				details TEXT NOT NULL,
				opened_at INTEGER NOT NULL,
				last_notified_at INTEGER NOT NULL,
				resolved_at INTEGER
			)`,
			`CREATE INDEX alert_incidents_active_idx ON alert_incidents(active, last_notified_at)`,
			`CREATE TABLE notification_outbox (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				alert_key TEXT NOT NULL REFERENCES alert_incidents(alert_key),
				event TEXT NOT NULL,
				recipients TEXT NOT NULL,
				subject TEXT NOT NULL,
				body TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'queued',
				attempts INTEGER NOT NULL DEFAULT 0,
				next_attempt_at INTEGER NOT NULL,
				created_at INTEGER NOT NULL,
				sent_at INTEGER,
				last_error TEXT NOT NULL DEFAULT ''
			)`,
			`CREATE INDEX notification_outbox_due_idx ON notification_outbox(status, next_attempt_at, id)`,
		}
		for _, statement := range statements {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("create monitoring schema: %w", err)
			}
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 4"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}
	if version < 5 {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE rabbitmq_queues ADD COLUMN node TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add RabbitMQ node to current queues: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `ALTER TABLE queue_observations ADD COLUMN node TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add RabbitMQ node to queue history: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 5"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}
	if version < 6 {
		if _, err := conn.ExecContext(ctx, `CREATE TABLE systemd_workers (
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
			return fmt.Errorf("create systemd worker status table: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 6"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}
	if version < 7 {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE systemd_workers ADD COLUMN load_state TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add systemd worker load state: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 7"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}
	if version < 8 {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE notification_outbox ADD COLUMN claimed_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add notification claim time: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `ALTER TABLE notification_outbox ADD COLUMN claim_token TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add notification claim token: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `CREATE INDEX notification_outbox_claim_idx ON notification_outbox(status, claimed_at, id)`); err != nil {
			return fmt.Errorf("index notification claims: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 8"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}
	if version < 9 {
		if _, err := conn.ExecContext(ctx, `ALTER TABLE notification_outbox ADD COLUMN item_count INTEGER NOT NULL DEFAULT 1`); err != nil {
			return fmt.Errorf("add notification batch count: %w", err)
		}
		if _, err := conn.ExecContext(ctx, `CREATE INDEX notification_outbox_batch_idx ON notification_outbox(event, created_at, status)`); err != nil {
			return fmt.Errorf("index notification batches: %w", err)
		}
		if _, err := conn.ExecContext(ctx, "PRAGMA user_version = 9"); err != nil {
			return fmt.Errorf("set schema version: %w", err)
		}
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	committed = true
	return nil
}
