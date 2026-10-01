package main

import (
	"context"
	"errors"
	"math"
	"sync"
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
	incomeFactory func(from, to int64) []*income.Income
	incomeErr     error
}

type marginInterestSyncTestQuerier struct {
	pageFactory func(page int) ([]exchange.MarginInterestRecord, int64)
	calls       []int
}

func (q *marginInterestSyncTestQuerier) GetMarginInterestHistory(_ context.Context, asset string, _, _ int64, page, _ int) ([]exchange.MarginInterestRecord, int64, error) {
	if asset != "" {
		return nil, 0, errors.New("expected unfiltered account-scoped margin interest query")
	}
	q.calls = append(q.calls, page)
	records, total := q.pageFactory(page)
	return records, total, nil
}

func TestMarginInterestSyncPersistsEveryPageBeforeAdvancingCoverage(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/margin-interest-sync.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	recordTime := now.Add(-time.Hour).UnixMilli()
	querier := &marginInterestSyncTestQuerier{pageFactory: func(page int) ([]exchange.MarginInterestRecord, int64) {
		if page == 1 {
			records := make([]exchange.MarginInterestRecord, 100)
			for i := range records {
				records[i] = exchange.MarginInterestRecord{TransactionID: int64(i + 1), AccruedAt: recordTime, Asset: "BNB", RawAsset: "BTC", Principal: 0.4, Interest: 0.00001, Rate: 0.00025, Type: "PERIODIC_CONVERTED"}
			}
			return records, 101
		}
		return []exchange.MarginInterestRecord{{TransactionID: 101, AccruedAt: recordTime, Asset: "BNB", RawAsset: "BTC", Principal: 0.4, Interest: 0.00001, Rate: 0.00025, Type: "PERIODIC_CONVERTED"}}, 101
	}}
	if err := syncMarginInterestOnce(context.Background(), st, querier, "binance", "acct", "margin-scope-a", now); err != nil {
		t.Fatal("sync all margin interest pages:", err)
	}
	if len(querier.calls) != 2 || querier.calls[0] != 1 || querier.calls[1] != 2 {
		t.Fatalf("queried pages=%v, want [1 2]", querier.calls)
	}
	from, through, err := st.GetMarginInterestCoverage("binance", "margin-scope-a", "*")
	if err != nil || from.IsZero() || through.IsZero() {
		t.Fatalf("complete sync did not advance coverage: (%v,%v), err=%v", from, through, err)
	}
	total, err := st.GetMarginInterestTotalByAccountScope("binance", "margin-scope-a", "BNB", now.Add(-2*time.Hour), through)
	if err != nil || math.Abs(total-0.00101) > 1e-12 {
		t.Fatalf("persisted BNB-converted interest=%v err=%v, want 0.00101 BNB", total, err)
	}
}

func TestMarginInterestSyncPersistsAllocationOnlyAfterBotDebtMatchesAccountPrincipal(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/margin-interest-attributed.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	accruedAt := now.Add(-time.Hour)
	botState := fundingCarryTestDebtState(t, "bot-btc", "margin-attribution-scope", []fundingCarryMarginDebtEventSnapshot{
		{Action: "borrow", TransferID: 7701, Asset: "BTC", Amount: 0.4, Principal: 0.4, OccurredAt: accruedAt.Add(-time.Hour), AccountScope: "margin-attribution-scope"},
	})
	if err := st.SetStrategyRuntimeState(botState); err != nil {
		t.Fatal("save Bot-scoped debt ledger:", err)
	}
	querier := &marginInterestSyncTestQuerier{pageFactory: func(int) ([]exchange.MarginInterestRecord, int64) {
		return []exchange.MarginInterestRecord{{TransactionID: 7702, AccruedAt: accruedAt.UnixMilli(), Asset: "BTC", RawAsset: "BTC", Principal: 0.4, Interest: 0.0001, Rate: 0.00025, Type: "PERIODIC",
			ValuationAsset: "USDT", ValuationRate: 600, ValuationAmount: 0.06, ValuationStatus: "VALUED", ValuationMinute: 1_790_503_140_000, ValuationSource: "BINANCE_SPOT_1M_CLOSE"}}, 1
	}}
	if err := syncMarginInterestOnce(context.Background(), st, querier, "binance", "acct", "margin-attribution-scope", now); err != nil {
		t.Fatal("sync and attribute reconciled interest:", err)
	}
	allocations, err := st.ListMarginInterestAllocations("binance", "margin-attribution-scope", 7702)
	if err != nil {
		t.Fatal("read verified allocation:", err)
	}
	if len(allocations) != 1 || allocations[0].BotID != "bot-btc" || allocations[0].BotPrincipal != 0.4 || allocations[0].AccountPrincipal != 0.4 || allocations[0].Interest != 0.0001 ||
		allocations[0].ValuationStatus != "VALUED" || allocations[0].ValuationAsset != "USDT" || allocations[0].ValuationRate != 600 || allocations[0].ValuationAmount != 0.06 ||
		allocations[0].ValuationMinute != 1_790_503_140_000 || allocations[0].ValuationSource != "BINANCE_SPOT_1M_CLOSE" {
		t.Fatalf("unexpected persisted interest allocation: %+v", allocations)
	}
}

