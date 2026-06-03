package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAPIHandlersMissingDependencySmoke(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetWebProviderStateForTest()
	SetStorageProvider(nil)
	SetTaskProvider(nil)
	SetNewsMonitorProvider(nil)
	SetInstanceManager(nil)
	SetEventProvider(nil)
	SetPlanManagerProvider(nil)
	SetDataSourceProvider(nil)

	tests := []struct {
		name    string
		method  string
		target  string
		body    string
		params  gin.Params
		handler func(*gin.Context)
	}{
		{"param advisor missing symbol", http.MethodGet, "/advisor", "", nil, getParamAdvisor},
		{"exchange fees default", http.MethodGet, "/fees?exchange=binance", "", nil, getExchangeFees},
		{"news analysis missing provider", http.MethodGet, "/news/analysis", "", nil, getNewsAnalysis},
		{"news predictions missing provider", http.MethodGet, "/news/predictions", "", nil, getNewsPredictions},
		{"news analyze missing provider", http.MethodPost, "/news/analyze", `{}`, nil, postNewsAnalyze},
		{"news collected missing provider", http.MethodGet, "/news/collected", "", nil, getNewsCollected},
		{"news keywords missing provider", http.MethodGet, "/news/keywords", "", nil, getNewsKeywords},
		{"news history missing storage", http.MethodGet, "/news/history", "", nil, getNewsHistory},
		{"opening status missing runtime", http.MethodGet, "/opening?exchange=binance&symbol=BTCUSDT", "", nil, getOpeningControlStatus},
		{"pause opening missing runtime", http.MethodPost, "/opening/pause?exchange=binance&symbol=BTCUSDT", `{}`, nil, pauseOpening},
		{"resume opening missing runtime", http.MethodPost, "/opening/resume?exchange=binance&symbol=BTCUSDT", `{}`, nil, resumeOpening},
		{"opening config missing runtime", http.MethodGet, "/opening/config?exchange=binance&symbol=BTCUSDT", "", nil, getOpeningControlConfig},
		{"ai tasks missing provider", http.MethodGet, "/ai/tasks", "", nil, getAITasks},
		{"ai task stats missing provider", http.MethodGet, "/ai/tasks/stats", "", nil, getAITaskStats},
		{"ai task status missing provider", http.MethodGet, "/ai/tasks/task-1", "", gin.Params{{Key: "id", Value: "task-1"}}, getAITaskStatus},
		{"market ticker missing symbol", http.MethodGet, "/ticker", "", nil, getMarketTicker},
		{"funding rate empty", http.MethodGet, "/funding/current", "", nil, getFundingRate},
		{"observability get nil provider", http.MethodGet, "/observability", "", nil, getObservabilityConfig},
		{"observability put nil provider", http.MethodPut, "/observability", `{}`, nil, putObservabilityConfig},
		{"option hedge status missing", http.MethodGet, "/option-hedge/status?bot_id=bot-1", "", nil, getOptionHedgeStatus},
		{"emergency scenarios", http.MethodGet, "/emergency/scenarios", "", nil, getEmergencyScenarios},
		{"emergency execute bad body", http.MethodPost, "/emergency/execute", `{}`, nil, executeEmergencyScenario},
		{"payment status missing", http.MethodGet, "/payments/missing", "", gin.Params{{Key: "id", Value: "missing"}}, getPaymentStatusHandler},
		{"supported currencies", http.MethodGet, "/payments/currencies", "", nil, getSupportedCryptoCurrenciesHandler},
		{"auth status", http.MethodGet, "/auth/status", "", nil, getAuthStatus},
		{"set password bad request", http.MethodPost, "/auth/password", `{}`, nil, setPassword},
		{"verify password bad request", http.MethodPost, "/auth/verify", `{}`, nil, verifyPassword},
		{"logout no session", http.MethodPost, "/auth/logout", `{}`, nil, logout},
		{"ai analysis status", http.MethodGet, "/ai/status", "", nil, getAIAnalysisStatus},
		{"ai market missing provider", http.MethodGet, "/ai/market", "", nil, getAIMarketAnalysis},
		{"ai risk missing provider", http.MethodGet, "/ai/risk", "", nil, getAIRiskAnalysis},
		{"webauthn list missing manager", http.MethodGet, "/webauthn/credentials", "", nil, listWebAuthnCredentials},
		{"strategy detail missing id", http.MethodGet, "/strategies/", "", nil, getStrategyDetailHandler},
		{"strategy runtime all missing provider", http.MethodGet, "/strategies/runtime", "", nil, getStrategyRuntimeAllHandler},
		{"events missing provider", http.MethodGet, "/events", "", nil, handleGetEvents},
		{"event detail missing provider", http.MethodGet, "/events/e1", "", gin.Params{{Key: "id", Value: "e1"}}, handleGetEventDetail},
		{"kline files missing storage", http.MethodGet, "/kline-files", "", nil, listKlineFilesHandler},
		{"positions missing provider", http.MethodGet, "/positions", "", nil, getPositions},
		{"positions summary missing provider", http.MethodGet, "/positions/summary", "", nil, getPositionsSummary},
		{"fix sessions missing storage", http.MethodGet, "/fix/sessions", "", nil, getFixSessions},
		{"config get missing manager", http.MethodGet, "/config", "", nil, getConfigHandler},
		{"mcp config nil provider", http.MethodGet, "/mcp/config", "", nil, getMCPConfig},
		{"position plans missing manager", http.MethodGet, "/plans", "", nil, getPositionPlans},
		{"optimizer status missing", http.MethodGet, "/optimizer/status/task-1", "", gin.Params{{Key: "id", Value: "task-1"}}, getOptimizerStatus},
		{"bot account balances missing", http.MethodGet, "/bots/bot-1/balances", "", gin.Params{{Key: "bot_id", Value: "bot-1"}}, getBotAccountBalances},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(tc.method, tc.target, bytes.NewBufferString(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = tc.params
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("handler panicked: %v", r)
				}
			}()
			tc.handler(c)
			if w.Code == 0 {
				t.Fatalf("handler did not write a response")
			}
		})
	}
}
