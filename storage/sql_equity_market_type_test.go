package storage

import (
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestMarketTypeSnapshotMigrationPreservesLegacyAndSeparatesMarkets(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "legacy-equity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE hourly_equity_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT, exchange TEXT NOT NULL, symbol TEXT NOT NULL, account TEXT NOT NULL,
		timestamp DATETIME NOT NULL, equity REAL NOT NULL, unrealized_pnl REAL NOT NULL,
		total_position_value REAL NOT NULL, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE account_equity_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT, exchange TEXT NOT NULL, account TEXT NOT NULL,
		timestamp DATETIME NOT NULL, account_equity REAL NOT NULL, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(exchange, account, timestamp));
		INSERT INTO account_equity_records(exchange,account,timestamp,account_equity) VALUES
		('binance','acct','2026-09-26 12:00:00',900),('binance','acct2','2026-09-26 12:00:00',700);
		CREATE TABLE daily_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT, exchange TEXT NOT NULL, symbol TEXT NOT NULL, account TEXT NOT NULL,
		date DATE NOT NULL, unrealized_pnl REAL NOT NULL, total_position_value REAL NOT NULL,
		intraday_max_drawdown REAL NOT NULL, intraday_max_drawdown_pct REAL NOT NULL,
		intraday_peak_equity REAL NOT NULL, closing_price REAL NOT NULL, snapshot_time TIMESTAMP NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, UNIQUE(exchange, symbol, account, date));
		INSERT INTO daily_snapshots(exchange,symbol,account,date,unrealized_pnl,total_position_value,intraday_max_drawdown,
		intraday_max_drawdown_pct,intraday_peak_equity,closing_price,snapshot_time) VALUES('binance','BTCUSDT','acct','2026-09-26',7,100,2,2,102,65000,'2026-09-26 23:59:00');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateHourlyEquityAndDailySnapshotTables(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateAccountEquitySnapshotColumns(db); err != nil {
		t.Fatal(err)
	}
	store := &SQLStorage{db: db, dbType: "sqlite"}
	legacyAccountRows, err := store.QueryAccountEquityRecordsByMarketType("binance", "", "acct", time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if err != nil || len(legacyAccountRows) != 1 || legacyAccountRows[0].AccountEquity != 900 {
		t.Fatalf("legacy account sample should remain explicitly unclassified: rows=%+v err=%v", legacyAccountRows, err)
	}
	otherLegacyAccountRows, err := store.QueryAccountEquityRecordsByMarketType("binance", "", "acct2", time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC), time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))
	if err != nil || len(otherLegacyAccountRows) != 1 || otherLegacyAccountRows[0].AccountEquity != 700 {
		t.Fatalf("migration must preserve distinct legacy account identities at the same time: rows=%+v err=%v", otherLegacyAccountRows, err)
	}
	if err := migrateHourlyEquityAndDailySnapshotTables(db); err != nil {
		t.Fatalf("second migration should be idempotent: %v", err)
	}
	date := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	legacy, err := store.GetDailySnapshotByMarketType("binance", "unknown", "BTCUSDT", "acct", date)
	if err != nil || legacy == nil || legacy.UnrealizedPnL != 7 || legacy.MarketType != "" {
		t.Fatalf("legacy snapshot should be preserved as unclassified: snapshot=%+v err=%v", legacy, err)
	}
	for _, mt := range []string{"spot", "futures"} {
		qty := 0.5
		if err := store.SaveDailySnapshot(&DailySnapshot{Exchange: "binance", MarketType: mt, Symbol: "BTCUSDT", Account: "acct", Date: date, SnapshotTime: date, UnrealizedPnL: 11, UnrealizedPnLAsset: "USDT", TotalPositionValue: 200, ClosingPrice: 60000, SpotPositionQty: &qty}); err != nil {
			t.Fatalf("save %s snapshot: %v", mt, err)
		}
	}
	for _, mt := range []string{"spot", "futures"} {
		snapshot, err := store.GetDailySnapshotByMarketType("binance", mt, "BTCUSDT", "acct", date)
		if err != nil || snapshot == nil || snapshot.MarketType != mt || snapshot.UnrealizedPnL != 11 || snapshot.UnrealizedPnLAsset != "USDT" || snapshot.SpotPositionQty == nil || *snapshot.SpotPositionQty != 0.5 || snapshot.ClosingPrice != 60000 {
			t.Fatalf("market snapshot %s missing or contaminated: snapshot=%+v err=%v", mt, snapshot, err)
		}
	}

	qty := 0.5
	if err := store.SaveHourlyEquityRecord(&HourlyEquityRecord{Exchange: "binance", MarketType: "spot", Symbol: "BTCUSDT", Account: "acct", Timestamp: date, Equity: 50, UnrealizedPnLAsset: "USDT", MarketPrice: 60000, SpotPositionQty: &qty}); err != nil {
		t.Fatal(err)
	}
	hourly, err := store.QueryHourlyEquityRecordsByMarketType("binance", "spot", "BTCUSDT", "acct", date.Add(-time.Minute), date.Add(time.Minute))
	if err != nil || len(hourly) != 1 || hourly[0].MarketType != "spot" || hourly[0].MarketPrice != 60000 || hourly[0].UnrealizedPnLAsset != "USDT" || hourly[0].SpotPositionQty == nil || *hourly[0].SpotPositionQty != 0.5 {
		t.Fatalf("hourly market snapshot mismatch: rows=%+v err=%v", hourly, err)
	}
}

