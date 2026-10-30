package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"moitoring/internal/models"
)

var (
	ErrInvalidCredentials        = errors.New("invalid credentials")
	ErrRateLimited               = errors.New("too many login attempts")
	ErrPasswordTooWeak           = errors.New("password must be 12 to 72 bytes and at least 12 characters")
	ErrPasswordChangeNotRequired = errors.New("password change is not required")
)

const SessionLifetime = 8 * time.Hour

type Repository interface {
	FindUserByEmail(context.Context, string) (models.User, error)
	CreateSession(context.Context, int64, string, int64) error
	FindSession(context.Context, string, int64) (int64, int64, error)
	DeleteSession(context.Context, string) error
	UpdatePassword(context.Context, int64, string, string) error
}

type Service struct {
	repository       Repository
	adminEmail       string
	clearBootstrap   func() error
	changeRequired   atomic.Bool
	passwordChangeMu sync.Mutex
	loginAttemptRate *loginLimiter
}

type IssuedSession struct {
	Token     string
	ExpiresAt time.Time
}

type SessionState struct {
	UserID             int64
	MustChangePassword bool
	ExpiresAt          time.Time
}

func New(repository Repository, adminEmail string, mustChangePassword bool, clearBootstrap func() error) *Service {
	service := &Service{
		repository:       repository,
		adminEmail:       strings.TrimSpace(adminEmail),
		clearBootstrap:   clearBootstrap,
		loginAttemptRate: newLoginLimiter(),
	}
	service.changeRequired.Store(mustChangePassword)
	return service
}

func (s *Service) Login(ctx context.Context, password, clientKey string) (IssuedSession, error) {
	if !s.loginAttemptRate.Allow(clientKey) {
		return IssuedSession{}, ErrRateLimited
	}
	user, err := s.repository.FindUserByEmail(ctx, s.adminEmail)
	if errors.Is(err, models.ErrUserNotFound) || (err == nil && bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil) {
		return IssuedSession{}, ErrInvalidCredentials
	}
	if err != nil {
		return IssuedSession{}, fmt.Errorf("load admin account: %w", err)
	}

	randomToken := make([]byte, 32)
	if _, err := rand.Read(randomToken); err != nil {
		return IssuedSession{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(randomToken)
	expiresAt := time.Now().UTC().Add(SessionLifetime)
	if err := s.repository.CreateSession(ctx, user.ID, hashToken(token), expiresAt.Unix()); err != nil {
		return IssuedSession{}, fmt.Errorf("create session: %w", err)
	}
	s.loginAttemptRate.Reset(clientKey)
	return IssuedSession{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Service) ResolveSession(ctx context.Context, token string) (SessionState, bool, error) {
	if token == "" {
		return SessionState{}, false, nil
	}
	userID, expiresAt, err := s.repository.FindSession(ctx, hashToken(token), time.Now().UTC().Unix())
	if errors.Is(err, models.ErrUserNotFound) || errors.Is(err, sql.ErrNoRows) {
		return SessionState{}, false, nil
	}
	if err != nil {
		return SessionState{}, false, fmt.Errorf("load session: %w", err)
	}
	return SessionState{
		UserID:             userID,
		MustChangePassword: s.changeRequired.Load(),
		ExpiresAt:          time.Unix(expiresAt, 0).UTC(),
	}, true, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := s.repository.DeleteSession(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, password, currentToken string) error {
	s.passwordChangeMu.Lock()
	defer s.passwordChangeMu.Unlock()

	if !s.changeRequired.Load() {
		return ErrPasswordChangeNotRequired
	}
	if currentToken == "" {
		return errors.New("current session is required to change password")
	}
	if s.clearBootstrap == nil {
		return fmt.Errorf("clear bootstrap password-change flag: no config updater is configured")
	}
	if utf8.RuneCountInString(password) < 12 || len([]byte(password)) > 72 {
		return ErrPasswordTooWeak
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	if err := s.repository.UpdatePassword(ctx, userID, string(hash), hashToken(currentToken)); err != nil {
		return fmt.Errorf("save new password: %w", err)
	}
	if err := s.clearBootstrap(); err != nil {
		return fmt.Errorf("clear bootstrap password-change flag: %w", err)
	}
	s.changeRequired.Store(false)
	return nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string][]time.Time)}
}

func (l *loginLimiter) Allow(clientKey string) bool {
	if clientKey == "" {
		clientKey = "unknown"
	}
	now := time.Now()
	cutoff := now.Add(-15 * time.Minute)
	l.mu.Lock()
	defer l.mu.Unlock()

	for key, attempts := range l.attempts {
		active := attempts[:0]
		for _, attempt := range attempts {
			if attempt.After(cutoff) {
				active = append(active, attempt)
			}
		}
		if len(active) == 0 {
			delete(l.attempts, key)
		} else {
			l.attempts[key] = active
		}
	}
	if len(l.attempts[clientKey]) >= 5 {
		return false
	}
	if _, exists := l.attempts[clientKey]; !exists && len(l.attempts) >= 4096 {
		return false
	}
	l.attempts[clientKey] = append(l.attempts[clientKey], now)
	return true
}

func (l *loginLimiter) Reset(clientKey string) {
	if clientKey == "" {
		clientKey = "unknown"
	}
	l.mu.Lock()
	delete(l.attempts, clientKey)
	l.mu.Unlock()
}
