package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/cfgmgr"
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
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

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

type mockBotManagerForDeleteGroupTest struct {
	stopCalls []string
	mu        sync.Mutex
}

func (m *mockBotManagerForDeleteGroupTest) ListBots() []BotResponse { return nil }
func (m *mockBotManagerForDeleteGroupTest) GetBot(botID string) (*BotDetailResponse, bool) {
	return nil, false
}
func (m *mockBotManagerForDeleteGroupTest) StartBot(ctx context.Context, cfg config.BotConfig) error {
	return nil
}
func (m *mockBotManagerForDeleteGroupTest) StopBot(botID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalls = append(m.stopCalls, botID)
	return nil
}
func (m *mockBotManagerForDeleteGroupTest) EnableBot(botID string) error { return nil }

type mockBotManagerForGroupConsistencyTest struct {
	running map[string]bool
}

func (m *mockBotManagerForGroupConsistencyTest) ListBots() []BotResponse { return nil }
func (m *mockBotManagerForGroupConsistencyTest) GetBot(botID string) (*BotDetailResponse, bool) {
	r, ok := m.running[botID]
	if !ok {
		return nil, false
	}
	return &BotDetailResponse{
		BotResponse: BotResponse{
			BotID:   botID,
			Running: r,
		},
	}, true
}
func (m *mockBotManagerForGroupConsistencyTest) StartBot(ctx context.Context, cfg config.BotConfig) error {
	return nil
}
func (m *mockBotManagerForGroupConsistencyTest) StopBot(botID string) error { return nil }
func (m *mockBotManagerForGroupConsistencyTest) EnableBot(botID string) error { return nil }

func TestDeleteBotGroupStopsRunningBotsBeforeRemove(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	cfg := &config.Config{
		Bots: []config.BotConfig{
			{ID: "futures-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", PriceInterval: 100, OrderQuantity: 100, MinOrderValue: 6, BuyWindowSize: 10, SellWindowSize: 10},
			{ID: "spot-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", PriceInterval: 100, OrderQuantity: 100, MinOrderValue: 6, BuyWindowSize: 10, SellWindowSize: 10},
		},
		BotGroups: []config.BotGroup{
			{ID: "g1", Name: "pair-1", BotIDs: []string{"futures-bot", "spot-bot"}},
		},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}
	if err := config.SaveConfig(cfg, configPath); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	fcm := NewFileConfigManager(configPath)
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

	mock := &mockBotManagerForDeleteGroupTest{}
	origProvider := botManagerProvider
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/bot-groups/g1", nil)
	c.Params = gin.Params{{Key: "id", Value: "g1"}}

	deleteBotGroup(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	mock.mu.Lock()
	calls := append([]string(nil), mock.stopCalls...)
	mock.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("expected StopBot called twice, got %d (%v)", len(calls), calls)
	}

	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatalf("GetLatestConfig: %v", err)
	}
	if len(latest.BotGroups) != 0 {
		t.Fatalf("group should be removed, got groups=%d", len(latest.BotGroups))
	}

	_ = os.Remove(configPath)
}

func TestGetBotGroupByIDIncludesConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")
	cfg := &config.Config{
		Bots: []config.BotConfig{
			{ID: "fut-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", PriceInterval: 100, OrderQuantity: 100, MinOrderValue: 6, BuyWindowSize: 10, SellWindowSize: 10},
			{ID: "spot-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", PriceInterval: 100, OrderQuantity: 100, MinOrderValue: 6, BuyWindowSize: 10, SellWindowSize: 10},
		},
		BotGroups: []config.BotGroup{
			{ID: "g-consistency", Name: "hedge-btc", Type: "futures_spot_hedge", BotIDs: []string{"fut-bot", "spot-bot"}},
		},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}
	if err := config.SaveConfig(cfg, configPath); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	fcm := NewFileConfigManager(configPath)
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

	mock := &mockBotManagerForGroupConsistencyTest{
		running: map[string]bool{
			"fut-bot":  true,
			"spot-bot": false,
		},
	}
	origProvider := botManagerProvider
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/bot-groups/g-consistency", nil)
	c.Params = gin.Params{{Key: "id", Value: "g-consistency"}}

	getBotGroupByID(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	groupObj, ok := resp["bot_group"].(map[string]interface{})
	if !ok {
		t.Fatalf("bot_group should be object")
	}
	consistency, ok := groupObj["consistency"].(map[string]interface{})
	if !ok {
		t.Fatalf("bot_group.consistency should exist")
	}
	if consistency["status"] != "single_leg_running" {
		t.Fatalf("expected status single_leg_running, got %v", consistency["status"])
	}
}
