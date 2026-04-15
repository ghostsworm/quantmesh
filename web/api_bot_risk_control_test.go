package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/cfgmgr"
	"quantmesh/config"
)

// TestUpdateBotRiskControlWhenStopped_FromMainSnapshot Bot 未運行時 PUT 風控應寫入配置而非 404
func TestUpdateBotRiskControlWhenStopped_FromMainSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	botID := "29fd3f14-1e3f-4a92-904a-a040610ed41d"
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
		{ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", PriceInterval: 100, OrderQuantity: 100},
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

	origExt := botExtendedProvider
	RegisterBotExtendedProvider(&mockBotExtendedProviderNotRunning{})
	t.Cleanup(func() { RegisterBotExtendedProvider(origExt) })

	body := map[string]interface{}{"enabled": true}
	raw, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/v2/bots/"+botID+"/risk-control", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: botID}}

	updateBotRiskControl(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for i := range latest.Bots {
		id := latest.Bots[i].ID
		if id == botID {
			found = true
			brc := latest.Bots[i].OpenPositionControl.BotRiskControl
			if brc == nil || !brc.Enabled {
				t.Fatalf("expected persisted enabled=true, got %+v", brc)
			}
			break
		}
	}
	if !found {
		t.Fatal("bot not in latest config")
	}
}

type mockBotExtendedProviderNotRunning struct{}

func (m *mockBotExtendedProviderNotRunning) GetBot(botID string) (BotExtended, bool) {
	return nil, false
}
