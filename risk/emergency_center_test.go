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
}

func (b *emergencyMockBot) PauseOpening(reason string) {
	b.pauseReasons = append(b.pauseReasons, reason)
}

func (b *emergencyMockBot) ResumeOpening() {}

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