func TestMarginInterestSyncKeepsCoverageWhenPaginationIsIncomplete(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/margin-interest-incomplete.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	priorFrom, priorThrough := now.Add(-3*time.Hour), now.Add(-2*time.Hour)
	if err := st.MarkMarginInterestCoverage("binance", "margin-scope-b", "*", priorFrom, priorThrough); err != nil {
		t.Fatal("seed existing coverage:", err)
	}
	querier := &marginInterestSyncTestQuerier{pageFactory: func(page int) ([]exchange.MarginInterestRecord, int64) {
		if page == 1 {
			return []exchange.MarginInterestRecord{{TransactionID: 1, AccruedAt: now.Add(-time.Hour).UnixMilli(), Asset: "BTC", Principal: 1, Interest: 0.01, Rate: 0.001, Type: "PERIODIC"}}, 2
		}
		return nil, 2
	}}
	if err := syncMarginInterestOnce(context.Background(), st, querier, "binance", "acct", "margin-scope-b", now); err == nil {
		t.Fatal("incomplete history pagination must not advance coverage")
	}
	from, through, err := st.GetMarginInterestCoverage("binance", "margin-scope-b", "*")
	if err != nil || !from.Equal(priorFrom) || !through.Equal(priorThrough) {
		t.Fatalf("incomplete sync changed coverage to (%v,%v), err=%v", from, through, err)
	}
}

func TestMarginInterestSyncRejectsMissingRawAssetWithoutAdvancingCoverage(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/margin-interest-missing-raw-asset.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	priorFrom, priorThrough := now.Add(-3*time.Hour), now.Add(-2*time.Hour)
	if err := st.MarkMarginInterestCoverage("binance", "margin-raw-asset-scope", "*", priorFrom, priorThrough); err != nil {
		t.Fatal("seed existing coverage:", err)
	}
	querier := &marginInterestSyncTestQuerier{pageFactory: func(int) ([]exchange.MarginInterestRecord, int64) {
		return []exchange.MarginInterestRecord{{TransactionID: 91, AccruedAt: now.Add(-time.Hour).UnixMilli(), Asset: "BNB", Principal: 0.4, Interest: 0.0001, Rate: 0.00025, Type: "PERIODIC_CONVERTED"}}, 1
	}}
	if err := syncMarginInterestOnce(context.Background(), st, querier, "binance", "acct", "margin-raw-asset-scope", now); err == nil {
		t.Fatal("interest without rawAsset must not be accepted")
	}
	from, through, err := st.GetMarginInterestCoverage("binance", "margin-raw-asset-scope", "*")
	if err != nil || !from.Equal(priorFrom) || !through.Equal(priorThrough) {
		t.Fatalf("missing rawAsset changed coverage to (%v,%v), err=%v", from, through, err)
	}
}

func TestConcurrentMarginInterestSyncsSerializePerAccountScope(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/margin-interest-concurrent.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC().Truncate(time.Millisecond)
	makeQuerier := func() *marginInterestSyncTestQuerier {
		return &marginInterestSyncTestQuerier{pageFactory: func(int) ([]exchange.MarginInterestRecord, int64) {
			return []exchange.MarginInterestRecord{{TransactionID: 88, AccruedAt: now.Add(-time.Hour).UnixMilli(), Asset: "BNB", RawAsset: "BTC", Principal: 0.4, Interest: 0.0001, Rate: 0.00025, Type: "PERIODIC_CONVERTED"}}, 1
		}}
	}
	queriers := []*marginInterestSyncTestQuerier{makeQuerier(), makeQuerier()}
	errs := make(chan error, len(queriers))
	var workers sync.WaitGroup
	for _, querier := range queriers {
		workers.Add(1)
		go func(querier *marginInterestSyncTestQuerier) {
			defer workers.Done()
			errs <- syncMarginInterestOnce(context.Background(), st, querier, "binance", "acct", "margin-concurrent-scope", now)
		}(querier)
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("concurrent account-scoped margin interest sync:", err)
		}
	}
	total, err := st.GetMarginInterestTotalByAccountScope("binance", "margin-concurrent-scope", "BNB", now.Add(-2*time.Hour), now.Add(-6*time.Minute))
	if err != nil || total != 0.0001 {
		t.Fatalf("concurrent sync duplicated or lost the interest charge: total=%v err=%v", total, err)
	}
}

func (e *fundingIncomeSyncTestExchange) GetIncomeHistory(_ context.Context, _, _ string, from, to int64) ([]*income.Income, error) {
	e.requestedFrom, e.requestedTo = from, to
	if e.incomeFactory != nil {
		return e.incomeFactory(from, to), nil
	}
	return e.incomes, e.incomeErr
}

