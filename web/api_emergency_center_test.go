package web

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"quantmesh/config"
	"quantmesh/risk"

	"github.com/gin-gonic/gin"
)

type emergencyHandlerNoBots struct{}

func (emergencyHandlerNoBots) GetAllBots() []risk.BotController { return nil }

// emergencyHandlerBot 最小 Bot 實現，PauseOpening 調用會寫入 paused
type emergencyHandlerBot struct{ paused chan struct{} }

func (b emergencyHandlerBot) PauseOpening(string) {
	select {
	case b.paused <- struct{}{}:
	default:
	}
}
func (emergencyHandlerBot) ResumeOpening()             {}
func (emergencyHandlerBot) CancelAllOpenOrders() error { return nil }
func (emergencyHandlerBot) CloseAllPositions(context.Context, string, int) error {
	return nil
}
func (emergencyHandlerBot) GetPositionSummary() (float64, float64, error) { return 0, 0, nil }

func postEmergencyExecute(body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/emergency/execute", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	executeEmergencyScenario(c)
	return w
}

func TestExecuteEmergencyScenarioRequiresConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prev := globalEmergencyCenter
	defer SetEmergencyCenter(prev)

	SetEmergencyCenter(risk.NewEmergencyCenter(&config.EmergencyCenterConfig{Enabled: true, RequireConfirmation: true}, nil, emergencyHandlerNoBots{}))

	if w := postEmergencyExecute(`{"scenario":"network_issue"}`); w.Code != http.StatusPreconditionRequired {
		t.Fatalf("未確認應返回 428, got %d: %s", w.Code, w.Body.String())
	}
	if w := postEmergencyExecute(`{"scenario":"network_issue","confirm":true,"confirm_scenario":"market_crash"}`); w.Code != http.StatusPreconditionRequired {
		t.Fatalf("確認場景不匹配應返回 428, got %d", w.Code)
	}
	if w := postEmergencyExecute(`{"scenario":"network_issue","confirm":true,"confirm_scenario":"network_issue"}`); w.Code != http.StatusOK {
		t.Fatalf("正確確認應執行, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecuteEmergencyScenarioWithoutConfirmationConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prev := globalEmergencyCenter
	defer SetEmergencyCenter(prev)

	bot := emergencyHandlerBot{paused: make(chan struct{}, 1)}
	SetEmergencyCenter(risk.NewEmergencyCenter(&config.EmergencyCenterConfig{Enabled: true}, nil, &staticBotProvider{bots: []risk.BotController{bot}}))

	if w := postEmergencyExecute(`{"scenario":"network_issue"}`); w.Code != http.StatusOK {
		t.Fatalf("未啟用 require_confirmation 時應直接執行, got %d: %s", w.Code, w.Body.String())
	}
}

type staticBotProvider struct{ bots []risk.BotController }

func (p *staticBotProvider) GetAllBots() []risk.BotController { return p.bots }
