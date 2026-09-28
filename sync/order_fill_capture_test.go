package sync

import (
	"context"
	"errors"
	"testing"

	"quantmesh/exchange"
	"quantmesh/position"
	"quantmesh/storage"
)

type captureFillProvider struct {
	fills []*exchange.OrderFill
	err   error
}

func (p captureFillProvider) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	return p.fills, p.err
}

type captureFillWriter struct{ fills []*storage.OrderFill }

func (w *captureFillWriter) SaveOrderFill(fill *storage.OrderFill) error {
	w.fills = append(w.fills, fill)
	return nil
}

func TestPersistOwnedOrderFillsChecksScopeAndCumulativeQuantity(t *testing.T) {
	provider := captureFillProvider{fills: []*exchange.OrderFill{
		{OrderID: 42, TradeID: "a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.4, Commission: 0.01, CommissionAsset: "USDT", TradeTime: 1_790_000_000_000},
		{OrderID: 42, TradeID: "b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 101, Quantity: 0.6, Commission: 0.02, CommissionAsset: "USDT", TradeTime: 1_790_000_000_001},
	}}
	writer := &captureFillWriter{}
	update := position.OrderUpdate{OrderID: 42, Symbol: "BTCUSDT", Side: "BUY", ExecutedQty: 1}
	if err := PersistOwnedOrderFills(context.Background(), provider, writer, update, "binance", "futures", "scope", "acct", "bot"); err != nil {
		t.Fatal(err)
	}
	if len(writer.fills) != 2 || writer.fills[0].BotID != "bot" || writer.fills[0].AccountScope != "scope" || writer.fills[0].TradeID != "a" {
		t.Fatalf("unexpected persisted execution ledger rows: %+v", writer.fills)
	}
}

func TestPersistOwnedOrderFillsPreservesExchangeRebate(t *testing.T) {
	provider := captureFillProvider{fills: []*exchange.OrderFill{{OrderID: 42, TradeID: "rebate", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 1, Commission: -0.002, CommissionAsset: "USDT", TradeTime: 1_790_000_000_000}}}
	writer := &captureFillWriter{}
	update := position.OrderUpdate{OrderID: 42, Symbol: "BTCUSDT", ExecutedQty: 1}
	if err := PersistOwnedOrderFills(context.Background(), provider, writer, update, "bybit", "spot", "scope", "acct", "bot"); err != nil {
		t.Fatalf("negative commission rebate should be persisted: %v", err)
	}
	if len(writer.fills) != 1 || writer.fills[0].Commission != -0.002 {
		t.Fatalf("rebate amount and sign must be preserved, got %+v", writer.fills)
	}
}

func TestPersistOwnedOrderFillsFailsClosed(t *testing.T) {
	update := position.OrderUpdate{OrderID: 7, Symbol: "ETHUSDT", ExecutedQty: 2}
	valid := &exchange.OrderFill{OrderID: 7, TradeID: "trade", Symbol: "ETHUSDT", Side: exchange.SideSell, Price: 10, Quantity: 1, TradeTime: 1_790_000_000_000}
	cases := []struct {
		name     string
		provider captureFillProvider
	}{
		{name: "unsupported", provider: captureFillProvider{}},
		{name: "query error", provider: captureFillProvider{err: errors.New("offline")}},
		{name: "quantity mismatch", provider: captureFillProvider{fills: []*exchange.OrderFill{valid}}},
		{name: "duplicate ID", provider: captureFillProvider{fills: []*exchange.OrderFill{valid, valid}}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := PersistOwnedOrderFills(context.Background(), test.provider, &captureFillWriter{}, update, "binance", "futures", "scope", "acct", "bot"); err == nil {
				t.Fatal("incomplete execution evidence must be rejected")
			}
		})
	}
}
