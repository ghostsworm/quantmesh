package storage

import (
	"math"
	"testing"
	"time"
)

func TestPartialTradeRowsHaveDistinctStableIdentityAndAggregate(t *testing.T) {
	st := newSQLStorageForTest(t)
	now := time.Now().UTC().Truncate(time.Second)
	for _, pnl := range []float64{5, 15} {
		if err := st.SaveTrade(&Trade{SellOrderID: 123, Exchange: "binance", Account: "test-account", Symbol: "BTCUSDT", Quantity: 0.5, PnL: pnl, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := st.QueryTrades(now.Add(-time.Second), now.Add(time.Second), 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID == 0 || rows[1].ID == 0 || rows[0].ID == rows[1].ID {
		t.Fatalf("missing independent identities: %+v", rows)
	}
	repeated, err := st.QueryTrades(now.Add(-time.Second), now.Add(time.Second), 10, 0)
	if err != nil || repeated[0].ID != rows[0].ID {
		t.Fatal("identity/order changed on repeat read")
	}
	aggregate, err := st.GetTradesBySellOrderIDs([]int64{123})
	if err != nil {
		t.Fatal(err)
	}
	if aggregate[123] != 20 {
		t.Fatalf("partial fills overwritten: %v", aggregate)
	}
}

func TestSaveTradeIdempotentDeduplicatesRetriesAndRejectsKeyReuse(t *testing.T) {
	st := newSQLStorageForTest(t)
	trade := &Trade{
		ExecutionKey: "dca-fill-1", BuyOrderID: 10, SellOrderID: 20, BotID: "bot-a", Exchange: "binance",
		MarketType: "futures", Symbol: "BTCUSDT", BuyPrice: 100, SellPrice: 110,
		Quantity: 0.5, PnL: 5, ExchangePnL: 4.9, Fee: 0.1, FeeAsset: "USDT", CreatedAt: time.Now().UTC(),
	}
	if err := st.SaveTradeIdempotent(trade); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTradeIdempotent(trade); err != nil {
		t.Fatalf("identical retry should be idempotent: %v", err)
	}
	rows, err := st.QueryTrades(trade.CreatedAt.Add(-time.Second), trade.CreatedAt.Add(time.Second), 10, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("retry wrote duplicate rows: rows=%d err=%v", len(rows), err)
	}
	conflict := *trade
	conflict.PnL = 500
	if err := st.SaveTradeIdempotent(&conflict); err == nil {
		t.Fatal("execution key reuse with different PnL must fail")
	}
}

func TestSaveTradeIdempotentRejectsNonFiniteEconomics(t *testing.T) {
	st := newSQLStorageForTest(t)
	base := &Trade{ExecutionKey: "invalid-economics", BuyPrice: 100, SellPrice: 101, Quantity: 1, PnL: 1, Fee: 0.1}
	tests := []struct {
		name   string
		mutate func(*Trade)
	}{
		{"non-finite pnl", func(trade *Trade) { trade.PnL = math.Inf(1) }},
		{"non-finite fee", func(trade *Trade) { trade.Fee = math.NaN() }},
		{"overflowed price deviation", func(trade *Trade) { trade.BuyPriceDeviation = math.Inf(-1) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trade := *base
			tt.mutate(&trade)
			if err := st.SaveTradeIdempotent(&trade); err == nil {
				t.Fatal("non-finite economics accepted")
			}
		})
	}
}

func TestSaveTradeIdempotentRejectsNegativeQuantity(t *testing.T) {
	st := newSQLStorageForTest(t)
	trade := &Trade{ExecutionKey: "negative-quantity", Quantity: -1}
	if err := st.SaveTradeIdempotent(trade); err == nil {
		t.Fatal("negative trade quantity accepted")
	}
}
