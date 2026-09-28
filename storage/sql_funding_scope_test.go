package storage

import (
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"
)

func TestDailyFundingPaymentsRequireExactAccountMarketAndSymbolScope(t *testing.T) {
	path := t.TempDir() + "/funding-scope.db"
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	defer os.Remove(path)

	day := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	payments := []FundingPayment{
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 1, Asset: "USDT", Income: -2, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 2, Asset: "USDT", Income: 0.5, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "spot", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 3, Asset: "USDT", Income: 100, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-b", IncomeType: "FUNDING_FEE", TransactionID: 4, Asset: "USDT", Income: 200, TradeTime: day},
		{Exchange: "binance", Symbol: "ETHUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 5, Asset: "USDT", Income: 300, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "spot", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 6, Asset: "USDT", Income: 400, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", IncomeType: "FUNDING_FEE", TransactionID: 7, Asset: "BTC", Income: 0.01, TradeTime: day},
	}
	for i := range payments {
		if err := st.SaveFundingPayment(&payments[i]); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.GetDailyFundingPaymentsByScope("acct", "binance", "futures", "BTCUSDT", "scope-a", day.Truncate(24*time.Hour), day.Truncate(24*time.Hour).Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got["USDT"] != -1.5 || got["BTC"] != 0.01 || len(got) != 2 {
		t.Fatalf("unexpected scoped denomination totals: %#v", got)
	}
	if _, err := st.GetDailyFundingPaymentsByScope("acct", "binance", "futures", "BTCUSDT", "", day, day.Add(24*time.Hour)); err == nil {
		t.Fatal("missing account scope must not execute an unscoped funding query")
	}
}

func TestSaveFundingPaymentIsIdempotentAndRejectsIdentityConflicts(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/funding-idempotency.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	payment := FundingPayment{
		Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a",
		IncomeType: "FUNDING_FEE", TransactionID: 991, Income: -1.25, Asset: "USDT",
		TradeTime: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC),
	}
	if err := st.SaveFundingPayment(&payment); err != nil {
		t.Fatal(err)
	}
	const concurrentReplays = 8
	var wg sync.WaitGroup
	errs := make(chan error, concurrentReplays)
	for i := 0; i < concurrentReplays; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- st.SaveFundingPayment(&payment)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("identical concurrent replay should be idempotent: %v", err)
		}
	}
	conflict := payment
	conflict.Income = -9
	if err := st.SaveFundingPayment(&conflict); err == nil {
		t.Fatal("reused transaction identity with a different amount must fail")
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM funding_payments`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("funding payment rows=%d, want 1", count)
	}
}

func TestSaveFundingPaymentRequiresStableIdentity(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/funding-invalid-identity.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	payment := FundingPayment{Exchange: "binance", IncomeType: "FUNDING_FEE", TransactionID: 1, AccountScope: "scope-a"}
	if err := st.SaveFundingPayment(&payment); err == nil {
		t.Fatal("funding payment without a symbol or market must fail")
	}
	payment.Symbol, payment.MarketType, payment.TransactionID = "BTCUSDT", "futures", 0
	if err := st.SaveFundingPayment(&payment); err == nil {
		t.Fatal("funding payment without a stable transaction id must fail")
	}
}

func TestMigrateFundingPaymentScopePreservesLegacyRows(t *testing.T) {
	path := t.TempDir() + "/legacy-funding.db"
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE funding_payments (id INTEGER PRIMARY KEY, exchange TEXT, symbol TEXT, account TEXT, income_type TEXT, income REAL, asset TEXT, info TEXT, transaction_id INTEGER, trade_time TIMESTAMP, created_at TIMESTAMP); INSERT INTO funding_payments(exchange, symbol, account) VALUES ('binance', 'BTCUSDT', 'acct')`); err != nil {
		t.Fatal(err)
	}
	if err := migrateFundingPaymentsTable(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateFundingPaymentsTable(db); err != nil {
		t.Fatalf("migration should be idempotent: %v", err)
	}
	var marketType, accountScope string
	if err := db.QueryRow(`SELECT market_type, account_scope FROM funding_payments WHERE id = 1`).Scan(&marketType, &accountScope); err != nil {
		t.Fatal(err)
	}
	if marketType != "" || accountScope != "" {
		t.Fatalf("legacy rows must remain unclassified: market=%q scope=%q", marketType, accountScope)
	}
	var tableCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='funding_income_sync_state'`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 1 {
		t.Fatal("funding income coverage table was not created")
	}
}
