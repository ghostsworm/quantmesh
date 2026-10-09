package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func getCurrentSystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "system_metrics_unavailable"})
		return
	}
	metrics, err := systemMetricsProvider.GetCurrentMetrics()
	if err != nil || metrics == nil {
		// Never expose raw diagnostic errors or invent a fresh zero sample.
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "system_metrics_unavailable"})
		return
	}
	c.JSON(http.StatusOK, metrics)
}
