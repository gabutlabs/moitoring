package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterWebUIDisabledKeepsAPIHandlerAndDoesNotServeRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	registerWebUI(router, false)

	apiResponse := httptest.NewRecorder()
	router.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if apiResponse.Code != http.StatusOK {
		t.Fatalf("API status = %d, want %d", apiResponse.Code, http.StatusOK)
	}

	webResponse := httptest.NewRecorder()
	router.ServeHTTP(webResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if webResponse.Code != http.StatusNotFound {
		t.Fatalf("root status = %d, want %d when web UI is disabled", webResponse.Code, http.StatusNotFound)
	}
}

func TestRegisterWebUIEnabledServesRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerWebUI(router, true)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("root status = %d, want %d when web UI is enabled", response.Code, http.StatusOK)
	}
}
