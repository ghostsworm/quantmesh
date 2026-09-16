package risk

import (
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
)

// waitOperationDone 等待异步操作结束并返回快照
func waitOperationDone(t *testing.T, ec *EmergencyCenter, id string) *EmergencyOperation {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, op := range ec.GetOperations(0) {
			if op.ID == id && op.Status != OperationStatusExecuting {
				return op
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("操作 %s 未在超时内结束", id)
	return nil
}

func newTestEmergencyCenter(cfg *config.EmergencyCenterConfig, bots ...BotController) *EmergencyCenter {
	return NewEmergencyCenter(cfg, nil, &circuitBreakerMockProvider{bots: bots})
}

func TestEmergencyOperationPartialWhenSomeBotsFail(t *testing.T) {
	ok := &safeMockBot{}
	bad := &safeMockBot{closeErr: errors.New("insufficient margin")}
	ec := newTestEmergencyCenter(&config.EmergencyCenterConfig{}, ok, bad)

	op, err := ec.ExecuteScenario("market_crash", "tester", "test")
	if err != nil {
		t.Fatal(err)
	}
	done := waitOperationDone(t, ec, op.ID)
	if done.Status != OperationStatusPartial {
		t.Fatalf("部分 Bot 平仓失败应为 partial, got %s (error=%s)", done.Status, done.Error)
	}
	if !strings.Contains(done.Error, "insufficient margin") {
		t.Fatalf("应保留失败原因, got %q", done.Error)
	}
	if !strings.Contains(done.Results[string(EmergencyActionCloseAllPositions)], "1/2") {
		t.Fatalf("结果应体现 1/2, got %q", done.Results[string(EmergencyActionCloseAllPositions)])
	}
}

func TestEmergencyOperationFailedWhenAllBotsFail(t *testing.T) {
	bad := &safeMockBot{closeErr: errors.New("rejected")}
	ec := newTestEmergencyCenter(&config.EmergencyCenterConfig{}, bad)
	ec.scenarios["close_only"] = &EmergencyScenario{
		Name:    "close_only",
		Actions: []EmergencyAction{EmergencyActionCloseAllPositions},
	}

	op, err := ec.ExecuteScenario("close_only", "tester", "test")
	if err != nil {
		t.Fatal(err)
	}
	done := waitOperationDone(t, ec, op.ID)
	if done.Status != OperationStatusFailed {
		t.Fatalf("全部失败应为 failed, got %s", done.Status)
	}
}

func TestEmergencyCloseWithZeroTimeoutUsesDefault(t *testing.T) {
	bot := &safeMockBot{}
	ec := newTestEmergencyCenter(&config.EmergencyCenterConfig{}, bot)
	ec.scenarios["zero_timeout"] = &EmergencyScenario{
		Name:    "zero_timeout",
		Actions: []EmergencyAction{EmergencyActionCloseAllPositions},
		Timeout: 0,
	}

	op, err := ec.ExecuteScenario("zero_timeout", "tester", "test")
	if err != nil {
		t.Fatal(err)
	}
	done := waitOperationDone(t, ec, op.ID)
	if done.Status != OperationStatusCompleted {
		t.Fatalf("timeout=0 不应让 ctx 立即过期, status=%s error=%s", done.Status, done.Error)
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.closeTimeout != int(DefaultBotActionTimeout/time.Second) {
		t.Fatalf("应传递默认超时, got %d", bot.closeTimeout)
	}
}

func TestEmergencyValidateConfirmation(t *testing.T) {
	ec := newTestEmergencyCenter(&config.EmergencyCenterConfig{RequireConfirmation: true})
	if err := ec.ValidateConfirmation("market_crash", false, ""); err == nil {
		t.Fatal("未确认应拒绝")
	}
	if err := ec.ValidateConfirmation("market_crash", true, "full_shutdown"); err == nil {
		t.Fatal("确认场景不匹配应拒绝")
	}
	if err := ec.ValidateConfirmation("market_crash", true, "market_crash"); err != nil {
		t.Fatalf("正确确认应通过: %v", err)
	}

	noConfirm := newTestEmergencyCenter(&config.EmergencyCenterConfig{})
	if err := noConfirm.ValidateConfirmation("market_crash", false, ""); err != nil {
		t.Fatalf("未启用 require_confirmation 时不应要求确认: %v", err)
	}
}
