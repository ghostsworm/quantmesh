package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPremiumStrategyCannotBeEnabledWithoutVerifiedLicense(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/strategies/:id/enable", enableStrategyHandler)
	router.PUT("/strategies/:id/config", updateStrategyConfigHandler)

	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/strategies/trend_following/enable", ""},
		{http.MethodPut, "/strategies/mean_reversion/config", `{"enabled":true}`},
	} {
		req := httptest.NewRequest(request.method, request.path, strings.NewReader(request.body))
		if request.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want %d; body=%s", request.method, request.path, response.Code, http.StatusForbidden, response.Body.String())
		}
	}
}

func TestStrategyPurchaseAndUnimplementedBatchUpdateDoNotClaimSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/strategies/:id/purchase", purchaseStrategyHandler)
	router.POST("/strategies/batch-update", batchUpdateStrategiesHandler)

	for _, request := range []struct {
		path string
		body string
	}{
		{"/strategies/trend_following/purchase", `{"tier":"enterprise"}`},
		{"/strategies/batch-update", `{"updates":[{"strategyId":"grid","enabled":true}]}`},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, request.path, strings.NewReader(request.body)))
		if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), `"success":true`) {
			t.Errorf("POST %s falsely reported success: status=%d body=%s", request.path, response.Code, response.Body.String())
		}
	}
}

func TestStrategyDetailDoesNotPublishUnverifiedPerformanceNumbers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/strategies/:id", getStrategyDetailHandler)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/strategies/trend_following", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK || strings.Contains(body, `"winRate":65.5`) || strings.Contains(body, `"totalTrades":1523`) {
		t.Fatalf("strategy detail contains unverified performance data: status=%d body=%s", response.Code, body)
	}
}
