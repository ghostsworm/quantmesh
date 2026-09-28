package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

type runtimeRiskTestBot struct {
	BotExtended
	rc           config.BotRiskControl
	grid         config.GridRiskControl
	capitalLimit float64
}

func (b *runtimeRiskTestBot) GetBotRiskControl() *config.BotRiskControl  { return &b.rc }
func (b *runtimeRiskTestBot) GetGridRiskControl() config.GridRiskControl { return b.grid }
func (b *runtimeRiskTestBot) SetRiskControls(rc *config.BotRiskControl, grid config.GridRiskControl) error {
	b.rc, b.grid = *rc, grid
	if b.capitalLimit > 0 && b.rc.MaxPositionValue > b.capitalLimit {
		b.rc.MaxPositionValue = b.capitalLimit
	}
	return nil
}

func runtimeRiskRequest(t *testing.T) (int, map[string]interface{}) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "risk-test"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"enabled":true,"max_position_value":200,"grid_risk_control":{"max_grid_layers":4}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	updateBotRiskControl(c)
	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return w.Code, response
}

func newRiskPersistenceConfig() *config.Config {
	cfg := &config.Config{}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "test-key", SecretKey: "test-secret", FeeRate: 0.0002}}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Bots = []config.BotConfig{{ID: "risk-test", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", PriceInterval: 100, OrderQuantity: 100,
		OpenPositionControl: config.OpenPositionControl{BotRiskControl: &config.BotRiskControl{MaxPositionValue: 100}}}}
	return cfg
}

func TestRunningRiskControlPersistenceFailuresAreTruthful(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldFCM, oldProvider, oldStorage := fileConfigManager, botExtendedProvider, primaryStorageForAppConfig
	t.Cleanup(func() {
		fileConfigManager, botExtendedProvider, primaryStorageForAppConfig = oldFCM, oldProvider, oldStorage
	})
	for _, scenario := range []string{"missing_manager", "missing_bot", "write_failure"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := newRiskPersistenceConfig()
			fileConfigManager = &FileConfigManager{currentConfig: cfg}
			primaryStorageForAppConfig = nil
			if scenario == "missing_manager" {
				fileConfigManager = nil
			}
			if scenario == "missing_bot" {
				cfg.Bots[0].ID = "different"
			}
			bot := &runtimeRiskTestBot{}
			botExtendedProvider = protectiveResumeProvider{bot: bot}
			code, response := runtimeRiskRequest(t)
			if code != http.StatusInternalServerError || response["applied"] != true || response["persisted"] != false {
				t.Fatalf("false persistence claim: code=%d response=%v", code, response)
			}
			if bot.rc.MaxPositionValue != 200 || bot.grid.MaxGridLayers != 4 {
				t.Fatal("runtime update was not applied")
			}
			if cfg.Bots[0].OpenPositionControl.BotRiskControl.MaxPositionValue != 100 {
				t.Fatal("failed persistence mutated published configuration")
			}
		})
	}
}

func TestRunningRiskControlPersistsBothSectionsInDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))
	oldFCM, oldProvider := fileConfigManager, botExtendedProvider
	t.Cleanup(func() { fileConfigManager, botExtendedProvider = oldFCM, oldProvider })
	fileConfigManager = NewFileConfigManager("")
	if err := fileConfigManager.UpdateConfig(newRiskPersistenceConfig()); err != nil {
		t.Fatal(err)
	}
	bot := &runtimeRiskTestBot{rc: config.BotRiskControl{MaxPositionQuantity: 7}, grid: config.GridRiskControl{StopLossRatio: 0.1}}
	botExtendedProvider = protectiveResumeProvider{bot: bot}
	code, response := runtimeRiskRequest(t)
	if code != http.StatusOK || response["applied"] != true || response["persisted"] != true {
		t.Fatalf("%d %v", code, response)
	}
	persisted, err := loadConfigFromPrimaryDB()
	if err != nil {
		t.Fatal(err)
	}
	b := persisted.Bots[0]
	if b.OpenPositionControl.BotRiskControl.MaxPositionValue != 200 || b.OpenPositionControl.BotRiskControl.MaxPositionQuantity != 7 || b.GridRiskControl.MaxGridLayers != 4 || b.GridRiskControl.StopLossRatio != 0.1 {
		t.Fatal("database snapshot lost a section or omitted field")
	}
}

func TestRunningRiskControlPersistsEffectiveClampedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))
	oldFCM, oldProvider := fileConfigManager, botExtendedProvider
	t.Cleanup(func() { fileConfigManager, botExtendedProvider = oldFCM, oldProvider })
	fileConfigManager = NewFileConfigManager("")
	if err := fileConfigManager.UpdateConfig(newRiskPersistenceConfig()); err != nil {
		t.Fatal(err)
	}
	bot := &runtimeRiskTestBot{capitalLimit: 100}
	botExtendedProvider = protectiveResumeProvider{bot: bot}
	code, response := runtimeRiskRequest(t)
	if code != http.StatusOK || response["max_position_value"] != float64(100) {
		t.Fatalf("API did not report effective clamped limit: status=%d response=%v", code, response)
	}
	persisted, err := loadConfigFromPrimaryDB()
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.Bots[0].OpenPositionControl.BotRiskControl.MaxPositionValue; got != 100 {
		t.Fatalf("persisted risk limit = %v, want effective cap 100", got)
	}
}
