package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/risk"
)

func TestEquityHealthAPIReportsUnverifiedAndRejectsOverwrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := globalCircuitBreaker
	t.Cleanup(func() { globalCircuitBreaker = previous })
	globalCircuitBreaker = risk.NewGlobalCircuitBreaker(&config.CircuitBreakerConfig{}, nil, nil)
	globalCircuitBreaker.UpdateMetricsObservation(risk.MetricsSnapshot{MaxDrawdownPct: 20}, risk.MetricsHealth{
		Available: true, DrawdownAvailable: true, CashFlowAdjusted: false, Persisted: true, CheckedAt: time.Now(), ValidUntil: time.Now().Add(time.Minute),
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	getCircuitBreakerStatus(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"cash_flow_adjusted":false`) || !strings.Contains(w.Body.String(), `"metrics_health"`) {
		t.Fatalf("missing data provenance: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/risk/metrics", strings.NewReader(`{"daily_pnl":0,"max_drawdown":0,"consecutive_losses":0}`))
	c.Request.Header.Set("Content-Type", "application/json")
	updateCircuitBreakerMetrics(c)
	if w.Code != http.StatusConflict {
		t.Fatalf("managed metrics overwrite accepted: %d %s", w.Code, w.Body.String())
	}
}
