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

type fundingCarrySumStub struct {
	total float64
	err   error
	calls int
}

func (s *fundingCarrySumStub) GetFundingPaymentsSumByScope(_, _, _, _, _ string, _, _ time.Time) (float64, error) {
	s.calls++
	return s.total, s.err
}

func TestReadFundingCarrySumFailsClosed(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name  string
		total float64
		err   error
	}{
		{name: "non-finite", total: math.NaN()},
		{name: "query error", total: 10, err: errors.New("database unavailable")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fundingCarrySumStub{total: tt.total, err: tt.err}
			if _, err := readFundingCarrySum(reader, "binance", "BTCUSDT", "scope-a", now.Add(-time.Hour), now); err == nil {
				t.Fatal("invalid funding income was accepted")
			}
			if reader.calls != 1 {
				t.Fatalf("query calls=%d, want 1", reader.calls)
			}
		})
	}
	reader := &fundingCarrySumStub{total: -1.25}
	if got, err := readFundingCarrySum(reader, "binance", "BTCUSDT", "scope-a", now.Add(-time.Hour), now); err != nil || got != -1.25 {
		t.Fatalf("valid negative funding income=%v err=%v", got, err)
	}
	if _, err := readFundingCarrySum(reader, "binance", "BTCUSDT", "", now.Add(-time.Hour), now); err == nil {
		t.Fatal("empty account scope was accepted")
	}
}

func TestMergeFundingCarryDailyTotalsRejectsInvalidRowsAndOverflow(t *testing.T) {
	tests := []struct {
		name   string
		start  map[string]float64
		source map[string]float64
	}{
		{name: "invalid date", source: map[string]float64{"yesterday": 1}},
		{name: "non-finite amount", source: map[string]float64{"2026-10-01": math.Inf(1)}},
		{name: "overflow", start: map[string]float64{"2026-10-01": math.MaxFloat64}, source: map[string]float64{"2026-10-01": math.MaxFloat64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := mergeFundingCarryDailyTotals(tt.start, tt.source); err == nil {
				t.Fatal("invalid daily funding aggregate was accepted")
			}
		})
	}
	totals := map[string]float64{}
	if err := mergeFundingCarryDailyTotals(totals, map[string]float64{"2026-10-01": 1.5}); err != nil || totals["2026-10-01"] != 1.5 {
		t.Fatalf("valid daily total=%v err=%v", totals, err)
	}
}

