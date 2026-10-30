package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"moitoring/internal/models"
)

type AuthRepository struct {
	db *sql.DB
}

func NewAuthRepository(db *sql.DB) AuthRepository {
	return AuthRepository{db: db}
}

func (r AuthRepository) FindUserByEmail(ctx context.Context, email string) (models.User, error) {
	var user models.User
	err := r.db.QueryRowContext(ctx, `SELECT id, password FROM users WHERE email = ?`, email).Scan(&user.ID, &user.PasswordHash)
	if err == sql.ErrNoRows {
		return models.User{}, models.ErrUserNotFound
	}
	if err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (r AuthRepository) CreateSession(ctx context.Context, userID int64, tokenHash string, expiresAt int64) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at <= ?`, time.Now().Unix()); err != nil {
		return fmt.Errorf("remove expired sessions: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `INSERT INTO auth_sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, tokenHash, userID, expiresAt); err != nil {
		return fmt.Errorf("store auth session: %w", err)
	}
	return nil
}

func (r AuthRepository) FindSession(ctx context.Context, tokenHash string, now int64) (int64, int64, error) {
	var userID int64
	var expiresAt int64
	err := r.db.QueryRowContext(ctx, `SELECT user_id, expires_at FROM auth_sessions WHERE token_hash = ? AND expires_at > ?`, tokenHash, now).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		return 0, 0, models.ErrUserNotFound
	}
	return userID, expiresAt, err
}

func (r AuthRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete auth session: %w", err)
	}
	return nil
}

func (r AuthRepository) UpdatePassword(ctx context.Context, userID int64, passwordHash, keepSessionHash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin password update: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE users SET password = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, passwordHash, userID)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check password update result: %w", err)
	}
	if updated != 1 {
		return models.ErrUserNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id = ? AND token_hash <> ?`, userID, keepSessionHash); err != nil {
		return fmt.Errorf("revoke other user sessions: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit password update: %w", err)
	}
	return nil
}
