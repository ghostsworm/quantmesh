package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/risk"

	"github.com/gin-gonic/gin"
)

type protectiveResumeBot struct {
	BotExtended
	err            error
	paused         bool
	manualCalls    int
	automaticCalls int
	riskControl    *config.BotRiskControl
}

func (b *protectiveResumeBot) ResumeOpeningManually() error {
	b.manualCalls++
	return b.err
}

func (b *protectiveResumeBot) ResumeOpening()                                       { b.automaticCalls++ }
func (b *protectiveResumeBot) PauseOpening(string)                                  {}
func (b *protectiveResumeBot) CancelAllOpenOrders() error                           { return nil }
func (b *protectiveResumeBot) CloseAllPositions(context.Context, string, int) error { return nil }
func (b *protectiveResumeBot) GetPositionSummary() (float64, float64, error)        { return 0, 0, nil }

func (b *protectiveResumeBot) GetPositionStatus() map[string]interface{} {
	return map[string]interface{}{"paused": b.paused}
}

func (b *protectiveResumeBot) GetBotRiskControl() *config.BotRiskControl {
	if b.riskControl == nil {
		return &config.BotRiskControl{}
	}
	copy := *b.riskControl
	return &copy
}

func (b *protectiveResumeBot) SetBotRiskControl(rc *config.BotRiskControl) error {
	copy := *rc
	b.riskControl = &copy
	return nil
}

type protectiveResumeProvider struct{ bot BotExtended }

func (p protectiveResumeProvider) GetBot(string) (BotExtended, bool) { return p.bot, true }

func TestProtectiveResumeAPIUsesExplicitManualRecovery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setupPausePersistenceConfig(t, "test", true)
	original := botExtendedProvider
	t.Cleanup(func() { botExtendedProvider = original })
	previousCoordinator := globalOpeningPauseCoordinator
	globalOpeningPauseCoordinator = risk.NewOpeningPauseCoordinator()
	t.Cleanup(func() { globalOpeningPauseCoordinator = previousCoordinator })
	for _, tc := range []struct {
		name               string
		err                error
		paused             bool
		code               int
		body               string
		wantPersistedPause bool
	}{
		{"unverified", errors.New("protective liquidation not verified"), true, http.StatusConflict, "not verified", true},
		{"independent_hold", nil, true, http.StatusOK, `"status":"paused"`, true},
		{"verified", nil, false, http.StatusOK, `"status":"resumed"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bot := &protectiveResumeBot{err: tc.err, paused: tc.paused, riskControl: &config.BotRiskControl{PauseOpening: true, PauseOpeningReason: "manual", AutoResumeAfter: 30}}
			botExtendedProvider = protectiveResumeProvider{bot: bot}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v2/bots/test/resume", nil)
			c.Params = gin.Params{{Key: "id", Value: "test"}}
			resumeBotOpening(c)
			if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.body) {
				t.Fatalf("response: %d %s", w.Code, w.Body.String())
			}
			if bot.manualCalls != 1 || bot.automaticCalls != 0 {
				t.Fatal("manual endpoint used automatic recovery")
			}
			persistedPause, _ := persistedBotPause(t, "test")
			if persistedPause != tc.wantPersistedPause {
				t.Fatalf("persisted pause=%v want %v", persistedPause, tc.wantPersistedPause)
			}
		})
	}
	botExtendedProvider = nil
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	resumeBotOpening(c)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing provider status = %d", w.Code)
	}
}

func TestManualBotResumeFailsClosedWithoutPauseCoordinator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousProvider := botExtendedProvider
	previousCoordinator := globalOpeningPauseCoordinator
	t.Cleanup(func() {
		botExtendedProvider = previousProvider
		globalOpeningPauseCoordinator = previousCoordinator
	})
	bot := &protectiveResumeBot{paused: true}
	botExtendedProvider = protectiveResumeProvider{bot: bot}
	globalOpeningPauseCoordinator = nil

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v2/bots/test/resume", nil)
	c.Params = gin.Params{{Key: "id", Value: "test"}}
	resumeBotOpening(c)

	if w.Code != http.StatusServiceUnavailable || bot.manualCalls != 0 || bot.automaticCalls != 0 {
		t.Fatalf("resume must fail closed without coordinator: status=%d manual=%d automatic=%d body=%s", w.Code, bot.manualCalls, bot.automaticCalls, w.Body.String())
	}
}

func TestManualBotResumeRejectedWhileRiskCoordinatorHolds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousProvider := botExtendedProvider
	previousCoordinator := globalOpeningPauseCoordinator
	t.Cleanup(func() {
		botExtendedProvider = previousProvider
		globalOpeningPauseCoordinator = previousCoordinator
	})
	bot := &protectiveResumeBot{paused: true}
	botExtendedProvider = protectiveResumeProvider{bot: bot}
	coordinator := risk.NewOpeningPauseCoordinator()
	coordinator.Pause("global_circuit_breaker", "daily_loss", []risk.BotController{bot})
	globalOpeningPauseCoordinator = coordinator

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v2/bots/test/resume", nil)
	c.Params = gin.Params{{Key: "id", Value: "test"}}
	resumeBotOpening(c)

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "risk_pause_active") {
		t.Fatalf("response: %d %s", w.Code, w.Body.String())
	}
	if bot.manualCalls != 0 || bot.automaticCalls != 0 {
		t.Fatalf("risk-held Bot must not receive a resume call: manual=%d automatic=%d", bot.manualCalls, bot.automaticCalls)
	}
}
