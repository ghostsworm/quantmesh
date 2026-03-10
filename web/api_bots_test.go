package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

func TestUpdateBotStrategyRequest_SmartOrderEnabled(t *testing.T) {
	// 驗證 smart_order_enabled 能正確解析並持久化
	body := `{"strategies":[{"type":"grid","weight":1}],"smart_order_enabled":true,"smart_order_max_open_orders":4,"smart_order_open_order_distance":6}`
	var req UpdateBotStrategyRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if req.SmartOrderEnabled == nil || !*req.SmartOrderEnabled {
		t.Error("smart_order_enabled should be true")
	}
	if req.SmartOrderMaxOpenOrders == nil || *req.SmartOrderMaxOpenOrders != 4 {
		t.Error("smart_order_max_open_orders should be 4")
	}
	if req.SmartOrderOpenOrderDistance == nil || *req.SmartOrderOpenOrderDistance != 6 {
		t.Error("smart_order_open_order_distance should be 6")
	}
}

func TestBuildGridRiskControlFromRequest(t *testing.T) {
	tests := []struct {
		name string
		req  CreateBotRequest
		want config.GridRiskControl
	}{
		{
			name: "empty_request",
			req:  CreateBotRequest{},
			want: config.GridRiskControl{},
		},
		{
			name: "with_risk_control",
			req: CreateBotRequest{
				GridRiskControlEnabled:         true,
				GridRiskControlStopLossRatio:   0.1,
				GridRiskControlTakeProfitRatio: 0.08,
				GridRiskControlTrailingRatio:   0.02,
				GridRiskControlTrendFilter:     true,
				GridRiskControlMaxGridLayers:   20,
				GridRiskControlMaxOpenOrdersCap: 5,
			},
			want: config.GridRiskControl{
				Enabled:                 true,
				StopLossRatio:           0.1,
				TakeProfitTriggerRatio:  0.08,
				TrailingTakeProfitRatio: 0.02,
				TrendFilterEnabled:      true,
				MaxGridLayers:           20,
				MaxOpenOrdersAtCap:      5,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGridRiskControlFromRequest(tt.req)
			if got.Enabled != tt.want.Enabled ||
				got.StopLossRatio != tt.want.StopLossRatio ||
				got.TakeProfitTriggerRatio != tt.want.TakeProfitTriggerRatio ||
				got.TrailingTakeProfitRatio != tt.want.TrailingTakeProfitRatio ||
				got.TrendFilterEnabled != tt.want.TrendFilterEnabled ||
				got.MaxGridLayers != tt.want.MaxGridLayers ||
				got.MaxOpenOrdersAtCap != tt.want.MaxOpenOrdersAtCap {
				t.Errorf("buildGridRiskControlFromRequest() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// mockBotManagerForStartTest 用於驗證 postBotStart 在啟動前會調用 EnableBot 清除禁用標記
type mockBotManagerForStartTest struct {
	enableBotCalled bool
	mu             sync.Mutex
}

func (m *mockBotManagerForStartTest) ListBots() []BotResponse { return nil }
func (m *mockBotManagerForStartTest) GetBot(botID string) (*BotDetailResponse, bool) {
	return nil, false // 不在運行，繼續啟動流程
}
func (m *mockBotManagerForStartTest) StartBot(ctx context.Context, cfg config.BotConfig) error { return nil }
func (m *mockBotManagerForStartTest) StopBot(botID string) error                              { return nil }
func (m *mockBotManagerForStartTest) EnableBot(botID string) error {
	m.mu.Lock()
	m.enableBotCalled = true
	m.mu.Unlock()
	return nil
}

// TestPostBotStartCallsEnableBotBeforeStart 驗證：用戶通過 Web UI 停止後再次點擊啟動時，
// postBotStart 會先調用 EnableBot 清除數據庫中的禁用標記，否則 StartBot 會因 bot_disabled_in_database 失敗
func TestPostBotStartCallsEnableBotBeforeStart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	cfg := &config.Config{}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Bots = []config.BotConfig{
		{ID: "test-bot-id", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", PriceInterval: 100, OrderQuantity: 100},
	}
	if err := config.SaveConfig(cfg, configPath); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	fcm := NewFileConfigManager(configPath)
	fcm.UpdateConfig(cfg)
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })

	mock := &mockBotManagerForStartTest{}
	origProvider := botManagerProvider
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bots/test-bot-id/start", nil)
	c.Params = gin.Params{{Key: "id", Value: "test-bot-id"}}

	postBotStart(c)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d body=%s", w.Code, w.Body.String())
	}
	mock.mu.Lock()
	called := mock.enableBotCalled
	mock.mu.Unlock()
	if !called {
		t.Error("postBotStart 應在啟動前調用 EnableBot 清除禁用標記，否則 StartBot 會因 bot_disabled_in_database 失敗")
	}
}
