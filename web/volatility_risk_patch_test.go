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

func TestVolatilityRiskPatchPersistsAndReloads(t *testing.T) {
	t.Cleanup(setupTestPrimaryAppConfigStorage(t))
	oldFCM, oldProvider := fileConfigManager, botExtendedProvider
	t.Cleanup(func() { fileConfigManager, botExtendedProvider = oldFCM, oldProvider })
	fileConfigManager = NewFileConfigManager("")
	if err := fileConfigManager.UpdateConfig(newRiskPersistenceConfig()); err != nil {
		t.Fatal(err)
	}
	bot := &runtimeRiskTestBot{rc: config.BotRiskControl{VolatilityPauseConfig: config.VolatilityPauseConfig{PauseOnExtremeVolatility: true, TrendCheckPeriod: 15}}}
	botExtendedProvider = protectiveResumeProvider{bot: bot}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "risk-test"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"enabled":true,"volatility_pause_enabled":true,"volatility_pause_config":{"auto_resume_on_normal":true,"resume_threshold":1.5}}`))
	c.Request.Header.Set("Content-Type", "application/json")
	updateBotRiskControl(c)
	if w.Code != 200 {
		t.Fatalf("code=%d response=%s", w.Code, w.Body.String())
	}
	var response config.BotRiskControl
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.VolatilityPauseEnabled || !response.VolatilityPauseConfig.PauseOnExtremeVolatility || response.VolatilityPauseConfig.ResumeThreshold != 1.5 || response.VolatilityPauseConfig.TrendCheckPeriod != 15 {
		t.Fatalf("nested fields lost: %+v", response)
	}
	loaded, err := loadConfigFromPrimaryDB()
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Bots[0].OpenPositionControl.BotRiskControl; got == nil || got.VolatilityPauseConfig != response.VolatilityPauseConfig || !got.VolatilityPauseEnabled {
		t.Fatalf("restart config differs: %+v", got)
	}
}

func TestVolatilityRiskPatchRejectsInvalidThresholds(t *testing.T) {
	for _, payload := range []string{`{"volatility_pause_config":{"resume_threshold":-1}}`, `{"volatility_pause_config":{"trend_check_period":1441}}`, `{"volatility_pause_config":{"trend_up_threshold":-1}}`} {
		var req BotRiskControlRequest
		if err := json.Unmarshal([]byte(payload), &req); err != nil {
			t.Fatal(err)
		}
		if validateBotRiskControlRequest(&req) == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
}
