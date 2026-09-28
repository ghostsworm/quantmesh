package storage

import (
	"database/sql"
	"os"
	"testing"
	"time"
)

// Set QUANTMESH_MYSQL_TEST_DSN only for an isolated disposable MySQL schema.
func TestMySQLOrderFillCoverageMigrationAndRoundTrip(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("open isolated MySQL test database:", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal("connect to isolated MySQL test database:", err)
	}
	if err := migrateOrderFillsTable(db, true); err != nil {
		t.Fatal("apply order fill migrations to isolated MySQL schema:", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	scope := "mysql-test-" + time.Now().UTC().Format("20060102150405.000000000")
	start := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	through := start.Add(12 * time.Hour)
	pnl := 2.5
	fill := &OrderFill{Exchange: "binance", MarketType: "spot", AccountScope: scope, Account: "mysql-test", Symbol: "BTCUSDT", TradeID: "fill-1",
		OrderID: 91, Side: "SELL", Price: 100, Quantity: 0.5, Commission: 0.01, CommissionAsset: "BNB", RealizedPnL: &pnl, TradeTime: start.Add(time.Hour)}
	if err := store.SaveOrderFill(fill); err != nil {
		t.Fatal("insert MySQL execution fixture:", err)
	}
	enriched := *fill
	enriched.CommissionQuote, enriched.CommissionQuoteRate, enriched.CommissionQuoteKnown = 0.1, 10, true
	if err := store.SaveOrderFill(&enriched); err != nil {
		t.Fatal("enrich MySQL execution with verified fee conversion:", err)
	}
	defer db.Exec(`DELETE FROM order_fills WHERE account_scope = ?`, scope)
	summary, err := store.QueryDailyOrderFillsByScope("mysql-test", "binance", "spot", "BTCUSDT", scope, start, through)
	if err != nil {
		t.Fatal("aggregate MySQL execution ledger:", err)
	}
	if summary.SellOrders != 1 || summary.SellQty != 0.5 || summary.SellValue != 50 || summary.RealizedPnL != pnl || summary.FeesByAsset["BNB"] != 0.01 || summary.HistoricalFeeQuoteValueByAsset["BNB"] != 0.1 || summary.FeeQuoteUnknownCountByAsset["BNB"] != 0 {
		t.Fatalf("unexpected MySQL execution summary: %+v", summary)
	}
	winners, losers, err := store.QueryTopDailyRealizedFills("mysql-test", "binance", "spot", "BTCUSDT", scope, start, through)
	if err != nil || len(winners) != 1 || len(losers) != 0 || winners[0].OrderID != 91 {
		t.Fatalf("unexpected MySQL realized-fill ranking: winners=%+v losers=%+v err=%v", winners, losers, err)
	}
	if err := store.AdvanceOrderFillCoverage("binance", "spot", "BTCUSDT", scope, start, through); err != nil {
		t.Fatal("persist MySQL order fill coverage:", err)
	}
	coverage, err := store.GetOrderFillCoverage("binance", "spot", "BTCUSDT", scope)
	if err != nil {
		t.Fatal("read MySQL order fill coverage:", err)
	}
	if coverage == nil || !coverage.CoveredFrom.Equal(start) || !coverage.CoveredThrough.Equal(through) {
		t.Fatalf("MySQL coverage round trip mismatch: %+v", coverage)
	}
	_, err = db.Exec(`DELETE FROM order_fill_sync_coverage WHERE scope_hash = ?`, orderFillScopeHash(scope))
	if err != nil {
		t.Fatal("clean isolated MySQL coverage fixture:", err)
	}
}

func TestMySQLSpotInventorySnapshotMigrationAndRoundTrip(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("open isolated MySQL test database:", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal("connect to isolated MySQL test database:", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS hourly_equity_records (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, exchange VARCHAR(64) NOT NULL, symbol VARCHAR(64) NOT NULL,
		account VARCHAR(255) NOT NULL, timestamp DATETIME(3) NOT NULL, equity DOUBLE NOT NULL,
		unrealized_pnl DOUBLE NOT NULL, total_position_value DOUBLE NOT NULL, account_equity DOUBLE,
		created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3)) ENGINE=InnoDB`)
	if err != nil {
		t.Fatal("create legacy hourly snapshot fixture:", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS daily_snapshots (
		id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY, exchange VARCHAR(64) NOT NULL, symbol VARCHAR(64) NOT NULL,
		account VARCHAR(255) NOT NULL, date DATE NOT NULL, unrealized_pnl DOUBLE NOT NULL,
		total_position_value DOUBLE NOT NULL, intraday_max_drawdown DOUBLE NOT NULL,
		intraday_max_drawdown_pct DOUBLE NOT NULL, intraday_peak_equity DOUBLE NOT NULL,
		closing_price DOUBLE NOT NULL, snapshot_time TIMESTAMP(3) NOT NULL, account_equity DOUBLE,
		created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		UNIQUE KEY uk_daily_snapshots_dim (exchange, symbol, account, date)) ENGINE=InnoDB`)
	if err != nil {
		t.Fatal("create legacy daily snapshot fixture:", err)
	}
	if err := migrateHourlyEquityRecordsTableMySQL(db); err != nil {
		t.Fatal("migrate legacy hourly snapshot schema:", err)
	}
	if err := migrateDailySnapshotsTableMySQL(db); err != nil {
		t.Fatal("migrate legacy daily snapshot schema:", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	scope := "mysql-inventory-" + time.Now().UTC().Format("20060102150405.000000000")
	ts := time.Date(2026, 9, 27, 23, 0, 0, 0, time.UTC)
	qty := 1.25
	if err := store.SaveHourlyEquityRecord(&HourlyEquityRecord{Exchange: "binance", MarketType: "spot", AccountScope: scope, Symbol: "BTCUSDT", Account: "mysql-test", Timestamp: ts, Equity: 75000, TotalPositionValue: 75000, MarketPrice: 60000, SpotPositionQty: &qty}); err != nil {
		t.Fatal("write MySQL hourly inventory snapshot:", err)
	}
	hourly, err := store.QueryHourlyEquityRecordsByScope("binance", "spot", "BTCUSDT", scope, ts.Add(-time.Minute), ts.Add(time.Minute))
	if err != nil || len(hourly) != 1 || hourly[0].MarketPrice != 60000 || hourly[0].SpotPositionQty == nil || *hourly[0].SpotPositionQty != qty {
		t.Fatalf("MySQL hourly inventory round trip mismatch: rows=%+v err=%v", hourly, err)
	}
	defer db.Exec(`DELETE FROM hourly_equity_records WHERE account_scope = ?`, scope)
	if err := store.SaveDailySnapshot(&DailySnapshot{Exchange: "binance", MarketType: "spot", AccountScope: scope, Symbol: "BTCUSDT", Account: "mysql-test", Date: ts, SnapshotTime: ts, TotalPositionValue: 75000, ClosingPrice: 60000, SpotPositionQty: &qty}); err != nil {
		t.Fatal("write MySQL daily inventory snapshot:", err)
	}
	daily, err := store.GetDailySnapshotByScope("binance", "spot", "BTCUSDT", scope, ts)
	if err != nil || daily == nil || daily.ClosingPrice != 60000 || daily.SpotPositionQty == nil || *daily.SpotPositionQty != qty {
		t.Fatalf("MySQL daily inventory round trip mismatch: snapshot=%+v err=%v", daily, err)
	}
	defer db.Exec(`DELETE FROM daily_snapshots WHERE account_scope = ?`, scope)
}
