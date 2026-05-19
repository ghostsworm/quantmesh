package strategy

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/position"
)

type hedgeOrderExecutor struct {
	orders []*position.OrderRequest
}

func (e *hedgeOrderExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	e.orders = append(e.orders, req)
	return &position.Order{OrderID: int64(len(e.orders)), Side: req.Side, Quantity: req.Quantity}, nil
}

func (e *hedgeOrderExecutor) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	out := make([]*position.Order, 0, len(orders))
	for _, req := range orders {
		order, _ := e.PlaceOrder(req)
		out = append(out, order)
	}
	return out, false
}

func (e *hedgeOrderExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	placed, hasErr := e.BatchPlaceOrders(orders)
	return &position.BatchPlaceOrdersResult{PlacedOrders: placed, HasMarginError: hasErr, ReduceOnlyErrors: map[string]bool{}}
}

func (e *hedgeOrderExecutor) BatchCancelOrders(orderIDs []int64) error {
	return nil
}

type hedgeExchange struct {
	positions []*position.PositionInfo
	price     float64
}

func (e *hedgeExchange) GetName() string { return "mock" }

func (e *hedgeExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	return e.positions, nil
}

func (e *hedgeExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}

func (e *hedgeExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}

func (e *hedgeExchange) GetBaseAsset() string { return "BTC" }

func (e *hedgeExchange) CancelAllOrders(ctx context.Context, symbol string) error { return nil }

func (e *hedgeExchange) GetAccount(ctx context.Context) (interface{}, error) { return nil, nil }

func (e *hedgeExchange) GetPriceDecimals() int { return 2 }

func (e *hedgeExchange) GetQuantityDecimals() int { return 6 }

func (e *hedgeExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*position.OrderBook, error) {
	return &position.OrderBook{}, nil
}

func (e *hedgeExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}

func (e *hedgeExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if e.price <= 0 {
		return 50000, nil
	}
	return e.price, nil
}

func (e *hedgeExchange) GetQuoteAsset() string { return "USDT" }

func (e *hedgeExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	return 100000, nil
}

func TestFuturesHedgeStrategiesCloseWhenTargetZero(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"

	shortExec := &hedgeOrderExecutor{}
	shortEx := &hedgeExchange{positions: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: -2}}, price: 50000}
	shortStrategy := NewFuturesShortStrategy("futures_short", cfg, shortExec, shortEx, map[string]interface{}{})
	shortStrategy.onHedgeSignal(&event.Event{Data: map[string]interface{}{
		"symbol":               "BTCUSDT",
		"target_futures_short": 0.0,
	}})
	if len(shortExec.orders) != 1 {
		t.Fatalf("期货空头目标归零应下平空单，实际订单数 %d", len(shortExec.orders))
	}
	if got := shortExec.orders[0]; got.Side != "BUY" || !got.ReduceOnly || got.Quantity != 2 {
		t.Fatalf("平空单=%+v want BUY reduce-only qty=2", got)
	}

	longExec := &hedgeOrderExecutor{}
	longEx := &hedgeExchange{positions: []*position.PositionInfo{{Symbol: "BTCUSDT", Size: 2}}, price: 50000}
	longStrategy := NewFuturesLongStrategy("futures_long", cfg, longExec, longEx, map[string]interface{}{})
	longStrategy.onHedgeSignal(&event.Event{Data: map[string]interface{}{
		"symbol":              "BTCUSDT",
		"target_futures_long": 0.0,
	}})
	if len(longExec.orders) != 1 {
		t.Fatalf("期货多头目标归零应下平多单，实际订单数 %d", len(longExec.orders))
	}
	if got := longExec.orders[0]; got.Side != "SELL" || !got.ReduceOnly || got.Quantity != 2 {
		t.Fatalf("平多单=%+v want SELL reduce-only qty=2", got)
	}
}
