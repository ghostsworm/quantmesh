package web

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/position"
)

type openingTestProvider struct {
	SymbolManagerProvider
	runtime interface{}
}

func (p openingTestProvider) GetByBotID(id string) (interface{}, bool) {
	return p.runtime, id == "risk-test" && p.runtime != nil
}
func (p openingTestProvider) GetEx(string, string, string) (interface{}, bool) {
	panic("explicit bot must never fall back to symbol lookup")
}

func TestOpeningSaveRuntimeUnavailableIsRejectedBeforePersistence(t *testing.T) {
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))
	oldFCM, oldProvider := fileConfigManager, symbolManagerProvider
	t.Cleanup(func() { fileConfigManager, symbolManagerProvider = oldFCM, oldProvider })
	fileConfigManager = NewFileConfigManager("")
	if err := fileConfigManager.UpdateConfig(newRiskPersistenceConfig()); err != nil {
		t.Fatal(err)
	}
	rt := &struct {
		Config            config.SymbolConfig
		OpeningController *position.OpeningController
	}{
		Config: config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
	}
	symbolManagerProvider = openingTestProvider{runtime: rt}
	w := openingSaveRequest(`{"max_position_value":200,"max_position_layers":3}`, "&bot_id=risk-test")
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"persisted":false`) || !strings.Contains(w.Body.String(), `"applied":false`) {
		t.Fatalf("false runtime success: %d %s", w.Code, w.Body)
	}
	if cfg, err := loadConfigFromPrimaryDB(); err != nil || cfg.Bots[0].OpenPositionControl.MaxPositionValue != 0 {
		t.Fatalf("unapplied control leaked to persisted config: cfg=%+v err=%v", cfg, err)
	}
	if rt.Config.OpenPositionControl.MaxPositionValue != 0 {
		t.Fatal("mutated shared runtime config")
	}
}

func TestOpeningSaveAppliesThroughSpecializedRuntimeController(t *testing.T) {
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))
	oldFCM, oldProvider := fileConfigManager, symbolManagerProvider
	t.Cleanup(func() { fileConfigManager, symbolManagerProvider = oldFCM, oldProvider })
	fileConfigManager = NewFileConfigManager("")
	if err := fileConfigManager.UpdateConfig(newRiskPersistenceConfig()); err != nil {
		t.Fatal(err)
	}
	var applied config.OpenPositionControl
	rt := &struct {
		Config            config.SymbolConfig
		OpeningController *position.OpeningController
		UpdateOpenControl func(config.OpenPositionControl) error
	}{
		Config: config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
		UpdateOpenControl: func(control config.OpenPositionControl) error {
			applied = control
			return nil
		},
	}
	symbolManagerProvider = openingTestProvider{runtime: rt}
	w := openingSaveRequest(`{"max_position_value":200,"max_position_layers":3}`, "&bot_id=risk-test")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"applied":true`) || applied.MaxPositionValue != 200 || applied.MaxPositionLayers != 3 {
		t.Fatalf("specialized runtime control not applied: response=%d %s applied=%+v", w.Code, w.Body, applied)
	}
}

func openingSaveRequest(body, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/?exchange=binance&symbol=BTCUSDT&market_type=futures"+query, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	putOpeningControlConfig(c)
	return w
}

func TestOpeningTargetRequiresExactUnambiguousOwnership(t *testing.T) {
	cfg := newRiskPersistenceConfig()
	for _, id := range []string{"missing", "RISK-TEST"} {
		if _, _, err := openingControlTarget(cfg, id, "binance", "BTCUSDT", "futures"); err == nil {
			t.Fatal("unknown ID fell back")
		}
	}
	if _, _, err := openingControlTarget(cfg, "risk-test", "binance", "ETHUSDT", "futures"); err == nil {
		t.Fatal("mismatched symbol accepted")
	}
	other := cfg.Bots[0]
	other.ID = "second"
	cfg.Bots = append(cfg.Bots, other)
	if _, _, err := openingControlTarget(cfg, "", "binance", "BTCUSDT", "futures"); err == nil {
		t.Fatal("ambiguous symbol accepted")
	}
	if _, id, err := openingControlTarget(cfg, "second", "binance", "BTCUSDT", "futures"); err != nil || id != "second" {
		t.Fatal("exact target rejected")
	}
}

func TestOpeningSaveFailurePreservesPublishedConfiguration(t *testing.T) {
	oldFCM, oldStorage := fileConfigManager, primaryStorageForAppConfig
	t.Cleanup(func() { fileConfigManager, primaryStorageForAppConfig = oldFCM, oldStorage })
	cfg := newRiskPersistenceConfig()
	cfg.Bots[0].OpenPositionControl.MaxPositionValue = 100
	fileConfigManager = &FileConfigManager{currentConfig: cfg}
	primaryStorageForAppConfig = nil
	w := openingSaveRequest(`{"max_position_value":200,"max_position_layers":3}`, "&bot_id=risk-test")
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), `"persisted":false`) {
		t.Fatalf("false success: %d %s", w.Code, w.Body)
	}
	if fileConfigManager.currentConfig != cfg || cfg.Bots[0].OpenPositionControl.MaxPositionValue != 100 {
		t.Fatal("failed save mutated published state")
	}
}

