package storage

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
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
	payment.ValuationAsset, payment.ValuationRate, payment.ValuationAmount = "USDT", 600, 0.06
	payment.ValuationStatus, payment.ValuationMinute, payment.ValuationSource = "VALUED", 1790503140000, "BINANCE_SPOT_1M_CLOSE"
	if err := store.SaveMarginInterestPayment(payment); err != nil {
		t.Fatal("save margin interest:", err)
	}
	if err := store.SaveMarginInterestPayment(payment); err != nil {
		t.Fatal("identical replay must be idempotent:", err)
	}
	var quoteAsset, quoteStatus, quoteSource string
	var quoteRate, quoteAmount float64
	var quoteMinute int64
	if err := store.db.QueryRow(`SELECT valuation_asset, valuation_rate, valuation_amount, valuation_status, valuation_minute, valuation_source FROM margin_interest_payments WHERE identity_key = ?`,
		marginInterestIdentity(payment)).Scan(&quoteAsset, &quoteRate, &quoteAmount, &quoteStatus, &quoteMinute, &quoteSource); err != nil {
		t.Fatal("read persisted valuation provenance:", err)
	}
	if quoteAsset != "USDT" || quoteRate != 600 || quoteAmount != 0.06 || quoteStatus != "VALUED" || quoteMinute != 1790503140000 || quoteSource != "BINANCE_SPOT_1M_CLOSE" {
		t.Fatalf("persisted valuation provenance mismatch: %s %v %v %s %d %s", quoteAsset, quoteRate, quoteAmount, quoteStatus, quoteMinute, quoteSource)
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
	precisionPayment := &MarginInterestPayment{
		Exchange: "binance", AccountScope: "scope-a", Asset: "BTC", RawAsset: "BTC", Principal: 1.0 / 3,
		Interest: 0.00010000000049, Rate: 0.00012345678901234567, InterestType: "PERIODIC", TransactionID: 8002,
		AccruedAt: accruedAt.Add(time.Hour),
	}
	if err := store.SaveMarginInterestPayment(precisionPayment); err != nil {
		t.Fatal("save payment with values beyond MySQL decimal scale:", err)
	}
	if err := store.SaveMarginInterestPayment(precisionPayment); err != nil {
		t.Fatal("idempotently replay normalized payment:", err)
	}
	var principal, interest, rate float64
	if err := store.db.QueryRow(`SELECT principal, interest, interest_rate FROM margin_interest_payments WHERE identity_key = ?`,
		marginInterestIdentity(precisionPayment)).Scan(&principal, &interest, &rate); err != nil {
		t.Fatal("read normalized payment values:", err)
	}
	if principal != 0.333333333333 || interest != 0.0001 || rate != 0.0001234567890123 {
		t.Fatalf("normalized payment principal=%0.15f interest=%0.15f rate=%0.18f", principal, interest, rate)
	}
	tooSmall := *precisionPayment
	tooSmall.TransactionID++
	tooSmall.Interest = 1e-14
	if err := store.SaveMarginInterestPayment(&tooSmall); err == nil {
		t.Fatal("positive interest below DECIMAL scale must not be silently stored as zero")
	}
	unvalued := *precisionPayment
	unvalued.TransactionID++
	unvalued.ValuationStatus, unvalued.ValuationAmount = "UNVALUED", 0
	if err := store.SaveMarginInterestPayment(&unvalued); err != nil {
		t.Fatal("missing price must persist explicitly as unvalued:", err)
	}
	upgraded := unvalued
	upgraded.ValuationAsset, upgraded.ValuationRate, upgraded.ValuationAmount = "USDT", 2, 0.0002
	upgraded.ValuationStatus, upgraded.ValuationMinute, upgraded.ValuationSource = "VALUED", 1790503140000, "BINANCE_SPOT_1M_CLOSE"
	if err := store.SaveMarginInterestPayment(&upgraded); err != nil {
		t.Fatal("a later verified historical price must upgrade an unvalued row:", err)
	}
	if err := store.db.QueryRow(`SELECT valuation_status, valuation_amount FROM margin_interest_payments WHERE identity_key = ?`,
		marginInterestIdentity(&upgraded)).Scan(&quoteStatus, &quoteAmount); err != nil {
		t.Fatal("read upgraded valuation:", err)
	}
	if quoteStatus != "VALUED" || quoteAmount != 0.0002 {
		t.Fatalf("late valuation upgrade was not persisted: %s %v", quoteStatus, quoteAmount)
	}
	badUnvalued := unvalued
	badUnvalued.TransactionID++
	badUnvalued.ValuationAmount = 0.01
	if err := store.SaveMarginInterestPayment(&badUnvalued); err == nil {
		t.Fatal("unvalued interest must not carry a fabricated quote amount")
	}
	inconsistentValuation := *payment
	inconsistentValuation.TransactionID++
	inconsistentValuation.ValuationAmount += 0.01
	if err := store.SaveMarginInterestPayment(&inconsistentValuation); err == nil {
		t.Fatal("valued interest amount must reconcile to the stored amount and historical rate")
	}
}

