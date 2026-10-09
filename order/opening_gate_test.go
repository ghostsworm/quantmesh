package order

import (
	"context"
	"errors"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

type leaseRetryOrderExchange struct{ fakeOrderExchange }

func (e *leaseRetryOrderExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	e.placed = append(e.placed, req)
	if len(e.placed) == 1 {
		return nil, errors.New("-1003 rate limit")
	}
	return &exchange.Order{OrderID: int64(len(e.placed)), ClientOrderID: req.ClientOrderID, Status: exchange.OrderStatusFilled, ExecutedQty: req.Quantity}, nil
}

type gatedOrderExchange struct {
	fakeOrderExchange
	market string
}

func (e *gatedOrderExchange) GetMarketType() string { return e.market }

func TestOpeningGateAtPhysicalExecutor(t *testing.T) {
	for _, tc := range []struct {
		name, market, direction, leg, side string
		reduce, wantAllowed                bool
	}{
		{"grid long open", "futures", "LONG", "", "BUY", false, false},
		{"short open", "futures", "SHORT", "SHORT", "SELL", false, false},
		{"unclassified both", "futures", "BOTH", "", "SELL", false, false},
		{"long close", "futures", "LONG", "LONG", "SELL", true, true},
		{"short close", "futures", "SHORT", "SHORT", "BUY", true, true},
		{"unsafe futures non-reduce", "futures", "LONG", "LONG", "SELL", false, false},
		{"spot open", "spot", "LONG", "LONG", "BUY", false, false},
		{"spot inventory sale", "spot", "LONG", "LONG", "SELL", false, true},
		{"spot default direction close", "spot", "", "", "SELL", false, true},
		{"spot reduce flag cannot authorize buy", "spot", "LONG", "LONG", "BUY", true, false},
		{"spot short inventory rebuy", "spot", "LONG", "SHORT", "BUY", false, true},
		{"unknown spot leg", "spot", "BOTH", "", "SELL", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := &gatedOrderExchange{market: tc.market}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			var gate execution.OpeningGate
			oe.SetOpeningGate(&gate, tc.direction)
			gate.Block("test")
			req := &OrderRequest{Symbol: "BTCUSDT", Side: tc.side, Price: 100, Quantity: 1, PositionSide: tc.leg, ReduceOnly: tc.reduce, ClientOrderID: "test"}
			ord, err := oe.PlaceOrder(req)
			if tc.wantAllowed {
				if err != nil || ord == nil || len(ex.placed) != 1 {
					t.Fatalf("close blocked: order=%v err=%v calls=%d", ord, err, len(ex.placed))
				}
			} else if !errors.Is(err, execution.ErrOpeningPaused) || ord != nil || len(ex.placed) != 0 {
				t.Fatalf("opening reached exchange: order=%v err=%v calls=%d", ord, err, len(ex.placed))
			}
		})
	}
}

func TestOpeningGateBatchAndResume(t *testing.T) {
	ex := &gatedOrderExchange{market: "futures"}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	gate.Block("manual")
	reqs := []*OrderRequest{
		{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "open"},
		{Symbol: "BTCUSDT", Side: "SELL", Price: 101, Quantity: 1, ReduceOnly: true, ClientOrderID: "close"},
	}
	result := oe.BatchPlaceOrdersWithDetails(reqs)
	if len(result.PlacedOrders) != 1 || result.PlacedOrders[0].Side != "SELL" || len(ex.placed) != 1 {
		t.Fatalf("batch bypassed gate or lost close: %+v", result)
	}
	gate.Unblock("manual")
	if _, err := oe.PlaceOrder(reqs[0]); err != nil || len(ex.placed) != 2 {
		t.Fatalf("resume did not restore admission: %v", err)
	}
}

func TestOpeningAdmissionGuardRunsOnlyForOpeningsAndFailsClosed(t *testing.T) {
	ex := &gatedOrderExchange{market: "futures"}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	guardCalls := 0
	oe.SetOpeningAdmissionGuard(func(context.Context) error {
		guardCalls++
		return errors.New("wallet evidence unavailable")
	})
	opening := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1}
	if _, err := oe.PlaceOrder(opening); err == nil || guardCalls != 1 || len(ex.placed) != 0 {
		t.Fatalf("opening guard did not fail closed: calls=%d placed=%d err=%v", guardCalls, len(ex.placed), err)
	}
	closing := &OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true}
	if _, err := oe.PlaceOrder(closing); err != nil || guardCalls != 1 || len(ex.placed) != 1 {
		t.Fatalf("opening guard blocked protective close: calls=%d placed=%d err=%v", guardCalls, len(ex.placed), err)
	}
}

func TestOpeningAdmissionGuardRechecksBeforeEveryPhysicalRetry(t *testing.T) {
	ex := &leaseRetryOrderExchange{}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	guardCalls := 0
	leaseLost := errors.New("runtime ownership lease expired")
	oe.SetOpeningAdmissionGuard(func(context.Context) error {
		guardCalls++
		if guardCalls == 3 {
			return leaseLost
		}
		return nil
	})
	_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "lease-retry"})
	if !errors.Is(err, leaseLost) {
		t.Fatalf("retry after ownership loss error = %v, want %v", err, leaseLost)
	}
	if guardCalls != 3 || len(ex.placed) != 1 {
		t.Fatalf("ownership admission was not rechecked before each physical RPC: guard=%d submissions=%d", guardCalls, len(ex.placed))
	}
}
