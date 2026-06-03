package order

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/lock"
)

func TestNewExchangeOrderExecutorTrimsBotID(t *testing.T) {
	var ex exchange.IExchange
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 1, 100, nil, "  bid-1  ")
	if oe.botID != "bid-1" {
		t.Fatalf("botID=%q", oe.botID)
	}
}

func TestIsMarginInsufficientError_OKX51008(t *testing.T) {
	if !isMarginInsufficientError("下單失败: 51008 - insufficient balance") {
		t.Fatal("expected 51008 to be margin insufficient")
	}
	if isMarginInsufficientError("unknown") {
		t.Fatal("unexpected")
	}
}

type fakeOrderExchange struct {
	exchange.IExchange
	placed          []*exchange.OrderRequest
	cancelled       []int64
	batchCancelled  [][]int64
	batchCancelErr  error
	cancelErr       error
	order           *exchange.Order
	openOrders      []*exchange.Order
	quantityDecimal int
}

func (f *fakeOrderExchange) GetName() string { return "fake" }
func (f *fakeOrderExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	f.placed = append(f.placed, req)
	return &exchange.Order{
		OrderID:       int64(len(f.placed)),
		ClientOrderID: req.ClientOrderID,
		Status:        exchange.OrderStatusFilled,
		ExecutedQty:   req.Quantity,
	}, nil
}
func (f *fakeOrderExchange) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	f.cancelled = append(f.cancelled, orderID)
	return f.cancelErr
}
func (f *fakeOrderExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	f.batchCancelled = append(f.batchCancelled, orderIDs)
	return f.batchCancelErr
}
func (f *fakeOrderExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	return f.order, nil
}
func (f *fakeOrderExchange) GetOpenOrders(ctx context.Context, symbol string) ([]*exchange.Order, error) {
	return f.openOrders, nil
}
func (f *fakeOrderExchange) GetQuantityDecimals() int { return f.quantityDecimal }
func (f *fakeOrderExchange) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	if reduceOnly {
		return price * quantity
	}
	return price*quantity + 1
}

type denyOrderLock struct {
	lock.DistributedLock
}

func (d denyOrderLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return false, nil
}

func TestExchangeOrderExecutorPlaceCancelAndQueryPaths(t *testing.T) {
	ex := &fakeOrderExchange{
		order:           &exchange.Order{Status: exchange.OrderStatusFilled, ExecutedQty: 0.5},
		openOrders:      []*exchange.Order{{OrderID: 7}},
		quantityDecimal: 3,
	}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-1")

	placed, err := oe.PlaceOrder(&OrderRequest{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Price:         50000,
		Quantity:      0.01,
		PriceDecimals: 2,
		PostOnly:      true,
		ClientOrderID: "cid-1",
		StrategyName:  "grid",
		StrategyType:  "grid",
	})
	if err != nil {
		t.Fatalf("PlaceOrder returned error: %v", err)
	}
	if placed.OrderID != 1 || placed.ClientOrderID != "cid-1" || len(ex.placed) != 1 {
		t.Fatalf("unexpected placed order: %#v placed=%#v", placed, ex.placed)
	}
	if !ex.placed[0].PostOnly || ex.placed[0].StrategyName != "grid" {
		t.Fatalf("exchange request not populated: %#v", ex.placed[0])
	}

	skippedExecutor := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, denyOrderLock{}, "")
	skipped, err := skippedExecutor.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 50000, Quantity: 0.01})
	if err != nil || skipped != nil {
		t.Fatalf("locked PlaceOrder = %#v err=%v, want nil nil", skipped, err)
	}

	if err := oe.CancelOrder(99); err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
	ex.cancelErr = errors.New("Unknown order sent")
	if err := oe.CancelOrder(100); err != nil {
		t.Fatalf("unknown order cancel should be ignored: %v", err)
	}

	status, executed, err := oe.CheckOrderStatus(7)
	if err != nil || status != string(exchange.OrderStatusFilled) || executed != 0.5 {
		t.Fatalf("CheckOrderStatus = %q %f err=%v", status, executed, err)
	}
	open, err := oe.GetOpenOrders()
	if err != nil || len(open) != 1 {
		t.Fatalf("GetOpenOrders = %#v err=%v", open, err)
	}
	if oe.GetQuantityDecimals() != 3 {
		t.Fatalf("quantity decimals = %d, want 3", oe.GetQuantityDecimals())
	}
	if got := oe.RoundQuantity(1.2349); got != 1.234 {
		t.Fatalf("RoundQuantity = %f, want 1.234", got)
	}
	if got := oe.EstimateFinalOrderAmount("BTCUSDT", 10, 2, false); got != 21 {
		t.Fatalf("EstimateFinalOrderAmount = %f, want 21", got)
	}
	if oe.GetSymbol() != "BTCUSDT" {
		t.Fatalf("GetSymbol = %s", oe.GetSymbol())
	}
}

func TestExchangeOrderExecutorBatchPaths(t *testing.T) {
	ex := &fakeOrderExchange{quantityDecimal: 2}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")

	orders, hasMargin := oe.BatchPlaceOrders([]*OrderRequest{
		{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1},
		{Symbol: "BTCUSDT", Side: "SELL", Price: 101, Quantity: 1},
	})
	if hasMargin || len(orders) != 2 {
		t.Fatalf("BatchPlaceOrders = len %d margin %v", len(orders), hasMargin)
	}

	if err := oe.BatchCancelOrders(nil); err != nil {
		t.Fatalf("empty BatchCancelOrders returned error: %v", err)
	}
	ex.batchCancelErr = errors.New("batch unavailable")
	if err := oe.BatchCancelOrders([]int64{1, 2}); err != nil {
		t.Fatalf("fallback BatchCancelOrders returned error: %v", err)
	}
	if len(ex.batchCancelled) != 1 || len(ex.cancelled) != 2 {
		t.Fatalf("unexpected cancel calls: batch=%#v single=%#v", ex.batchCancelled, ex.cancelled)
	}
}

func TestOrderErrorClassifiers(t *testing.T) {
	if !isPostOnlyError(errors.New("Post Only order will be rejected")) {
		t.Fatal("expected post-only error")
	}
	if isPostOnlyError(nil) || isPostOnlyError(errors.New("other")) {
		t.Fatal("unexpected post-only classification")
	}
	if !isReduceOnlyError(errors.New("-2022 ReduceOnly Order is rejected")) {
		t.Fatal("expected reduce-only error")
	}
	if isReduceOnlyError(nil) || isReduceOnlyError(errors.New("-4164 reduce only notional too small")) {
		t.Fatal("unexpected reduce-only classification")
	}
}
