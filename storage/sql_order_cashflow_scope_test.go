package storage

import (
	"os"
	"testing"
	"time"
)

func TestQueryDailyOrderCashflowByScopeAggregatesBeyondPageLimit(t *testing.T) {
	path := t.TempDir() + "/order-cashflow-scope.db"
	st, err := NewSQLStorage(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	defer os.Remove(path)

	dayStart := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	tx, err := st.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO orders (order_id, bot_id, account, market_type, account_scope, symbol, side, exchange, price, quantity, filled_qty, status, realized_pnl, created_at, updated_at) VALUES (?, 'bot-a', 'acct', 'futures', 'scope-a', 'BTCUSDT', 'BUY', 'binance', 2, 1, 1, 'FILLED', 0.25, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10005; i++ {
		if _, err := stmt.Exec(i, dayStart.Add(time.Hour), dayStart.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	unrelatedPnL := 999.0
	for _, order := range []Order{
		{OrderID: 20001, BotID: "bot-a", Account: "acct", MarketType: "spot", AccountScope: "scope-a", Symbol: "BTCUSDT", Side: "BUY", Exchange: "binance", Price: 500, Quantity: 1, FilledQty: 1, Status: "FILLED", RealizedPnL: &unrelatedPnL, CreatedAt: dayStart.Add(time.Hour), UpdatedAt: dayStart.Add(time.Hour)},
		{OrderID: 20002, BotID: "bot-a", Account: "acct", MarketType: "futures", AccountScope: "scope-b", Symbol: "BTCUSDT", Side: "BUY", Exchange: "binance", Price: 500, Quantity: 1, FilledQty: 1, Status: "FILLED", RealizedPnL: &unrelatedPnL, CreatedAt: dayStart.Add(time.Hour), UpdatedAt: dayStart.Add(time.Hour)},
		{OrderID: 20003, BotID: "bot-a", Account: "acct", MarketType: "futures", AccountScope: "scope-a", Symbol: "BTCUSDT", Side: "BUY", Exchange: "binance", Price: 7, Quantity: 7, FilledQty: 7, Status: "FILLED", CreatedAt: dayStart.Add(-time.Hour), UpdatedAt: dayStart.Add(-time.Hour)},
	} {
		if err := st.SaveOrder(&order); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.QueryDailyOrderCashflowByScope("acct", "binance", "futures", "BTCUSDT", "scope-a", "bot-a", dayStart, dayStart.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got.BuyOrders != 10005 || got.BuyQty != 10005 || got.BuyValue != 20010 || got.RealizedPnL != 2501.25 {
		t.Fatalf("daily aggregation was truncated or mixed dimensions: %+v", got)
	}
	if got.StartBuyQty != 7 || got.StartSellQty != 0 {
		t.Fatalf("beginning inventory must use the same account scope: %+v", got)
	}
	winners, losers, err := st.QueryTopDailyOrdersByScope("acct", "binance", "futures", "BTCUSDT", "scope-a", "bot-a", dayStart, dayStart.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(winners) != 20 || len(losers) != 0 || winners[0].RealizedPnL == nil || *winners[0].RealizedPnL != 0.25 {
		t.Fatalf("top realized pnl must be scoped and SQL-ranked: winners=%+v losers=%+v", winners, losers)
	}
	existingIDs, err := st.GetExistingOrderIDsByScope("binance", "futures", "BTCUSDT", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(existingIDs) != 10006 || existingIDs[20001] || existingIDs[20002] || !existingIDs[20003] {
		t.Fatalf("existing order deduplication crossed credential or market scope: count=%d includesOtherMarket=%v includesOtherScope=%v includesPrior=%v", len(existingIDs), existingIDs[20001], existingIDs[20002], existingIDs[20003])
	}
	bachedIDs, err := st.GetExistingOrderIDsForScope("binance", "futures", "BTCUSDT", "scope-a", []int64{20003, 20002})
	if err != nil {
		t.Fatal(err)
	}
	if len(bachedIDs) != 1 || !bachedIDs[20003] || bachedIDs[20002] {
		t.Fatalf("batch order lookup must return only matching IDs: %+v", bachedIDs)
	}
	if _, err := st.QueryDailyOrderCashflowByScope("acct", "binance", "futures", "BTCUSDT", "", "", dayStart, dayStart.Add(24*time.Hour)); err == nil {
		t.Fatal("missing credential scope must fail closed")
	}
}
