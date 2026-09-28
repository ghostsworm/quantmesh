package storage

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMySQLBackfillTradesBotIDFromOrdersRejectsAmbiguousOwnership(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("open isolated MySQL test database:", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		t.Fatal("connect to isolated MySQL test database:", err)
	}

	fixtureID := time.Now().UTC().UnixNano()
	ordersTable := fmt.Sprintf("qm_backfill_orders_%d", fixtureID)
	tradesTable := fmt.Sprintf("qm_backfill_trades_%d", fixtureID)
	defer db.Exec("DROP TABLE IF EXISTS " + ordersTable)
	defer db.Exec("DROP TABLE IF EXISTS " + tradesTable)
	_, err = db.Exec(`CREATE TABLE ` + ordersTable + ` (
		order_id BIGINT, bot_id VARCHAR(128), exchange VARCHAR(64), account VARCHAR(255),
		market_type VARCHAR(32), account_scope VARCHAR(512)) ENGINE=InnoDB`)
	if err != nil {
		t.Fatal("create isolated order fixture:", err)
	}
	_, err = db.Exec(`CREATE TABLE ` + tradesTable + ` (
		id BIGINT PRIMARY KEY, sell_order_id BIGINT, buy_order_id BIGINT, bot_id VARCHAR(128),
		exchange VARCHAR(64), account VARCHAR(255), market_type VARCHAR(32), account_scope VARCHAR(512)) ENGINE=InnoDB`)
	if err != nil {
		t.Fatal("create isolated paired-trade fixture:", err)
	}
	_, err = db.Exec(`INSERT INTO ` + ordersTable + ` (order_id, bot_id, exchange, account, market_type, account_scope) VALUES
		(500, 'wrong-scope', 'okx', 'acct-a', 'futures', 'scope-a'),
		(500, 'owner-a', 'binance', 'acct-a', 'futures', 'scope-a'),
		(501, 'owner-b', 'binance', 'acct-a', 'futures', 'scope-a'),
		(502, 'owner-b', 'binance', 'acct-a', 'futures', 'scope-a'),
		(503, 'owner-left', 'binance', 'acct-a', 'futures', 'scope-a'),
		(504, 'owner-right', 'binance', 'acct-a', 'futures', 'scope-a'),
		(505, 'BotCase', 'binance', 'acct-a', 'futures', 'scope-a'),
		(505, 'botcase', 'binance', 'acct-a', 'futures', 'scope-a')`)
	if err != nil {
		t.Fatal("insert temporary order fixture:", err)
	}
	_, err = db.Exec(`INSERT INTO ` + tradesTable + ` (id, sell_order_id, buy_order_id, bot_id, exchange, account, market_type, account_scope) VALUES
		(1, 500, NULL, '', 'binance', 'acct-a', 'futures', 'scope-a'),
		(2, 501, 502, '', 'binance', 'acct-a', 'futures', 'scope-a'),
		(3, 503, 504, '', 'binance', 'acct-a', 'futures', 'scope-a'),
		(4, 505, NULL, '', 'binance', 'acct-a', 'futures', 'scope-a')`)
	if err != nil {
		t.Fatal("insert temporary paired-trade fixture:", err)
	}

	if err := backfillTradesBotIDFromOrders(db, tradesTable, ordersTable); err != nil {
		t.Fatalf("backfill MySQL legacy ownership: %v", err)
	}
	want := map[int64]string{1: "owner-a", 2: "owner-b", 3: "", 4: ""}
	rows, err := db.Query(`SELECT id, bot_id FROM ` + tradesTable + ` ORDER BY id`)
	if err != nil {
		t.Fatal("read migrated MySQL ownership:", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var botID string
		if err := rows.Scan(&id, &botID); err != nil {
			t.Fatal("scan migrated MySQL ownership:", err)
		}
		if botID != want[id] {
			t.Errorf("row %d bot_id = %q, want %q", id, botID, want[id])
		}
		delete(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal("iterate migrated MySQL ownership:", err)
	}
	if len(want) != 0 {
		t.Errorf("MySQL migration omitted rows: %+v", want)
	}
}
