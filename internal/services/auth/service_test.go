package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"moitoring/internal/repository/sqlite"
)

const (
	testAdminEmail        = "admin@example.local"
	testBootstrapPassword = "initial-admin-password"
)

func testService(t *testing.T, mustChange bool, clearBootstrap func() error) (*Service, int64) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := sqlite.EnsureAdmin(context.Background(), db, "admin", testAdminEmail, testBootstrapPassword); err != nil {
		t.Fatalf("create test admin: %v", err)
	}
	var userID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, testAdminEmail).Scan(&userID); err != nil {
		t.Fatalf("read test admin id: %v", err)
	}
	return New(sqlite.NewAuthRepository(db), testAdminEmail, mustChange, clearBootstrap), userID
}

func TestLoginCreatesOpaqueSessionAndLogoutRevokesIt(t *testing.T) {
	service, _ := testService(t, false, func() error { return nil })
	session, err := service.Login(context.Background(), testBootstrapPassword, "127.0.0.1")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Token == "" || session.ExpiresAt.IsZero() {
		t.Fatalf("Login() session = %#v, want token and expiry", session)
	}
	state, ok, err := service.ResolveSession(context.Background(), session.Token)
	if err != nil || !ok {
		t.Fatalf("ResolveSession() = (%#v, %t, %v), want active session", state, ok, err)
	}

	if err := service.Logout(context.Background(), session.Token); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, ok, err := service.ResolveSession(context.Background(), session.Token); err != nil || ok {
		t.Fatalf("ResolveSession() after logout = (ok %t, err %v), want inactive", ok, err)
	}
}

func TestLoginRejectsInvalidPassword(t *testing.T) {
	service, _ := testService(t, false, func() error { return nil })
	if _, err := service.Login(context.Background(), "wrong-password", "127.0.0.2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestResolveSessionRejectsExpiredSession(t *testing.T) {
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "expired-session.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	if _, err := sqlite.EnsureAdmin(context.Background(), db, "admin", testAdminEmail, testBootstrapPassword); err != nil {
		t.Fatalf("create test admin: %v", err)
	}
	var userID int64
	if err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, testAdminEmail).Scan(&userID); err != nil {
		t.Fatalf("read test admin id: %v", err)
	}
	const token = "expired-session-token"
	repository := sqlite.NewAuthRepository(db)
	if err := repository.CreateSession(context.Background(), userID, hashToken(token), time.Now().Add(-time.Minute).Unix()); err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	service := New(repository, testAdminEmail, false, func() error { return nil })
	if _, ok, err := service.ResolveSession(context.Background(), token); err != nil || ok {
		t.Fatalf("ResolveSession() for expired token = (ok %t, err %v), want inactive", ok, err)
	}
}

func TestPasswordChangeClearsBootstrapRequirement(t *testing.T) {
	cleared := false
	service, userID := testService(t, true, func() error {
		cleared = true
		return nil
	})
	session, err := service.Login(context.Background(), testBootstrapPassword, "127.0.0.3")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	state, ok, err := service.ResolveSession(context.Background(), session.Token)
	if err != nil || !ok || !state.MustChangePassword {
		t.Fatalf("ResolveSession() = (%#v, %t, %v), want password change required", state, ok, err)
	}
	otherSession, err := service.Login(context.Background(), testBootstrapPassword, "127.0.0.4")
	if err != nil {
		t.Fatalf("Login() second bootstrap session error = %v", err)
	}

	const newPassword = "operator-password-2026"
	if err := service.ChangePassword(context.Background(), userID, newPassword, session.Token); err != nil {
		t.Fatalf("ChangePassword() error = %v", err)
	}
	if !cleared {
		t.Fatal("bootstrap settings were not cleared")
	}
	state, ok, err = service.ResolveSession(context.Background(), session.Token)
	if err != nil || !ok || state.MustChangePassword {
		t.Fatalf("ResolveSession() after change = (%#v, %t, %v), want active session without forced change", state, ok, err)
	}
	if _, ok, err := service.ResolveSession(context.Background(), otherSession.Token); err != nil || ok {
		t.Fatalf("ResolveSession() for other session after change = (ok %t, err %v), want revoked", ok, err)
	}
	if _, err := service.Login(context.Background(), newPassword, "127.0.0.3"); err != nil {
		t.Fatalf("Login() with changed password error = %v", err)
	}
	if _, err := service.Login(context.Background(), testBootstrapPassword, "127.0.0.4"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() with bootstrap password error = %v, want ErrInvalidCredentials", err)
	}
}

func TestChangePasswordRequiresStrongPassword(t *testing.T) {
	service, userID := testService(t, true, func() error { return nil })
	if err := service.ChangePassword(context.Background(), userID, "short", "current-session"); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("ChangePassword() error = %v, want ErrPasswordTooWeak", err)
	}
}

func TestConcurrentPasswordChangesCompleteOnce(t *testing.T) {
	clearCount := 0
	service, userID := testService(t, true, func() error {
		clearCount++
		return nil
	})
	errorsFound := make(chan error, 2)
	var wait sync.WaitGroup
	for _, password := range []string{"operator-password-2026", "another-password-2026"} {
		wait.Add(1)
		go func(password string) {
			defer wait.Done()
			errorsFound <- service.ChangePassword(context.Background(), userID, password, "current-session")
		}(password)
	}
	wait.Wait()
	close(errorsFound)

	successes := 0
	alreadyChanged := 0
	for err := range errorsFound {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrPasswordChangeNotRequired):
			alreadyChanged++
		default:
			t.Errorf("ChangePassword() error = %v", err)
		}
	}
	if successes != 1 || alreadyChanged != 1 || clearCount != 1 {
		t.Fatalf("changes = %d successful, %d already complete, config clears = %d; want 1, 1, 1", successes, alreadyChanged, clearCount)
	}
}

func TestLoginLimiterAllowsFiveAttemptsThenResets(t *testing.T) {
	limiter := newLoginLimiter()
	for attempt := 1; attempt <= 5; attempt++ {
		if !limiter.Allow("client-a") {
			t.Fatalf("Allow() attempt %d = false, want true", attempt)
		}
	}
	if limiter.Allow("client-a") {
		t.Fatal("Allow() sixth attempt = true, want rate limit")
	}
	limiter.Reset("client-a")
	if !limiter.Allow("client-a") {
		t.Fatal("Allow() after reset = false, want true")
	}
}
