package storage

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestMySQLFundingPaymentIdentityAndCoverage requires a disposable MySQL schema.
func TestMySQLFundingPaymentIdentityAndCoverage(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("open isolated MySQL test database:", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatal("connect to isolated MySQL test database:", err)
	}
	if err := migrateFundingPaymentsTableMySQL(db); err != nil {
		t.Fatal("apply production funding payment migration:", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	unique := fmt.Sprintf("mysql-funding-%d", time.Now().UTC().UnixNano())
	accountScope := unique
	symbol := "BTCUSDT"
	coverageKey := fundingIncomeCoverageKey("binance", symbol, "futures", accountScope)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM funding_payments WHERE account_scope = ?`, accountScope)
		_, _ = db.Exec(`DELETE FROM funding_income_sync_state WHERE scope_key = ?`, coverageKey)
	})

	tradeTime := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	payment := &FundingPayment{
		Exchange: "binance", Symbol: symbol, Account: unique, MarketType: "futures", AccountScope: accountScope,
		IncomeType: "FUNDING_FEE", Income: -1.23456789, Asset: "USDT", TransactionID: time.Now().UnixNano(), TradeTime: tradeTime,
	}
	start, through := tradeTime.Add(-2*time.Hour), tradeTime.Add(-30*time.Second)
	var workers sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < cap(errs); i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			errs <- store.SaveFundingPayment(payment)
		}()
	}
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("identical concurrent MySQL replay must be idempotent: %v", err)
		}
	}
	conflict := *payment
	conflict.Income -= 1
	if err := store.SaveFundingPayment(&conflict); err == nil {
		t.Fatal("MySQL identity reuse with different amount must fail")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM funding_payments WHERE account_scope = ?`, accountScope).Scan(&count); err != nil {
		t.Fatal("count MySQL funding payment rows:", err)
	}
	if count != 1 {
		t.Fatalf("MySQL funding payment rows=%d, want exactly one", count)
	}

	if err := store.MarkFundingIncomeCoverage("binance", symbol, "futures", accountScope, start, through); err != nil {
		t.Fatal("persist MySQL funding coverage:", err)
	}
	gotStart, gotThrough, err := store.GetFundingIncomeCoverage("binance", symbol, "futures", accountScope)
	if err != nil || !gotStart.Equal(start) || !gotThrough.Equal(through) {
		t.Fatalf("MySQL coverage round-trip=(%v,%v), want=(%v,%v), err=%v", gotStart, gotThrough, start, through, err)
	}
	covered, err := store.HasFundingIncomeCoverage("binance", symbol, "futures", accountScope, start.Add(time.Minute), through.Add(-time.Minute))
	if err != nil || !covered {
		t.Fatalf("contained MySQL funding interval covered=%v err=%v", covered, err)
	}
	covered, err = store.HasFundingIncomeCoverage("binance", symbol, "futures", accountScope, start.Add(-time.Second), through)
	if err != nil || covered {
		t.Fatalf("uncovered MySQL funding interval covered=%v err=%v", covered, err)
	}
}
