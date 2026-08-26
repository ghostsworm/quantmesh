package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

type localDevSettingsProvider struct{}

func (localDevSettingsProvider) GetSystemSettingBool(ctx context.Context, key string, defaultValue bool) (bool, error) {
	if key == "local_dev_mode" {
		return true, nil
	}
	return defaultValue, nil
}

func (localDevSettingsProvider) GetSystemSettings(ctx context.Context, filter *storage.SystemSettingFilter) ([]*storage.SystemSetting, error) {
	return nil, nil
}

func (localDevSettingsProvider) GetSystemSetting(ctx context.Context, key string) (*storage.SystemSetting, error) {
	return nil, nil
}

func (localDevSettingsProvider) SetSystemSettingBool(ctx context.Context, key string, value bool) error {
	return nil
}

func (localDevSettingsProvider) SetSystemSettingString(ctx context.Context, key, value string) error {
	return nil
}

func (localDevSettingsProvider) SaveSystemSetting(ctx context.Context, key, value, settingType string) error {
	return nil
}

func (localDevSettingsProvider) DeleteSystemSetting(ctx context.Context, key string) error {
	return nil
}

func TestProtectedRoutesSmokeEarlyExitPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalProvider := storageProvider
	SetStorageProvider(localDevSettingsProvider{})
	t.Cleanup(func() { SetStorageProvider(originalProvider) })

	router := gin.New()
	router.Use(gin.Recovery())
	cfg := &config.Config{}
	cfg.Web.SharedDir = t.TempDir()
	SetupRoutesWithConfig(router, cfg)

	requests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/status", ""},
		{http.MethodGet, "/api/statuses", ""},
		{http.MethodGet, "/api/services/status", ""},
		{http.MethodGet, "/api/symbols", ""},
		{http.MethodGet, "/api/bots", ""},
		{http.MethodGet, "/api/bots/missing", ""},
		{http.MethodGet, "/api/bots/missing/account-balances", ""},
		{http.MethodPost, "/api/bots/create", `{}`},
		{http.MethodPost, "/api/bots/preflight-funding", `{}`},
		{http.MethodGet, "/api/funding-carry/dashboard", ""},
		{http.MethodGet, "/api/funding-carry/status/missing", ""},
		{http.MethodGet, "/api/funding-carry/income-history", ""},
		{http.MethodGet, "/api/bots/missing/config-file", ""},
		{http.MethodPut, "/api/bots/missing/config-file", `{}`},
		{http.MethodDelete, "/api/bots/missing/config-file", ""},
		{http.MethodGet, "/api/bots/missing/validate", ""},
		{http.MethodGet, "/api/bots/missing/hybrid-config", ""},
		{http.MethodGet, "/api/bots/missing/hybrid-status", ""},
		{http.MethodGet, "/api/hybrid/rules/templates", ""},
		{http.MethodGet, "/api/strategy-templates", ""},
		{http.MethodGet, "/api/strategy-templates/grid_basic", ""},
		{http.MethodGet, "/api/strategy-templates/category/grid", ""},
		{http.MethodGet, "/api/strategy-templates/grid_basic/export", ""},
		{http.MethodPost, "/api/strategy-templates/custom", `{}`},
		{http.MethodPost, "/api/strategy-templates/import", `{}`},
		{http.MethodGet, "/api/bot-groups", ""},
		{http.MethodGet, "/api/bot-groups/missing", ""},
		{http.MethodPost, "/api/bot-groups", `{}`},
		{http.MethodGet, "/api/exchanges", ""},
		{http.MethodGet, "/api/positions", ""},
		{http.MethodGet, "/api/positions/summary", ""},
		{http.MethodGet, "/api/positions/exchange-summary", ""},
		{http.MethodGet, "/api/positions/summary/all", ""},
		{http.MethodGet, "/api/orders", ""},
		{http.MethodGet, "/api/orders/history", ""},
		{http.MethodPost, "/api/orders/sync", `{}`},
		{http.MethodGet, "/api/statistics", ""},
		{http.MethodGet, "/api/statistics/daily", ""},
		{http.MethodGet, "/api/statistics/daily/breakdown", ""},
		{http.MethodGet, "/api/statistics/trades", ""},
		{http.MethodGet, "/api/statistics/pnl/symbol", ""},
		{http.MethodGet, "/api/statistics/pnl/time-range", ""},
		{http.MethodGet, "/api/statistics/pnl/exchange", ""},
		{http.MethodGet, "/api/statistics/pnl/diagnosis", ""},
		{http.MethodGet, "/api/statistics/anomalous-trades", ""},
		{http.MethodGet, "/api/reconciliation/status", ""},
		{http.MethodGet, "/api/allocation/status", ""},
		{http.MethodGet, "/api/allocation/status/binance/BTCUSDT", ""},
		{http.MethodGet, "/api/opening-control/status", ""},
		{http.MethodGet, "/api/opening-control/config", ""},
		{http.MethodPost, "/api/opening-control/pause", `{}`},
		{http.MethodPost, "/api/opening-control/resume", `{}`},
		{http.MethodGet, "/api/position-plans/check", ""},
		{http.MethodGet, "/api/position-plans", ""},
		{http.MethodGet, "/api/position-plans/1", ""},
		{http.MethodPost, "/api/position-plans", `{}`},
		{http.MethodGet, "/api/backtest/strategies", ""},
		{http.MethodGet, "/api/backtest/presets/BTCUSDT", ""},
		{http.MethodGet, "/api/backtest/exchanges", ""},
		{http.MethodGet, "/api/backtest/symbols", ""},
		{http.MethodGet, "/api/backtest/config-params", ""},
		{http.MethodGet, "/api/backtest/cache/status", ""},
		{http.MethodGet, "/api/backtest/cache/stats", ""},
		{http.MethodGet, "/api/backtest/cache/list", ""},
		{http.MethodGet, "/api/backtest/tasks", ""},
		{http.MethodGet, "/api/backtest/tasks/missing", ""},
		{http.MethodGet, "/api/backtest/tasks/missing/result", ""},
		{http.MethodGet, "/api/backtest/tasks/missing/klines", ""},
		{http.MethodGet, "/api/backtest/tasks/missing/report", ""},
		{http.MethodGet, "/api/backtest/tasks/missing/trades", ""},
		{http.MethodGet, "/api/backtest/smart-params", ""},
		{http.MethodPost, "/api/backtest/smart-params", `{}`},
		{http.MethodGet, "/api/backtest/precomputed", ""},
		{http.MethodGet, "/api/backtest/precomputed/BTCUSDT/grid", ""},
		{http.MethodGet, "/api/backtest/scheduler/status", ""},
		{http.MethodGet, "/api/backtest/optim/tasks", ""},
		{http.MethodGet, "/api/backtest/optim/tasks/missing", ""},
		{http.MethodGet, "/api/backtest/optim/tasks/missing/result", ""},
		{http.MethodGet, "/api/backtest/optim/space/grid", ""},
		{http.MethodGet, "/api/reconciliation/history", ""},
		{http.MethodGet, "/api/reconciliation/aggregated", ""},
		{http.MethodGet, "/api/risk/status", ""},
		{http.MethodGet, "/api/risk/monitor", ""},
		{http.MethodGet, "/api/news/analysis", ""},
		{http.MethodGet, "/api/news/predictions", ""},
		{http.MethodGet, "/api/news/collected", ""},
		{http.MethodGet, "/api/news/keywords", ""},
		{http.MethodGet, "/api/news/history", ""},
		{http.MethodGet, "/api/news/history/missing", ""},
		{http.MethodGet, "/api/predictions/accuracy", ""},
		{http.MethodGet, "/api/predictions/history", ""},
		{http.MethodGet, "/api/risk/history", ""},
		{http.MethodGet, "/api/risk/newbie-check", ""},
		{http.MethodGet, "/api/market/ticker", ""},
		{http.MethodGet, "/api/config/param-advisor", ""},
		{http.MethodGet, "/api/config/exchange-fees", ""},
		{http.MethodGet, "/api/config/price-range", ""},
		{http.MethodGet, "/api/config", ""},
		{http.MethodGet, "/api/config/json", ""},
		{http.MethodPost, "/api/config/validate", `{}`},
		{http.MethodPost, "/api/config/preview", `{}`},
		{http.MethodPost, "/api/config/test-notification", `{}`},
		{http.MethodPost, "/api/config/test-gemini", `{}`},
		{http.MethodPost, "/api/config/test-exchange", `{}`},
		{http.MethodGet, "/api/config/security/status", ""},
		{http.MethodGet, "/api/export/config", ""},
		{http.MethodGet, "/api/export/trades", ""},
		{http.MethodGet, "/api/export/orders", ""},
		{http.MethodGet, "/api/export/positions", ""},
		{http.MethodGet, "/api/export/statistics", ""},
		{http.MethodGet, "/api/export/reconciliation", ""},
		{http.MethodGet, "/api/export/risk-checks", ""},
		{http.MethodGet, "/api/export/system-metrics", ""},
		{http.MethodGet, "/api/export/logs", ""},
		{http.MethodGet, "/api/export/audit-logs", ""},
		{http.MethodGet, "/api/export/backtest-reports", ""},
		{http.MethodGet, "/api/export/all", ""},
		{http.MethodPost, "/api/config/import-migration", `{}`},
		{http.MethodPost, "/api/trading/start", `{}`},
		{http.MethodPost, "/api/trading/stop", `{}`},
		{http.MethodPost, "/api/trading/close-positions", `{}`},
		{http.MethodGet, "/api/system/metrics", ""},
		{http.MethodGet, "/api/system/metrics/current", ""},
		{http.MethodGet, "/api/system/metrics/daily", ""},
		{http.MethodGet, "/api/system/settings", ""},
		{http.MethodGet, "/api/system/settings/local_dev_mode", ""},
		{http.MethodPost, "/api/system/settings", `{}`},
		{http.MethodDelete, "/api/system/settings/local_dev_mode", ""},
		{http.MethodGet, "/api/system/local-dev-mode", ""},
		{http.MethodPost, "/api/system/local-dev-mode", `{}`},
		{http.MethodGet, "/api/observability/config", ""},
		{http.MethodPut, "/api/observability/config", `{}`},
		{http.MethodPost, "/api/observability/test", `{}`},
		{http.MethodGet, "/api/mcp/config", ""},
		{http.MethodPut, "/api/mcp/config", `{}`},
		{http.MethodPost, "/api/mcp/token/rotate", `{}`},
		{http.MethodDelete, "/api/mcp/token", ""},
		{http.MethodGet, "/api/mcp/client-snippet", ""},
		{http.MethodGet, "/api/logs", ""},
		{http.MethodPost, "/api/logs/clean", `{}`},
		{http.MethodGet, "/api/logs/stats", ""},
		{http.MethodPost, "/api/logs/vacuum", `{}`},
		{http.MethodGet, "/api/slots", ""},
		{http.MethodGet, "/api/strategies/allocation", ""},
		{http.MethodGet, "/api/orders/pending", ""},
		{http.MethodGet, "/api/orders/exchange-open", ""},
		{http.MethodPost, "/api/orders/123/cancel", `{}`},
		{http.MethodPost, "/api/orders/cancel", `{}`},
		{http.MethodPost, "/api/orders/cancel-all-exchange", `{}`},
		{http.MethodGet, "/api/klines", ""},
		{http.MethodGet, "/api/funding/current", ""},
		{http.MethodGet, "/api/ai/status", ""},
		{http.MethodGet, "/api/ai/analysis/market", ""},
		{http.MethodGet, "/api/ai/analysis/parameter", ""},
		{http.MethodGet, "/api/ai/analysis/risk", ""},
		{http.MethodGet, "/api/ai/analysis/sentiment", ""},
		{http.MethodGet, "/api/ai/analysis/polymarket", ""},
		{http.MethodPost, "/api/ai/analysis/trigger/market", `{}`},
		{http.MethodGet, "/api/ai/prompts", ""},
		{http.MethodPost, "/api/ai/prompts", `{}`},
		{http.MethodPost, "/api/ai/generate-config", `{}`},
		{http.MethodGet, "/api/ai/task/missing", ""},
		{http.MethodGet, "/api/ai/tasks", ""},
		{http.MethodGet, "/api/ai/tasks/stats", ""},
		{http.MethodGet, "/api/ai/gemini/usage", ""},
		{http.MethodPost, "/api/ai/apply-config", `{}`},
		{http.MethodPost, "/api/ai/market-interpret", `{}`},
		{http.MethodGet, "/api/ai/market-interpret/latest", ""},
		{http.MethodGet, "/api/ai/market-interpret/history", ""},
		{http.MethodGet, "/api/ai/market-interpret/missing", ""},
		{http.MethodGet, "/api/funding/history", ""},
		{http.MethodGet, "/api/basis/config", ""},
		{http.MethodPut, "/api/basis/config", `{}`},
		{http.MethodGet, "/api/basis/current", ""},
		{http.MethodGet, "/api/basis/history", ""},
		{http.MethodGet, "/api/basis/statistics", ""},
		{http.MethodGet, "/api/market-intelligence/news-digest", ""},
		{http.MethodGet, "/api/market-intelligence", ""},
		{http.MethodGet, "/api/macro/events", ""},
		{http.MethodGet, "/api/macro/impact", ""},
		{http.MethodGet, "/api/permissions/check", ""},
		{http.MethodGet, "/api/audit/logs", ""},
		{http.MethodGet, "/api/strategies", ""},
		{http.MethodGet, "/api/strategies/templates", ""},
		{http.MethodGet, "/api/strategies/types", ""},
		{http.MethodGet, "/api/strategies/configs", ""},
		{http.MethodGet, "/api/strategies/enabled", ""},
		{http.MethodGet, "/api/strategies/runtime", ""},
		{http.MethodGet, "/api/strategies/runtime/all", ""},
		{http.MethodGet, "/api/strategies/runtime/missing", ""},
		{http.MethodPost, "/api/strategies/batch-update", `{}`},
		{http.MethodPost, "/api/strategies/release-all-capital", `{}`},
		{http.MethodGet, "/api/strategies/grid/license", ""},
		{http.MethodPut, "/api/strategies/grid/config", `{}`},
		{http.MethodPost, "/api/strategies/grid/purchase", `{}`},
		{http.MethodPost, "/api/strategies/grid/release-capital", `{}`},
		{http.MethodGet, "/api/profit/summary", ""},
		{http.MethodGet, "/api/profit/funding", ""},
		{http.MethodGet, "/api/profit/by-strategy", ""},
		{http.MethodGet, "/api/profit/by-strategy/grid", ""},
		{http.MethodGet, "/api/profit/withdraw-rules", ""},
		{http.MethodPut, "/api/profit/withdraw-rules", `{}`},
		{http.MethodPost, "/api/profit/withdraw-rules/upsert", `{}`},
		{http.MethodDelete, "/api/profit/withdraw-rules/rule-1", ""},
		{http.MethodPost, "/api/profit/withdraw", `{}`},
		{http.MethodGet, "/api/profit/history", ""},
		{http.MethodGet, "/api/profit/trend", ""},
		{http.MethodPost, "/api/profit/withdraw/estimate", `{}`},
		{http.MethodPost, "/api/profit/withdraw/w1/cancel", `{}`},
		{http.MethodGet, "/api/profit/withdraw/w1", ""},
		{http.MethodGet, "/api/capital/overview", ""},
		{http.MethodGet, "/api/capital/usage", ""},
		{http.MethodGet, "/api/capital/allocation", ""},
		{http.MethodPut, "/api/capital/allocation", `{}`},
		{http.MethodGet, "/api/capital/allocation/grid", ""},
		{http.MethodPut, "/api/capital/allocation/grid", `{}`},
		{http.MethodPost, "/api/capital/allocation/grid/lock", `{}`},
		{http.MethodPost, "/api/capital/rebalance", `{}`},
		{http.MethodGet, "/api/capital/history", ""},
		{http.MethodPut, "/api/capital/reserve", `{}`},
		{http.MethodGet, "/api/kline-files", ""},
		{http.MethodGet, "/api/kline-files/available", ""},
		{http.MethodPost, "/api/kline-files/sample.csv/protect", `{}`},
		{http.MethodDelete, "/api/kline-files/sample.csv/protect", ""},
		{http.MethodGet, "/api/kline-files/sample.csv/download", ""},
		{http.MethodPost, "/api/v2/bots/bot-1/close-positions", `{}`},
		{http.MethodGet, "/api/v2/bots/bot-1/close-records", ""},
		{http.MethodGet, "/api/v2/bots/bot-1/slots", ""},
		{http.MethodGet, "/api/v2/bots/bot-1/slot-filter", ""},
		{http.MethodPost, "/api/v2/bots/bot-1/slot-filter", `{}`},
		{http.MethodPost, "/api/v2/bots/bot-1/slot-filter/rules", `{}`},
		{http.MethodDelete, "/api/v2/bots/bot-1/slot-filter/rules/0", ""},
		{http.MethodPost, "/api/v2/bots/bot-1/slots/toggle", `{}`},
		{http.MethodGet, "/api/v2/bots/bot-1/risk-control/events/export", ""},
		{http.MethodGet, "/api/v2/bots/bot-1/risk-control/events", ""},
		{http.MethodGet, "/api/v2/bots/bot-1/risk-control", ""},
		{http.MethodPut, "/api/v2/bots/bot-1/risk-control", `{}`},
		{http.MethodPost, "/api/v2/bots/bot-1/pause-opening", `{}`},
		{http.MethodPost, "/api/v2/bots/bot-1/resume-opening", `{}`},
		{http.MethodGet, "/api/v2/bots/bot-1/position-status", ""},
		{http.MethodGet, "/api/v2/bots/bot-1/option-hedge/status", ""},
		{http.MethodPost, "/api/v2/bots/bot-1/option-hedge/sync", `{}`},
		{http.MethodGet, "/api/v2/bots/bot-1/option-hedge/roll-suggestions", ""},
		{http.MethodPost, "/api/v2/bots/bot-1/option-hedge/execute-roll", `{}`},
		{http.MethodPost, "/api/v2/bots/bot-1/backtest", `{}`},
		{http.MethodGet, "/api/v2/bots/bot-1/backtest/tasks", ""},
		{http.MethodPost, "/api/v2/close-records/record-1/retry", `{}`},
		{http.MethodGet, "/api/v2/bot/backtest/task-1", ""},
		{http.MethodGet, "/api/v2/bot/backtest/task-1/result", ""},
		{http.MethodDelete, "/api/v2/bot/backtest/task-1", ""},
		{http.MethodPost, "/api/v2/backtest/data/download", `{}`},
		{http.MethodGet, "/api/v2/backtest/data/info", ""},
		{http.MethodGet, "/api/v2/backtest/data/availability", ""},
		{http.MethodGet, "/api/v2/backtest/data/latest", ""},
		{http.MethodGet, "/api/circuit-breaker/status", ""},
		{http.MethodGet, "/api/circuit-breaker/events", ""},
		{http.MethodPost, "/api/circuit-breaker/trigger", `{}`},
		{http.MethodPost, "/api/circuit-breaker/recover", `{}`},
		{http.MethodPost, "/api/circuit-breaker/metrics", `{}`},
		{http.MethodPost, "/api/circuit-breaker/report/auth-failure", `{}`},
		{http.MethodPost, "/api/circuit-breaker/report/websocket-disconnect", `{}`},
		{http.MethodPost, "/api/circuit-breaker/report/websocket-reconnected", `{}`},
		{http.MethodGet, "/api/emergency/scenarios", ""},
		{http.MethodPost, "/api/emergency/execute", `{}`},
		{http.MethodGet, "/api/emergency/operations", ""},
		{http.MethodGet, "/api/emergency/mode", ""},
		{http.MethodPost, "/api/emergency/mode/disable", `{}`},
		{http.MethodGet, "/api/dynamic-stop-loss/slots", ""},
		{http.MethodGet, "/api/dynamic-stop-loss/stats", ""},
		{http.MethodPost, "/api/dynamic-stop-loss/adjust", `{}`},
		{http.MethodGet, "/api/agent/sessions", ""},
		{http.MethodPost, "/api/agent/sessions", `{}`},
		{http.MethodPost, "/api/agent/sessions/session-1/messages", `{}`},
		{http.MethodGet, "/api/agent/sessions/session-1/history", ""},
		{http.MethodGet, "/api/agent/sessions/session-1/config", ""},
		{http.MethodPost, "/api/agent/sessions/session-1/apply", `{}`},
		{http.MethodDelete, "/api/agent/sessions/session-1", ""},
	}

	for _, tc := range requests {
		tc := tc
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			body := bytes.NewBufferString(tc.body)
			req := httptest.NewRequest(tc.method, tc.path, body)
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code == http.StatusUnauthorized {
				t.Fatalf("route unexpectedly hit auth failure: body=%s", w.Body.String())
			}
		})
	}
}
