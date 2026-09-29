package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestSQLStorageScanTradesContextOrdersAndStopsEarly(t *testing.T) {
	st := newSQLStorageForTest(t)
	base := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	for index, pnl := range []float64{10, -2, -3} {
		if err := st.SaveTrade(&Trade{
			Exchange: "binance", Account: "scan-test", Symbol: "BTCUSDT",
			PnLAsset: "USDT", FeeAsset: "USDT", PnL: pnl,
			CreatedAt: base.Add(time.Duration(index) * time.Minute),
		}); err != nil {
			t.Fatalf("SaveTrade(%d): %v", index, err)
		}
	}

	var gotIDs []int64
	var gotPnL []float64
	var gotAssets, gotFeeAssets []string
	err := st.ScanTradesContext(context.Background(), base.Add(-time.Minute), base.Add(time.Hour), func(trade *Trade) bool {
		gotIDs = append(gotIDs, trade.ID)
		gotPnL = append(gotPnL, trade.PnL)
		gotAssets = append(gotAssets, trade.PnLAsset)
		gotFeeAssets = append(gotFeeAssets, trade.FeeAsset)
		return len(gotIDs) < 2
	})
	if err != nil {
		t.Fatalf("ScanTradesContext: %v", err)
	}
	if len(gotIDs) != 2 || gotIDs[0] <= gotIDs[1] {
		t.Fatalf("expected newest-first scan to stop after two rows, got IDs %v", gotIDs)
	}
	if gotPnL[0] != -3 || gotPnL[1] != -2 {
		t.Fatalf("unexpected streamed PnL order: %v", gotPnL)
	}
	if gotAssets[0] != "USDT" || gotFeeAssets[0] != "USDT" {
		t.Fatalf("stream did not preserve denomination metadata: pnl_asset=%q fee_asset=%q", gotAssets[0], gotFeeAssets[0])
	}
}

func TestMigrateTradesPnLAssetLeavesLegacyRowsUnclassified(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE trades (id INTEGER PRIMARY KEY, symbol TEXT); INSERT INTO trades(symbol) VALUES ('BTCUSDT')`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := migrateTradesPnLAsset(db); err != nil {
		t.Fatalf("migrateTradesPnLAsset: %v", err)
	}
	if err := migrateTradesPnLAsset(db); err != nil {
		t.Fatalf("migrateTradesPnLAsset second run: %v", err)
	}
	var asset string
	if err := db.QueryRow(`SELECT pnl_asset FROM trades WHERE id = 1`).Scan(&asset); err != nil {
		t.Fatalf("read legacy PnL asset: %v", err)
	}
	if asset != "" {
		t.Fatalf("legacy PnL unit must remain unknown, got %q", asset)
	}
}