func TestFundingPaymentItemFromRecordRequiresExactScopeAndFiniteAmount(t *testing.T) {
	valid := &storage.FundingPayment{
		Exchange: "Binance", Symbol: "BTCUSDT", AccountScope: "scope-a", Asset: "USDT",
		Income: 1.123456789, TradeTime: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	item, err := fundingPaymentItemFromRecord(valid, "scope-a", "binance")
	if err != nil || item.Income != 1.12345679 {
		t.Fatalf("valid scoped funding row=%+v err=%v", item, err)
	}
	tests := []struct {
		name    string
		payment *storage.FundingPayment
	}{
		{name: "nil row"},
		{name: "wrong scope", payment: &storage.FundingPayment{Exchange: "binance", AccountScope: "scope-b", Asset: "USDT"}},
		{name: "wrong exchange", payment: &storage.FundingPayment{Exchange: "okx", AccountScope: "scope-a", Asset: "USDT"}},
		{name: "missing asset", payment: &storage.FundingPayment{Exchange: "binance", AccountScope: "scope-a"}},
		{name: "nan amount", payment: &storage.FundingPayment{Exchange: "binance", AccountScope: "scope-a", Asset: "USDT", Income: math.NaN()}},
		{name: "infinite amount", payment: &storage.FundingPayment{Exchange: "binance", AccountScope: "scope-a", Asset: "USDT", Income: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := fundingPaymentItemFromRecord(tt.payment, "scope-a", "binance"); err == nil {
				t.Fatal("invalid or out-of-scope funding row was accepted")
			}
		})
	}
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
	if got, ok := roundProfitToPrecision(1.123456789, 1e8); !ok || got != 1.12345679 {
		t.Fatalf("8-decimal precision rounding = %v, %v; want 1.12345679, true", got, ok)
	}
	if got, ok := roundProfitToPrecision(math.MaxFloat64, 1e8); !ok || got != math.MaxFloat64 {
		t.Fatalf("extreme finite funding amount must not overflow: %v, %v", got, ok)
	}
	if _, ok := roundProfitToPrecision(1, 0); ok {
		t.Fatal("zero precision was accepted")
	}
}

func TestAddFiniteProfitToMap(t *testing.T) {
	totals := map[string]float64{}
	if err := addFiniteProfitToMap(totals, "2026-10-01", 1.25); err != nil {
		t.Fatal(err)
	}
	if err := addFiniteProfitToMap(totals, "2026-10-01", 2.5); err != nil {
		t.Fatal(err)
	}
	if totals["2026-10-01"] != 3.75 {
		t.Fatalf("finite daily total=%v, want 3.75", totals["2026-10-01"])
	}
	for _, value := range []float64{math.NaN(), math.Inf(1)} {
		if err := addFiniteProfitToMap(totals, "2026-10-01", value); err == nil {
			t.Fatalf("non-finite or overflowing value %v was accepted", value)
		}
	}
	extremeTotals := map[string]float64{"2026-10-01": math.MaxFloat64}
	if err := addFiniteProfitToMap(extremeTotals, "2026-10-01", math.MaxFloat64); err == nil {
		t.Fatal("overflowing daily total was accepted")
	}
	if err := addFiniteProfitToMap(nil, "2026-10-01", 1); err == nil {
		t.Fatal("missing trend totals map was accepted")
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

func TestValidateProfitStatisticsSnapshot(t *testing.T) {
	valid := &storage.Statistics{TotalTrades: 1, TotalVolume: 2, TotalPnL: -1, TotalFee: 0.1, WinRate: 0.5}
	if err := validateProfitStatisticsSnapshot(valid); err != nil {
		t.Fatalf("valid summary rejected: %v", err)
	}
	invalid := []*storage.Statistics{
		nil,
		{TotalTrades: -1},
		{TotalVolume: math.NaN()},
		{TotalPnL: math.Inf(1)},
		{WinRate: 1.1},
	}
	for i, summary := range invalid {
		if err := validateProfitStatisticsSnapshot(summary); err == nil {
			t.Fatalf("invalid summary %d accepted", i)
		}
	}
}

func TestValidateStrategyPnLStreamRejectsCorruptFinancialRows(t *testing.T) {
	base := storage.PnLBySymbol{Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", PnLAsset: "USDT", TotalTrades: 2, TotalVolume: 10, WinRate: .5, ExchangeWinRate: .25}
	tests := []struct {
		name   string
		mutate func(*storage.PnLBySymbol)
	}{
		{name: "nil row"},
		{name: "nan net pnl", mutate: func(row *storage.PnLBySymbol) { row.TotalPnL = math.NaN() }},
		{name: "infinite volume", mutate: func(row *storage.PnLBySymbol) { row.TotalVolume = math.Inf(1) }},
		{name: "invalid win rate", mutate: func(row *storage.PnLBySymbol) { row.WinRate = 1.1 }},
		{name: "negative trades", mutate: func(row *storage.PnLBySymbol) { row.TotalTrades = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var row *storage.PnLBySymbol
			if tt.mutate != nil {
				copy := base
				tt.mutate(&copy)
				row = &copy
			}
			if err := validateStrategyPnLStream(row); err == nil {
				t.Fatal("corrupt PnL row was accepted")
			}
		})
	}
	if err := validateStrategyPnLStream(&base); err != nil {
		t.Fatalf("valid PnL row rejected: %v", err)
	}
}

func TestRoundStrategyProfitAmountsAvoidsOverflowAndRejectsNonFiniteValues(t *testing.T) {
	profit := &StrategyProfit{TotalProfit: math.MaxFloat64, WithdrawnProfit: -math.MaxFloat64}
	if err := roundStrategyProfitAmounts(profit); err != nil {
		t.Fatalf("finite extreme strategy amounts must not overflow during cents rounding: %v", err)
	}
	if profit.TotalProfit != math.MaxFloat64 || profit.WithdrawnProfit != -math.MaxFloat64 {
		t.Fatalf("extreme finite values changed unexpectedly: %+v", profit)
	}
	profit.ExchangeTotalProfit = math.NaN()
	if err := roundStrategyProfitAmounts(profit); err == nil {
		t.Fatal("non-finite strategy amount was accepted")
	}
}
