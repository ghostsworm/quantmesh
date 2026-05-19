package strategy

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

type signalTestExecutor struct {
	orders []*position.OrderRequest
	nextID int64
}

func (e *signalTestExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	e.nextID++
	copied := *req
	e.orders = append(e.orders, &copied)
	return &position.Order{
		OrderID:       e.nextID,
		ClientOrderID: req.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        "PLACED",
	}, nil
}

func (e *signalTestExecutor) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	return nil, false
}

func (e *signalTestExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return &position.BatchPlaceOrdersResult{}
}

func (e *signalTestExecutor) BatchCancelOrders(orderIDs []int64) error {
	return nil
}

type signalTestExchange struct{}

func (e *signalTestExchange) GetName() string { return "test" }
func (e *signalTestExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (e *signalTestExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (e *signalTestExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}
func (e *signalTestExchange) GetBaseAsset() string { return "BTC" }
func (e *signalTestExchange) CancelAllOrders(ctx context.Context, symbol string) error {
	return nil
}
func (e *signalTestExchange) GetAccount(ctx context.Context) (interface{}, error) {
	return nil, nil
}
func (e *signalTestExchange) GetPriceDecimals() int    { return 2 }
func (e *signalTestExchange) GetQuantityDecimals() int { return 6 }
func (e *signalTestExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*position.OrderBook, error) {
	return nil, nil
}
func (e *signalTestExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}
func (e *signalTestExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 100, nil
}
func (e *signalTestExchange) GetQuoteAsset() string { return "USDT" }
func (e *signalTestExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	return 1000, nil
}

func TestMeanReversionAutoTradesOpenAndCloseLong(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.MarketType = "futures"

	executor := &signalTestExecutor{}
	strategy := NewMeanReversionStrategy("mean_reversion", cfg, executor, &signalTestExchange{}, map[string]interface{}{
		"period":         2,
		"std_multiplier": 0.5,
		"order_amount":   100.0,
		"slippage":       0.001,
	})
	if err := strategy.Start(context.Background()); err != nil {
		t.Fatalf("start strategy: %v", err)
	}

	for _, price := range []float64{100, 100, 90} {
		if err := strategy.OnPriceChange(price); err != nil {
			t.Fatalf("price change: %v", err)
		}
	}
	if len(executor.orders) != 1 {
		t.Fatalf("expected one open order, got %d", len(executor.orders))
	}
	open := executor.orders[0]
	if open.Side != "BUY" || open.ReduceOnly {
		t.Fatalf("open order direction wrong: side=%s reduceOnly=%v", open.Side, open.ReduceOnly)
	}

	strategy.OnOrderUpdate(&position.OrderUpdate{
		OrderID:     1,
		Symbol:      "BTCUSDT",
		Status:      "FILLED",
		ExecutedQty: open.Quantity,
		AvgPrice:    open.Price,
	})
	if got := strategy.GetPositions(); len(got) != 1 || got[0].Size <= 0 {
		t.Fatalf("expected filled position, got %+v", got)
	}

	if err := strategy.OnPriceChange(110); err != nil {
		t.Fatalf("close price change: %v", err)
	}
	if len(executor.orders) != 2 {
		t.Fatalf("expected close order, got %d", len(executor.orders))
	}
	closeReq := executor.orders[1]
	if closeReq.Side != "SELL" || !closeReq.ReduceOnly {
		t.Fatalf("close order direction wrong: side=%s reduceOnly=%v", closeReq.Side, closeReq.ReduceOnly)
	}
}

func TestMomentumAndTrendVisualizationReportAutoTrading(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.MarketType = "futures"

	executor := &signalTestExecutor{}
	exchange := &signalTestExchange{}

	momentum := NewMomentumStrategy("momentum", cfg, executor, exchange, map[string]interface{}{"order_amount": 100.0})
	if err := momentum.Start(context.Background()); err != nil {
		t.Fatalf("start momentum: %v", err)
	}
	if got := momentum.GetVisualizationData()["autoTradingEnabled"]; got != true {
		t.Fatalf("momentum should report auto trading enabled, got %v", got)
	}

	trend := NewTrendFollowingStrategy("trend", cfg, executor, exchange, map[string]interface{}{"order_amount": 100.0})
	if err := trend.Start(context.Background()); err != nil {
		t.Fatalf("start trend: %v", err)
	}
	if got := trend.GetVisualizationData()["executionMode"]; got != "auto_trade" {
		t.Fatalf("trend should report auto_trade mode, got %v", got)
	}
}
