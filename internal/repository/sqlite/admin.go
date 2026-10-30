package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/mail"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func EnsureAdmin(ctx context.Context, db *sql.DB, username, email, password string) (bool, error) {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username == "" {
		return false, fmt.Errorf("admin username cannot be empty")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return false, fmt.Errorf("admin email must be a valid email address")
	}

	var userID int64
	err = db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ? OR email = ? LIMIT 1`, username, email).Scan(&userID)
	if err == nil {
		if _, err := db.ExecContext(ctx, `UPDATE users SET email = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND email IS NOT ?`, email, userID, email); err != nil {
			return false, fmt.Errorf("set admin email: %w", err)
		}
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, fmt.Errorf("check admin user: %w", err)
	}
	if password == "" {
		return false, fmt.Errorf("admin password is required to create the initial admin user")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("hash admin password: %w", err)
	}

	result, err := db.ExecContext(ctx, `
		INSERT INTO users (username, email, password)
		VALUES (?, ?, ?)
		ON CONFLICT(username) DO NOTHING
	`, username, email, string(hash))
	if err != nil {
		return false, fmt.Errorf("create admin user: %w", err)
	}

	created, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check admin insert result: %w", err)
	}
	return created == 1, nil
}
