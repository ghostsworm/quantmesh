package risk

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type ownedEquityBot struct {
	circuitBreakerMockBot
	held bool
}

func (b *ownedEquityBot) SetRiskDataUnavailable(held bool) { b.held = held }

type assetPnLBot struct {
	ownedEquityBot
	pnl   float64
	asset string
}

func (b *assetPnLBot) GetPositionSummary() (float64, float64, error) { return b.pnl, 0, nil }
func (b *assetPnLBot) RiskPnLQuoteAsset() string                     { return b.asset }

func TestMetricsHealthUnverifiedEquityPausesWithoutLiquidating(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.MaxDrawdown.Enabled, cfg.Triggers.MaxDrawdown.Threshold = true, 10
	bot := &ownedEquityBot{}
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})
	// Enable after construction to keep this unit test free of a background ticker.
	cfg.Enabled = true
	now := time.Now()
	health := MetricsHealth{Available: true, DrawdownAvailable: true, Persisted: true, CheckedAt: now, ValidUntil: now.Add(time.Minute)}
	gcb.UpdateMetricsObservation(MetricsSnapshot{MaxDrawdownPct: 20}, health)
	if !bot.held {
		t.Fatal("unadjusted equity did not block new risk")
	}
	if _, _, hit := gcb.checkMaxDrawdownTrigger(gcb.snapshotMetrics()); hit {
		t.Fatal("unadjusted balance change triggered liquidation")
	}
	if err := gcb.UpdateExternalMetrics(0, 0, 0); err == nil {
		t.Fatal("external metric update overwrote account observation")
	}
	health.CashFlowAdjusted = true
	gcb.UpdateMetricsObservation(MetricsSnapshot{MaxDrawdownPct: 20}, health)
	if bot.held || bot.resumeCount != 0 {
		t.Fatal("data recovery did not release only its own hold")
	}
	if _, _, hit := gcb.checkMaxDrawdownTrigger(gcb.snapshotMetrics()); !hit {
		t.Fatal("verified drawdown did not trigger risk")
	}
	health.ValidUntil = now.Add(-time.Second)
	gcb.UpdateMetricsHealth(health)
	if !bot.held {
		t.Fatal("stale data did not reestablish hold")
	}
}

func TestMetricsHealthAndValuesPublishAtomically(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.MaxDrawdown.Enabled, cfg.Triggers.MaxDrawdown.Threshold = true, 10
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{})
	now := time.Now()
	verified := MetricsHealth{Available: true, DrawdownAvailable: true, Persisted: true, CashFlowAdjusted: true, CheckedAt: now, ValidUntil: now.Add(time.Minute)}
	unverified := verified
	unverified.CashFlowAdjusted = false
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			gcb.UpdateMetricsObservation(MetricsSnapshot{MaxDrawdownPct: 0}, verified)
			gcb.UpdateMetricsObservation(MetricsSnapshot{MaxDrawdownPct: 50}, unverified)
		}
	}()
	for i := 0; i < 1000; i++ {
		if _, _, hit := gcb.checkMaxDrawdownTrigger(gcb.snapshotMetrics()); hit {
			t.Error("mixed metric/health generations caused false liquidation")
			break
		}
	}
	wg.Wait()
}

func TestMetricsHealthKeepsDataFailureSeparateFromZeroDrawdown(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.MaxDrawdown.Enabled = true
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{})
	source := &fakeEquitySource{equity: 1000}
	f := NewMetricsFeeder(gcb, nil, source, nil, MetricsFeederOptions{})
	if _, err := f.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	health := gcb.GetMetricsHealth()
	if !health.Available || !health.DrawdownAvailable || health.CashFlowAdjusted || health.Persisted {
		t.Fatalf("raw equity incorrectly certified: %+v", health)
	}
	if len(gcb.autoResumeBlockers(gcb.snapshotMetrics())) == 0 {
		t.Fatal("automatic recovery accepted unadjusted/unpersisted equity")
	}
	source.err = errors.New("account unavailable")
	if _, err := f.Tick(t.Context()); err == nil {
		t.Fatal("expected failure")
	}
	health = gcb.GetMetricsHealth()
	if health.Available || health.DrawdownAvailable || health.Error == "" {
		t.Fatalf("failed data reported as zero risk: %+v", health)
	}
	gcb.UpdateMetricsHealth(MetricsHealth{Available: true, DrawdownAvailable: true, CashFlowAdjusted: true, Persisted: true, CheckedAt: time.Now().Add(-time.Hour), ValidUntil: time.Now().Add(-time.Minute)})
	if gcb.GetMetricsHealth().Available || len(gcb.autoResumeBlockers(gcb.snapshotMetrics())) == 0 {
		t.Fatal("stale metrics allowed recovery")
	}
}

