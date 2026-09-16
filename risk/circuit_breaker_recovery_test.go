package risk

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/event"
)

// safeMockBot 并发安全的 Bot mock（runOnBots 会并行调用）
type safeMockBot struct {
	pauses   atomic.Int32
	resumes  atomic.Int32
	cancels  atomic.Int32
	closes   atomic.Int32
	closeErr error

	mu           sync.Mutex
	closeCtxLeft time.Duration
	closeTimeout int
}

func (b *safeMockBot) PauseOpening(string) { b.pauses.Add(1) }
func (b *safeMockBot) ResumeOpening()      { b.resumes.Add(1) }
func (b *safeMockBot) CancelAllOpenOrders() error {
	b.cancels.Add(1)
	return nil
}
func (b *safeMockBot) CloseAllPositions(ctx context.Context, _ string, timeout int) error {
	b.closes.Add(1)
	b.mu.Lock()
	if dl, ok := ctx.Deadline(); ok {
		b.closeCtxLeft = time.Until(dl)
	}
	b.closeTimeout = timeout
	b.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return b.closeErr
}
func (b *safeMockBot) GetPositionSummary() (float64, float64, error) { return 0, 0, nil }

type recordingNotifier struct {
	mu     sync.Mutex
	events []*event.Event
}

func (n *recordingNotifier) Send(evt *event.Event) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, evt)
}

func (n *recordingNotifier) types() []event.EventType {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]event.EventType, 0, len(n.events))
	for _, e := range n.events {
		out = append(out, e.Type)
	}
	return out
}

// forceTrippedAt 把触发时间拨回过去，模拟暂停期已过
func forceTrippedAt(gcb *GlobalCircuitBreaker, at time.Time) {
	gcb.statusMu.Lock()
	gcb.trippedAt = at
	gcb.statusMu.Unlock()
}

func TestCircuitBreakerPauseDurationZeroMeansNoAutoResume(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Actions.PauseDuration = 0
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{&safeMockBot{}}})

	gcb.ManualTrigger("tester", "risk")
	forceTrippedAt(gcb, time.Now().Add(-24*time.Hour))
	gcb.checkRecovery()

	if !gcb.IsTripped() {
		t.Fatal("pause_duration=0 表示无限期，不应自动恢复")
	}
}

func TestCircuitBreakerAutoResumeBlockedWhileDailyLossExceeded(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Actions.PauseDuration = 1
	cfg.Triggers.TotalDailyLoss.Enabled = true
	cfg.Triggers.TotalDailyLoss.Threshold = 100
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{&safeMockBot{}}})

	gcb.UpdateMetrics(-150, 0, 0)
	gcb.checkTriggers()
	if !gcb.IsTripped() {
		t.Fatal("单日亏损超限应触发")
	}

	forceTrippedAt(gcb, time.Now().Add(-time.Minute))
	gcb.checkRecovery()
	if !gcb.IsTripped() {
		t.Fatal("亏损仍超限时不应自动恢复（避免 触发→平仓→恢复→重建仓 循环）")
	}

	gcb.UpdateMetrics(-50, 0, 0)
	gcb.checkRecovery()
	if gcb.IsTripped() {
		t.Fatal("指标回到阈值内且暂停期已过应自动恢复")
	}
}

func TestCircuitBreakerAutoResumeResetsCountersAndStreakMark(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Actions.PauseDuration = 1
	cfg.Triggers.ConsecutiveLosses.Enabled = true
	cfg.Triggers.ConsecutiveLosses.Count = 3
	cfg.Triggers.APIAuthFailed.Enabled = true
	cfg.Triggers.APIAuthFailed.Count = 5
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{&safeMockBot{}}})

	gcb.UpdateMetrics(-10, 0, 4)
	gcb.ReportAuthFailure()
	gcb.checkTriggers()
	if !gcb.IsTripped() {
		t.Fatal("连续亏损超限应触发")
	}

	forceTrippedAt(gcb, time.Now().Add(-time.Minute))
	before := time.Now()
	gcb.checkRecovery()
	if gcb.IsTripped() {
		t.Fatal("连亏为计数型指标，暂停期后应自动恢复")
	}

	m := gcb.snapshotMetrics()
	if m.consecutiveLosses != 0 || m.authFailCount != 0 {
		t.Fatalf("自动恢复应清零计数: losses=%d auth=%d", m.consecutiveLosses, m.authFailCount)
	}
	if m.dailyPnL != -10 {
		t.Fatalf("自动恢复不应清零日内盈亏, got %v", m.dailyPnL)
	}
	marks := gcb.MetricsResetMarks()
	if marks.Streak.Before(before) || !marks.All.IsZero() {
		t.Fatalf("自动恢复只应推进 Streak 基线: %+v", marks)
	}
}

func TestCircuitBreakerManualRecoverResetsAllMarks(t *testing.T) {
	gcb := NewGlobalCircuitBreaker(newCircuitBreakerTestConfig(), nil, &circuitBreakerMockProvider{bots: []BotController{&safeMockBot{}}})
	gcb.UpdateMetrics(-500, 30, 9)
	gcb.ManualTrigger("tester", "risk")
	if err := gcb.ManualRecover("tester"); err != nil {
		t.Fatal(err)
	}
	m := gcb.snapshotMetrics()
	if m.dailyPnL != 0 || m.maxDrawdown != 0 || m.consecutiveLosses != 0 {
		t.Fatalf("手动恢复应重置指标: %+v", m)
	}
	if gcb.MetricsResetMarks().All.IsZero() {
		t.Fatal("手动恢复应推进 All 基线")
	}
}

