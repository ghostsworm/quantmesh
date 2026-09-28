package strategy

import (
	"context"
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
)

type uncertainStrategyVenue struct {
	exchange.IExchange
	sends int
}

func (*uncertainStrategyVenue) GetName() string       { return "fake" }
func (*uncertainStrategyVenue) GetMarketType() string { return "futures" }
func (*uncertainStrategyVenue) EstimateFinalOrderAmount(_ string, price, qty float64, _ bool) float64 {
	return price * qty
}
func (v *uncertainStrategyVenue) PlaceOrder(context.Context, *exchange.OrderRequest) (*exchange.Order, error) {
	v.sends++
	return nil, errors.New("lost acknowledgement")
}
func (*uncertainStrategyVenue) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return nil, nil
}

func newUncertainStrategyExecutor() (*MultiStrategyExecutor, *uncertainStrategyVenue) {
	cfg := &config.Config{}
	cfg.Trading.Direction = "LONG"
	v := &uncertainStrategyVenue{}
	physical := order.NewExchangeOrderExecutor(v, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	physical.SetOpeningGate(&execution.OpeningGate{}, "LONG")
	allocator := NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("dca", 1, 0)
	allocator.Allocate()
	return NewMultiStrategyExecutor(physical, allocator), v
}

func TestUnknownStrategyOrderRetainsReservationAndRoute(t *testing.T) {
	mse, venue := newUncertainStrategyExecutor()
	req := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1}
	if _, err := mse.PlaceOrder("dca", req); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatal(err)
	}
	if mse.allocator.GetAvailable("dca") != 900 || req.ClientOrderID == "" || mse.GetStrategyByClientOrderID(req.ClientOrderID) != "dca" {
		t.Fatal("UNKNOWN lost reservation or pre-registered route")
	}
	if _, err := mse.PlaceOrder("dca", req); !errors.Is(err, execution.ErrIntentPending) || mse.allocator.GetAvailable("dca") != 900 || venue.sends != 1 {
		t.Fatalf("duplicate intent changed reservation or reached venue: %v", err)
	}
	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 42, ClientOrderID: req.ClientOrderID, Status: "PARTIALLY_FILLED", ExecutedQty: 0.4})
	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 42, ClientOrderID: req.ClientOrderID, Status: "CANCELED", ExecutedQty: 0.4})
	if mse.allocator.GetAvailable("dca") != 960 {
		t.Fatalf("partial fill capital lost: available=%f", mse.allocator.GetAvailable("dca"))
	}
}

func TestUnknownStrategyBatchRetainsOnlyUncertainCapital(t *testing.T) {
	mse, venue := newUncertainStrategyExecutor()
	reqs := []*position.OrderRequest{
		{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1},
		{Symbol: "BTCUSDT", Side: "BUY", Price: 90, Quantity: 1},
	}
	result := mse.BatchPlaceOrdersWithDetails("dca", reqs)
	if len(result.UnknownOrders) != 1 || !result.UnknownOrders[reqs[0].ClientOrderID] || len(result.PlacedOrders) != 0 || venue.sends != 1 {
		t.Fatalf("batch uncertainty classification: %+v sends=%d", result, venue.sends)
	}
	if mse.allocator.GetAvailable("dca") != 900 {
		t.Fatalf("unknown released or unsent retained: available=%f", mse.allocator.GetAvailable("dca"))
	}
}
