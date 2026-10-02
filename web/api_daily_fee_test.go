package web

import (
	"math"
	"testing"
	"time"

	"quantmesh/storage"
)

func TestDailyFeeTotalRequiresQuoteCurrencyAndFiniteValues(t *testing.T) {
	tests := []struct {
		name  string
		fees  map[string]float64
		quote string
		want  float64
		valid bool
	}{
		{name: "quote asset", fees: map[string]float64{"usdt": 0.25, "USDT": 0.5}, quote: "USDT", want: 0.75, valid: true},
		{name: "zero unknown fee", fees: map[string]float64{"": 0}, quote: "USDT", want: 0, valid: true},
		{name: "non quote asset", fees: map[string]float64{"BNB": 0.01}, quote: "USDT"},
		{name: "missing quote asset", fees: map[string]float64{"USDT": 0.01}},
		{name: "non finite fee", fees: map[string]float64{"USDT": math.Inf(1)}, quote: "USDT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dailyFeeTotalByQuote(tt.fees, tt.quote)
			if tt.valid && (err != nil || got != tt.want) {
				t.Fatalf("dailyFeeTotalByQuote() = %v, %v; want %v, nil", got, err, tt.want)
			}
			if !tt.valid && err == nil {
				t.Fatalf("dailyFeeTotalByQuote() accepted unsafe fees: %v", got)
			}
		})
	}
}

func TestDailyFeeTotalConvertsOnlyBaseAssetAtExecutionPrices(t *testing.T) {
	fees := map[string]float64{"USDT": 0.5, "BTC": 0.01}
	quoteValues := map[string]float64{"BTC": 600}
	got, err := dailyFeeTotalByQuoteAndBase(fees, quoteValues, nil, "usdt", "btc", true)
	if err != nil || got != 600.5 {
		t.Fatalf("quote plus execution-price converted base fees = %v, %v", got, err)
	}
	if _, err := dailyFeeTotalByQuoteAndBase(fees, quoteValues, nil, "USDT", "BTC", false); err == nil {
		t.Fatal("futures/base-asset fee must not be valued using spot conversion semantics")
	}
	if _, err := dailyFeeTotalByQuoteAndBase(map[string]float64{"BNB": 0.01}, map[string]float64{"BNB": 500}, nil, "USDT", "BTC", true); err == nil {
		t.Fatal("third-asset fees require an actual historical exchange rate, not trade price")
	}
	if got, err := dailyFeeTotalByQuoteAndBase(map[string]float64{"BTC": -0.01}, map[string]float64{"BTC": -600}, nil, "USDT", "BTC", true); err != nil || got != -600 {
		t.Fatalf("base-asset rebate should retain its sign: %v, %v", got, err)
	}
}

func TestDailyFeeTotalRequiresVerifiedThirdAssetConversion(t *testing.T) {
	fees := map[string]float64{"BNB": 0.01}
	got, err := dailyFeeTotalByQuoteAndBase(fees, nil, map[string]float64{"BNB": 6.2}, "USDT", "BTC", true, map[string]int{"BNB": 0})
	if err != nil || math.Abs(got-6.2) > 1e-9 {
		t.Fatalf("verified historical conversion = %v, %v", got, err)
	}
	if _, err := dailyFeeTotalByQuoteAndBase(fees, nil, map[string]float64{"BNB": 6.2}, "USDT", "BTC", true, map[string]int{"BNB": 1}); err == nil {
		t.Fatal("expected unknown fill to fail closed")
	}
	if _, err := dailyFeeTotalByQuoteAndBase(map[string]float64{"BNB": 0}, nil, nil, "USDT", "BTC", true, map[string]int{"BNB": 1}); err == nil {
		t.Fatal("expected zero aggregate with unknown fee evidence to fail closed")
	}
}

