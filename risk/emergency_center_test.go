package risk

import (
	"context"
	"strings"
	"testing"
)

type emergencyMockBot struct {
	pauseReasons []string
	cancelCount  int
	closeCount   int
	closeMethod  string
	closeTimeout int
	resumeCount  int
}

func (b *emergencyMockBot) PauseOpening(reason string) {
	b.pauseReasons = append(b.pauseReasons, reason)
}

func (b *emergencyMockBot) ResumeOpening() { b.resumeCount++ }

func (b *emergencyMockBot) CancelAllOpenOrders() error {
	b.cancelCount++
	return nil
}

func (b *emergencyMockBot) CloseAllPositions(ctx context.Context, method string, timeout int) error {
	b.closeCount++
	b.closeMethod = method
	b.closeTimeout = timeout
	return nil
}

func (b *emergencyMockBot) GetPositionSummary() (float64, float64, error) {
	return 0, 0, nil
}

func TestEmergencyReducePositionFallsBackToCloseAll(t *testing.T) {
	bot := &emergencyMockBot{}
	ec := &EmergencyCenter{}

	result, err := ec.reducePositions(context.Background(), []BotController{bot}, "limit", 60)
	if err != nil {
		t.Fatalf("reducePositions() error=%v", err)
	}
	if bot.closeCount != 1 {
		t.Fatalf("减仓兜底应执行全平，closeCount=%d", bot.closeCount)
	}
	if bot.closeMethod != "limit" || bot.closeTimeout != 60 {
		t.Fatalf("全平参数未传递: method=%q timeout=%d", bot.closeMethod, bot.closeTimeout)
	}
	if !strings.Contains(result, "已执行全平保护") {
		t.Fatalf("结果应明确说明全平兜底，got %q", result)
	}
}

func TestDisableEmergencyModeDoesNotResumeRiskPausedBots(t *testing.T) {
	bot := &emergencyMockBot{}
	ec := NewEmergencyCenter(nil, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})
	ec.emergencyMode = true

	if err := ec.DisableEmergencyMode("operator"); err != nil {
		t.Fatalf("DisableEmergencyMode() error=%v", err)
	}
	if ec.IsEmergencyMode() {
		t.Fatal("emergency mode should be disabled")
	}
	if bot.resumeCount != 0 {
		t.Fatalf("disabling emergency mode must not clear another risk source pause; resumeCount=%d", bot.resumeCount)
	}
}

func TestEmergencyPauseReleasePreservesOtherRiskHolds(t *testing.T) {
	bot := &circuitBreakerMockBot{}
	bots := []BotController{bot}
	provider := &circuitBreakerMockProvider{bots: bots}
	coordinator := NewOpeningPauseCoordinator()
	coordinator.Pause("global_circuit_breaker", "daily_loss", bots)
	emergency := NewEmergencyCenter(nil, nil, provider)
	emergency.SetPauseCoordinator(coordinator)
	if _, err := emergency.pauseAllBots(bots); err != nil {
		t.Fatal(err)
	}
	emergency.emergencyMode = true

	if err := emergency.DisableEmergencyMode("operator"); err != nil {
		t.Fatal(err)
	}
	if bot.resumeCount != 0 || !coordinator.IsHeldBy("global_circuit_breaker") || coordinator.IsHeldBy(emergencyCenterPauseSource) {
		t.Fatalf("emergency release must retain independent risk hold: resumes=%d holders=%v", bot.resumeCount, coordinator.Holders())
	}
	coordinator.Release("global_circuit_breaker", bots)
	if bot.resumeCount != 1 {
		t.Fatalf("last risk source release should resume once, got %d", bot.resumeCount)
	}
}
