package backtest

import (
	"math"
	"testing"
)

func TestTickKlinePricePathAndCrossHelpers(t *testing.T) {
	up := TickKline{Open: 100, High: 110, Low: 95, Close: 105}
	upPath := up.GetPricePath()
	if len(upPath) != 4 || upPath[1].Price != 95 || upPath[2].Price != 110 {
		t.Fatalf("up GetPricePath() = %+v, want open-low-high-close", upPath)
	}

	down := TickKline{Open: 105, High: 110, Low: 95, Close: 100}
	downPath := down.GetPricePath()
	if len(downPath) != 4 || downPath[1].Price != 110 || downPath[2].Price != 95 {
		t.Fatalf("down GetPricePath() = %+v, want open-high-low-close", downPath)
	}

	if !crossesDown(105, 100, 100) || crossesDown(100, 99, 100) {
		t.Fatalf("crossesDown() boundary behavior mismatch")
	}
	if !crossesUp(95, 100, 100) || crossesUp(100, 101, 100) {
		t.Fatalf("crossesUp() boundary behavior mismatch")
	}
}

func TestTickMatcherProcessPathAndLimits(t *testing.T) {
	matcher := NewTickMatcher(MatcherConfig{})
	if matcher.BuySlippage != 1.0001 || matcher.SellSlippage != 0.9999 || matcher.MaxVolumeRatio != 0.2 || matcher.MaxGridTradesPerMinute != 5 {
		t.Fatalf("NewTickMatcher defaults = %+v", matcher)
	}

	var callbacks []TickTrade
	matcher.SetTradeCallback(func(trade *TickTrade) {
		callbacks = append(callbacks, *trade)
	})

	kline := &TickKline{Timestamp: 60_000, Open: 100, High: 110, Low: 90, Close: 105, Volume: 10}
	orders := []TickOrder{
		{OrderID: "buy-99", Side: "buy", Price: 99, Size: 1, Strategy: "grid", StrategyID: "s1", AccountID: "a1", IsGrid: true, GridLevel: 1},
		{OrderID: "sell-108", Side: "sell", Price: 108, Size: 0.5, Strategy: "grid", StrategyID: "s1", AccountID: "a1", IsGrid: true, GridLevel: 2},
		{OrderID: "too-large", Side: "buy", Price: 98, Size: 10, IsGrid: true},
	}
	trades := matcher.ProcessPath(kline, orders, kline.Timestamp, 0)
	if len(trades) != 2 {
		t.Fatalf("ProcessPath() length = %d, want 2: %+v", len(trades), trades)
	}
	if len(callbacks) != 2 {
		t.Fatalf("callback count = %d, want 2", len(callbacks))
	}
	if trades[0].OrderID != "buy-99" || trades[0].Price <= 99 {
		t.Fatalf("first trade = %+v, want buy with positive slippage", trades[0])
	}
	if trades[1].OrderID != "sell-108" || trades[1].Price >= 108 {
		t.Fatalf("second trade = %+v, want sell with negative slippage", trades[1])
	}
	stats := matcher.GetStats()
	if stats.TotalVolumeUsed != 1.5 || stats.TotalGridTrades != 2 || stats.TotalTrades != 2 {
		t.Fatalf("GetStats() = %+v, want volume/grid/trade counters", stats)
	}
	if got := matcher.EstimateSlippage(trades); got <= 0 {
		t.Fatalf("EstimateSlippage() = %.8f, want positive cost", got)
	}
}

