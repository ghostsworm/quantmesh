package backtest

import (
	"strings"
	"testing"

	"quantmesh/exchange"
)

func makeRiskCandles(close, volume float64, count int) []*exchange.Candle {
	candles := make([]*exchange.Candle, 0, count)
	for i := 0; i < count; i++ {
		candles = append(candles, &exchange.Candle{
			Close:     close,
			Volume:    volume,
			Timestamp: int64(i+1) * 60_000,
		})
	}
	return candles
}

func TestRiskSimulatorDefaultsAndInvalidInputs(t *testing.T) {
	cfg := DefaultRiskSimulatorConfig()
	if cfg.VolumeMultiplier != 3 || cfg.AverageWindow != 20 || cfg.MinDepthUSDT != 10000 || cfg.DepthWindow != 10 {
		t.Fatalf("DefaultRiskSimulatorConfig() = %+v", cfg)
	}

	rs := NewRiskSimulator(&RiskSimulatorConfig{VolumeMultiplier: 2, AverageWindow: 3, Direction: "SHORT"})
	if rs.cfg.VolumeMultiplier != 2 || rs.cfg.AverageWindow != 3 || rs.cfg.Direction != "SHORT" {
		t.Fatalf("NewRiskSimulator(custom) cfg = %+v", rs.cfg)
	}
	if skip, reason := rs.Check(nil, 0); skip || reason != "" {
		t.Fatalf("Check(nil) = %v/%q, want false/empty", skip, reason)
	}
	if skip, reason := rs.Check(makeRiskCandles(100, 10, 2), 5); skip || reason != "" {
		t.Fatalf("Check(out of range) = %v/%q, want false/empty", skip, reason)
	}
}

func TestRiskSimulatorVolumeTriggerRecoverAndSkippedBuys(t *testing.T) {
	rs := NewRiskSimulator(&RiskSimulatorConfig{VolumeMultiplier: 2, AverageWindow: 3})
	candles := makeRiskCandles(100, 10, 5)
	candles[3].Close = 90
	candles[3].Volume = 25

	skip, reason := rs.Check(candles, 3)
	if !skip || !strings.Contains(reason, "低於均線") {
		t.Fatalf("Check(trigger) = %v/%q, want long volume risk", skip, reason)
	}
	rs.RecordSkippedBuy()
	rs.RecordSkippedBuy()

	candles[4].Close = 105
	candles[4].Volume = 10
	skip, reason = rs.Check(candles, 4)
	if skip || reason != "" {
		t.Fatalf("Check(recover) = %v/%q, want recovered", skip, reason)
	}
	interventions := rs.GetInterventions()
	if len(interventions) != 1 {
		t.Fatalf("GetInterventions() length = %d, want 1", len(interventions))
	}
	if interventions[0].SkippedBuys != 2 || interventions[0].Duration == 0 || interventions[0].RiskType != "volume_spike" {
		t.Fatalf("intervention = %+v, want skipped buys and volume risk", interventions[0])
	}
	if interventions[0].TimeStr == "" {
		t.Fatalf("intervention TimeStr is empty")
	}
}

func TestRiskSimulatorDepthRiskAndOpenInterventionFlush(t *testing.T) {
	candles := makeRiskCandles(100, 10, 4)
	depths := []*DepthSnapshotForBacktest{
		{TotalDepth: 20_000},
		{TotalDepth: 20_000},
		{TotalDepth: 5_000},
		{TotalDepth: 5_000},
	}
	rs := NewRiskSimulatorWithDepth(&RiskSimulatorConfig{
		AverageWindow:      2,
		VolumeMultiplier:   10,
		MinDepthUSDT:       10_000,
		DepthDropThreshold: 0.5,
		DepthWindow:        2,
	}, depths)

	skip, reason := rs.Check(candles, 2)
	if !skip || !strings.Contains(reason, "深度不足") {
		t.Fatalf("Check(depth risk) = %v/%q", skip, reason)
	}
	rs.RecordSkippedBuy()
	interventions := rs.GetInterventions()
	if len(interventions) != 1 || interventions[0].RiskType != "depth_risk" || interventions[0].SkippedBuys != 1 {
		t.Fatalf("GetInterventions() = %+v, want flushed depth intervention", interventions)
	}
}

func TestRiskSimulatorShortDirectionAndTimestampFormat(t *testing.T) {
	rs := NewRiskSimulator(&RiskSimulatorConfig{VolumeMultiplier: 2, AverageWindow: 3, Direction: "SHORT"})
	candles := makeRiskCandles(100, 10, 4)
	candles[3].Close = 110
	candles[3].Volume = 25

	skip, reason := rs.Check(candles, 3)
	if !skip || !strings.Contains(reason, "高於均線") {
		t.Fatalf("Check(short trigger) = %v/%q", skip, reason)
	}

	if got := formatTimestamp(60_000_000_000); got != "1971-11-26 18:40:00" {
		t.Fatalf("formatTimestamp(ms) = %q", got)
	}
	if got := formatTimestamp(60); got != "1970-01-01 08:01:00" {
		t.Fatalf("formatTimestamp(sec) = %q", got)
	}
}

func TestBuildComparisonResult(t *testing.T) {
	if BuildComparisonResult(nil, &BacktestResult{}, nil) != nil {
		t.Fatalf("BuildComparisonResult(nil, withRisk) should be nil")
	}

	noRisk := &BacktestResult{Metrics: Metrics{TotalReturn: 10, MaxDrawdown: 4, TotalTrades: 5}}
	withRisk := &BacktestResult{Metrics: Metrics{TotalReturn: 12.5, MaxDrawdown: 2.5, TotalTrades: 3}}
	result := BuildComparisonResult(noRisk, withRisk, []RiskIntervention{{SkippedBuys: 2}, {SkippedBuys: 3}})
	if result == nil || result.NoRiskResult != noRisk || result.WithRiskResult != withRisk {
		t.Fatalf("BuildComparisonResult() = %+v, want linked results", result)
	}
	if result.Comparison.ReturnDiff != 2.5 || result.Comparison.DrawdownDiff != -1.5 || result.Comparison.TradeCountDiff != -2 {
		t.Fatalf("comparison metrics = %+v", result.Comparison)
	}
	if result.Comparison.RiskInterventionCount != 2 || result.Comparison.SkippedSignals != 5 {
		t.Fatalf("comparison intervention metrics = %+v", result.Comparison)
	}
}
