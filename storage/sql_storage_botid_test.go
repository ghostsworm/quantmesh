package storage

import (
	"os"
	"testing"
	"time"
)

func TestGetStatisticsSummaryByExchangeAndSymbolBotID(t *testing.T) {
	dbPath := "./test_quantmesh_botid_stats.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLStorage(dbPath)
	if err != nil {
		t.Fatalf("NewSQLStorage: %v", err)
	}
	defer st.Close()

	acc := "acc1"
	ts := time.Now().UTC()
	base := Trade{
		Exchange:  "binance",
		Account:   acc,
		Symbol:    "BTCUSDT",
		BuyPrice:  1,
		SellPrice: 2,
		Quantity:  1,
		Fee:       0,
		CreatedAt: ts,
	}
	a := base
	a.BuyOrderID, a.SellOrderID, a.BotID, a.PnL = 1, 2, "bot-a", 10
	if err := st.SaveTrade(&a); err != nil {
		t.Fatal(err)
	}
	b := base
	b.BuyOrderID, b.SellOrderID, b.BotID, b.PnL = 3, 4, "bot-b", 20
	if err := st.SaveTrade(&b); err != nil {
		t.Fatal(err)
	}

	all, err := st.GetStatisticsSummaryByExchangeAndSymbol("binance", "BTCUSDT", acc, "")
	if err != nil {
		t.Fatal(err)
	}
	if all.TotalPnL != 30 {
		t.Fatalf("no bot filter: want TotalPnL 30, got %v", all.TotalPnL)
	}

	onlyA, err := st.GetStatisticsSummaryByExchangeAndSymbol("binance", "BTCUSDT", acc, "bot-a")
	if err != nil {
		t.Fatal(err)
	}
	if onlyA.TotalPnL != 10 {
		t.Fatalf("bot-a: want 10, got %v", onlyA.TotalPnL)
	}
}