func TestDailyPnLFeeDeductionDoesNotDoubleCountSpotBaseFees(t *testing.T) {
	fees := map[string]float64{"USDT": 0.5, "BTC": 0.001, "BNB": 0.001}
	quoteValues := map[string]float64{"BTC": 0.6, "BNB": 0.62}
	got, err := dailyPnLFeeDeduction(1.72, fees, quoteValues, "USDT", "BTC", "spot")
	if err != nil || math.Abs(got-1.12) > 1e-9 {
		t.Fatalf("spot PnL fee deduction = %v, %v; want quote and third-asset fees only", got, err)
	}
	futures, err := dailyPnLFeeDeduction(1.72, fees, quoteValues, "USDT", "BTC", "futures")
	if err != nil || futures != 1.72 {
		t.Fatalf("futures PnL fee deduction = %v, %v; want all fees", futures, err)
	}
	if got, err := dailyPnLFeeDeduction(0.5, map[string]float64{"BTC": -0.01}, map[string]float64{"BTC": -6}, "USDT", "BTC", "spot"); err != nil || got != 6.5 {
		t.Fatalf("spot base-asset rebate deduction = %v, %v", got, err)
	}
}

func TestDailyNetTradingPnLUsesMarketSpecificAccounting(t *testing.T) {
	spot, spotMethod, err := dailyNetTradingPnL("spot", -25, 30, 999)
	if err != nil || spot != 5 || spotMethod != "cashflow_position_change" {
		t.Fatalf("spot PnL = %v %q %v", spot, spotMethod, err)
	}
	futures, futuresMethod, err := dailyNetTradingPnL("futures", -25, 30, 7.5)
	if err != nil || futures != 7.5 || futuresMethod != "exchange_realized" {
		t.Fatalf("futures PnL must use exchange realized PnL, got %v %q %v", futures, futuresMethod, err)
	}
	if _, _, err := dailyNetTradingPnL("margin", 0, 0, 0); err == nil {
		t.Fatal("unsupported market type must not reuse spot/futures arithmetic")
	}
}

func TestSpotDailyPositionValuesRequireFreshSnapshots(t *testing.T) {
	start := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	openingQty, closingQty := 2.0, 3.0
	previous := &storage.DailySnapshot{SnapshotTime: start.Add(-time.Hour), SpotPositionQty: &openingQty, ClosingPrice: 5}
	current := &storage.DailySnapshot{SnapshotTime: end.Add(-time.Hour), SpotPositionQty: &closingQty, ClosingPrice: 5}
	opening, closing, err := spotDailyPositionValues(start, end, previous, current)
	if err != nil || opening != 10 || closing != 15 {
		t.Fatalf("fresh snapshot pair = %v %v %v", opening, closing, err)
	}
	stale := *previous
	stale.SnapshotTime = start.Add(-3 * time.Hour)
	if _, _, err := spotDailyPositionValues(start, end, &stale, current); err == nil {
		t.Fatal("stale opening snapshot must not produce a daily PnL")
	}
	if _, _, err := spotDailyPositionValues(start, end, previous, nil); err == nil {
		t.Fatal("missing closing snapshot must not be treated as zero inventory")
	}
	withoutInventory := *previous
	withoutInventory.SpotPositionQty = nil
	if _, _, err := spotDailyPositionValues(start, end, &withoutInventory, current); err == nil {
		t.Fatal("legacy snapshots without an actual account balance must not infer inventory from orders")
	}
}

func TestSpotInventoryDeltaMustReconcileToFillsAndBaseAssetFees(t *testing.T) {
	if err := spotInventoryDeltaMatchesFills(1, 1.99, 1, 0, 0.01); err != nil {
		t.Fatalf("base-asset commission should explain the observed inventory decrease: %v", err)
	}
	if err := spotInventoryDeltaMatchesFills(1, 2.5, 1, 0, 0); err == nil {
		t.Fatal("unexplained external deposit or missing fill must fail closed")
	}
	if err := spotInventoryDeltaMatchesFills(1, 0.5, 0, 0.5, 0); err != nil {
		t.Fatalf("ledger sell should reconcile with account inventory: %v", err)
	}
}

func TestDailyPnLIntervalUsesLocalCalendarDayAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		date  string
		hours float64
	}{{date: "2026-03-08", hours: 23}, {date: "2026-11-01", hours: 25}} {
		start, end, err := dailyPnLInterval(tt.date, loc)
		if err != nil || end.Sub(start).Hours() != tt.hours {
			t.Fatalf("daily interval %s = %v to %v (%v), want %v hours", tt.date, start, end, err, tt.hours)
		}
	}
}