func TestFundingIncomeSyncPersistsAllRowsFromPaginatedHistory(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/funding-sync.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const exchangeID, symbol, scope = "binance", "BTCUSDT", "paginated-history-scope"
	ex := &fundingIncomeSyncTestExchange{incomeFactory: func(from, _ int64) []*income.Income {
		rows := make([]*income.Income, 1001)
		for i := range rows {
			rows[i] = &income.Income{Symbol: symbol, IncomeType: "FUNDING_FEE", Income: -0.01, Asset: "USDT", TransactionID: int64(i + 1), TradeTime: time.UnixMilli(from).UTC()}
		}
		return rows
	}}
	if err := syncFundingIncomeOnce(context.Background(), st, ex, exchangeID, symbol, "acct", "futures", scope); err != nil {
		t.Fatal("sync paginated funding history:", err)
	}
	payments, err := st.GetFundingPaymentsByAccountScope(scope, exchangeID, time.UnixMilli(ex.requestedFrom).Add(-time.Millisecond), time.UnixMilli(ex.requestedTo).Add(time.Millisecond))
	if err != nil {
		t.Fatal("read paginated funding payments:", err)
	}
	if len(payments) != 1001 {
		t.Fatalf("persisted funding rows=%d, want 1001", len(payments))
	}
	coveredFrom, coveredThrough, err := st.GetFundingIncomeCoverage(exchangeID, symbol, "futures", scope)
	if err != nil || coveredFrom.IsZero() || coveredThrough.IsZero() {
		t.Fatalf("complete paginated history did not advance coverage: (%v, %v), err=%v", coveredFrom, coveredThrough, err)
	}
}

func TestFundingIncomeSyncDoesNotAdvanceCoverageWhenHistoryIsUnsupported(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/funding-sync.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	priorFrom := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
	priorThrough := priorFrom.Add(time.Hour)
	const exchangeID, symbol, scope = "binance", "BTCUSDT", "unsupported-history-scope"
	if err := st.MarkFundingIncomeCoverage(exchangeID, symbol, "futures", scope, priorFrom, priorThrough); err != nil {
		t.Fatal("seed prior funding coverage:", err)
	}
	ex := &fundingIncomeSyncTestExchange{incomeErr: exchange.ErrNotImplemented}
	if err := syncFundingIncomeOnce(context.Background(), st, ex, exchangeID, symbol, "acct", "futures", scope); !errors.Is(err, exchange.ErrNotImplemented) {
		t.Fatalf("unsupported funding history error = %v, want ErrNotImplemented", err)
	}
	gotFrom, gotThrough, err := st.GetFundingIncomeCoverage(exchangeID, symbol, "futures", scope)
	if err != nil {
		t.Fatal("read funding coverage:", err)
	}
	if !gotFrom.Equal(priorFrom) || !gotThrough.Equal(priorThrough) {
		t.Fatalf("unsupported history changed coverage to (%v, %v), want (%v, %v)", gotFrom, gotThrough, priorFrom, priorThrough)
	}
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
			if err := syncFundingIncomeOnce(context.Background(), st, ex, "binance", "BTCUSDT", "acct", "futures", "scope-invalid"); err == nil {
				t.Fatal("syncFundingIncomeOnce() = nil; want incomplete-history error")
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
	start, end, err := fundingIncomeSyncWindow(time.Time{}, time.Time{}, now)
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
		name           string
		coveredFrom    time.Time
		coveredThrough time.Time
		wantStart      time.Time
	}{
		{name: "newer cursor uses bounded overlap", coveredFrom: now.Add(-10 * 24 * time.Hour), coveredThrough: now.Add(-2 * time.Hour), wantStart: now.Add(-26 * time.Hour)},
		{name: "young scope does not backfill before observation", coveredFrom: now.Add(-12 * time.Hour), coveredThrough: now.Add(-2 * time.Hour), wantStart: now.Add(-12 * time.Hour)},
		{name: "advance beyond exchange retention", coveredFrom: now.Add(-45 * 24 * time.Hour), coveredThrough: now.Add(-31 * 24 * time.Hour), wantStart: now.AddDate(0, 0, -30)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := fundingIncomeSyncWindow(tt.coveredFrom, tt.coveredThrough, now)
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
	if _, _, err := fundingIncomeSyncWindow(now.Add(-time.Minute), now.Add(time.Second), now); err == nil {
		t.Fatal("future funding watermark must not authorize a sync window")
	}
	if _, _, err := fundingIncomeSyncWindow(time.Time{}, time.Time{}, time.Time{}); err == nil {
		t.Fatal("zero sync time must be rejected")
	}
	if _, _, err := fundingIncomeSyncWindow(now, time.Time{}, now.Add(time.Minute)); err == nil {
		t.Fatal("incomplete funding coverage interval must be rejected")
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
