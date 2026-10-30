package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	authservice "moitoring/internal/services/auth"
)

const authCookieName = "moitoring_session"

type AuthHandler struct {
	service      *authservice.Service
	adminEmail   string
	secureCookie bool
}

func NewAuthHandler(service *authservice.Service, adminEmail string, secureCookie bool) *AuthHandler {
	return &AuthHandler{service: service, adminEmail: adminEmail, secureCookie: secureCookie}
}

func (h *AuthHandler) Register(router gin.IRouter) {
	router.POST("/auth/login", h.Login)
	router.GET("/auth/session", h.Session)
	router.POST("/auth/logout", h.Logout)
	router.POST("/auth/password", h.ChangePassword)
}

func (h *AuthHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if path == "/api/health" || path == "/api/auth/login" || path == "/api/auth/session" || path == "/api/auth/logout" {
			c.Next()
			return
		}

		session, ok, err := h.currentSession(c)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not validate session"})
			c.Abort()
			return
		}
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
			c.Abort()
			return
		}
		if session.MustChangePassword && !(path == "/api/auth/password" && c.Request.Method == http.MethodPost) {
			c.JSON(http.StatusForbidden, gin.H{"error": "Password change required"})
			c.Abort()
			return
		}
		c.Set("auth_user_id", session.UserID)
		c.Next()
	}
}

func (h *AuthHandler) Login(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var request struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || request.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enter your password"})
		return
	}

	session, err := h.service.Login(c.Request.Context(), request.Password, c.ClientIP())
	if errors.Is(err, authservice.ErrInvalidCredentials) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid password"})
		return
	}
	if errors.Is(err, authservice.ErrRateLimited) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many sign-in attempts. Try again in 15 minutes."})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not sign in"})
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     authCookieName,
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		MaxAge:   int(authservice.SessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteStrictMode,
	})
	c.JSON(http.StatusOK, gin.H{"authenticated": true})
}

func (h *AuthHandler) Session(c *gin.Context) {
	session, ok, err := h.currentSession(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not validate session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"authenticated":        ok,
		"admin_email":          h.adminEmail,
		"must_change_password": ok && session.MustChangePassword,
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	token, _ := c.Cookie(authCookieName)
	if err := h.service.Logout(c.Request.Context(), token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not sign out"})
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteStrictMode,
	})
	c.Status(http.StatusNoContent)
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var request struct {
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Enter and confirm your new password"})
		return
	}
	if request.Password != request.ConfirmPassword {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Passwords do not match"})
		return
	}
	userID, ok := c.Get("auth_user_id")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
		return
	}
	id, ok := userID.(int64)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not validate session"})
		return
	}
	token, _ := c.Cookie(authCookieName)
	if err := h.service.ChangePassword(c.Request.Context(), id, request.Password, token); err != nil {
		if errors.Is(err, authservice.ErrPasswordTooWeak) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, authservice.ErrPasswordChangeNotRequired) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not change password"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AuthHandler) currentSession(c *gin.Context) (authservice.SessionState, bool, error) {
	token, err := c.Cookie(authCookieName)
	if err != nil {
		return authservice.SessionState{}, false, nil
	}
	return h.service.ResolveSession(c.Request.Context(), token)
}