func TestMetricsHealthDataFailureBlocksDailyLossAndStreakTriggers(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.TotalDailyLoss.Enabled = true
	bot := &ownedEquityBot{}
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})
	cfg.Enabled = true
	if !gcb.RequiresTradeHistory() {
		t.Fatal("daily loss trigger must require a trade history source")
	}
	gcb.UpdateMetricsHealth(MetricsHealth{Available: false, CheckedAt: time.Now(), ValidUntil: time.Now().Add(time.Minute), Error: "position summary unavailable"})
	if !bot.held {
		t.Fatal("incomplete PnL metrics did not block opening when daily-loss protection is enabled")
	}
	if bot.cancelCount != 0 || bot.closeCount != 0 {
		t.Fatal("data-health hold should not cancel orders or liquidate positions")
	}
}

func TestUnknownPnLAssetMarksHealthUnavailableAndPreservesLastMetrics(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.TotalDailyLoss.Enabled = true
	bot := &assetPnLBot{}
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})
	cfg.Enabled = true
	now := time.Now()
	valid := MetricsHealth{Available: true, CheckedAt: now, ValidUntil: now.Add(time.Minute)}
	gcb.UpdateMetricsObservation(MetricsSnapshot{DailyPnL: -25}, valid)
	if bot.held {
		t.Fatal("valid initial observation unexpectedly blocked opening")
	}

	trades := &fakeTradeSource{trades: []TradeOutcome{{Key: "usd", NetPnL: -4, PnLAsset: "USD", FeeAsset: "USD", ClosedAt: now}}}
	feeder := NewMetricsFeeder(gcb, trades, nil, &circuitBreakerMockProvider{bots: []BotController{bot}}, MetricsFeederOptions{
		Now: func() time.Time { return now }, Location: time.UTC, RequireTradeHistory: true,
	})
	if _, err := feeder.Tick(context.Background()); err == nil {
		t.Fatal("USD trade was incorrectly compared to the USDT circuit-breaker threshold")
	}
	if health := gcb.GetMetricsHealth(); health.Available || health.Error == "" || !bot.held {
		t.Fatalf("invalid denomination did not fail closed: held=%v health=%+v", bot.held, health)
	}
	if got := gcb.snapshotMetrics().dailyPnL; got != -25 {
		t.Fatalf("failed observation replaced last valid metrics: got %v, want -25", got)
	}
	if bot.cancelCount != 0 || bot.closeCount != 0 {
		t.Fatal("denomination uncertainty should block new risk without cancelling or liquidating")
	}
}

func TestMissingTradeHistoryPropagatesToOpeningGate(t *testing.T) {
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.TotalDailyLoss.Enabled = true
	bot := &ownedEquityBot{}
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})
	cfg.Enabled = true
	feeder := NewMetricsFeeder(gcb, nil, nil, nil, MetricsFeederOptions{RequireTradeHistory: gcb.RequiresTradeHistory()})
	if _, err := feeder.Tick(t.Context()); err == nil {
		t.Fatal("missing trade history unexpectedly produced a healthy risk observation")
	}
	if !bot.held || gcb.GetMetricsHealth().Available {
		t.Fatalf("missing realized PnL did not propagate unavailable health to the opening gate: held=%v health=%+v", bot.held, gcb.GetMetricsHealth())
	}
}
