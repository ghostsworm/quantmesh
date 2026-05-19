package risk

import (
	"context"
	"testing"
	"time"

	"quantmesh/config"
)

type circuitBreakerMockBot struct {
	pauseCount  int
	cancelCount int
	closeCount  int
	resumeCount int
}

func (b *circuitBreakerMockBot) PauseOpening(reason string) {
	b.pauseCount++
}

func (b *circuitBreakerMockBot) ResumeOpening() {
	b.resumeCount++
}

func (b *circuitBreakerMockBot) CancelAllOpenOrders() error {
	b.cancelCount++
	return nil
}

func (b *circuitBreakerMockBot) CloseAllPositions(ctx context.Context, method string, timeout int) error {
	b.closeCount++
	return nil
}

func (b *circuitBreakerMockBot) GetPositionSummary() (float64, float64, error) {
	return 0, 0, nil
}

type circuitBreakerMockProvider struct {
	bots []BotController
}

func (p *circuitBreakerMockProvider) GetAllBots() []BotController {
	return p.bots
}

func newCircuitBreakerTestConfig() *config.CircuitBreakerConfig {
	cfg := &config.CircuitBreakerConfig{}
	cfg.Actions.StopAllNewOrders = true
	cfg.Actions.CancelAllOpenOrders = true
	cfg.Actions.ClosePositions.Enabled = true
	cfg.Actions.ClosePositions.Method = "market"
	cfg.Actions.ClosePositions.Timeout = 1
	cfg.Recovery.AutoResume = true
	cfg.Recovery.CooldownMinutes = 1
	return cfg
}

func TestCircuitBreakerIgnoresDuplicateTrip(t *testing.T) {
	bot := &circuitBreakerMockBot{}
	gcb := NewGlobalCircuitBreaker(newCircuitBreakerTestConfig(), nil, &circuitBreakerMockProvider{
		bots: []BotController{bot},
	})

	gcb.ManualTrigger("tester", "first")
	gcb.ManualTrigger("tester", "second")

	events := gcb.GetEvents(10)
	if len(events) != 1 {
		t.Fatalf("重复触发不应重复记录事件，events=%d", len(events))
	}
	if events[0].Reason != "first" {
		t.Fatalf("应保留首次触发原因，got %q", events[0].Reason)
	}
	if gcb.GetStatus() != CircuitBreakerStatusTripped {
		t.Fatalf("熔断状态应保持 tripped，got %s", gcb.GetStatus())
	}
}

func TestCircuitBreakerManualRecoverDoesNotDeadlockAndResumesOnce(t *testing.T) {
	bot := &circuitBreakerMockBot{}
	gcb := NewGlobalCircuitBreaker(newCircuitBreakerTestConfig(), nil, &circuitBreakerMockProvider{
		bots: []BotController{bot},
	})

	gcb.ManualTrigger("tester", "risk")

	done := make(chan error, 1)
	go func() {
		done <- gcb.ManualRecover("tester")
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ManualRecover() error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ManualRecover() 超时，可能存在锁内恢复死锁")
	}

	if gcb.GetStatus() != CircuitBreakerStatusNormal {
		t.Fatalf("恢复后状态应为 normal，got %s", gcb.GetStatus())
	}
	if bot.resumeCount != 1 {
		t.Fatalf("恢复应只调用一次 ResumeOpening，got %d", bot.resumeCount)
	}
	if err := gcb.ManualRecover("tester"); err == nil {
		t.Fatal("重复恢复应返回错误")
	}
}

func TestCircuitBreakerManualRequiredBlocksAutoRecover(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Recovery.ManualRequired = true
	cfg.Actions.PauseDuration = 0

	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{
		bots: []BotController{&circuitBreakerMockBot{}},
	})

	gcb.ManualTrigger("tester", "risk")
	gcb.checkRecovery()

	if gcb.GetStatus() != CircuitBreakerStatusTripped {
		t.Fatalf("manual_required=true 时不应自动恢复，got %s", gcb.GetStatus())
	}
}
