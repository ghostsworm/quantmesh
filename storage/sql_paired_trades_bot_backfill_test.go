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

	_, err = db.Exec(`CREATE TABLE orders (order_id INTEGER, bot_id TEXT, exchange TEXT, account TEXT, market_type TEXT, symbol TEXT, account_scope TEXT);
		CREATE TABLE legacy_paired_trades (id INTEGER PRIMARY KEY, sell_order_id INTEGER, buy_order_id INTEGER, bot_id TEXT,
			exchange TEXT, account TEXT, market_type TEXT, symbol TEXT, account_scope TEXT);`)
	if err != nil {
		t.Fatal("create legacy migration fixture:", err)
	}
	_, err = db.Exec(`INSERT INTO orders (order_id, bot_id, exchange, account, market_type, symbol, account_scope) VALUES
		(101, 'wrong-exchange-owner', 'okx', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(101, 'wrong-account-owner', 'binance', 'acct-b', 'futures', 'BTCUSDT', 'scope-b'),
		(101, 'wrong-market-owner', 'binance', 'acct-a', 'spot', 'BTCUSDT', 'scope-a'),
		(101, 'sell-owner', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(102, 'sell-owner', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(103, 'buy-only-owner', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(104, 'wrong-scope-owner', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-b'),
		(105, 'conflicting-owner-a', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(105, 'conflicting-owner-b', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(106, 'sell-owner', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(107, 'different-buy-owner', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(108, 'BotCase', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(108, 'botcase', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(109, 'wrong-symbol-owner', 'binance', 'acct-a', 'futures', 'ETHUSDT', 'scope-a');
		INSERT INTO legacy_paired_trades (id, sell_order_id, buy_order_id, bot_id, exchange, account, market_type, symbol, account_scope) VALUES
		(1, 101, 102, '', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(2, 999, 103, '', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(3, 101, 102, 'existing-owner', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(4, 999, 998, '', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(5, 104, 998, '', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(6, 105, 103, '', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(7, 106, 107, '', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(8, 108, 998, '', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a'),
		(9, 109, 998, '', 'binance', 'acct-a', 'futures', 'BTCUSDT', 'scope-a');`)
	if err != nil {
		t.Fatal("insert legacy migration rows:", err)
	}

	if err := backfillTradesBotIDFromOrders(db, "legacy_paired_trades", "orders"); err != nil {
		t.Fatalf("backfill legacy ownership: %v", err)
	}
	if err := backfillTradesBotIDFromOrders(db, "legacy_paired_trades", "orders"); err != nil {
		t.Fatalf("repeat legacy ownership backfill: %v", err)
	}
	want := map[int]string{1: "sell-owner", 2: "buy-only-owner", 3: "existing-owner", 4: "", 5: "", 6: "", 7: "", 8: "", 9: ""}
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

	if err := backfillTradesBotIDFromOrders(db, "missing_legacy_table", "orders"); err == nil {
		t.Fatal("backfill must return an error when its migration source tables are missing")
	}
}
