package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/cfgmgr"
	"quantmesh/config"
	"quantmesh/storage"
)

// setupTestPrimaryAppConfigStorage 為需調用 FileConfigManager.UpdateConfig 的測試注入主庫 SQLite。
func setupTestPrimaryAppConfigStorage(t *testing.T) func() {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "api_bots_test_app.db")
	st, err := storage.NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureAppConfigDocumentTables(); err != nil {
		t.Fatal(err)
	}
	orig := primaryStorageForAppConfig
	SetPrimaryStorageForAppConfig(st)
	return func() {
		SetPrimaryStorageForAppConfig(orig)
		_ = st.Close()
	}
}

func TestFindGroupNameByBotID(t *testing.T) {
	tests := []struct {
		name   string
		cfg    *config.Config
		botID  string
		want   string
	}{
		{
			name:  "nil_config",
			cfg:   nil,
			botID: "bot-1",
			want:  "",
		},
		{
			name:  "nil_bot_groups",
			cfg:   &config.Config{BotGroups: nil},
			botID: "bot-1",
			want:  "",
		},
		{
			name:  "empty_bot_groups",
			cfg:   &config.Config{BotGroups: []config.BotGroup{}},
			botID: "bot-1",
			want:  "",
		},
		{
			name: "bot_in_group",
			cfg: &config.Config{
				BotGroups: []config.BotGroup{
					{ID: "g1", Name: "ETH Hedge", BotIDs: []string{"futures-bot", "spot-bot"}},
				},
			},
			botID: "futures-bot",
			want:  "ETH Hedge",
		},
		{
			name: "bot_not_in_any_group",
			cfg: &config.Config{
				BotGroups: []config.BotGroup{
					{ID: "g1", Name: "ETH Hedge", BotIDs: []string{"futures-bot", "spot-bot"}},
				},
			},
			botID: "other-bot",
			want:  "",
		},
		{
			name: "bot_in_second_group",
			cfg: &config.Config{
				BotGroups: []config.BotGroup{
					{ID: "g1", Name: "Group A", BotIDs: []string{"a1", "a2"}},
					{ID: "g2", Name: "Group B", BotIDs: []string{"b1", "b2"}},
				},
			},
			botID: "b2",
			want:  "Group B",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindGroupNameByBotID(tt.cfg, tt.botID)
			if got != tt.want {
				t.Errorf("FindGroupNameByBotID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetBotsReturnsBotResponseFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mock := &mockBotManagerForGetBotsTest{
		bots: []BotResponse{
			{
				BotID:          "bot-1",
				Name:           "Test Bot",
				CreatedAt:      "2026-03-13T10:00:00Z",
				HedgeGroupName:  "ETH Hedge",
				Direction:      "LONG",
			},
		},
	}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/bots", nil)

	getBots(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Bots []struct {
			BotID         string `json:"bot_id"`
			CreatedAt     string `json:"created_at"`
			HedgeGroupName string `json:"hedge_group_name"`
			Direction     string `json:"direction"`
		} `json:"bots"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(resp.Bots) != 1 {
		t.Fatalf("expected 1 bot, got %d", len(resp.Bots))
	}
	b := resp.Bots[0]
	if b.CreatedAt != "2026-03-13T10:00:00Z" {
		t.Errorf("created_at = %q, want 2026-03-13T10:00:00Z", b.CreatedAt)
	}
	if b.HedgeGroupName != "ETH Hedge" {
		t.Errorf("hedge_group_name = %q, want ETH Hedge", b.HedgeGroupName)
	}
	if b.Direction != "LONG" {
		t.Errorf("direction = %q, want LONG", b.Direction)
	}
}

type mockBotManagerForGetBotsTest struct {
	bots []BotResponse
}

func (m *mockBotManagerForGetBotsTest) ListBots() []BotResponse { return m.bots }
func (m *mockBotManagerForGetBotsTest) GetBot(botID string) (*BotDetailResponse, bool) {
	for _, b := range m.bots {
		if b.BotID == botID {
			return &BotDetailResponse{BotResponse: b}, true
		}
	}
	return nil, false
}
func (m *mockBotManagerForGetBotsTest) StartBot(ctx context.Context, cfg config.BotConfig) error { return nil }
func (m *mockBotManagerForGetBotsTest) StopBot(botID string) error                              { return nil }
func (m *mockBotManagerForGetBotsTest) EnableBot(botID string) error                           { return nil }

func TestUpdateBotStrategyRequest_StrategyConfigNested(t *testing.T) {
	body := `{"strategies":[{"type":"trend_following","weight":1,"config":{"fast_period":12,"slow_period":26}}]}`
	var req UpdateBotStrategyRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(req.Strategies) != 1 {
		t.Fatalf("expected 1 strategy, got %d", len(req.Strategies))
	}
	if req.Strategies[0].Config == nil {
		t.Fatal("expected strategies[0].config")
	}
	if fp, ok := req.Strategies[0].Config["fast_period"].(float64); !ok || fp != 12 {
		t.Fatalf("fast_period: %#v", req.Strategies[0].Config["fast_period"])
	}
}

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
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

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

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

	mock := &mockBotManagerForStartTest{}
	origProvider := botManagerProvider()
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
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

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

	fcm := NewFileConfigManager("")
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
	origProvider := botManagerProvider()
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
}

// mockBotManagerForGroupCreateTest 用於對沖組創建測試，可返回運行中/已停止的 Bot 列表
type mockBotManagerForGroupCreateTest struct {
	bots []BotResponse
}

func (m *mockBotManagerForGroupCreateTest) ListBots() []BotResponse { return m.bots }
func (m *mockBotManagerForGroupCreateTest) GetBot(botID string) (*BotDetailResponse, bool) {
	for _, b := range m.bots {
		if b.BotID == botID {
			return &BotDetailResponse{BotResponse: b}, true
		}
	}
	return nil, false
}
func (m *mockBotManagerForGroupCreateTest) StartBot(ctx context.Context, cfg config.BotConfig) error { return nil }
func (m *mockBotManagerForGroupCreateTest) StopBot(botID string) error                              { return nil }
func (m *mockBotManagerForGroupCreateTest) EnableBot(botID string) error                              { return nil }

// TestPostBotGroupCreateAllowsWhenOnlyStoppedBotExists 驗證：當同交易對僅有已停止的 Bot 時，對沖組創建應成功
func TestPostBotGroupCreateAllowsWhenOnlyStoppedBotExists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	futuresID := config.GenerateBotID("binance", "ETHUSDT", "futures")
	cfg := &config.Config{
		Bots: []config.BotConfig{
			{ID: futuresID, Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures", PriceInterval: 100, OrderQuantity: 100, MinOrderValue: 6, BuyWindowSize: 10, SellWindowSize: 10},
		},
		BotGroups: []config.BotGroup{},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	configManager = nil
	t.Cleanup(func() { configManager = &cfgmgr.ConfigManager{} })

	// 無運行中 Bot（ListBots 返回空或僅已停止的）
	mock := &mockBotManagerForGroupCreateTest{
		bots: []BotResponse{
			{BotID: futuresID, Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures", Running: false},
		},
	}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	body := `{
		"name": "test-hedge",
		"type": "futures_spot_hedge",
		"hedge_config": {"hedge_ratio": 0.5, "short_notional_ratio": 0.25, "hedge_trigger_layers": 3, "rebalance_interval": 3600},
		"futures_bot": {"exchange": "binance", "symbol": "ETHUSDT", "strategies": [{"type": "grid", "weight": 1}], "price_interval": 100, "order_quantity": 100},
		"spot_bot": {"exchange": "binance", "symbol": "ETHUSDT", "strategies": [{"type": "grid", "weight": 1}], "price_interval": 100, "order_quantity": 100}
	}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bot-groups", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBotGroupCreate(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when only stopped bot exists, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestPostBotGroupCreateRejectsWhenRunningBotExists 驗證：當同交易對有運行中的 Bot 時，對沖組創建應拒絕
func TestPostBotGroupCreateRejectsWhenRunningBotExists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	cfg := &config.Config{
		Bots:      []config.BotConfig{},
		BotGroups: []config.BotGroup{},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbol = "ETHUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	configManager = nil
	t.Cleanup(func() { configManager = &cfgmgr.ConfigManager{} })

	// 有運行中的 Bot
	mock := &mockBotManagerForGroupCreateTest{
		bots: []BotResponse{
			{BotID: "running-bot", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures", Running: true},
		},
	}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	body := `{
		"name": "test-hedge",
		"type": "futures_spot_hedge",
		"hedge_config": {"hedge_ratio": 0.5, "short_notional_ratio": 0.25, "hedge_trigger_layers": 3, "rebalance_interval": 3600},
		"futures_bot": {"exchange": "binance", "symbol": "ETHUSDT", "strategies": [{"type": "grid", "weight": 1}], "price_interval": 100, "order_quantity": 100},
		"spot_bot": {"exchange": "binance", "symbol": "ETHUSDT", "strategies": [{"type": "grid", "weight": 1}], "price_interval": 100, "order_quantity": 100}
	}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bot-groups", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBotGroupCreate(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when running bot exists, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["error_key"] != "error.bot_symbol_running" {
		t.Errorf("expected error_key=error.bot_symbol_running, got %v", resp["error_key"])
	}
}

// mockBotManagerForCreateTest 用於單 Bot 創建測試
type mockBotManagerForCreateTest struct {
	bots []BotResponse
}

func (m *mockBotManagerForCreateTest) ListBots() []BotResponse { return m.bots }
func (m *mockBotManagerForCreateTest) GetBot(botID string) (*BotDetailResponse, bool) {
	for _, b := range m.bots {
		if b.BotID == botID {
			return &BotDetailResponse{BotResponse: b}, true
		}
	}
	return nil, false
}
func (m *mockBotManagerForCreateTest) StartBot(ctx context.Context, cfg config.BotConfig) error { return nil }
func (m *mockBotManagerForCreateTest) StopBot(botID string) error                              { return nil }
func (m *mockBotManagerForCreateTest) EnableBot(botID string) error                            { return nil }

// TestPostBotCreateAllowsWhenOnlyStoppedBotExists 驗證：當同交易對僅有已停止的 Bot 時，單 Bot 創建應成功
func TestPostBotCreateAllowsWhenOnlyStoppedBotExists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	cfg := &config.Config{
		Bots: []config.BotConfig{
			{ID: "stopped-bot-id", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", PriceInterval: 100, OrderQuantity: 100, MinOrderValue: 6, BuyWindowSize: 10, SellWindowSize: 10},
		},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

	// 無運行中 Bot（ListBots 返回空或僅已停止的）
	mock := &mockBotManagerForCreateTest{
		bots: []BotResponse{
			{BotID: "stopped-bot-id", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", Running: false},
		},
	}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	body := `{
		"exchange": "binance",
		"symbol": "BTCUSDT",
		"market_type": "futures",
		"strategies": [{"type": "grid", "weight": 1}],
		"price_interval": 100,
		"order_quantity": 100,
		"min_order_value": 6
	}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bots/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBotCreate(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when only stopped bot exists, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	botID, _ := resp["bot_id"].(string)
	if botID == "" {
		t.Fatalf("response should contain bot_id")
	}
	// 驗證新建 Bot 的 created_at 已寫入
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatalf("GetLatestConfig: %v", err)
	}
	var found *config.BotConfig
	for i := range latest.Bots {
		if latest.Bots[i].ID == botID {
			found = &latest.Bots[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("created bot %q not found in config", botID)
	}
	if found.CreatedAt == "" {
		t.Error("created bot should have non-empty created_at")
	}
}

// TestPostBotCreateRejectsWhenRunningBotExists 驗證：當同交易對有運行中的 Bot 時，單 Bot 創建應拒絕
func TestPostBotCreateRejectsWhenRunningBotExists(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	cfg := &config.Config{
		Bots: []config.BotConfig{},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

	// 有運行中的 Bot
	mock := &mockBotManagerForCreateTest{
		bots: []BotResponse{
			{BotID: "running-bot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", Running: true},
		},
	}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	body := `{
		"exchange": "binance",
		"symbol": "BTCUSDT",
		"market_type": "futures",
		"strategies": [{"type": "grid", "weight": 1}],
		"price_interval": 100,
		"order_quantity": 100,
		"min_order_value": 6
	}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bots/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBotCreate(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when running bot exists, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["error_key"] != "error.bot_symbol_running" {
		t.Errorf("expected error_key=error.bot_symbol_running, got %v", resp["error_key"])
	}
}

// TestPostBotCreateFundingCarrySuccess 資金費套利：單策略 funding_carry、無同幣種配置時創建成功
func TestPostBotCreateFundingCarrySuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	cfg := &config.Config{Bots: []config.BotConfig{}}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

	mock := &mockBotManagerForCreateTest{bots: nil}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	body := `{
		"exchange": "binance",
		"symbol": "BTCUSDT",
		"market_type": "funding_carry",
		"strategies": [{"type": "funding_carry", "weight": 1, "config": {"min_funding_rate": 0.0004}}],
		"total_allocated_capital": 1000
	}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bots/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBotCreate(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	botID, _ := resp["bot_id"].(string)
	if botID == "" {
		t.Fatal("expected bot_id")
	}
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatalf("GetLatestConfig: %v", err)
	}
	var found *config.BotConfig
	for i := range latest.Bots {
		if latest.Bots[i].ID == botID {
			found = &latest.Bots[i]
			break
		}
	}
	if found == nil || found.GetMarketType() != config.MarketTypeFundingCarry {
		t.Fatalf("created bot missing or wrong market_type: %+v", found)
	}
	if len(found.Strategies) != 1 || found.Strategies[0].Type != "funding_carry" {
		t.Fatalf("strategies: %+v", found.Strategies)
	}
}

// TestPostBotCreateFundingCarryRejectsNonSingleStrategy 必須僅含 funding_carry 單策略
func TestPostBotCreateFundingCarryRejectsNonSingleStrategy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	cfg := &config.Config{Bots: []config.BotConfig{}}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

	mock := &mockBotManagerForCreateTest{}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	body := `{
		"exchange": "binance",
		"symbol": "BTCUSDT",
		"market_type": "funding_carry",
		"strategies": [{"type": "grid", "weight": 1}, {"type": "funding_carry", "weight": 0}]
	}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bots/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBotCreate(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["error_key"] != "error.funding_carry_single_strategy" {
		t.Errorf("error_key = %v, want error.funding_carry_single_strategy", resp["error_key"])
	}
}

// TestPostBotCreateFundingCarryConflictsWithExistingSpot 同幣種已有 spot Bot 時拒絕 funding_carry
func TestPostBotCreateFundingCarryConflictsWithExistingSpot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	cfg := &config.Config{
		Bots: []config.BotConfig{
			{
				ID: "spot-1", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot",
				PriceInterval: 100, OrderQuantity: 100, MinOrderValue: 6, BuyWindowSize: 10, SellWindowSize: 10,
			},
		},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = origCM })

	mock := &mockBotManagerForCreateTest{}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	body := `{
		"exchange": "binance",
		"symbol": "BTCUSDT",
		"market_type": "funding_carry",
		"strategies": [{"type": "funding_carry", "weight": 1}]
	}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bots/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBotCreate(c)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp["error_key"] != "error.bot_symbol_market_conflict" {
		t.Errorf("error_key = %v, want error.bot_symbol_market_conflict", resp["error_key"])
	}
}

// TestPostBotGroupCreateWorksWithFileConfigManagerOnly 驗證：當 configManager 為 nil 但 fileConfigManager 已設置時，
// postBotGroupCreate 仍可成功創建對沖組（修復 503 Service Unavailable）
func TestPostBotGroupCreateWorksWithFileConfigManagerOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	cfg := &config.Config{
		Bots:      []config.BotConfig{},
		BotGroups: []config.BotGroup{},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "k", SecretKey: "s", FeeRate: 0.0002},
	}

	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	origFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })
	origCM := configManager
	configManager = nil // 模擬 configManager 未初始化
	t.Cleanup(func() { configManager = origCM })

	mock := &mockBotManagerForGroupConsistencyTest{}
	origProvider := botManagerProvider()
	RegisterBotManagerProvider(mock)
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })

	body := `{
		"name": "test-hedge",
		"type": "futures_spot_hedge",
		"hedge_config": {"hedge_ratio": 0.5, "short_notional_ratio": 0.25, "hedge_trigger_layers": 3, "rebalance_interval": 3600},
		"futures_bot": {"exchange": "binance", "symbol": "ETHUSDT", "strategies": [{"type": "grid", "weight": 1}], "price_interval": 100, "order_quantity": 100},
		"spot_bot": {"exchange": "binance", "symbol": "ETHUSDT", "strategies": [{"type": "grid", "weight": 1}], "price_interval": 100, "order_quantity": 100}
	}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bot-groups", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	postBotGroupCreate(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := resp["group_id"]; !ok {
		t.Fatalf("response should contain group_id")
	}
}

func TestGetBotGroupByIDIncludesConsistency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

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
	fcm := NewFileConfigManager("")
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
	origProvider := botManagerProvider()
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
