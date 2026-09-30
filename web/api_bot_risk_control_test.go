package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/cfgmgr"
	"quantmesh/config"
)

func setupPausePersistenceConfig(t *testing.T, botID string, paused bool) {
	t.Helper()
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))
	cfg := &config.Config{}
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "test-key", SecretKey: "test-secret", FeeRate: 0.0002},
	}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.MinOrderValue = 6
	cfg.Bots = []config.BotConfig{{
		ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures",
		PriceInterval: 100, OrderQuantity: 100,
		OpenPositionControl: config.OpenPositionControl{
			PauseOpening:   paused,
			BotRiskControl: &config.BotRiskControl{PauseOpening: paused, PauseOpeningReason: "manual", AutoResumeAfter: 30},
		},
	}}
	fcm := NewFileConfigManager("")
	if err := fcm.UpdateConfig(cfg); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}
	previousFCM := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(previousFCM) })
	previousCM := configManager
	configManager = &cfgmgr.ConfigManager{}
	t.Cleanup(func() { configManager = previousCM })
}

func persistedBotPause(t *testing.T, botID string) (bool, *config.BotRiskControl) {
	t.Helper()
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Bots {
		if cfg.Bots[i].ID == botID {
			return cfg.Bots[i].OpenPositionControl.PauseOpening, cfg.Bots[i].OpenPositionControl.BotRiskControl
		}
	}
	t.Fatalf("Bot %q missing from persisted config", botID)
	return false, nil
}

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

