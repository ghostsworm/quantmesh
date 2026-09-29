package order

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

type contextOrderVenue struct {
	fakeOrderExchange
	onPlace func(context.Context, *exchange.OrderRequest) (*exchange.Order, error)
	queries int
}

func (v *contextOrderVenue) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	v.placed = append(v.placed, req)
	return v.onPlace(ctx, req)
}

func (v *contextOrderVenue) GetOpenOrders(ctx context.Context, symbol string) ([]*exchange.Order, error) {
	v.queries++
	return nil, nil
}

type cancelOrderLock struct {
	lock.DistributedLock
	cancel context.CancelFunc
}

func (l cancelOrderLock) TryLock(ctx context.Context, _ string, _ time.Duration) (bool, error) {
	l.cancel()
	<-ctx.Done()
	return false, ctx.Err()
}

func contextCloseRequest() *OrderRequest {
	return &OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Type: "MARKET", Quantity: 1, ReduceOnly: true, PositionSide: "LONG"}
}

func TestOrderContextCancelledBeforeSubmission(t *testing.T) {
	for _, stage := range []string{"entry", "lock", "rate"} {
		t.Run(stage, func(t *testing.T) {
			ex := &fakeOrderExchange{}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch stage {
			case "entry":
				cancel()
			case "lock":
				oe.lock = cancelOrderLock{cancel: cancel}
			case "rate":
				oe.rateLimiter = rate.NewLimiter(rate.Every(time.Hour), 1)
				oe.rateLimiter.Allow()
			}
			_, err := oe.PlaceOrderContext(ctx, contextCloseRequest())
			if err == nil || len(ex.placed) != 0 || len(oe.snapshotOwnedIntents()) != 0 || oe.IsOpeningPaused() {
				t.Fatalf("unsent request acquired risk: err=%v calls=%d", err, len(ex.placed))
			}
			if stage != "rate" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation cause: %v", err)
			}
		})
	}
}

func TestOrderContextCancellationAfterSendRemainsUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	v := &contextOrderVenue{}
	v.onPlace = func(callCtx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
		cancel()
		<-callCtx.Done()
		return nil, callCtx.Err()
	}
	oe := NewExchangeOrderExecutor(v, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	req := contextCloseRequest()
	_, err := oe.PlaceOrderContext(ctx, req)
	if !errors.Is(err, execution.ErrOrderUnknown) || len(v.placed) != 1 || v.queries != 0 || req.ClientOrderID == "" {
		t.Fatalf("cancelled send lost identity/retried: err=%v calls=%d queries=%d cid=%q", err, len(v.placed), v.queries, req.ClientOrderID)
	}
	intents := oe.snapshotOwnedIntents()
	if len(intents) != 1 || !intents[0].unknown || !oe.IsOpeningPaused() {
		t.Fatalf("uncertain request not retained: %+v", intents)
	}
	if _, err := oe.PlaceOrder(contextCloseRequest()); !errors.Is(err, execution.ErrIntentPending) || len(v.placed) != 1 {
		t.Fatalf("another close duplicated uncertain exposure: %v", err)
	}
}

func TestPriceLockRenewalFailureDuringSendRemainsUnknown(t *testing.T) {
	v := &contextOrderVenue{}
	v.onPlace = func(callCtx context.Context, _ *exchange.OrderRequest) (*exchange.Order, error) {
		<-callCtx.Done()
		return nil, callCtx.Err()
	}
	oe := NewExchangeOrderExecutor(v, "BTCUSDT", 0, 0,
		failOrderLockRenewal{DistributedLock: lock.NewNopLock()}, "")
	_, err := oe.PlaceOrderContext(context.Background(), &OrderRequest{
		Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "lost-lease-in-flight",
	})
	if !errors.Is(err, execution.ErrOrderUnknown) || errors.Is(err, ErrOrderLockLost) {
		t.Fatalf("in-flight lease loss error = %v, want UNKNOWN", err)
	}
	if len(v.placed) != 1 {
		t.Fatalf("venue submissions = %d, want one", len(v.placed))
	}
	intents := oe.snapshotOwnedIntents()
	if len(intents) != 1 || !intents[0].unknown || !oe.IsOpeningPaused() {
		t.Fatalf("in-flight order uncertainty not retained: intents=%+v paused=%t", intents, oe.IsOpeningPaused())
	}
}

