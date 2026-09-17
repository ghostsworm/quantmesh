package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/cfgmgr"
	"quantmesh/config"
)

func setupBotCreateTestEnv(t *testing.T) {
	t.Helper()
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

	origProvider := botManagerProvider()
	RegisterBotManagerProvider(&mockBotManagerForCreateTest{})
	t.Cleanup(func() { RegisterBotManagerProvider(origProvider) })
}

func doBotCreate(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/bots/create", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	postBotCreate(c)
	return w
}

const botCreateBodyPrefix = `{
	"exchange": "binance",
	"symbol": "BTCUSDT",
	"market_type": "futures",
	"strategies": [{"type": "grid", "weight": 1}],
	"price_interval": 100,
	"order_quantity": 100,
	"min_order_value": 6,`

func TestPostBotCreatePersistsTradingOverrides(t *testing.T) {
	setupBotCreateTestEnv(t)

	w := doBotCreate(t, botCreateBodyPrefix+`
	"trading_overrides": {
		"regime_filter": {"enabled": true, "kline_interval": "4h"},
		"inventory_skew": {"enabled": true, "strength": 0.3},
		"funding_rate": {"pre_settlement_pause_minutes": 0}
	}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	botID, _ := resp["bot_id"].(string)
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	var found *config.BotConfig
	for i := range latest.Bots {
		if latest.Bots[i].ID == botID {
			found = &latest.Bots[i]
		}
	}
	if found == nil || found.TradingOverrides == nil {
		t.Fatalf("bot or overrides not persisted: %+v", found)
	}
	o := found.TradingOverrides
	if o.RegimeFilter == nil || o.RegimeFilter.KlineInterval != "4h" || o.InventorySkew == nil ||
		o.InventorySkew.Strength != 0.3 || o.FundingRate == nil || o.FundingRate.PreSettlementPauseMinutes == nil ||
		o.FeeAwareSpread != nil {
		t.Fatalf("unexpected persisted overrides: %+v", o)
	}
}

func TestPostBotCreateRejectsInvalidTradingOverrides(t *testing.T) {
	setupBotCreateTestEnv(t)

	w := doBotCreate(t, botCreateBodyPrefix+`
	"trading_overrides": {"adaptive_interval": {"enabled": true, "min_interval": 50, "max_interval": 10}}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(latest.Bots) != 0 {
		t.Fatalf("invalid bot must not be saved, got %d bots", len(latest.Bots))
	}
}

func TestPutBotConfigFileRejectsInvalidTradingOverrides(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mgr, err := config.NewBotConfigManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	orig := botConfigManager
	botConfigManager = mgr
	t.Cleanup(func() { botConfigManager = orig })

	body := `{"bot_id":"b1","exchange":"binance","symbol":"BTCUSDT","market_type":"futures",
		"trading_overrides":{"inventory_skew":{"enabled":true,"strength":3}}}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "b1"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/api/bots/b1/config-file", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	putBotConfigFile(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if mgr.BotConfigExists("b1") {
		t.Fatal("invalid config must not be saved")
	}
}
