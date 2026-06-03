package backtest

import (
	"strings"
	"testing"
	"time"
)

func TestAutoBacktestSchedulerDefaultsAndState(t *testing.T) {
	s := NewAutoBacktestScheduler(nil, nil, AutoSchedulerConfig{})
	cfg := s.GetConfig()
	if cfg.ScheduleInterval != 6*time.Hour || cfg.ResultTTL != 24*time.Hour || cfg.MaxConcurrentTasks != 3 {
		t.Fatalf("default timing config = %#v", cfg)
	}
	if cfg.DefaultCapital != 10000 || cfg.DefaultExchange != "binance" || cfg.DefaultMarketType != "futures" {
		t.Fatalf("default market config = %#v", cfg)
	}
	if len(cfg.Symbols) != 4 {
		t.Fatalf("default symbols length = %d", len(cfg.Symbols))
	}
	if s.IsRunning() {
		t.Fatal("new scheduler should not be running")
	}

	s.Start()
	if !s.IsRunning() {
		t.Fatal("disabled scheduler still records running state")
	}
	s.Stop()
	if s.IsRunning() {
		t.Fatal("scheduler should stop")
	}
	s.Stop()

	updated := AutoSchedulerConfig{Enabled: false, ResultTTL: time.Minute, DefaultCapital: 123}
	s.UpdateConfig(updated)
	if got := s.GetConfig(); got.ResultTTL != time.Minute || got.DefaultCapital != 123 {
		t.Fatalf("updated config = %#v", got)
	}
	if err := s.TriggerPrecompute("BTCUSDT", "binance", "futures", "grid"); err == nil {
		t.Fatal("expected uninitialized services error")
	}
}

func TestAutoBacktestSchedulerResultQueriesAndExpiry(t *testing.T) {
	now := time.Now()
	s := NewAutoBacktestScheduler(nil, nil, AutoSchedulerConfig{ResultTTL: time.Hour})
	s.precomputedResults["BTCUSDT:binance:grid"] = &PrecomputedResult{
		Symbol: "BTCUSDT", Exchange: "binance", Strategy: "grid",
		Recommendation: &SmartParamsRecommendation{Confidence: 80},
		Result:         &BacktestResult{Metrics: Metrics{TotalReturn: 10}},
		GeneratedAt:    now, IsReady: true, TaskStatus: "completed",
	}
	s.precomputedResults["ETHUSDT:binance:dca"] = &PrecomputedResult{
		Symbol: "ETHUSDT", Exchange: "binance", Strategy: "dca",
		Recommendation: &SmartParamsRecommendation{Confidence: 90},
		Result:         &BacktestResult{Metrics: Metrics{TotalReturn: 3}},
		GeneratedAt:    now, IsReady: true, TaskStatus: "completed",
	}
	s.precomputedResults["SOLUSDT:binance:grid"] = &PrecomputedResult{
		Symbol: "SOLUSDT", Exchange: "binance", Strategy: "grid",
		GeneratedAt: now, TaskStatus: "running",
	}
	s.precomputedResults["OLD:binance:grid"] = &PrecomputedResult{
		Symbol: "OLD", Exchange: "binance", Strategy: "grid",
		GeneratedAt: now.Add(-2 * time.Hour), TaskStatus: "completed",
	}

	if key := s.getCacheKey("BTCUSDT", "binance", "grid"); key != "BTCUSDT:binance:grid" {
		t.Fatalf("cache key = %q", key)
	}
	if !s.hasValidResult("BTCUSDT:binance:grid") || !s.hasValidResult("SOLUSDT:binance:grid") {
		t.Fatal("expected fresh completed/running results to be valid")
	}
	if s.hasValidResult("OLD:binance:grid") || s.hasValidResult("missing") {
		t.Fatal("expired or missing result should be invalid")
	}

	all := s.GetPrecomputedResults()
	if len(all) != 4 || all[0].Symbol != "ETHUSDT" {
		t.Fatalf("sorted all results = %#v", all)
	}
	ready := s.GetReadyResults()
	if len(ready) != 2 || ready[0].Symbol != "BTCUSDT" {
		t.Fatalf("ready results = %#v", ready)
	}
	bySymbol := s.GetResultsBySymbol("BTCUSDT")
	if len(bySymbol) != 1 || bySymbol[0].Strategy != "grid" {
		t.Fatalf("by symbol = %#v", bySymbol)
	}
	if got := s.GetPrecomputedResult("BTCUSDT", "binance", "grid"); got == nil || got.Symbol != "BTCUSDT" {
		t.Fatalf("single result = %#v", got)
	}

	s.CleanExpiredResults()
	if _, ok := s.precomputedResults["OLD:binance:grid"]; ok {
		t.Fatal("expired result was not cleaned")
	}
}

func TestAutoBacktestSchedulerReasoningReportGrades(t *testing.T) {
	s := NewAutoBacktestScheduler(nil, nil, AutoSchedulerConfig{})
	base := &PrecomputedResult{
		Symbol: "BTCUSDT", Strategy: "grid",
		Recommendation: &SmartParamsRecommendation{Reasoning: "range market", Confidence: 88},
		Result:         &BacktestResult{Metrics: Metrics{TotalTrades: 12, WinRate: 60}},
	}

	if got := s.generateReasoningReport(&PrecomputedResult{}); got != "" {
		t.Fatalf("empty report = %q", got)
	}

	base.Result.Metrics.TotalReturn = 12
	base.Result.Metrics.SharpeRatio = 1.2
	if got := s.generateReasoningReport(base); !strings.Contains(got, "推薦使用") {
		t.Fatalf("good report = %s", got)
	}

	base.Result.Metrics.TotalReturn = 4
	base.Result.Metrics.SharpeRatio = 0.7
	if got := s.generateReasoningReport(base); !strings.Contains(got, "謹慎使用") {
		t.Fatalf("caution report = %s", got)
	}

	base.Result.Metrics.TotalReturn = -1
	base.Result.Metrics.SharpeRatio = 0.1
	if got := s.generateReasoningReport(base); !strings.Contains(got, "不建議使用") {
		t.Fatalf("bad report = %s", got)
	}
}