func TestMarketTypeSchemaUpgradePreservesDistinctLegacyAccounts(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "market-scoped-legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE account_equity_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT, exchange TEXT NOT NULL, market_type TEXT NOT NULL DEFAULT '', account TEXT NOT NULL,
		timestamp DATETIME NOT NULL, account_equity REAL NOT NULL, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(exchange, market_type, account, timestamp));
		INSERT INTO account_equity_records(exchange,market_type,account,timestamp,account_equity) VALUES
		('binance','futures','acct-a','2026-09-26 12:00:00',100),('binance','futures','acct-b','2026-09-26 12:00:00',200);
		CREATE TABLE hourly_equity_records (id INTEGER PRIMARY KEY AUTOINCREMENT, exchange TEXT NOT NULL, market_type TEXT NOT NULL DEFAULT '', symbol TEXT NOT NULL, account TEXT NOT NULL, timestamp DATETIME NOT NULL, equity REAL NOT NULL, unrealized_pnl REAL NOT NULL, total_position_value REAL NOT NULL, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
		CREATE TABLE daily_snapshots (id INTEGER PRIMARY KEY AUTOINCREMENT, exchange TEXT NOT NULL, market_type TEXT NOT NULL DEFAULT '', symbol TEXT NOT NULL, account TEXT NOT NULL, date DATE NOT NULL, unrealized_pnl REAL NOT NULL, total_position_value REAL NOT NULL, intraday_max_drawdown REAL NOT NULL, intraday_max_drawdown_pct REAL NOT NULL, intraday_peak_equity REAL NOT NULL, closing_price REAL NOT NULL, snapshot_time TIMESTAMP NOT NULL, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, UNIQUE(exchange, market_type, symbol, account, date));
		INSERT INTO daily_snapshots(exchange,market_type,symbol,account,date,unrealized_pnl,total_position_value,intraday_max_drawdown,intraday_max_drawdown_pct,intraday_peak_equity,closing_price,snapshot_time) VALUES
		('binance','futures','BTCUSDT','acct-a','2026-09-26',1,100,1,1,101,65000,'2026-09-26 23:59:00'),('binance','futures','BTCUSDT','acct-b','2026-09-26',2,200,2,1,202,65000,'2026-09-26 23:59:00');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateAccountEquityMarketTypeSQLite(db); err != nil {
		t.Fatal(err)
	}
	if err := migrateMarketTypeSnapshotColumnsSQLite(db); err != nil {
		t.Fatal(err)
	}
	store := &SQLStorage{db: db, dbType: "sqlite"}
	start := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	for account, want := range map[string]float64{"acct-a": 100, "acct-b": 200} {
		rows, err := store.QueryAccountEquityRecordsByMarketType("binance", "futures", account, start, start.Add(24*time.Hour))
		if err != nil || len(rows) != 1 || rows[0].AccountEquity != want {
			t.Fatalf("legacy equity %s lost during upgrade: rows=%+v err=%v", account, rows, err)
		}
		snapshot, err := store.GetDailySnapshotByMarketType("binance", "futures", "BTCUSDT", account, start)
		wantSnapshot := want / 100
		if err != nil || snapshot == nil || snapshot.UnrealizedPnL != wantSnapshot {
			t.Fatalf("legacy snapshot %s lost during upgrade: snapshot=%+v err=%v", account, snapshot, err)
		}
	}
}

