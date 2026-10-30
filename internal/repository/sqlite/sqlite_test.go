package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestOpenCreatesDatabaseAndUsersTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "moitoring.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}

	if _, err := db.Exec(`INSERT INTO users (username, password) VALUES (?, ?)`, "operator", "bcrypt-hash"); err != nil {
		t.Fatalf("insert user with timestamp defaults: %v", err)
	}

	var createdAt, updatedAt string
	var deletedAt sql.NullString
	if err := db.QueryRow(`SELECT created_at, updated_at, deleted_at FROM users WHERE username = ?`, "operator").Scan(&createdAt, &updatedAt, &deletedAt); err != nil {
		t.Fatalf("read user timestamps: %v", err)
	}
	if createdAt == "" || updatedAt == "" {
		t.Fatalf("timestamp defaults = (%q, %q), want both populated", createdAt, updatedAt)
	}
	if deletedAt.Valid {
		t.Fatalf("deleted_at = %q, want NULL for a new user", deletedAt.String)
	}

	if _, err := db.Exec(`INSERT INTO users (username, password) VALUES (?, ?)`, "operator", "another-hash"); err == nil {
		t.Fatal("duplicate username insert succeeded, want unique constraint error")
	}
}

func TestOpenReusesExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moitoring.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("first Open() error = %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, password) VALUES (?, ?)`, "existing", "bcrypt-hash"); err != nil {
		db.Close()
		t.Fatalf("insert existing user: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}

	db, err = Open(context.Background(), path)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer db.Close()

	var password string
	if err := db.QueryRow(`SELECT password FROM users WHERE username = ?`, "existing").Scan(&password); err != nil {
		t.Fatalf("read preserved user: %v", err)
	}
	if password != "bcrypt-hash" {
		t.Fatalf("preserved password value = %q, want %q", password, "bcrypt-hash")
	}
}

func TestOpenMigratesLegacyDatabaseWithoutLosingAdmin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if _, err := legacyDB.Exec(`
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			deleted_at DATETIME NULL
		);
		PRAGMA user_version = 1;
		INSERT INTO users (username, password) VALUES ('admin', 'existing-hash');
	`); err != nil {
		legacyDB.Close()
		t.Fatalf("initialize legacy database: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() migrated database error = %v", err)
	}
	defer db.Close()
	if _, err := EnsureAdmin(context.Background(), db, "admin", "admin@example.local", ""); err != nil {
		t.Fatalf("backfill legacy admin identity: %v", err)
	}
	var username, email, password string
	if err := db.QueryRow(`SELECT username, email, password FROM users WHERE id = 1`).Scan(&username, &email, &password); err != nil {
		t.Fatalf("read migrated admin: %v", err)
	}
	if username != "admin" || email != "admin@example.local" || password != "existing-hash" {
		t.Fatalf("migrated admin = (%q, %q, %q), want existing identity/hash with configured email", username, email, password)
	}
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read migrated schema version: %v", err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
}

func TestEnsureAdminCreatesHashedPasswordAndSkipsExistingUser(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "moitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	created, err := EnsureAdmin(context.Background(), db, "admin", "admin@example.local", "first-secret")
	if err != nil {
		t.Fatalf("first EnsureAdmin() error = %v", err)
	}
	if !created {
		t.Fatal("first EnsureAdmin() created = false, want true")
	}

	var storedPassword string
	if err := db.QueryRow(`SELECT password FROM users WHERE username = ?`, "admin").Scan(&storedPassword); err != nil {
		t.Fatalf("read admin password: %v", err)
	}
	if storedPassword == "first-secret" {
		t.Fatal("password was stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte("first-secret")); err != nil {
		t.Fatalf("stored password is not a valid hash of the configured password: %v", err)
	}

	created, err = EnsureAdmin(context.Background(), db, "admin", "admin@example.local", "replacement-secret")
	if err != nil {
		t.Fatalf("second EnsureAdmin() error = %v", err)
	}
	if created {
		t.Fatal("second EnsureAdmin() created = true, want existing user skipped")
	}

	var unchangedPassword string
	if err := db.QueryRow(`SELECT password FROM users WHERE username = ?`, "admin").Scan(&unchangedPassword); err != nil {
		t.Fatalf("read existing admin password: %v", err)
	}
	if unchangedPassword != storedPassword {
		t.Fatal("existing admin password was changed")
	}
}

func TestEnsureAdminRequiresPasswordOnlyWhenCreating(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "moitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	if _, err := EnsureAdmin(context.Background(), db, "admin", "admin@example.local", ""); err == nil {
		t.Fatal("EnsureAdmin() without a password succeeded for a missing user")
	}

	if _, err := db.Exec(`INSERT INTO users (username, password) VALUES (?, ?)`, "admin", "existing-hash"); err != nil {
		t.Fatalf("insert existing admin: %v", err)
	}
	created, err := EnsureAdmin(context.Background(), db, "admin", "admin@example.local", "")
	if err != nil {
		t.Fatalf("EnsureAdmin() for existing admin without config password: %v", err)
	}
	if created {
		t.Fatal("EnsureAdmin() created = true, want existing user skipped")
	}
}

func TestConcurrentEnsureAdminCreatesOneUser(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "moitoring.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	const starters = 8
	var wait sync.WaitGroup
	created := make(chan bool, starters)
	errors := make(chan error, starters)
	for range starters {
		wait.Add(1)
		go func() {
			defer wait.Done()
			wasCreated, err := EnsureAdmin(context.Background(), db, "admin", "admin@example.local", "shared-secret")
			if err != nil {
				errors <- err
				return
			}
			created <- wasCreated
		}()
	}
	wait.Wait()
	close(created)
	close(errors)

	createdCount := 0
	for wasCreated := range created {
		if wasCreated {
			createdCount++
		}
	}
	for err := range errors {
		t.Errorf("concurrent EnsureAdmin(): %v", err)
	}
	if createdCount != 1 {
		t.Fatalf("concurrent EnsureAdmin() created %d users, want exactly 1", createdCount)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ?`, "admin").Scan(&count); err != nil {
		t.Fatalf("count admin users: %v", err)
	}
	if count != 1 {
		t.Fatalf("admin user rows = %d, want 1", count)
	}
}

func TestConcurrentOpenAppliesMigrationOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "moitoring.db")
	const workers = 8
	var wait sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			db, err := Open(context.Background(), path)
			if err != nil {
				errors <- err
				return
			}
			defer db.Close()
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'users'`).Scan(&count); err != nil {
				errors <- err
				return
			}
			if count != 1 {
				errors <- fmt.Errorf("users table count = %d, want 1", count)
			}
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent Open(): %v", err)
	}
}
