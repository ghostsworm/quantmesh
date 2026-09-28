package storage

import (
	"database/sql"
	"testing"
)

func TestBackfillTradesBotIDFromOrdersPreservesAndRecoversOwnership(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("open in-memory database:", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	_, err = db.Exec(`CREATE TABLE orders (order_id INTEGER, bot_id TEXT);
		CREATE TABLE legacy_paired_trades (id INTEGER PRIMARY KEY, sell_order_id INTEGER, buy_order_id INTEGER, bot_id TEXT);`)
	if err != nil {
		t.Fatal("create legacy migration fixture:", err)
	}
	_, err = db.Exec(`INSERT INTO orders (order_id, bot_id) VALUES
		(101, 'sell-owner'), (102, 'buy-owner'), (103, 'buy-only-owner');
		INSERT INTO legacy_paired_trades (id, sell_order_id, buy_order_id, bot_id) VALUES
		(1, 101, 102, ''), (2, 999, 103, ''), (3, 101, 102, 'existing-owner'), (4, 999, 998, '');`)
	if err != nil {
		t.Fatal("insert legacy migration rows:", err)
	}

	if err := backfillTradesBotIDFromOrders(db, "legacy_paired_trades"); err != nil {
		t.Fatalf("backfill legacy ownership: %v", err)
	}
	if err := backfillTradesBotIDFromOrders(db, "legacy_paired_trades"); err != nil {
		t.Fatalf("repeat legacy ownership backfill: %v", err)
	}
	want := map[int]string{1: "sell-owner", 2: "buy-only-owner", 3: "existing-owner", 4: ""}
	rows, err := db.Query(`SELECT id, bot_id FROM legacy_paired_trades ORDER BY id`)
	if err != nil {
		t.Fatal("read migrated ownership:", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var botID string
		if err := rows.Scan(&id, &botID); err != nil {
			t.Fatal("scan migrated ownership:", err)
		}
		if botID != want[id] {
			t.Errorf("row %d bot_id = %q, want %q", id, botID, want[id])
		}
		delete(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal("iterate migrated ownership:", err)
	}
	if len(want) != 0 {
		t.Errorf("migration omitted rows: %+v", want)
	}
}

func TestBackfillTradesBotIDFromOrdersReturnsFailure(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("open in-memory database:", err)
	}
	defer db.Close()

	if err := backfillTradesBotIDFromOrders(db, "missing_legacy_table"); err == nil {
		t.Fatal("backfill must return an error when its migration source tables are missing")
	}
}
