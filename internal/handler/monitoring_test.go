package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"moitoring/internal/models"
)

type testMonitoringReader struct {
	snapshot models.MonitoringSnapshot
	err      error
}

func (r testMonitoringReader) Snapshot(context.Context) (models.MonitoringSnapshot, error) {
	return r.snapshot, r.err
}

func TestMonitoringHandlerReturnsSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewMonitoringHandler(testMonitoringReader{snapshot: models.MonitoringSnapshot{
		Source: models.SourceStatus{Name: "rabbitmq", Status: "up"},
		Queues: []models.QueueStatus{{QueueKey: models.QueueKey{VHost: "/", Name: "jobs"}, Status: "healthy", State: "idle"}},
	}}).Register(router)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/monitoring", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body models.MonitoringSnapshot
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode monitoring snapshot: %v", err)
	}
	if body.Source.Status != "up" || len(body.Queues) != 1 || body.Queues[0].Name != "jobs" {
		t.Fatalf("snapshot = %+v", body)
	}
}

func TestMonitoringHandlerHidesRepositoryErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewMonitoringHandler(testMonitoringReader{err: errors.New("database details")}).Register(router)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/monitoring", nil)
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if response.Body.String() == "" || response.Body.String() == "database details" {
		t.Fatalf("response body = %q, want generic error", response.Body.String())
	}
}
