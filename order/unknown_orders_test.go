package order

import (
	"context"
	"errors"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

func TestUnknownSubmissionBlocksStandaloneExecutor(t *testing.T) {
	ex := &fakeOrderExchange{placeErr: errors.New("timeout after send")}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1}); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatal(err)
	}
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 99, Quantity: 1}); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("standalone executor allowed exposure after UNKNOWN: %v", err)
	}
	if len(ex.placed) != 1 {
		t.Fatalf("unexpected sends: %d", len(ex.placed))
	}
}

type ambiguousAcknowledgementVenue struct{ fakeOrderExchange }

func (v *ambiguousAcknowledgementVenue) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	ord, _ := v.fakeOrderExchange.PlaceOrder(ctx, req)
	return ord, errors.New("insufficient response data after acceptance")
}

func TestAcknowledgementWithErrorCannotReleaseAcceptedIntent(t *testing.T) {
	v := &ambiguousAcknowledgementVenue{}
	oe := NewExchangeOrderExecutor(v, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "ack-error"})
	if !errors.Is(err, execution.ErrOrderUnknown) || len(v.placed) != 1 || !oe.IsOpeningPaused() {
		t.Fatalf("contradictory acknowledgement was released/retried: %v sends=%d", err, len(v.placed))
	}
	intents := oe.snapshotOwnedIntents()
	if len(intents) != 1 || !intents[0].unknown || intents[0].order == nil || intents[0].order.OrderID != 1 {
		t.Fatalf("lost authoritative identity: %+v", intents)
	}
}

func TestUnknownSubmissionNeverBlindlyRetries(t *testing.T) {
	ex := &gatedOrderExchange{market: "futures", fakeOrderExchange: fakeOrderExchange{placeErr: errors.New("connection reset after send")}}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "test")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1}
	if _, err := oe.PlaceOrder(req); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("ambiguous result must stay UNKNOWN: %v", err)
	}
	if len(ex.placed) != 1 || req.ClientOrderID == "" || !gate.Blocked() {
		t.Fatalf("calls=%d cid=%q blocked=%v", len(ex.placed), req.ClientOrderID, gate.Blocked())
	}
	gate.Unblock("opening_manager")
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 99, Quantity: 1}); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("new exposure was not blocked: %v", err)
	}
	if len(ex.placed) != 1 {
		t.Fatal("sent a new order after UNKNOWN")
	}
	ex.placeErr = nil
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 99, Quantity: 1, ReduceOnly: true}); err != nil {
		t.Fatalf("known inventory close should remain possible: %v", err)
	}
}

func TestAbsentLookupAfterTimeoutIsNotProofOfRejection(t *testing.T) {
	ex := &fakeCIDQuerierExchange{fakeOrderExchange: &fakeOrderExchange{placeErr: errors.New("deadline exceeded")}}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "uncertain"})
	if !errors.Is(err, execution.ErrOrderUnknown) || len(ex.placed) != 1 || ex.queryCalls != 1 {
		t.Fatalf("early not-found released or retried uncertain submission: %v, sends=%d query=%d", err, len(ex.placed), ex.queryCalls)
	}
}

func TestLostAcknowledgementFindsAlreadyFilledOrderWithoutResend(t *testing.T) {
	ex := &fakeCIDQuerierExchange{fakeOrderExchange: &fakeOrderExchange{placeErr: errors.New("deadline exceeded")},
		byCID: map[string]*exchange.Order{"filled": {OrderID: 42, ClientOrderID: "filled", Status: exchange.OrderStatusFilled,
			Price: 99, Quantity: 1.2, ExecutedQty: 1.2, AvgPrice: 98}}}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	ord, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "filled"})
	if err != nil || ord == nil || ord.OrderID != 42 || len(ex.placed) != 1 {
		t.Fatalf("filled lookup: order=%v err=%v calls=%d", ord, err, len(ex.placed))
	}
	if ord.Quantity != 1.2 || ord.Price != 98 || ord.ExecutedQty != 1.2 || ord.AvgPrice != 98 {
		t.Fatalf("lookup replaced execution with requested price/quantity: %+v", ord)
	}
}

func TestUnknownClosePreventsASecondCloseIntent(t *testing.T) {
	ex := &gatedOrderExchange{market: "spot", fakeOrderExchange: fakeOrderExchange{placeErr: errors.New("timeout")}}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	var gate execution.OpeningGate
	oe.SetOpeningGate(&gate, "LONG")
	req := OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ClientOrderID: "close-1", PositionSide: "LONG"}
	if _, err := oe.PlaceOrder(&req); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatal(err)
	}
	req.ClientOrderID = "close-2"
	if _, err := oe.PlaceOrder(&req); !errors.Is(err, execution.ErrIntentPending) || len(ex.placed) != 1 {
		t.Fatalf("uncertain spot close duplicated: %v calls=%d", err, len(ex.placed))
	}
}

func TestUnknownBatchHasSeparateOutcome(t *testing.T) {
	ex := &fakeOrderExchange{placeErr: errors.New("timeout")}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	res := oe.BatchPlaceOrdersWithDetails([]*OrderRequest{{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "uncertain"}})
	if !res.UnknownOrders["uncertain"] || len(res.PlacedOrders) != 0 || res.HasMarginError || len(res.ReduceOnlyErrors) != 0 {
		t.Fatalf("UNKNOWN collapsed into success/rejection: %+v", res)
	}
}