func TestCircuitBreakerSendsNotificationsOnTripAndRecover(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Notifications.Enabled = true
	cfg.Actions.ClosePositions.Enabled = false
	n := &recordingNotifier{}
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{&safeMockBot{}}})
	gcb.SetNotifier(n)

	gcb.ManualTrigger("tester", "risk")
	deadline := time.Now().Add(time.Second)
	for len(n.types()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if err := gcb.ManualRecover("tester"); err != nil {
		t.Fatal(err)
	}
	types := n.types()
	if len(types) != 2 || types[0] != event.EventTypeRiskTriggered || types[1] != event.EventTypeRiskRecovered {
		t.Fatalf("通知序列错误: %v", types)
	}
}

func TestCircuitBreakerCloseUsesDefaultTimeoutWhenZero(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Actions.ClosePositions.Timeout = 0
	bot := &safeMockBot{}
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})

	summary := gcb.closeAllPositions()
	if !strings.Contains(summary, "1/1") {
		t.Fatalf("timeout=0 时平仓不应因 ctx 立即过期而失败: %s", summary)
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if bot.closeCtxLeft <= DefaultBotActionTimeout/2 || bot.closeTimeout != int(DefaultBotActionTimeout/time.Second) {
		t.Fatalf("应使用默认超时: left=%v timeout=%d", bot.closeCtxLeft, bot.closeTimeout)
	}
}

func TestPauseCoordinatorDoesNotResumeWhileOtherSourceHolds(t *testing.T) {
	bot := &safeMockBot{}
	bots := []BotController{bot}
	provider := &circuitBreakerMockProvider{bots: bots}
	pauser := NewOpeningPauseCoordinator()

	cfg := newCircuitBreakerTestConfig()
	cfg.Actions.CancelAllOpenOrders = false
	cfg.Actions.ClosePositions.Enabled = false
	gcb := NewGlobalCircuitBreaker(cfg, nil, provider)
	gcb.SetPauseCoordinator(pauser)
	guard := NewCompositeRiskGuard(provider, pauser, nil)

	guard.Apply(CompositeRiskStop, "stop_trading", 85, nil)
	gcb.pauseAllBots(string(TriggerManual))
	gcb.statusMu.Lock()
	gcb.status = CircuitBreakerStatusTripped
	gcb.statusMu.Unlock()

	if err := gcb.ManualRecover("tester"); err != nil {
		t.Fatal(err)
	}
	if bot.resumes.Load() != 0 {
		t.Fatal("复合风控仍持有暂停时，熔断恢复不应恢复开仓")
	}

	guard.Apply(CompositeRiskHold, "pause_buying", 70, nil)
	if !guard.IsPaused() || bot.resumes.Load() != 0 {
		t.Fatal("pause_buying 为滞回区间，应保持暂停")
	}
	guard.Apply(CompositeRiskRelease, "caution", 30, nil)
	if guard.IsPaused() || bot.resumes.Load() != 1 {
		t.Fatalf("所有来源解除后应恢复一次, resumes=%d", bot.resumes.Load())
	}
}

func TestCompositeRiskGuardStopIsIdempotent(t *testing.T) {
	bot := &safeMockBot{}
	n := &recordingNotifier{}
	guard := NewCompositeRiskGuard(&circuitBreakerMockProvider{bots: []BotController{bot}}, nil, n)

	guard.Apply(CompositeRiskStop, "stop_trading", 90, []string{"news"})
	guard.Apply(CompositeRiskStop, "stop_trading", 92, []string{"news"})
	if bot.pauses.Load() != 1 {
		t.Fatalf("重复 stop 不应重复暂停, pauses=%d", bot.pauses.Load())
	}
	if len(n.types()) != 1 || n.types()[0] != event.EventTypeRiskTriggered {
		t.Fatalf("通知错误: %v", n.types())
	}
	guard.Apply(CompositeRiskRelease, "normal", 10, nil)
	guard.Apply(CompositeRiskRelease, "normal", 10, nil)
	if bot.resumes.Load() != 1 {
		t.Fatalf("重复 release 不应重复恢复, resumes=%d", bot.resumes.Load())
	}
}

func TestRunOnBotsAggregatesErrorsAndPanics(t *testing.T) {
	ok := &safeMockBot{}
	bad := &safeMockBot{closeErr: errors.New("reduceOnly rejected")}
	report := closePositionsOnBots(context.Background(), []BotController{ok, bad, panicBot{&safeMockBot{}}}, "market", 1)
	if report.Total != 3 || report.Succeeded != 1 || report.Status() != OperationStatusPartial {
		t.Fatalf("report=%+v status=%s", report, report.Status())
	}
	err := report.Err()
	if err == nil || !strings.Contains(err.Error(), "reduceOnly rejected") || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("应聚合全部错误, got %v", err)
	}
}

type panicBot struct{ *safeMockBot }

func (panicBot) CloseAllPositions(context.Context, string, int) error { panic("boom") }