func TestTickMatcherProcessPathWithPositionLimits(t *testing.T) {
	matcher := NewTickMatcher(MatcherConfig{
		BuySlippage:            1.01,
		SellSlippage:           0.99,
		MaxVolumeRatio:         1,
		MaxGridTradesPerMinute: 1,
	})
	kline := &TickKline{Open: 100, High: 110, Low: 90, Close: 105, Volume: 100}
	orders := []TickOrder{
		{OrderID: "buy-blocked-by-long-limit", Side: "buy", Price: 99, Size: 2, IsGrid: true},
		{OrderID: "sell-ok", Side: "sell", Price: 108, Size: 1, IsGrid: true},
		{OrderID: "sell-grid-limit", Side: "sell", Price: 107, Size: 1, IsGrid: true},
	}
	trades := matcher.ProcessPathWithLimit(kline, orders, 1, 0, 1, 5)
	if len(trades) != 1 {
		t.Fatalf("ProcessPathWithLimit() length = %d, want 1: %+v", len(trades), trades)
	}
	if trades[0].OrderID != "sell-ok" {
		t.Fatalf("filled order = %q, want sell-ok", trades[0].OrderID)
	}

	matcher.Reset()
	orders = []TickOrder{{OrderID: "short-blocked", Side: "sell", Price: 108, Size: 2}}
	if got := matcher.ProcessPathWithLimit(kline, orders, 1, 0, 10, 1); len(got) != 0 {
		t.Fatalf("ProcessPathWithLimit(short limit) length = %d, want 0", len(got))
	}
}

func TestTickMatcherValidationAndUtilityMethods(t *testing.T) {
	matcher := NewTickMatcher(MatcherConfig{BuySlippage: 1.02, SellSlippage: 0.98, MaxVolumeRatio: 0.5, MaxGridTradesPerMinute: 2})
	valid := TickOrder{OrderID: "ok", Side: "buy", Price: 100, Size: 1}
	if err := matcher.ValidateOrder(valid); err != nil {
		t.Fatalf("ValidateOrder(valid) error = %v", err)
	}
	for _, order := range []TickOrder{
		{Side: "buy", Price: 100, Size: 1},
		{OrderID: "bad-price", Side: "buy", Price: 0, Size: 1},
		{OrderID: "bad-size", Side: "buy", Price: 100, Size: 0},
		{OrderID: "bad-side", Side: "hold", Price: 100, Size: 1},
	} {
		if err := matcher.ValidateOrder(order); err == nil {
			t.Fatalf("ValidateOrder(%+v) expected error", order)
		}
	}

	if got := matcher.CalculateEffectivePrice(100, "buy", 1); got != 102 {
		t.Fatalf("CalculateEffectivePrice(buy) = %.2f, want 102", got)
	}
	if got := matcher.CalculateEffectivePrice(100, "sell", 1); got != 98 {
		t.Fatalf("CalculateEffectivePrice(sell) = %.2f, want 98", got)
	}

	clone := matcher.Clone()
	if clone == matcher || clone.BuySlippage != matcher.BuySlippage || clone.SellSlippage != matcher.SellSlippage {
		t.Fatalf("Clone() = %+v, want copied config and new pointer", clone)
	}
	matcher.ResetMinute(50)
	if !matcher.canTrade(10, false, 0, "buy") || matcher.canTrade(30, false, 0, "buy") {
		t.Fatalf("canTrade() volume limit mismatch")
	}
	matcher.Reset()
	if stats := matcher.GetStats(); stats.TotalVolumeUsed != 0 || stats.TotalTrades != 0 {
		t.Fatalf("Reset stats = %+v, want zero", stats)
	}

	if CalculatePriceImpact(1, 0, 100) != 0 {
		t.Fatalf("CalculatePriceImpact(volume=0) should be 0")
	}
	if CalculatePriceImpact(0.5, 100, 100) != 0 {
		t.Fatalf("CalculatePriceImpact(<1%% volume) should be 0")
	}
	if got := CalculatePriceImpact(1000, 100, 100); math.Abs(got-0.005) > 1e-12 {
		t.Fatalf("CalculatePriceImpact(clamped) = %.6f, want 0.005", got)
	}
	if CalculateEffectiveSpread(100, 0) != 0 || CalculateEffectiveSpread(0, 100) != 0 {
		t.Fatalf("CalculateEffectiveSpread zero guard mismatch")
	}
	if got := CalculateEffectiveSpread(99, 100); got != 0.01 {
		t.Fatalf("CalculateEffectiveSpread() = %.4f, want 0.01", got)
	}
}
