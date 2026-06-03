package backtest

import (
	"math"
	"testing"
)

func sampleAggTrades() []AggTradeRow {
	return []AggTradeRow{
		{AggTradeID: 1, Price: 101, Quantity: 0.5, Timestamp: 1000, IsBuyerMaker: false},
		{AggTradeID: 2, Price: 99, Quantity: 0.7, Timestamp: 2000, IsBuyerMaker: true},
		{AggTradeID: 3, Price: 103, Quantity: 1.1, Timestamp: 3000, IsBuyerMaker: false},
		{AggTradeID: 4, Price: 98, Quantity: 0.9, Timestamp: 4000, IsBuyerMaker: true},
	}
}

func TestRealTickMatcherConstructsAndMatchesOrders(t *testing.T) {
	matcher := NewRealTickMatcher(-1, -1)
	if matcher.SlippageBps != 1 || matcher.LatencyMs != 10 {
		t.Fatalf("NewRealTickMatcher() = slippage %.2f latency %d, want defaults 1/10", matcher.SlippageBps, matcher.LatencyMs)
	}

	var callbackTrades []TickTrade
	matcher.SetTradeCallback(func(trade *TickTrade) {
		callbackTrades = append(callbackTrades, *trade)
	})
	matcher.LoadAggTrades(sampleAggTrades())

	orders := []TickOrder{
		{OrderID: "sell-high", Side: "sell", Price: 102, Size: 0.2, Strategy: "grid", StrategyID: "s1", AccountID: "a1", GridLevel: 2},
		{OrderID: "buy-low", Side: "buy", Price: 100, Size: 0.3, Strategy: "grid", StrategyID: "s1", AccountID: "a1", GridLevel: 1},
	}

	trades := matcher.ProcessOrders(orders)
	if len(trades) != 2 {
		t.Fatalf("ProcessOrders() length = %d, want 2: %+v", len(trades), trades)
	}
	if len(callbackTrades) != 2 {
		t.Fatalf("callback trade count = %d, want 2", len(callbackTrades))
	}
	if trades[0].OrderID != "buy-low" || trades[0].Side != "buy" {
		t.Fatalf("first trade = %+v, want buy-low filled by price 99 tick", trades[0])
	}
	if trades[0].Price <= 99 {
		t.Fatalf("buy fill price = %.4f, want slippage above market trade", trades[0].Price)
	}
	if trades[1].OrderID != "sell-high" || trades[1].Side != "sell" {
		t.Fatalf("second trade = %+v, want sell-high filled by price 103 tick", trades[1])
	}
	if trades[1].Price >= 103 {
		t.Fatalf("sell fill price = %.4f, want slippage below market trade", trades[1].Price)
	}

	current, total := matcher.GetProgress()
	if total != 4 || current == 0 {
		t.Fatalf("GetProgress() = %d/%d, want processed progress over 4 ticks", current, total)
	}
	if matcher.GetProgressPercent() <= 0 || matcher.GetProgressPercent() > 100 {
		t.Fatalf("GetProgressPercent() = %.2f, want in (0,100]", matcher.GetProgressPercent())
	}
	if matcher.GetCurrentTick() == nil {
		t.Fatalf("GetCurrentTick() = nil, want current tick while matcher has remaining data")
	}

	stats := matcher.GetStats()
	if stats.TotalTicks != 4 || stats.ProcessedTicks != current {
		t.Fatalf("GetStats() = %+v, want total ticks and processed ticks", stats)
	}
	if matcher.EstimateSlippage(trades) == 0 {
		t.Fatalf("EstimateSlippage() = 0, want non-zero slippage")
	}
}

func TestRealTickMatcherWindowSnapshotResetAndClone(t *testing.T) {
	matcher := NewRealTickMatcher(5, 20)
	matcher.LoadAggTrades(sampleAggTrades())

	if got := matcher.ProcessOrders(nil); len(got) != 0 {
		t.Fatalf("ProcessOrders(nil) length = %d, want 0", len(got))
	}

	orders := []TickOrder{
		{OrderID: "window-buy", Side: "buy", Price: 99, Size: 0.1},
		{OrderID: "window-sell", Side: "sell", Price: 102, Size: 0.1},
	}
	trades := matcher.ProcessOrdersWithWindow(orders, 1500, 3500)
	if len(trades) != 2 {
		t.Fatalf("ProcessOrdersWithWindow() length = %d, want 2: %+v", len(trades), trades)
	}
	if got := matcher.ProcessOrdersWithWindow(orders, 5000, 6000); len(got) != 0 {
		t.Fatalf("ProcessOrdersWithWindow(out of range) length = %d, want 0", len(got))
	}
	if got := matcher.ProcessOrdersWithWindow(orders, 2500, 2500); len(got) != 0 {
		t.Fatalf("ProcessOrdersWithWindow(empty window) length = %d, want 0", len(got))
	}

	matcher.tickIndex = 4
	snapshot := matcher.GetMarketSnapshot(3)
	if snapshot == nil {
		t.Fatalf("GetMarketSnapshot() = nil")
	}
	if snapshot.LastPrice != 98 || snapshot.TradeCount != 3 || snapshot.Volume24h != 2.7 {
		t.Fatalf("GetMarketSnapshot() = %+v, want last 3 ticks summarized", snapshot)
	}
	if snapshot.BestBidPrice != 103 || snapshot.BestAskPrice != 98 {
		t.Fatalf("snapshot bid/ask = %.2f/%.2f, want 103/98", snapshot.BestBidPrice, snapshot.BestAskPrice)
	}

	clone := matcher.Clone()
	if clone == matcher || clone.SlippageBps != 5 || clone.LatencyMs != 20 || clone.GetProgressPercent() != 0 {
		t.Fatalf("Clone() = %+v, want fresh matcher with copied config/data", clone)
	}
	matcher.Reset()
	if current, total := matcher.GetProgress(); current != 0 || total != 4 {
		t.Fatalf("Reset progress = %d/%d, want 0/4", current, total)
	}

	empty := NewRealTickMatcher(0, 0)
	if empty.GetMarketSnapshot(5) != nil {
		t.Fatalf("empty GetMarketSnapshot() = non-nil")
	}
	if empty.GetProgressPercent() != 100 {
		t.Fatalf("empty GetProgressPercent() = %.2f, want 100", empty.GetProgressPercent())
	}
}

func TestTickMatcherPureHelpers(t *testing.T) {
	buy := TickOrder{Side: "buy", Price: 100}
	sell := TickOrder{Side: "sell", Price: 100}
	lowTrade := AggTradeRow{Price: 99}
	highTrade := AggTradeRow{Price: 101}

	if !ShouldMatchOrder(buy, lowTrade) || ShouldMatchOrder(buy, highTrade) {
		t.Fatalf("ShouldMatchOrder() buy branch mismatch")
	}
	if !ShouldMatchOrder(sell, highTrade) || ShouldMatchOrder(sell, lowTrade) {
		t.Fatalf("ShouldMatchOrder() sell branch mismatch")
	}

	if got := CalculateFillPrice(buy, lowTrade, 10); math.Abs(got-99.099) > 1e-9 {
		t.Fatalf("CalculateFillPrice(buy) = %.6f, want 99.099", got)
	}
	if got := CalculateFillPrice(sell, highTrade, 10); math.Abs(got-100.899) > 1e-9 {
		t.Fatalf("CalculateFillPrice(sell) = %.6f, want 100.899", got)
	}
}