func TestOrderContextCancelledDefinitiveRefusalDoesNotRetry(t *testing.T) {
	for _, refusal := range []string{"-1003 rate limit", "-5022 Post Only"} {
		t.Run(refusal, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			v := &contextOrderVenue{}
			v.onPlace = func(context.Context, *exchange.OrderRequest) (*exchange.Order, error) {
				cancel()
				return nil, errors.New(refusal)
			}
			oe := NewExchangeOrderExecutor(v, "BTCUSDT", 60, 0, lock.NewNopLock(), "")
			req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 100, PriceDecimals: 2, PostOnly: true}
			_, err := oe.PlaceOrderContext(ctx, req)
			if !errors.Is(err, context.Canceled) || len(v.placed) != 1 || v.queries != 0 || len(oe.snapshotOwnedIntents()) != 0 || oe.IsOpeningPaused() {
				t.Fatalf("refused order retained/retried: %v calls=%d", err, len(v.placed))
			}
		})
	}
}

func TestOrderContextBatchSeparatesUnknownFromUnsent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	v := &contextOrderVenue{}
	v.onPlace = func(context.Context, *exchange.OrderRequest) (*exchange.Order, error) {
		cancel()
		return nil, context.Canceled
	}
	oe := NewExchangeOrderExecutor(v, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	first, second := contextCloseRequest(), contextCloseRequest()
	first.ClientOrderID, second.ClientOrderID = "sent", "unsent"
	res := oe.BatchPlaceOrdersWithDetailsContext(ctx, []*OrderRequest{first, second})
	if len(v.placed) != 1 || len(res.UnknownOrders) != 1 || !res.UnknownOrders["sent"] || len(res.PlacedOrders) != 0 {
		t.Fatalf("batch conflated submission outcomes: %+v calls=%d", res, len(v.placed))
	}
}

func TestMarketCloseUsesIntentAndOpeningGate(t *testing.T) {
	ex := &fakeOrderExchange{}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	var gate execution.OpeningGate
	gate.Block("test")
	oe.SetOpeningGate(&gate, "LONG")
	req := contextCloseRequest()
	ord, err := oe.PlaceOrderContext(context.Background(), req)
	if err != nil || ord == nil || len(ex.placed) != 1 {
		t.Fatalf("protective market close blocked: %v", err)
	}
	sent := ex.placed[0]
	if sent.Type != exchange.OrderTypeMarket || sent.TimeInForce != "" || sent.PostOnly || !sent.ReduceOnly || sent.ClientOrderID != req.ClientOrderID || req.ClientOrderID == "" {
		t.Fatalf("lost market execution options: %+v", sent)
	}
	req = contextCloseRequest()
	req.Side, req.ReduceOnly = "BUY", false
	if _, err := oe.PlaceOrder(req); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("market open bypassed pause: %v", err)
	}
	gate.Unblock("test")
	if _, err := oe.PlaceOrder(req); err == nil || len(ex.placed) != 1 {
		t.Fatal("unbounded market open admitted without live notional reservation")
	}
}

func TestOrderExecutionOptionsValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*OrderRequest)
	}{
		{"nan_qty", func(r *OrderRequest) { r.Quantity = math.NaN() }},
		{"infinite_qty", func(r *OrderRequest) { r.Quantity = math.Inf(1) }},
		{"negative_qty", func(r *OrderRequest) { r.Quantity = -1 }},
		{"nan_price", func(r *OrderRequest) { r.Price = math.NaN() }},
		{"zero_limit", func(r *OrderRequest) { r.Type = "LIMIT" }},
		{"unknown_type", func(r *OrderRequest) { r.Type = "STOP" }},
		{"market_tif", func(r *OrderRequest) { r.TimeInForce = "GTC" }},
		{"market_postonly", func(r *OrderRequest) { r.PostOnly = true }},
		{"unknown_tif", func(r *OrderRequest) { r.Type, r.Price, r.TimeInForce = "LIMIT", 100, "BAD" }},
		{"postonly_ioc", func(r *OrderRequest) { r.Type, r.Price, r.TimeInForce, r.PostOnly = "LIMIT", 100, "IOC", true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := &fakeOrderExchange{}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			req := contextCloseRequest()
			tc.edit(req)
			if _, err := oe.PlaceOrder(req); err == nil || len(ex.placed) != 0 || len(oe.snapshotOwnedIntents()) != 0 {
				t.Fatalf("invalid order sent: %v", err)
			}
		})
	}
}