func TestOpeningSavePersistsSelectedBotAndPreservesOtherControls(t *testing.T) {
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))
	oldFCM, oldProvider := fileConfigManager, symbolManagerProvider
	t.Cleanup(func() { fileConfigManager, symbolManagerProvider = oldFCM, oldProvider })
	symbolManagerProvider = nil
	fileConfigManager = NewFileConfigManager("")
	cfg := newRiskPersistenceConfig()
	other := cfg.Bots[0]
	other.ID = "second"
	cfg.Bots = append(cfg.Bots, other)
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	w := openingSaveRequest(`{"max_position_value":250,"max_position_layers":4,"periodic_rule":{"enabled":true,"open_duration_min":20,"close_duration_min":10}}`, "&bot_id=second")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"persisted":true`) {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	saved, err := loadConfigFromPrimaryDB()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Bots[0].OpenPositionControl.MaxPositionValue != 0 || saved.Bots[1].OpenPositionControl.MaxPositionValue != 250 || saved.Bots[1].OpenPositionControl.BotRiskControl.MaxPositionValue != 100 {
		t.Fatal("wrong target or overwritten unrelated controls")
	}
	if cfg.Bots[1].OpenPositionControl.PeriodicRule != nil {
		t.Fatal("mutated old snapshot")
	}
}

func TestPersistOpeningPauseIsScopedAndDurable(t *testing.T) {
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))
	oldFCM := fileConfigManager
	t.Cleanup(func() { fileConfigManager = oldFCM })
	fileConfigManager = NewFileConfigManager("")
	cfg := newRiskPersistenceConfig()
	other := cfg.Bots[0]
	other.ID = "second"
	cfg.Bots = append(cfg.Bots, other)
	cfg.Bots[1].OpenPositionControl.BotRiskControl.PauseOpening = false
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := persistOpeningPause("second", "binance", "BTCUSDT", "futures", true); err != nil {
		t.Fatal(err)
	}
	saved, err := loadConfigFromPrimaryDB()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Bots[0].OpenPositionControl.PauseOpening || saved.Bots[0].OpenPositionControl.BotRiskControl.PauseOpening {
		t.Fatal("pause escaped selected Bot")
	}
	control := saved.Bots[1].OpenPositionControl
	if !control.PauseOpening || control.BotRiskControl == nil || !control.BotRiskControl.PauseOpening || control.BotRiskControl.PauseOpeningReason != "manual" {
		t.Fatalf("manual pause was not durably recorded: %+v", control)
	}
	if err := persistOpeningPause("second", "binance", "BTCUSDT", "futures", false); err != nil {
		t.Fatal(err)
	}
	saved, err = loadConfigFromPrimaryDB()
	if err != nil {
		t.Fatal(err)
	}
	control = saved.Bots[1].OpenPositionControl
	if control.PauseOpening || control.BotRiskControl.PauseOpening || control.BotRiskControl.PauseOpeningReason != "" {
		t.Fatalf("resume state was not durably recorded: %+v", control)
	}
}

func TestPersistOpeningPauseFailureDoesNotPublish(t *testing.T) {
	oldFCM, oldStorage := fileConfigManager, primaryStorageForAppConfig
	t.Cleanup(func() { fileConfigManager, primaryStorageForAppConfig = oldFCM, oldStorage })
	cfg := newRiskPersistenceConfig()
	fileConfigManager = &FileConfigManager{currentConfig: cfg}
	primaryStorageForAppConfig = nil
	if err := persistOpeningPause("risk-test", "binance", "BTCUSDT", "futures", true); err == nil {
		t.Fatal("expected persistence failure")
	}
	if fileConfigManager.currentConfig != cfg || cfg.Bots[0].OpenPositionControl.PauseOpening {
		t.Fatal("failed persistence changed published configuration")
	}
}

func TestOpeningSaveRejectsInvalidInputsBeforePersistence(t *testing.T) {
	for _, body := range []string{`{}`, `{"max_position_value":100}`, `{"max_position_value":-1,"max_position_layers":0}`, `{"max_position_value":1,"max_position_layers":1.5}`, `{"max_position_value":1,"max_position_layers":1,"periodic_rule":{"enabled":true,"open_duration_min":0,"close_duration_min":1}}`} {
		if w := openingSaveRequest(body, "&bot_id=risk-test"); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid input accepted: %d", w.Code)
		}
	}
	for _, req := range []config.OpenPositionControl{
		{MaxPositionValue: math.Inf(1)},
		{PeriodicRule: &config.PeriodicRule{Enabled: true, OpenDurationMin: math.MaxInt, CloseDurationMin: 1}},
		{ScheduleRules: []config.ScheduleRule{{Action: "pause", Time: "24:00"}}},
		{ScheduleRules: []config.ScheduleRule{{Action: "resume", Time: "12:00", Weekdays: []int{7}}}},
	} {
		if validateOpeningControl(req) == nil {
			t.Fatal("invalid control accepted")
		}
	}
}

func TestOpeningLimitStatusReportsActuallyEnforcedBotOverride(t *testing.T) {
	control := config.OpenPositionControl{
		MaxPositionValue:  900,
		MaxPositionLayers: 9,
		BotRiskControl:    &config.BotRiskControl{Enabled: true, MaxPositionQuantity: 2.5, MaxPositionValue: 250, MaxPositionLayers: 4},
	}
	status := openingLimitStatus(control)
	if status["bot_risk_control_overrides_limits"] != true || status["effective_max_position_quantity"] != 2.5 ||
		status["effective_max_position_value"] != float64(250) || status["effective_max_position_layers"] != 4 {
		t.Fatalf("incorrect effective limits: %#v", status)
	}
	if status["max_position_value"] != float64(900) || status["max_position_layers"] != 9 {
		t.Fatalf("fallback limits were lost: %#v", status)
	}
	control.BotRiskControl.Enabled = false
	status = openingLimitStatus(control)
	if status["bot_risk_control_overrides_limits"] != false || status["effective_max_position_value"] != float64(900) || status["effective_max_position_layers"] != 9 {
		t.Fatalf("disabled Bot override remained effective: %#v", status)
	}
}
