package risk

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

type fakeTradeSource struct {
	mu     sync.Mutex
	trades []TradeOutcome
	err    error
}

func (s *fakeTradeSource) add(t TradeOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.trades = append(s.trades, t)
}

func (s *fakeTradeSource) TradesBetween(_ context.Context, start, end time.Time, limit int) ([]TradeOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	var out []TradeOutcome
	for _, t := range s.trades {
		if !t.ClosedAt.Before(start) && !t.ClosedAt.After(end) {
			out = append(out, t)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type fakeEquitySource struct {
	equity float64
	err    error
	calls  int
}

func (s *fakeEquitySource) TotalEquity(context.Context) (float64, error) {
	s.calls++
	return s.equity, s.err
}

type fakeSink struct {
	daily  float64
	dd     float64
	losses int
	marks  MetricsResetMarks
	writes int
}

func (s *fakeSink) UpdateMetrics(d, dd float64, l int) {
	s.daily, s.dd, s.losses = d, dd, l
	s.writes++
}
func (s *fakeSink) MetricsResetMarks() MetricsResetMarks { return s.marks }

type pnlBot struct {
	circuitBreakerMockBot
	pnl float64
	err error
}

func (b *pnlBot) GetPositionSummary() (float64, float64, error) { return b.pnl, 0, b.err }

func approxEqual(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestMetricsFeederDailyPnLAndConsecutiveLosses(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, loc)
	trades := &fakeTradeSource{trades: []TradeOutcome{
		{Key: "y", NetPnL: -50, ClosedAt: now.Add(-24 * time.Hour)}, // 昨日，不计入日内
		{Key: "a", NetPnL: 30, ClosedAt: now.Add(-3 * time.Hour)},
		{Key: "b", NetPnL: -10, ClosedAt: now.Add(-2 * time.Hour)},
		{Key: "c", NetPnL: 0, ClosedAt: now.Add(-90 * time.Minute)}, // 持平跳过
		{Key: "d", NetPnL: -20, ClosedAt: now.Add(-1 * time.Hour)},
	}}
	sink := &fakeSink{}
	bots := &circuitBreakerMockProvider{bots: []BotController{
		&pnlBot{pnl: -15},
	}}
	f := NewMetricsFeeder(sink, trades, nil, bots, MetricsFeederOptions{Location: loc, Now: func() time.Time { return now }})

	snap, err := f.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick() error=%v", err)
	}
	if !approxEqual(snap.RealizedToday, 0) { // 30 - 10 + 0 - 20
		t.Fatalf("RealizedToday=%v, want 0", snap.RealizedToday)
	}
	if !approxEqual(sink.daily, -15) {
		t.Fatalf("dailyPnL=%v, want -15 (已实现 0 + 未实现 -15)", sink.daily)
	}
	if sink.losses != 2 {
		t.Fatalf("consecutiveLosses=%d, want 2", sink.losses)
	}
	if snap.DrawdownAvailable {
		t.Fatal("无权益数据源时回撤应不可用")
	}
}

func TestMetricsFeederPositionSummaryFailureDoesNotPublishPartialPnL(t *testing.T) {
	sink := &fakeSink{}
	bots := &circuitBreakerMockProvider{bots: []BotController{
		&pnlBot{pnl: -15},
		&pnlBot{err: errors.New("current mark unavailable")},
	}}
	f := NewMetricsFeeder(sink, nil, nil, bots, MetricsFeederOptions{})
	if _, err := f.Tick(context.Background()); err == nil {
		t.Fatal("partial unrealized PnL should not be published as a complete metric")
	}
	if sink.writes != 0 {
		t.Fatalf("partial metric was published %d times", sink.writes)
	}
}

func TestMetricsFeederRequiresTradeHistoryWhenConfigured(t *testing.T) {
	f := NewMetricsFeeder(&fakeSink{}, nil, nil, nil, MetricsFeederOptions{RequireTradeHistory: true})
	if _, err := f.Tick(context.Background()); err == nil {
		t.Fatal("missing realized trade history was treated as zero")
	}
}

func TestMetricsFeederResetMarksExcludeHistory(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	trades := &fakeTradeSource{trades: []TradeOutcome{
		{Key: "a", NetPnL: -10, ClosedAt: now.Add(-2 * time.Hour)},
		{Key: "b", NetPnL: -10, ClosedAt: now.Add(-1 * time.Hour)},
		{Key: "c", NetPnL: -5, ClosedAt: now.Add(-10 * time.Minute)},
	}}
	sink := &fakeSink{}
	f := NewMetricsFeeder(sink, trades, nil, nil, MetricsFeederOptions{Location: time.UTC, Now: func() time.Time { return now }})

	sink.marks.Streak = now.Add(-30 * time.Minute)
	if _, err := f.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.losses != 1 || !approxEqual(sink.daily, -25) {
		t.Fatalf("Streak 基线只影响连亏: losses=%d daily=%v", sink.losses, sink.daily)
	}

	sink.marks.All = now.Add(-30 * time.Minute)
	if _, err := f.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sink.losses != 1 || !approxEqual(sink.daily, -5) {
		t.Fatalf("All 基线应同时影响日内盈亏: losses=%d daily=%v", sink.losses, sink.daily)
	}
}

func TestMetricsFeederDrawdownIsTransferImmune(t *testing.T) {
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	clock := now
	trades := &fakeTradeSource{}
	equity := &fakeLedgerSource{observation: testEquityObservation(now, 1000)}
	sink := &fakeSink{}
	bot := &pnlBot{pnl: 0}
	f := NewMetricsFeeder(sink, trades, equity, &circuitBreakerMockProvider{bots: []BotController{bot}},
		MetricsFeederOptions{Location: time.UTC, Now: func() time.Time { return clock }})

	// 第一轮建立基准
	if _, err := f.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 盈利 100 → 高水位 1100
	clock = now.Add(time.Minute)
	equity.observation = testEquityObservation(clock, 1100)
	trades.add(TradeOutcome{Key: "p", NetPnL: 100, ClosedAt: clock.Add(-10 * time.Second)})
	if _, err := f.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 同一笔成交在重叠窗口内再次被查到不应重复累计；浮亏 -110 → 990，回撤 10%
	clock = now.Add(2 * time.Minute)
	equity.observation = testEquityObservation(clock, 500)
	equity.observation.Flows = []EquityCashFlow{{ID: "transfer", Kind: "transfer_out", Currency: "USDT", Amount: -490, At: clock.Add(-10 * time.Second)}}
	bot.pnl = -110
	snap, err := f.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.DrawdownAvailable || !approxEqual(snap.MaxDrawdownPct, 10) {
		t.Fatalf("drawdown=%v available=%v, want 10%%", snap.MaxDrawdownPct, snap.DrawdownAvailable)
	}
	if equity.calls != 3 {
		t.Fatalf("每轮都必须查询权益，calls=%d", equity.calls)
	}

	// 手动恢复重置基线 → 重新取权益，回撤归零
	sink.marks.All = clock
	clock = now.Add(3 * time.Minute)
	equity.observation = testEquityObservation(clock, 500)
	snap, err = f.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if equity.calls != 4 || snap.MaxDrawdownPct != 0 {
		t.Fatalf("重置后应重新建立基准: calls=%d dd=%v", equity.calls, snap.MaxDrawdownPct)
	}
}

func TestMetricsFeederSourceErrorKeepsPreviousMetrics(t *testing.T) {
	trades := &fakeTradeSource{err: errors.New("db down")}
	sink := &fakeSink{}
	f := NewMetricsFeeder(sink, trades, nil, nil, MetricsFeederOptions{})
	if _, err := f.Tick(context.Background()); err == nil {
		t.Fatal("数据源失败应返回错误")
	}
	if sink.writes != 0 {
		t.Fatal("数据源失败时不应写入残缺指标")
	}
}

func TestMetricsFeederEquityErrorDoesNotPublishIncompleteMetrics(t *testing.T) {
	sink := &fakeSink{}
	equity := &fakeEquitySource{err: ErrEquityUnavailable}
	f := NewMetricsFeeder(sink, nil, equity, &circuitBreakerMockProvider{bots: []BotController{&pnlBot{pnl: -7}}}, MetricsFeederOptions{})
	snap, err := f.Tick(context.Background())
	if err == nil || snap.DrawdownAvailable || sink.writes != 0 {
		t.Fatalf("权益不可用时不得发布零回撤: err=%v writes=%d available=%v", err, sink.writes, snap.DrawdownAvailable)
	}
}

func TestMetricsFeederDrivesCircuitBreakerTrip(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.TotalDailyLoss.Enabled = true
	cfg.Triggers.TotalDailyLoss.Threshold = 100
	bot := &pnlBot{pnl: -150}
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})

	f := NewMetricsFeeder(gcb, nil, nil, &circuitBreakerMockProvider{bots: []BotController{bot}}, MetricsFeederOptions{})
	if _, err := f.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	gcb.checkTriggers()
	if !gcb.IsTripped() {
		t.Fatal("喂入的单日亏损超过阈值后应触发熔断")
	}
	events := gcb.GetEvents(1)
	if events[0].Trigger != TriggerTotalDailyLoss {
		t.Fatalf("trigger=%s, want %s", events[0].Trigger, TriggerTotalDailyLoss)
	}
}
