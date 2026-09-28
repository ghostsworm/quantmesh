package strategy

import (
	"context"
	"errors"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/utils"
)

type brokerCapitalVenue struct {
	exchange.IExchange
	calls       int64
	status      exchange.OrderStatus
	executedQty float64
	unknown     bool
	restError   error
	beforeAck   func(*exchange.Order)
}

func (*brokerCapitalVenue) GetName() string       { return "binance" }
func (*brokerCapitalVenue) GetMarketType() string { return "futures" }
func (*brokerCapitalVenue) EstimateFinalOrderAmount(_ string, price, qty float64, _ bool) float64 {
	return price * qty
}
func (*brokerCapitalVenue) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return nil, nil
}
func (v *brokerCapitalVenue) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	v.calls++
	ord := &exchange.Order{OrderID: v.calls, ClientOrderID: utils.AddBrokerPrefix("binance", req.ClientOrderID),
		Symbol: req.Symbol, Side: req.Side, Price: req.Price, Quantity: req.Quantity,
		Status: v.status, ExecutedQty: v.executedQty, AvgPrice: req.Price}
	if v.beforeAck != nil {
		v.beforeAck(ord)
	}
	if v.restError != nil {
		return nil, v.restError
	}
	if v.unknown {
		return nil, errors.New("connection lost after accepting order")
	}
	return ord, nil
}

func newBrokerCapitalExecutor(v *brokerCapitalVenue) *MultiStrategyExecutor {
	cfg := &config.Config{}
	cfg.Trading.Direction = "LONG"
	allocator := NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("dca", 1, 0)
	allocator.Allocate()
	return NewMultiStrategyExecutor(order.NewExchangeOrderExecutor(v, "BTCUSDT", 0, 0, lock.NewNopLock(), ""), allocator)
}

func TestBrokerCapitalAcknowledgementUsesOneRecord(t *testing.T) {
	for _, batch := range []bool{false, true} {
		for _, status := range []exchange.OrderStatus{exchange.OrderStatusNew, exchange.OrderStatusPartiallyFilled, exchange.OrderStatusCanceled, exchange.OrderStatusFilled} {
			name := "single/" + string(status)
			if batch {
				name = "batch/" + string(status)
			}
			t.Run(name, func(t *testing.T) {
				qty := 0.4
				wantUsed, wantPosition := 100.0, 40.0
				if status == exchange.OrderStatusNew {
					qty, wantPosition = 0, 0
				}
				if status == exchange.OrderStatusCanceled {
					wantUsed = 40
				}
				if status == exchange.OrderStatusFilled {
					qty, wantPosition = 1, 100
				}
				v := &brokerCapitalVenue{status: status, executedQty: qty}
				mse := newBrokerCapitalExecutor(v)
				req := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "broker-open"}
				if batch {
					if got := mse.BatchPlaceOrdersWithDetails("dca", []*position.OrderRequest{req}); len(got.PlacedOrders) != 1 {
						t.Fatalf("batch acknowledgement: %+v", got)
					}
				} else if _, err := mse.PlaceOrder("dca", req); err != nil {
					t.Fatal(err)
				}
				assertStrategyUsed(t, mse.allocator, wantUsed)
				if got := mse.GetPositionCapital("dca", "LONG"); math.Abs(got-wantPosition) > 1e-9 {
					t.Fatalf("acknowledged fill was not booked: %v want %v", got, wantPosition)
				}
				if status == exchange.OrderStatusNew || status == exchange.OrderStatusPartiallyFilled {
					if mse.GetStrategyByClientOrderID(req.ClientOrderID) != "dca" || mse.GetStrategyByClientOrderID(utils.AddBrokerPrefix("binance", req.ClientOrderID)) != "dca" {
						t.Fatal("missing raw/broker route")
					}
				}
			})
		}
	}
}

func TestBrokerTerminalBeforeAcknowledgementIsNotResurrected(t *testing.T) {
	for _, batch := range []bool{false, true} {
		v := &brokerCapitalVenue{status: exchange.OrderStatusNew}
		mse := newBrokerCapitalExecutor(v)
		v.beforeAck = func(ord *exchange.Order) {
			if mse.GetStrategyByClientOrderID(ord.ClientOrderID) != "dca" {
				t.Fatal("broker route was unavailable before REST returned")
			}
			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: ord.OrderID, ClientOrderID: ord.ClientOrderID, Status: "CANCELED", ExecutedQty: 0.4})
		}
		req := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "early-broker"}
		if batch {
			mse.BatchPlaceOrdersWithDetails("dca", []*position.OrderRequest{req})
		} else if _, err := mse.PlaceOrder("dca", req); err != nil {
			t.Fatal(err)
		}
		assertStrategyUsed(t, mse.allocator, 40)
		if len(mse.ordersByClient) != 0 || len(mse.ordersByID) != 0 || len(mse.clientStrategies) != 0 {
			t.Fatalf("REST revived settled aliases: batch=%v", batch)
		}
		mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 1, ClientOrderID: utils.AddBrokerPrefix("binance", req.ClientOrderID), Status: "CANCELED", ExecutedQty: 0.4})
		assertStrategyUsed(t, mse.allocator, 40)
	}
}

func TestBrokerUnknownLateTerminalKeepsFilledCapital(t *testing.T) {
	v := &brokerCapitalVenue{unknown: true}
	mse := newBrokerCapitalExecutor(v)
	req := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "unknown-broker"}
	if _, err := mse.PlaceOrder("dca", req); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatal(err)
	}
	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 1, ClientOrderID: utils.AddBrokerPrefix("binance", req.ClientOrderID), Status: "CANCELED", ExecutedQty: 0.4})
	assertStrategyUsed(t, mse.allocator, 40)
	if len(mse.ordersByClient) != 0 || len(mse.ordersByID) != 0 {
		t.Fatal("terminal aliases were not cleaned together")
	}
}

func TestObservedTerminalCannotBeOverriddenByRESTRefusal(t *testing.T) {
	v := &brokerCapitalVenue{status: exchange.OrderStatusCanceled, executedQty: 0.4,
		restError: errors.New("insufficient balance")}
	mse := newBrokerCapitalExecutor(v)
	v.beforeAck = func(ord *exchange.Order) {
		mse.executor.ObserveOrder(ord)
		mse.OnOrderUpdate(&position.OrderUpdate{OrderID: ord.OrderID, ClientOrderID: ord.ClientOrderID,
			Status: string(ord.Status), ExecutedQty: ord.ExecutedQty})
	}
	_, err := mse.PlaceOrder("dca", &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "contradictory"})
	if !errors.Is(err, execution.ErrOrderUnknown) || !mse.executor.IsOpeningPaused() {
		t.Fatalf("observed fill was downgraded to deterministic refusal: %v", err)
	}
	assertStrategyUsed(t, mse.allocator, 40)
}