func TestMarginInterestMigrationAddsValuationColumnsToExistingLedger(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/margin-interest-legacy.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, table := range []string{"margin_interest_payments", "margin_interest_allocations"} {
		for _, column := range []string{"valuation_asset", "valuation_rate", "valuation_amount", "valuation_status", "valuation_minute", "valuation_source"} {
			if _, err := store.db.Exec(`ALTER TABLE ` + table + ` DROP COLUMN ` + column); err != nil {
				t.Fatalf("prepare legacy schema %s without %s: %v", table, column, err)
			}
		}
	}
	if err := migrateMarginInterestTables(store.db); err != nil {
		t.Fatal("upgrade existing margin interest ledger:", err)
	}
	var status string
	if err := store.db.QueryRow(`SELECT valuation_status FROM margin_interest_payments LIMIT 1`).Scan(&status); err != sql.ErrNoRows {
		t.Fatalf("legacy migration should leave an empty ledger and query no rows, got status=%q err=%v", status, err)
	}
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('margin_interest_payments') WHERE name LIKE 'valuation_%'`).Scan(&count); err != nil {
		t.Fatal("verify valuation migration columns:", err)
	}
	if count != 6 {
		t.Fatalf("valuation migration added %d columns, want 6", count)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('margin_interest_allocations') WHERE name LIKE 'valuation_%'`).Scan(&count); err != nil {
		t.Fatal("verify allocation valuation migration columns:", err)
	}
	if count != 6 {
		t.Fatalf("allocation valuation migration added %d columns, want 6", count)
	}
}

