package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"moitoring/internal/models"
)

type MonitoringReader interface {
	Snapshot(context.Context) (models.MonitoringSnapshot, error)
}

type MonitoringHandler struct {
	reader MonitoringReader
}

func NewMonitoringHandler(reader MonitoringReader) *MonitoringHandler {
	return &MonitoringHandler{reader: reader}
}

func (h *MonitoringHandler) Register(router gin.IRouter) {
	router.GET("/monitoring", h.Snapshot)
}

func (h *MonitoringHandler) Snapshot(c *gin.Context) {
	snapshot, err := h.reader.Snapshot(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not load monitoring status"})
		return
	}
	c.JSON(http.StatusOK, snapshot)
}