func TestAccountEquityRecordsUpsertByAccountScopeAndTimestamp(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "account-equity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	timestamp := time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC)
	for _, value := range []float64{1000, 1010} {
		if err := store.SaveAccountEquityRecord(&AccountEquityRecord{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "same-prefix", Timestamp: timestamp, AccountEquity: value}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveAccountEquityRecord(&AccountEquityRecord{Exchange: "binance", MarketType: "futures", AccountScope: "scope-b", Account: "same-prefix", Timestamp: timestamp, AccountEquity: 700}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAccountEquityRecord(&AccountEquityRecord{Exchange: "binance", MarketType: "spot", AccountScope: "scope-a", Account: "same-prefix", Timestamp: timestamp, AccountEquity: 250}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.QueryAccountEquityRecordsByScope("binance", "futures", "scope-a", timestamp.Add(-time.Minute), timestamp.Add(time.Minute))
	if err != nil || len(rows) != 1 || rows[0].AccountEquity != 1010 || rows[0].MarketType != "futures" {
		t.Fatalf("account equity sample should upsert once per scope/time: rows=%+v err=%v", rows, err)
	}
	otherRows, err := store.QueryAccountEquityRecordsByScope("binance", "futures", "scope-b", timestamp.Add(-time.Minute), timestamp.Add(time.Minute))
	if err != nil || len(otherRows) != 1 || otherRows[0].AccountEquity != 700 {
		t.Fatalf("accounts sharing a display prefix collided: rows=%+v err=%v", otherRows, err)
	}
	spotRows, err := store.QueryAccountEquityRecordsByScope("binance", "spot", "scope-a", timestamp.Add(-time.Minute), timestamp.Add(time.Minute))
	if err != nil || len(spotRows) != 1 || spotRows[0].AccountEquity != 250 {
		t.Fatalf("spot equity sample collided with futures: rows=%+v err=%v", spotRows, err)
	}
}

func TestAccountEquityRecordsRejectInvalidSamplesWithoutOverwritingValidData(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "invalid-account-equity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	timestamp := time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)
	valid := &AccountEquityRecord{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Timestamp: timestamp, AccountEquity: 1010}
	if err := store.SaveAccountEquityRecord(valid); err != nil {
		t.Fatal(err)
	}
	invalidRecords := []*AccountEquityRecord{
		nil,
		{MarketType: "futures", AccountScope: "scope-a", Account: "acct", Timestamp: timestamp, AccountEquity: 1},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Timestamp: timestamp, AccountEquity: 1},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", AccountEquity: 1},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Timestamp: timestamp, AccountEquity: math.NaN()},
		{Exchange: "binance", MarketType: "futures", AccountScope: "scope-a", Account: "acct", Timestamp: timestamp, AccountEquity: math.Inf(1)},
	}
	for index, invalid := range invalidRecords {
		if err := store.SaveAccountEquityRecord(invalid); err == nil {
			t.Fatalf("invalid sample %d was accepted", index)
		}
	}
	rows, err := store.QueryAccountEquityRecordsByScope("binance", "futures", "scope-a", timestamp.Add(-time.Minute), timestamp.Add(time.Minute))
	if err != nil || len(rows) != 1 || rows[0].AccountEquity != 1010 {
		t.Fatalf("invalid sample changed valid account equity: rows=%+v err=%v", rows, err)
	}
}

func TestPositionEquitySnapshotsRejectNonFiniteValues(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "invalid-position-equity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ts := time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC)
	validHourly := &HourlyEquityRecord{Exchange: "binance", MarketType: "spot", AccountScope: "scope-a", Symbol: "BTCUSDT", Account: "acct", Timestamp: ts}
	validDaily := &DailySnapshot{Exchange: "binance", MarketType: "spot", AccountScope: "scope-a", Symbol: "BTCUSDT", Account: "acct", Date: ts, SnapshotTime: ts}
	for _, invalid := range []*HourlyEquityRecord{nil, {Timestamp: ts, Equity: math.NaN()}, {Timestamp: ts, UnrealizedPnL: math.Inf(1)}, {Timestamp: ts, TotalPositionValue: math.Inf(-1)}, {Timestamp: ts, MarketPrice: math.NaN()}, {Timestamp: ts, SpotPositionQty: floatPointer(math.NaN())}, {Timestamp: ts, AccountEquity: floatPointer(math.Inf(1))}} {
		if err := store.SaveHourlyEquityRecord(invalid); err == nil {
			t.Errorf("SaveHourlyEquityRecord(%+v) accepted non-finite data", invalid)
		}
	}
	if err := store.SaveHourlyEquityRecord(validHourly); err != nil {
		t.Fatalf("valid hourly snapshot rejected: %v", err)
	}
	for _, invalid := range []*DailySnapshot{nil, {Date: ts, SnapshotTime: ts, UnrealizedPnL: math.NaN()}, {Date: ts, SnapshotTime: ts, TotalPositionValue: math.Inf(1)}, {Date: ts, SnapshotTime: ts, IntradayMaxDrawdown: math.Inf(-1)}, {Date: ts, SnapshotTime: ts, IntradayMaxDrawdownPct: math.NaN()}, {Date: ts, SnapshotTime: ts, IntradayPeakEquity: math.Inf(1)}, {Date: ts, SnapshotTime: ts, ClosingPrice: math.NaN()}, {Date: ts, SnapshotTime: ts, AccountEquity: floatPointer(math.NaN())}, {Date: ts, SnapshotTime: ts, SpotPositionQty: floatPointer(math.Inf(1))}} {
		if err := store.SaveDailySnapshot(invalid); err == nil {
			t.Errorf("SaveDailySnapshot(%+v) accepted non-finite data", invalid)
		}
	}
	if err := store.SaveDailySnapshot(validDaily); err != nil {
		t.Fatalf("valid daily snapshot rejected: %v", err)
	}
}

func TestPositionEquityAndDailySnapshotsSeparateCredentialScopes(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "scoped-position-equity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ts := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	for _, sample := range []struct {
		scope  string
		equity float64
	}{{"scope-a", 100}, {"scope-b", 900}} {
		if err := store.SaveHourlyEquityRecord(&HourlyEquityRecord{Exchange: "binance", MarketType: "futures", AccountScope: sample.scope, Symbol: "BTCUSDT", Account: "same-prefix", Timestamp: ts, Equity: sample.equity}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveDailySnapshot(&DailySnapshot{Exchange: "binance", MarketType: "futures", AccountScope: sample.scope, Symbol: "BTCUSDT", Account: "same-prefix", Date: ts, SnapshotTime: ts, TotalPositionValue: sample.equity}); err != nil {
			t.Fatal(err)
		}
	}
	for _, sample := range []struct {
		scope string
		want  float64
	}{{"scope-a", 100}, {"scope-b", 900}} {
		hourly, err := store.QueryHourlyEquityRecordsByScope("binance", "futures", "BTCUSDT", sample.scope, ts.Add(-time.Minute), ts.Add(time.Minute))
		if err != nil || len(hourly) != 1 || hourly[0].Equity != sample.want {
			t.Fatalf("hourly scope %s: rows=%+v err=%v", sample.scope, hourly, err)
		}
		snapshot, err := store.GetDailySnapshotByScope("binance", "futures", "BTCUSDT", sample.scope, ts)
		if err != nil || snapshot == nil || snapshot.TotalPositionValue != sample.want {
			t.Fatalf("daily scope %s: snapshot=%+v err=%v", sample.scope, snapshot, err)
		}
	}
}