func TestMarginInterestValuationTotalsRespectCoverageAndUnvaluedRows(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/margin-interest-valuations.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	from := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	base := &MarginInterestPayment{
		Exchange: "binance", AccountScope: "scope-a", Asset: "BNB", RawAsset: "BTC", Principal: 1,
		Interest: 0.1, Rate: 0.01, InterestType: "PERIODIC", TransactionID: 8501, AccruedAt: from.Add(10 * time.Minute),
		ValuationAsset: "USDT", ValuationRate: 2, ValuationAmount: 0.2, ValuationStatus: "VALUED",
		ValuationMinute: from.Add(10*time.Minute).UnixMilli() / int64(time.Minute/time.Millisecond) * int64(time.Minute/time.Millisecond),
		ValuationSource: "BINANCE_SPOT_1M_CLOSE",
	}
	if err := store.SaveMarginInterestPayment(base); err != nil {
		t.Fatal("save valued margin interest:", err)
	}
	unvalued := *base
	unvalued.TransactionID++
	unvalued.Interest, unvalued.ValuationAmount, unvalued.ValuationStatus = 0.05, 0, "UNVALUED"
	if err := store.SaveMarginInterestPayment(&unvalued); err != nil {
		t.Fatal("save unvalued margin interest:", err)
	}
	if _, _, err := store.GetMarginInterestValuationTotalsByAccountScope("binance", "scope-a", from, through); err == nil {
		t.Fatal("valuation totals must require full exchange-history coverage")
	}
	if err := store.MarkMarginInterestCoverage("binance", "scope-a", "*", from, through); err != nil {
		t.Fatal("mark covered margin-interest window:", err)
	}
	cost, unvaluedRows, err := store.GetMarginInterestValuationTotalsByAccountScope("binance", "scope-a", from, through)
	if err != nil || cost != 0.2 || unvaluedRows != 1 {
		t.Fatalf("valuation totals cost=%v unvalued=%d err=%v; want 0.2 and 1", cost, unvaluedRows, err)
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
		AccruedAt:      time.Date(2026, 10, 2, 5, 0, 0, 0, time.UTC),
		ValuationAsset: "USDT", ValuationRate: 2, ValuationAmount: 0.00008, ValuationStatus: "VALUED",
		ValuationMinute: 1790503140000, ValuationSource: "BINANCE_SPOT_1M_CLOSE",
	}
	if err := store.SaveMarginInterestAllocation(allocation); err != nil {
		t.Fatal("save verified allocation:", err)
	}
	if err := store.SaveMarginInterestAllocation(allocation); err != nil {
		t.Fatal("identical allocation replay must be idempotent:", err)
	}
	valuedReadback, err := store.ListMarginInterestAllocations("binance", "scope-a", allocation.TransactionID)
	if err != nil || len(valuedReadback) != 1 || valuedReadback[0].ValuationStatus != "VALUED" ||
		valuedReadback[0].ValuationAsset != "USDT" || valuedReadback[0].ValuationRate != 2 || valuedReadback[0].ValuationAmount != 0.00008 ||
		valuedReadback[0].ValuationMinute != allocation.ValuationMinute || valuedReadback[0].ValuationSource != allocation.ValuationSource {
		t.Fatalf("Bot allocation valuation provenance was not persisted: %+v err=%v", valuedReadback, err)
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
	otherBot.ValuationAmount = 0.00012
	if err := store.SaveMarginInterestAllocation(&otherBot); err != nil {
		t.Fatal("separate Bot allocation must have an independent identity:", err)
	}
	precisionAllocation := &MarginInterestAllocation{
		Exchange: "binance", AccountScope: "scope-a", TransactionID: 8101, BotID: "bot-third",
		Asset: "BTC", RawAsset: "BTC", BotPrincipal: 1.0 / 3, AccountPrincipal: 1, Interest: 0.0000333333333333,
		AccruedAt: allocation.AccruedAt,
	}
	if err := store.SaveMarginInterestAllocation(precisionAllocation); err != nil {
		t.Fatal("save repeating-decimal allocation at schema precision:", err)
	}
	if err := store.SaveMarginInterestAllocation(precisionAllocation); err != nil {
		t.Fatal("replay repeating-decimal allocation:", err)
	}
	readback, err := store.ListMarginInterestAllocations("binance", "scope-a", precisionAllocation.TransactionID)
	if err != nil || len(readback) != 1 || readback[0].BotPrincipal != 0.333333333333 || readback[0].Interest != 0.000033333333 {
		t.Fatalf("normalized allocation readback=%+v err=%v", readback, err)
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
		_, _ = db.Exec(`DELETE FROM margin_interest_allocations WHERE account_scope = ?`, unique)
		_, _ = db.Exec(`DELETE FROM margin_interest_sync_state WHERE scope_key = ?`, marginInterestCoverageKey(payment.Exchange, unique, "*"))
	})
	if err := store.SaveMarginInterestPayment(payment); err != nil {
		t.Fatal("save MySQL margin interest:", err)
	}
	if err := store.SaveMarginInterestPayment(payment); err != nil {
		t.Fatal("replay MySQL margin interest:", err)
	}
	precisionPayment := *payment
	precisionPayment.TransactionID++
	precisionPayment.Principal = 1.0 / 3
	precisionPayment.Interest = 0.00010000000049
	precisionPayment.Rate = 0.00012345678901234567
	if err := store.SaveMarginInterestPayment(&precisionPayment); err != nil {
		t.Fatal("save MySQL payment beyond decimal scale:", err)
	}
	if err := store.SaveMarginInterestPayment(&precisionPayment); err != nil {
		t.Fatal("replay normalized MySQL payment:", err)
	}
	allocation := &MarginInterestAllocation{
		Exchange: "binance", AccountScope: unique, TransactionID: payment.TransactionID, BotID: "mysql-repeating-ratio",
		Asset: "BTC", RawAsset: "BTC", BotPrincipal: 1.0 / 3, AccountPrincipal: 1, Interest: 0.0000333333333333,
		AccruedAt: payment.AccruedAt,
	}
	if err := store.SaveMarginInterestAllocation(allocation); err != nil {
		t.Fatal("save MySQL repeating-decimal allocation:", err)
	}
	if err := store.SaveMarginInterestAllocation(allocation); err != nil {
		t.Fatal("idempotently replay MySQL repeating-decimal allocation:", err)
	}
	allocations, err := store.ListMarginInterestAllocations("binance", unique, payment.TransactionID)
	if err != nil || len(allocations) != 1 || allocations[0].BotPrincipal != 0.333333333333 || allocations[0].Interest != 0.000033333333 {
		t.Fatalf("MySQL normalized allocation=%+v err=%v", allocations, err)
	}
	// The non-unique index prefix must never truncate identity or mix scopes
	// sharing its first 384 characters, including multibyte UTF-8 characters.
	common := strings.Repeat("账", 384)
	scopes := []string{common + strings.Repeat("x", 127) + "a", common + strings.Repeat("x", 127) + "b"}
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM margin_interest_allocations WHERE account_scope IN (?, ?)`, scopes[0], scopes[1])
	})
	for _, scope := range scopes {
		long := *allocation
		long.AccountScope = scope
		if err := store.SaveMarginInterestAllocation(&long); err != nil {
			t.Fatal("save complete long MySQL account scope:", err)
		}
	}
	for _, scope := range scopes {
		rows, err := store.ListMarginInterestAllocations("binance", scope, payment.TransactionID)
		if err != nil || len(rows) != 1 || rows[0].AccountScope != scope {
			t.Fatalf("prefix collision mixed complete account identities: count=%d err=%v", len(rows), err)
		}
	}
	from, through := payment.AccruedAt.Add(-time.Minute), payment.AccruedAt.Add(time.Minute)
	if err := store.MarkMarginInterestCoverage(payment.Exchange, unique, "*", from, through); err != nil {
		t.Fatal("write MySQL margin interest coverage:", err)
	}
	total, err := store.GetMarginInterestTotalByAccountScope(payment.Exchange, unique, payment.Asset, from, through)
	// The second precision fixture is a distinct transaction, not a replay.
	wantTotal := 0.0002
	if err != nil || total != wantTotal {
		t.Fatalf("MySQL scoped interest=%v err=%v, want both normalized transactions %v", total, err, wantTotal)
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
