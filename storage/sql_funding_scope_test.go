package storage

import (
	"database/sql"
	"os"
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
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", Asset: "USDT", Income: -2, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", Asset: "USDT", Income: 0.5, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "spot", AccountScope: "scope-a", Asset: "USDT", Income: 100, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-b", Asset: "USDT", Income: 200, TradeTime: day},
		{Exchange: "binance", Symbol: "ETHUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", Asset: "USDT", Income: 300, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", Asset: "USDT", Income: 400, TradeTime: day},
		{Exchange: "binance", Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: "scope-a", Asset: "BTC", Income: 0.01, TradeTime: day},
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
}
