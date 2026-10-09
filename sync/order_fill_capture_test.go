package sync

import (
	"context"
	"errors"
	"testing"
	"time"

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

type captureAtomicFillWriter struct {
	batches [][]*storage.OrderFill
}

func (*captureAtomicFillWriter) SaveOrderFill(*storage.OrderFill) error {
	return errors.New("individual writes bypassed atomic writer")
}

func (w *captureAtomicFillWriter) SaveOrderFillsAtomic(fills []*storage.OrderFill) error {
	w.batches = append(w.batches, append([]*storage.OrderFill(nil), fills...))
	return nil
}

func TestPersistOwnedOrderFillsUsesAtomicBatchWriter(t *testing.T) {
	provider := captureFillProvider{fills: []*exchange.OrderFill{
		{OrderID: 42, TradeID: "a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.4, TradeTime: 1_790_000_000_000},
		{OrderID: 42, TradeID: "b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 101, Quantity: 0.6, TradeTime: 1_790_000_000_001},
	}}
	writer := &captureAtomicFillWriter{}
	update := position.OrderUpdate{OrderID: 42, Symbol: "BTCUSDT", Side: "BUY", ExecutedQty: 1}
	if err := PersistOwnedOrderFills(context.Background(), provider, writer, update, "binance", "futures", "scope", "acct", "bot"); err != nil {
		t.Fatal(err)
	}
	if len(writer.batches) != 1 || len(writer.batches[0]) != 2 {
		t.Fatalf("expected one atomic batch of two fills, got %+v", writer.batches)
	}
}

func TestPersistOwnedOrderFillsUsesSQLStorageAtomicBatch(t *testing.T) {
	store, err := storage.NewSQLStorage(t.TempDir() + "/owned-order-fills.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tradeTime := int64(1_790_000_000_000)
	provider := captureFillProvider{fills: []*exchange.OrderFill{
		{OrderID: 77, TradeID: "atomic-a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.4, TradeTime: tradeTime},
		{OrderID: 77, TradeID: "atomic-b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 101, Quantity: 0.6, TradeTime: tradeTime + 1},
	}}
	update := position.OrderUpdate{OrderID: 77, Symbol: "BTCUSDT", Side: "BUY", ExecutedQty: 1}
	if err := PersistOwnedOrderFills(context.Background(), provider, store, update, "binance", "futures", "scope", "acct", "bot"); err != nil {
		t.Fatal(err)
	}
	start := time.UnixMilli(tradeTime - 1).UTC()
	summary, err := store.QueryDailyOrderFillsByScope("acct", "binance", "futures", "BTCUSDT", "scope", start, start.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if summary.FillCount != 2 || summary.BuyQty != 1 {
		t.Fatalf("production SQL writer did not persist complete order batch: %+v", summary)
	}
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

func TestPersistOwnedOrderFillsPreservesKnownZeroAndUnknownPnL(t *testing.T) {
	provider := captureFillProvider{fills: []*exchange.OrderFill{
		{OrderID: 42, TradeID: "known-zero", Symbol: "BTCUSDT", Side: exchange.SideSell, Price: 100, Quantity: 0.5, TradeTime: 1_790_000_000_000, RealizedPnLKnown: true},
		{OrderID: 42, TradeID: "unknown", Symbol: "BTCUSDT", Side: exchange.SideSell, Price: 100, Quantity: 0.5, TradeTime: 1_790_000_000_001},
	}}
	writer := &captureFillWriter{}
	update := position.OrderUpdate{OrderID: 42, Symbol: "BTCUSDT", ExecutedQty: 1}
	if err := PersistOwnedOrderFills(context.Background(), provider, writer, update, "binance", "futures", "scope", "acct", "bot"); err != nil {
		t.Fatal(err)
	}
	if len(writer.fills) != 2 || writer.fills[0].RealizedPnL == nil || *writer.fills[0].RealizedPnL != 0 || writer.fills[1].RealizedPnL != nil {
		t.Fatalf("known zero must remain distinct from missing PnL: %+v", writer.fills)
	}
}

func TestPersistOwnedOrderFillsFailsClosed(t *testing.T) {
	update := position.OrderUpdate{OrderID: 7, Symbol: "ETHUSDT", ExecutedQty: 2}
	valid := &exchange.OrderFill{OrderID: 7, TradeID: "trade", Symbol: "ETHUSDT", Side: exchange.SideSell, Price: 10, Quantity: 1, TradeTime: 1_790_000_000_000}
	cases := []struct {
		name     string
		provider captureFillProvider
		side     string
		noWrite  bool
	}{
		{name: "unsupported", provider: captureFillProvider{}},
		{name: "query error", provider: captureFillProvider{err: errors.New("offline")}},
		{name: "quantity mismatch", provider: captureFillProvider{fills: []*exchange.OrderFill{valid}}, noWrite: true},
		{name: "duplicate ID", provider: captureFillProvider{fills: []*exchange.OrderFill{valid, valid}}},
		{name: "invalid execution side", provider: captureFillProvider{fills: []*exchange.OrderFill{{OrderID: 7, TradeID: "bad-side", Symbol: "ETHUSDT", Side: "BID", Price: 10, Quantity: 2, TradeTime: 1_790_000_000_000}}}, noWrite: true},
		{name: "mismatched execution side", provider: captureFillProvider{fills: []*exchange.OrderFill{valid}}, side: "BUY", noWrite: true},
		{name: "invalid parent order side", provider: captureFillProvider{fills: []*exchange.OrderFill{valid}}, side: "BID", noWrite: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			writer := &captureFillWriter{}
			update.Side = test.side
			if err := PersistOwnedOrderFills(context.Background(), test.provider, writer, update, "binance", "futures", "scope", "acct", "bot"); err == nil {
				t.Fatal("incomplete execution evidence must be rejected")
			}
			if test.noWrite && len(writer.fills) != 0 {
				t.Fatalf("invalid executions must be rejected before persistence, got %d rows", len(writer.fills))
			}
		})
	}
}