func TestUpdateBotRiskControlRejectsUnsafeValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{
			name: "negative_position_value",
			body: map[string]interface{}{"max_position_value": -1},
		},
		{
			name: "stop_loss_over_100_percent",
			body: map[string]interface{}{"stop_loss_ratio": 1.5},
		},
		{
			name: "negative_open_orders",
			body: map[string]interface{}{"max_open_orders": -1},
		},
		{
			name: "unsafe_grid_risk_ratio",
			body: map[string]interface{}{
				"grid_risk_control": map[string]interface{}{
					"enabled":                    true,
					"stop_loss_ratio":            -0.1,
					"take_profit_trigger_ratio":  0.1,
					"trailing_take_profit_ratio": 0.03,
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.body)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/v2/bots/bot-1/risk-control", bytes.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: "bot-1"}}

			updateBotRiskControl(c)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

type pauseRequestTestBot struct {
	BotExtended
	safePauses     int
	ordinaryPauses int
	timedPauses    int
	riskControl    *config.BotRiskControl
}

func (b *pauseRequestTestBot) GetBotRiskControl() *config.BotRiskControl {
	if b.riskControl == nil {
		return &config.BotRiskControl{AutoResumeAfter: 30}
	}
	copy := *b.riskControl
	return &copy
}

func (b *pauseRequestTestBot) PauseOpeningWithoutAutoResume(string)   { b.safePauses++ }
func (b *pauseRequestTestBot) PauseOpening(string)                    { b.ordinaryPauses++ }
func (b *pauseRequestTestBot) PauseOpeningWithAutoResume(string, int) { b.timedPauses++ }
func (b *pauseRequestTestBot) SetBotRiskControl(rc *config.BotRiskControl) error {
	copy := *rc
	b.riskControl = &copy
	return nil
}

func TestPauseBotOpeningOnlyUsesExplicitNonNegativeAutoResume(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPausePersistenceConfig(t, "test", false)
	original := botExtendedProvider
	t.Cleanup(func() { botExtendedProvider = original })

	for _, tc := range []struct {
		name            string
		body            string
		wantCode        int
		wantPauses      int
		wantTimed       int
		wantResumeAfter int
	}{
		{name: "no_duration_does_not_inherit_saved_duration", body: `{"reason":"operator pause"}`, wantCode: http.StatusOK, wantPauses: 1, wantResumeAfter: 30},
		{name: "explicit_duration_enables_timer", body: `{"reason":"short pause","auto_resume_sec":45}`, wantCode: http.StatusOK, wantTimed: 1, wantResumeAfter: 45},
		{name: "zero_duration_disables_saved_timer", body: `{"reason":"indefinite pause","auto_resume_sec":0}`, wantCode: http.StatusOK, wantPauses: 1, wantResumeAfter: 0},
		{name: "negative_duration_rejected", body: `{"auto_resume_sec":-1}`, wantCode: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := &pauseRequestTestBot{}
			botExtendedProvider = protectiveResumeProvider{bot: bot}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v2/bots/test/pause", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Params = gin.Params{{Key: "id", Value: "test"}}

			pauseBotOpening(c)
			if w.Code != tc.wantCode {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
			if bot.safePauses != tc.wantPauses || bot.ordinaryPauses != 0 || bot.timedPauses != tc.wantTimed {
				t.Fatalf("pause calls: safe=%d ordinary=%d timed=%d", bot.safePauses, bot.ordinaryPauses, bot.timedPauses)
			}
			if tc.wantCode == http.StatusOK {
				paused, rc := persistedBotPause(t, "test")
				if !paused || rc == nil || !rc.PauseOpening {
					t.Fatalf("pause state not persisted: top=%v risk=%+v", paused, rc)
				}
				if rc.AutoResumeAfter != tc.wantResumeAfter {
					t.Fatalf("persisted AutoResumeAfter=%d want %d", rc.AutoResumeAfter, tc.wantResumeAfter)
				}
			}
		})
	}
}

func TestPauseBotOpeningPersistenceFailureStaysPausedWithoutTimer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousProvider, previousFCM := botExtendedProvider, fileConfigManager
	t.Cleanup(func() {
		botExtendedProvider = previousProvider
		SetFileConfigManager(previousFCM)
	})
	SetFileConfigManager(nil)
	bot := &pauseRequestTestBot{}
	botExtendedProvider = protectiveResumeProvider{bot: bot}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v2/bots/test/pause", strings.NewReader(`{"auto_resume_sec":45}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "test"}}

	pauseBotOpening(c)
	if w.Code != http.StatusInternalServerError || bot.safePauses != 1 || bot.timedPauses != 0 {
		t.Fatalf("persistence failure must keep a non-timed runtime pause: status=%d safe=%d timed=%d body=%s", w.Code, bot.safePauses, bot.timedPauses, w.Body.String())
	}
}

func TestUpdateBotRiskControlGridRiskPatchPreservesOmittedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))

	botID := "grid-risk-patch-bot"
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
		{
			ID:            botID,
			Exchange:      "binance",
			Symbol:        "BTCUSDT",
			MarketType:    "futures",
			PriceInterval: 100,
			OrderQuantity: 100,
			GridRiskControl: config.GridRiskControl{
				Enabled:                 true,
				StopLossRatio:           0.2,
				TakeProfitTriggerRatio:  0.08,
				TrailingTakeProfitRatio: 0.03,
				MaxGridLayers:           12,
			},
		},
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

	body := map[string]interface{}{
		"grid_risk_control": map[string]interface{}{
			"enabled": false,
		},
	}
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
	for i := range latest.Bots {
		if latest.Bots[i].ID != botID {
			continue
		}
		grc := latest.Bots[i].GridRiskControl
		if grc.Enabled {
			t.Fatal("expected enabled=false from patch")
		}
		if grc.StopLossRatio != 0.2 || grc.TakeProfitTriggerRatio != 0.08 || grc.TrailingTakeProfitRatio != 0.03 || grc.MaxGridLayers != 12 {
			t.Fatalf("partial grid_risk_control patch should preserve omitted fields, got %+v", grc)
		}
		return
	}
	t.Fatal("bot not in latest config")
}

type mockBotExtendedProviderNotRunning struct{}

func (m *mockBotExtendedProviderNotRunning) GetBot(botID string) (BotExtended, bool) {
	return nil, false
}
