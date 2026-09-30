package main

import (
	"context"
	"errors"
	"testing"

	"quantmesh/exchange"
	"quantmesh/storage"
)

type fundingPerpSpreadLedgerExchange struct {
	exchange.IExchange
	order *exchange.Order
	fills []*exchange.OrderFill
}

func (e *fundingPerpSpreadLedgerExchange) GetName() string       { return "binance" }
func (e *fundingPerpSpreadLedgerExchange) GetMarketType() string { return "futures" }
func (e *fundingPerpSpreadLedgerExchange) GetOrder(context.Context, string, int64) (*exchange.Order, error) {
	return e.order, nil
}
func (e *fundingPerpSpreadLedgerExchange) GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error) {
	return e.order, nil
}
func (e *fundingPerpSpreadLedgerExchange) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	return e.fills, nil
}

type fundingPerpSpreadLedgerWriter struct {
	fills []*storage.OrderFill
	err   error
}

func (w *fundingPerpSpreadLedgerWriter) SaveOrderFill(fill *storage.OrderFill) error {
	if w.err != nil {
		return w.err
	}
	w.fills = append(w.fills, fill)
	return nil
}

func TestFundingPerpSpreadExecutionRecorderSeparatesResolvedOrderFromLedgerFailure(t *testing.T) {
	request := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 0.5, ClientOrderID: "spread-cid"}
	placed := &exchange.Order{OrderID: 83, ClientOrderID: "spread-cid", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Quantity: 0.5, ExecutedQty: 0.5, Status: exchange.OrderStatusFilled}
	client := &fundingPerpSpreadLedgerExchange{fills: []*exchange.OrderFill{{
		OrderID: 83, TradeID: "trade-83", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100,
		Quantity: 0.5, TradeTime: 1_790_000_000_000,
	}}}
	writer := &fundingPerpSpreadLedgerWriter{err: errors.New("storage unavailable")}
	recorder := newFundingPerpSpreadExecutionRecorder(writer, "bot-1", []fundingPerpSpreadIncomeTarget{{
		Exchange: "binance", Symbol: "BTCUSDT", AccountScope: "account-scope-1",
	}})
	resolved, err := recorder(context.Background(), client, request, placed)
	if err == nil || !resolved {
		t.Fatalf("resolved order plus persistence failure = (%t, %v), want (true, error)", resolved, err)
	}
}

func TestFundingPerpSpreadExecutionRecorderResolvesAndPersistsExactOrderFills(t *testing.T) {
	request := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 0.5, ClientOrderID: "spread-cid"}
	placed := &exchange.Order{OrderID: 81, ClientOrderID: "spread-cid", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Quantity: 0.5, Status: exchange.OrderStatusNew}
	terminal := &exchange.Order{OrderID: 81, ClientOrderID: "spread-cid", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Quantity: 0.5, ExecutedQty: 0.5, AvgPrice: 100, Status: exchange.OrderStatusFilled}
	client := &fundingPerpSpreadLedgerExchange{order: terminal, fills: []*exchange.OrderFill{{
		OrderID: 81, TradeID: "trade-81", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.5, QuoteQuantity: 50, Commission: 0.02, CommissionAsset: "USDT", TradeTime: 1_790_000_000_000,
	}}}
	writer := &fundingPerpSpreadLedgerWriter{}
	recorder := newFundingPerpSpreadExecutionRecorder(writer, "bot-1", []fundingPerpSpreadIncomeTarget{{
		Exchange: "binance", Symbol: "BTCUSDT", AccountScope: "account-scope-1",
	}})
	resolved, err := recorder(context.Background(), client, request, placed)
	if err != nil {
		t.Fatalf("record resolved fills: %v", err)
	}
	if !resolved {
		t.Fatal("terminal order identity was not marked resolved")
	}
	if len(writer.fills) != 1 {
		t.Fatalf("persisted fill count = %d, want 1", len(writer.fills))
	}
	f := writer.fills[0]
	if f.TradeID != "trade-81" || f.OrderID != 81 || f.AccountScope != "account-scope-1" || f.BotID != "bot-1" || f.Quantity != 0.5 {
		t.Fatalf("persisted fill lost exact ownership or execution fields: %+v", f)
	}
}

func TestFundingPerpSpreadExecutionRecorderRecoversMissingOrderIDByClientID(t *testing.T) {
	request := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 0.25, ClientOrderID: "recover-cid"}
	client := &fundingPerpSpreadLedgerExchange{order: &exchange.Order{
		OrderID: 84, ClientOrderID: "recover-cid", Symbol: "BTCUSDT", Side: exchange.SideSell,
		Quantity: 0.25, ExecutedQty: 0.25, Status: exchange.OrderStatusFilled,
	}, fills: []*exchange.OrderFill{{
		OrderID: 84, TradeID: "trade-84", Symbol: "BTCUSDT", Side: exchange.SideSell,
		Price: 100, Quantity: 0.25, TradeTime: 1_790_000_000_000,
	}}}
	writer := &fundingPerpSpreadLedgerWriter{}
	recorder := newFundingPerpSpreadExecutionRecorder(writer, "bot-1", []fundingPerpSpreadIncomeTarget{{
		Exchange: "binance", Symbol: "BTCUSDT", AccountScope: "account-scope-1",
	}})
	resolved, err := recorder(context.Background(), client, request, nil)
	if err != nil || !resolved {
		t.Fatalf("recover order without response ID: resolved=%t err=%v", resolved, err)
	}
	if len(writer.fills) != 1 || writer.fills[0].OrderID != 84 {
		t.Fatalf("recovered executions = %+v, want exact order 84", writer.fills)
	}
}

func TestFundingPerpSpreadExecutionRecorderRejectsMismatchedClientOrderID(t *testing.T) {
	request := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 0.5, ClientOrderID: "expected-cid"}
	placed := &exchange.Order{OrderID: 82, ClientOrderID: "foreign-cid", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Quantity: 0.5, ExecutedQty: 0.5, Status: exchange.OrderStatusFilled}
	writer := &fundingPerpSpreadLedgerWriter{}
	client := &fundingPerpSpreadLedgerExchange{fills: []*exchange.OrderFill{{
		OrderID: 82, TradeID: "trade-82", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5, TradeTime: 1_790_000_000_000,
	}}}
	recorder := newFundingPerpSpreadExecutionRecorder(writer, "bot-1", []fundingPerpSpreadIncomeTarget{{
		Exchange: "binance", Symbol: "BTCUSDT", AccountScope: "account-scope-1",
	}})
	resolved, err := recorder(context.Background(), client, request, placed)
	if err == nil || resolved {
		t.Fatal("accepted an order with a client ID different from the durable strategy intent")
	}
	if len(writer.fills) != 0 {
		t.Fatalf("persisted %d fills for an unowned order", len(writer.fills))
	}
}
