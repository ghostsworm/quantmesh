package web

import (
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/storage"
)

type fundingTotalsStub struct {
	calls    int
	failCall int
}

func (s *fundingTotalsStub) GetFundingPaymentsSum(_, _ string, _, _ time.Time) (float64, error) {
	s.calls++
	if s.calls == s.failCall {
		return 0, errors.New("database unavailable")
	}
	return float64(s.calls), nil
}

func TestReadFundingProfitTotalsRejectsAnyFailedWindow(t *testing.T) {
	for failCall := 1; failCall <= 4; failCall++ {
		t.Run(string(rune('0'+failCall)), func(t *testing.T) {
			reader := &fundingTotalsStub{failCall: failCall}
			now := time.Now()
			_, err := readFundingProfitTotals(reader, "account", "binance", now.AddDate(-6, 0, 0), now.Add(-time.Hour), now.Add(-24*time.Hour), now.Add(-30*24*time.Hour), now)
			if err == nil {
				t.Fatalf("expected error for failed funding query %d", failCall)
			}
			if reader.calls != failCall {
				t.Fatalf("query calls=%d, want stop at failed query %d", reader.calls, failCall)
			}
		})
	}
}

func TestReadFundingProfitTotalsReturnsAllWindows(t *testing.T) {
	reader := &fundingTotalsStub{}
	now := time.Now()
	got, err := readFundingProfitTotals(reader, "account", "binance", now.AddDate(-6, 0, 0), now.Add(-time.Hour), now.Add(-24*time.Hour), now.Add(-30*24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Today != 2 || got.Week != 3 || got.Month != 4 {
		t.Fatalf("funding totals=%+v", got)
	}
}

func TestAddFiniteProfitValuesRejectsNonFiniteAndOverflow(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
	}{
		{name: "nan", values: []float64{math.NaN()}},
		{name: "positive infinity", values: []float64{math.Inf(1)}},
		{name: "negative infinity", values: []float64{math.Inf(-1)}},
		{name: "sum overflow", values: []float64{math.MaxFloat64, math.MaxFloat64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := addFiniteProfitValues(tt.values...); ok {
				t.Fatalf("addFiniteProfitValues() = %v, true; want rejected value", got)
			}
		})
	}
	if got, ok := addFiniteProfitValues(-3, 1.25); !ok || got != -1.75 {
		t.Fatalf("finite negative-profit sum = %v, %v; want -1.75, true", got, ok)
	}
	if got, ok := roundProfitToCents(math.MaxFloat64); !ok || got != math.MaxFloat64 {
		t.Fatalf("rounding a finite extreme must not overflow: %v, %v", got, ok)
	}
	if got, ok := roundProfitToCents(math.NaN()); ok || got != 0 {
		t.Fatalf("rounding NaN must fail closed: %v, %v", got, ok)
	}
}

func TestMergeProfitStatisticsRejectsInvalidOrOverflowWithoutPartialMutation(t *testing.T) {
	tests := []struct {
		name   string
		source *storage.Statistics
	}{
		{name: "nil summary"},
		{name: "nan pnl", source: &storage.Statistics{TotalPnL: math.NaN()}},
		{name: "infinite volume", source: &storage.Statistics{TotalVolume: math.Inf(1)}},
		{name: "invalid win rate", source: &storage.Statistics{WinRate: 1.1}},
		{name: "volume overflow", source: &storage.Statistics{TotalVolume: math.MaxFloat64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &storage.Statistics{TotalVolume: math.MaxFloat64, TotalPnL: 4}
			before := *target
			err := mergeProfitStatistics(target, tt.source)
			if err == nil {
				t.Fatal("invalid statistics were accepted")
			}
			if *target != before {
				t.Fatalf("failed merge partially mutated target: before=%+v after=%+v", before, *target)
			}
		})
	}
}
