package main

import (
	"context"
	"math"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/exchange/income"
	"quantmesh/storage"
)

type fundingIncomeSyncTestExchange struct {
	exchange.IExchange
	requestedFrom int64
	requestedTo   int64
	incomes       []*income.Income
}

func (e *fundingIncomeSyncTestExchange) GetIncomeHistory(_ context.Context, _, _ string, from, to int64) ([]*income.Income, error) {
	e.requestedFrom, e.requestedTo = from, to
	return e.incomes, nil
}

func TestFundingIncomeSyncRejectsNonFiniteAmountAndMissingAsset(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name   string
		amount float64
		asset  string
	}{
		{name: "nan amount", amount: math.NaN(), asset: "USDT"},
		{name: "positive infinity", amount: math.Inf(1), asset: "USDT"},
		{name: "negative infinity", amount: math.Inf(-1), asset: "USDT"},
		{name: "missing asset", amount: 1, asset: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, err := storage.NewSQLStorage(t.TempDir() + "/funding-sync.db")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			priorFrom, priorThrough := now.Add(-2*time.Minute), now.Add(-time.Minute)
			if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-invalid", priorFrom, priorThrough); err != nil {
				t.Fatal("seed prior funding coverage:", err)
			}
			ex := &fundingIncomeSyncTestExchange{incomes: []*income.Income{{
				Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", Income: tt.amount, Asset: tt.asset,
				TransactionID: 1, TradeTime: now,
			}}}
			if err := syncFundingIncomeOnce(context.Background(), st, ex, "binance", "BTCUSDT", "acct", "futures", "scope-invalid"); err != nil {
				t.Fatal("syncFundingIncomeOnce:", err)
			}
			payments, err := st.GetFundingPaymentsByAccountScope("scope-invalid", "binance", now.Add(-time.Minute), now.Add(time.Minute))
			if err != nil {
				t.Fatal("read funding payments:", err)
			}
			if len(payments) != 0 {
				t.Fatalf("invalid ledger entry was persisted: %+v", payments)
			}
			from, through, err := st.GetFundingIncomeCoverage("binance", "BTCUSDT", "futures", "scope-invalid")
			if err != nil {
				t.Fatal("read funding coverage:", err)
			}
			if !from.Equal(priorFrom) || !through.Equal(priorThrough) {
				t.Fatalf("invalid ledger entry changed prior coverage: (%v, %v), want (%v, %v)", from, through, priorFrom, priorThrough)
			}
		})
	}
}

func TestFundingIncomeSyncWindowDoesNotBackfillNewCredentialScope(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 30, 0, 123456789, time.UTC)
	start, end, err := fundingIncomeSyncWindow(time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !start.Equal(now.Truncate(time.Millisecond)) || !end.Equal(now) || start.Before(now.Add(-time.Millisecond)) {
		t.Fatalf("new-scope sync window=(%v,%v], must begin at first observation %v", start, end, now.Truncate(time.Millisecond))
	}
}

func TestFundingIncomeSpecialRuntimeOwnership(t *testing.T) {
	tests := []struct {
		marketType string
		managed    bool
	}{
		{marketType: config.MarketTypeFundingCarry, managed: true},
		{marketType: config.MarketTypeFundingPerpSpread, managed: true},
		{marketType: " futures ", managed: false},
		{marketType: "spot", managed: false},
	}
	for _, tt := range tests {
		t.Run(tt.marketType, func(t *testing.T) {
			if got := fundingIncomeManagedBySpecialRuntime(tt.marketType); got != tt.managed {
				t.Fatalf("fundingIncomeManagedBySpecialRuntime(%q) = %t, want %t", tt.marketType, got, tt.managed)
			}
		})
	}
}

func TestFundingIncomeSyncWindowPreservesOrAdvancesExistingCoverage(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 30, 0, 123456789, time.UTC)
	tests := []struct {
		name        string
		coveredFrom time.Time
		wantStart   time.Time
	}{
		{name: "preserve current credential history", coveredFrom: now.Add(-10 * 24 * time.Hour), wantStart: now.Add(-10 * 24 * time.Hour)},
		{name: "advance beyond exchange retention", coveredFrom: now.Add(-45 * 24 * time.Hour), wantStart: now.AddDate(0, 0, -30)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := fundingIncomeSyncWindow(tt.coveredFrom, now)
			if err != nil {
				t.Fatal(err)
			}
			if !start.Equal(tt.wantStart) || !end.Equal(now) {
				t.Fatalf("sync window=(%v,%v], want=(%v,%v]", start, end, tt.wantStart, now)
			}
		})
	}
}

func TestFundingIncomeSyncWindowRejectsFutureCoverage(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC)
	if _, _, err := fundingIncomeSyncWindow(now.Add(time.Second), now); err == nil {
		t.Fatal("future funding watermark must not authorize a sync window")
	}
	if _, _, err := fundingIncomeSyncWindow(time.Time{}, time.Time{}); err == nil {
		t.Fatal("zero sync time must be rejected")
	}
}

func TestFundingIncomeSyncOnceBootstrapsWithoutHistoricalBackfill(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/funding-sync.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ex := &fundingIncomeSyncTestExchange{}
	const exchangeID, symbol, scope = "binance", "BTCUSDT", "credential-scope-new"
	started := time.Now().UTC()
	if err := syncFundingIncomeOnce(context.Background(), st, ex, exchangeID, symbol, "acct", "futures", scope); err != nil {
		t.Fatal("bootstrap new funding scope:", err)
	}
	from := time.UnixMilli(ex.requestedFrom).UTC()
	if from.Before(started.Add(-time.Second)) || from.After(time.Now().UTC()) {
		t.Fatalf("first sync started at %v; it must not backfill the prior 30 days", from)
	}
	coveredFrom, coveredThrough, err := st.GetFundingIncomeCoverage(exchangeID, symbol, "futures", scope)
	if err != nil || !coveredFrom.Equal(from) || coveredThrough.IsZero() {
		t.Fatalf("bootstrap coverage=(%v,%v), err=%v", coveredFrom, coveredThrough, err)
	}
	if err := syncFundingIncomeOnce(context.Background(), st, ex, exchangeID, symbol, "acct", "futures", scope); err != nil {
		t.Fatal("repeat funding sync:", err)
	}
	if ex.requestedFrom != coveredFrom.UnixMilli() {
		t.Fatalf("repeat sync moved before credential-scope watermark: from=%d coverage=%v", ex.requestedFrom, coveredFrom)
	}
}
