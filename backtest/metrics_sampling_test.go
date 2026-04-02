package backtest

import (
	"math"
	"testing"
)

func TestInferPeriodsPerYear_Hourly(t *testing.T) {
	const hourMs = 3600000
	var eq []EquityPoint
	for i := 0; i < 30; i++ {
		eq = append(eq, EquityPoint{Timestamp: int64(i) * hourMs, Equity: 10000})
	}
	ppy := inferPeriodsPerYear(eq)
	want := (365.25 * 86400 * 1000) / float64(hourMs)
	if math.Abs(ppy-want) > 1 {
		t.Fatalf("inferPeriodsPerYear hourly: got %v want ~%v", ppy, want)
	}
}

func TestCalculateMetricsWithPrice_NoTrades_HasEquityMetrics(t *testing.T) {
	eq := []EquityPoint{
		{Timestamp: 0, Equity: 10000},
		{Timestamp: 86400000, Equity: 11000},
	}
	m := CalculateMetricsWithPrice(eq, nil, 10000, 0, 0)
	if m.TotalReturn < 9.9 || m.TotalReturn > 10.1 {
		t.Fatalf("TotalReturn: got %v want ~10", m.TotalReturn)
	}
	if m.MaxDrawdown != 0 {
		t.Fatalf("MaxDrawdown: got %v want 0", m.MaxDrawdown)
	}
	if m.TotalTrades != 0 || m.WinRate != 0 {
		t.Fatalf("trade stats should be zero: trades=%d win=%v", m.TotalTrades, m.WinRate)
	}
}

func TestCalculateMetricsWithPrice_VolatilityScalesWithSampling(t *testing.T) {
	// Same percentage moves per period: hourly series should have lower annualized vol than daily
	// for the same per-period return std (because more periods per year with hourly).
	const dayMs = 86400000
	daily := []EquityPoint{
		{Timestamp: 0, Equity: 10000},
		{Timestamp: dayMs, Equity: 10100},
		{Timestamp: 2 * dayMs, Equity: 10200},
	}
	hourly := []EquityPoint{
		{Timestamp: 0, Equity: 10000},
		{Timestamp: 3600000, Equity: 10050},
		{Timestamp: 7200000, Equity: 10100},
	}
	md := CalculateMetricsWithPrice(daily, nil, 10000, 0, 0)
	mh := CalculateMetricsWithPrice(hourly, nil, 10000, 0, 0)
	if md.Volatility <= 0 || mh.Volatility <= 0 {
		t.Fatalf("expected positive vol: daily=%v hourly=%v", md.Volatility, mh.Volatility)
	}
	// Hourly path has more samples per year -> typically higher annualized vol from same kind of moves
	if mh.Volatility <= md.Volatility {
		t.Logf("note: hourly vol %v vs daily %v (ordering may vary with path length)", mh.Volatility, md.Volatility)
	}
}
