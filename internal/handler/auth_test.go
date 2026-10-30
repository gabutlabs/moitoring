package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"moitoring/internal/repository/sqlite"
	authservice "moitoring/internal/services/auth"
)

func TestAuthRoutesRequirePasswordChangeBeforeProtectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	if _, err := sqlite.EnsureAdmin(context.Background(), db, "admin", "admin@example.local", "initial-admin-password"); err != nil {
		t.Fatalf("create test admin: %v", err)
	}
	var cleared bool
	service := authservice.New(sqlite.NewAuthRepository(db), "admin@example.local", true, func() error {
		cleared = true
		return nil
	})
	auth := NewAuthHandler(service, "admin@example.local", true)
	router := gin.New()
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	api := router.Group("/api")
	api.Use(auth.Middleware())
	auth.Register(api)
	api.GET("/private", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusNoContent {
		t.Fatalf("unauthenticated frontend route status = %d, want %d", page.Code, http.StatusNoContent)
	}

	private := httptest.NewRecorder()
	router.ServeHTTP(private, httptest.NewRequest(http.MethodGet, "/api/private", nil))
	if private.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated private route status = %d, want %d", private.Code, http.StatusUnauthorized)
	}

	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"initial-admin-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d: %s", login.Code, http.StatusOK, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookieName || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("login cookies = %#v, want HttpOnly and Secure session cookie", cookies)
	}

	private = httptest.NewRecorder()
	privateRequest := httptest.NewRequest(http.MethodGet, "/api/private", nil)
	privateRequest.AddCookie(cookies[0])
	router.ServeHTTP(private, privateRequest)
	if private.Code != http.StatusForbidden {
		t.Fatalf("private route while change required status = %d, want %d", private.Code, http.StatusForbidden)
	}

	change := httptest.NewRecorder()
	changeRequest := httptest.NewRequest(http.MethodPost, "/api/auth/password", strings.NewReader(`{"password":"operator-password-2026","confirm_password":"operator-password-2026"}`))
	changeRequest.Header.Set("Content-Type", "application/json")
	changeRequest.AddCookie(cookies[0])
	router.ServeHTTP(change, changeRequest)
	if change.Code != http.StatusNoContent || !cleared {
		t.Fatalf("password change status = %d, config cleared = %t; want 204 and true", change.Code, cleared)
	}

	private = httptest.NewRecorder()
	privateRequest = httptest.NewRequest(http.MethodGet, "/api/private", nil)
	privateRequest.AddCookie(cookies[0])
	router.ServeHTTP(private, privateRequest)
	if private.Code != http.StatusNoContent {
		t.Fatalf("private route after password change status = %d, want %d", private.Code, http.StatusNoContent)
	}

	logout := httptest.NewRecorder()
	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logoutRequest.AddCookie(cookies[0])
	router.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logout.Code, http.StatusNoContent)
	}

	private = httptest.NewRecorder()
	privateRequest = httptest.NewRequest(http.MethodGet, "/api/private", nil)
	privateRequest.AddCookie(cookies[0])
	router.ServeHTTP(private, privateRequest)
	if private.Code != http.StatusUnauthorized {
		t.Fatalf("private route after logout status = %d, want %d", private.Code, http.StatusUnauthorized)
	}
}

func TestAuthLoginRejectsWrongPasswordAndRequiresPasswordField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	if _, err := sqlite.EnsureAdmin(context.Background(), db, "admin", "admin@example.local", "initial-admin-password"); err != nil {
		t.Fatalf("create test admin: %v", err)
	}
	auth := NewAuthHandler(authservice.New(sqlite.NewAuthRepository(db), "admin@example.local", false, func() error { return nil }), "admin@example.local", false)
	router := gin.New()
	api := router.Group("/api")
	api.Use(auth.Middleware())
	auth.Register(api)

	for _, test := range []struct {
		name string
		body string
		want int
	}{
		{name: "missing password", body: `{}`, want: http.StatusBadRequest},
		{name: "wrong password", body: `{"password":"wrong-password"}`, want: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("login status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestAuthLoginRateLimitSeparatesClientsBehindTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	if _, err := sqlite.EnsureAdmin(context.Background(), db, "admin", "admin@example.local", "initial-admin-password"); err != nil {
		t.Fatalf("create test admin: %v", err)
	}
	auth := NewAuthHandler(authservice.New(sqlite.NewAuthRepository(db), "admin@example.local", false, func() error { return nil }), "admin@example.local", false)
	router := gin.New()
	if err := router.SetTrustedProxies([]string{"127.0.0.1/32"}); err != nil {
		t.Fatalf("configure trusted proxy: %v", err)
	}
	api := router.Group("/api")
	api.Use(auth.Middleware())
	auth.Register(api)

	login := func(clientIP string) int {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"wrong-password"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-For", clientIP)
		request.RemoteAddr = "127.0.0.1:9000"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response.Code
	}

	for attempt := 0; attempt < 5; attempt++ {
		if got := login("203.0.113.10"); got != http.StatusUnauthorized {
			t.Fatalf("client A login %d status = %d, want %d", attempt+1, got, http.StatusUnauthorized)
		}
	}
	if got := login("203.0.113.11"); got != http.StatusUnauthorized {
		t.Fatalf("client B status = %d, want independent attempt status %d", got, http.StatusUnauthorized)
	}
	if got := login("203.0.113.10"); got != http.StatusTooManyRequests {
		t.Fatalf("client A sixth login status = %d, want %d", got, http.StatusTooManyRequests)
	}
}

func TestAuthLoginIgnoresForwardedClientIPFromUntrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer db.Close()
	if _, err := sqlite.EnsureAdmin(context.Background(), db, "admin", "admin@example.local", "initial-admin-password"); err != nil {
		t.Fatalf("create test admin: %v", err)
	}
	auth := NewAuthHandler(authservice.New(sqlite.NewAuthRepository(db), "admin@example.local", false, func() error { return nil }), "admin@example.local", false)
	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatalf("disable trusted proxies: %v", err)
	}
	api := router.Group("/api")
	api.Use(auth.Middleware())
	auth.Register(api)

	for attempt := 0; attempt < 5; attempt++ {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"wrong-password"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Forwarded-For", "203.0.113.10")
		request.RemoteAddr = "198.51.100.20:1234"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("untrusted peer login %d status = %d, want %d", attempt+1, response.Code, http.StatusUnauthorized)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"wrong-password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "203.0.113.11")
	request.RemoteAddr = "198.51.100.20:1234"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed forwarded IP status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
}
