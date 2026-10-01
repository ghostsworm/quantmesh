package storage

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMarginInterestLedgerIsAccountAndAssetScopedAndIdempotent(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/margin-interest.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	accruedAt := time.Date(2026, 10, 2, 1, 2, 3, 4000000, time.UTC)
	payment := &MarginInterestPayment{Exchange: "BINANCE", Account: "acct", AccountScope: "scope-a", Asset: "BNB", RawAsset: "BTC",
		Principal: 0.4, Interest: 0.0001, Rate: 0.00025, InterestType: "PERIODIC", TransactionID: 8001, AccruedAt: accruedAt}
	if err := store.SaveMarginInterestPayment(payment); err != nil {
		t.Fatal("save margin interest:", err)
	}
	if err := store.SaveMarginInterestPayment(payment); err != nil {
		t.Fatal("identical replay must be idempotent:", err)
	}
	conflict := *payment
	conflict.Interest += 0.0001
	if err := store.SaveMarginInterestPayment(&conflict); err == nil {
		t.Fatal("conflicting replay must be rejected")
	}
	conflict = *payment
	conflict.RawAsset = "ETH"
	if err := store.SaveMarginInterestPayment(&conflict); err == nil {
		t.Fatal("replay that changes original liability asset must be rejected")
	}
	otherScope := *payment
	otherScope.AccountScope = "scope-b"
	if err := store.SaveMarginInterestPayment(&otherScope); err != nil {
		t.Fatal("same transaction in a different credential scope must be independent:", err)
	}
	otherAsset := *payment
	otherAsset.Asset = "ETH"
	if err := store.SaveMarginInterestPayment(&otherAsset); err != nil {
		t.Fatal("same transaction ID in a different asset must be independent:", err)
	}
	if _, err := store.GetMarginInterestTotalByAccountScope("binance", "scope-a", "BNB", accruedAt.Add(-time.Minute), accruedAt.Add(time.Minute)); err == nil {
		t.Fatal("interest totals without verified coverage must fail closed")
	}
	if err := store.MarkMarginInterestCoverage("binance", "scope-a", "*", accruedAt.Add(-time.Minute), accruedAt.Add(time.Minute)); err != nil {
		t.Fatal("mark complete margin interest coverage:", err)
	}
	total, err := store.GetMarginInterestTotalByAccountScope("binance", "scope-a", "bnb", accruedAt.Add(-time.Minute), accruedAt.Add(time.Minute))
	if err != nil || total != payment.Interest {
		t.Fatalf("scoped BTC interest=%v err=%v, want %v", total, err, payment.Interest)
	}
	if _, err := store.GetMarginInterestTotalByAccountScope("binance", "", "BTC", accruedAt.Add(-time.Minute), accruedAt.Add(time.Minute)); err == nil {
		t.Fatal("missing account scope must fail closed")
	}
}

func TestMarginInterestAllocationsAreBotScopedAndImmutable(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/margin-interest-allocations.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	allocation := &MarginInterestAllocation{
		Exchange: "binance", AccountScope: "scope-a", TransactionID: 8100, BotID: "bot-btc",
		Asset: "BTC", RawAsset: "BTC", BotPrincipal: 0.4, AccountPrincipal: 1, Interest: 0.00004,
		AccruedAt: time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC),
	}
	if err := store.SaveMarginInterestAllocation(allocation); err != nil {
		t.Fatal("save verified allocation:", err)
	}
	if err := store.SaveMarginInterestAllocation(allocation); err != nil {
		t.Fatal("identical allocation replay must be idempotent:", err)
	}
	conflict := *allocation
	conflict.Interest += 0.00001
	if err := store.SaveMarginInterestAllocation(&conflict); err == nil {
		t.Fatal("conflicting allocation for the same transaction and Bot must be rejected")
	}
	otherBot := *allocation
	otherBot.BotID = "bot-eth"
	otherBot.BotPrincipal = 0.6
	otherBot.Interest = 0.00006
	if err := store.SaveMarginInterestAllocation(&otherBot); err != nil {
		t.Fatal("separate Bot allocation must have an independent identity:", err)
	}
}

func TestMySQLMarginInterestLedgerAndCoverage(t *testing.T) {
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
	if err := migrateMarginInterestTablesMySQL(db); err != nil {
		t.Fatal("apply MySQL margin interest migrations:", err)
	}
	if err := migrateMarginInterestTablesMySQL(db); err != nil {
		t.Fatal("reapply MySQL margin interest migrations:", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	unique := fmt.Sprintf("mysql-margin-interest-%d", time.Now().UTC().UnixNano())
	payment := &MarginInterestPayment{Exchange: "binance", Account: unique, AccountScope: unique, Asset: "BNB", RawAsset: "BTC", Principal: 0.4,
		Interest: 0.0001, Rate: 0.00025, InterestType: "PERIODIC", TransactionID: time.Now().UnixNano(), AccruedAt: time.Now().UTC().Truncate(time.Millisecond)}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM margin_interest_payments WHERE account_scope = ?`, unique)
		_, _ = db.Exec(`DELETE FROM margin_interest_sync_state WHERE scope_key = ?`, marginInterestCoverageKey(payment.Exchange, unique, "*"))
	})
	if err := store.SaveMarginInterestPayment(payment); err != nil {
		t.Fatal("save MySQL margin interest:", err)
	}
	if err := store.SaveMarginInterestPayment(payment); err != nil {
		t.Fatal("replay MySQL margin interest:", err)
	}
	from, through := payment.AccruedAt.Add(-time.Minute), payment.AccruedAt.Add(time.Minute)
	if err := store.MarkMarginInterestCoverage(payment.Exchange, unique, "*", from, through); err != nil {
		t.Fatal("write MySQL margin interest coverage:", err)
	}
	total, err := store.GetMarginInterestTotalByAccountScope(payment.Exchange, unique, payment.Asset, from, through)
	if err != nil || total != payment.Interest {
		t.Fatalf("MySQL scoped interest=%v err=%v, want %v", total, err, payment.Interest)
	}
}

func TestMarginInterestCoverageDoesNotBridgeGapsOrRegress(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/margin-interest-coverage.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Millisecond)
	firstStart, firstEnd := now.Add(-72*time.Hour), now.Add(-48*time.Hour)
	secondStart, secondEnd := now.Add(-24*time.Hour), now
	if err := store.MarkMarginInterestCoverage("binance", "scope-a", "*", firstStart, firstEnd); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkMarginInterestCoverage("binance", "scope-a", "*", secondStart, secondEnd); err != nil {
		t.Fatal(err)
	}
	from, through, err := store.GetMarginInterestCoverage("binance", "scope-a", "*")
	if err != nil || !from.Equal(secondStart) || !through.Equal(secondEnd) {
		t.Fatalf("disjoint newer coverage=(%v,%v), want latest snapshot=(%v,%v), err=%v", from, through, secondStart, secondEnd, err)
	}
	if err := store.MarkMarginInterestCoverage("binance", "scope-a", "*", firstStart, firstEnd.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	from, through, err = store.GetMarginInterestCoverage("binance", "scope-a", "*")
	if err != nil || !from.Equal(secondStart) || !through.Equal(secondEnd) {
		t.Fatalf("late old completion regressed coverage=(%v,%v), err=%v", from, through, err)
	}
}
