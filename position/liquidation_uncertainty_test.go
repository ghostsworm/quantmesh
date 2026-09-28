package position

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

type uncertainLiquidationVenue struct {
	*liqFakeVenue
	ackOnly, queryFailure bool
	cancelCalls           int
	stateOverride         *LiquidationOrderState
}

func (v *uncertainLiquidationVenue) CancelOrder(ctx context.Context, symbol string, id int64) error {
	v.cancelCalls++
	if v.ackOnly {
		return nil
	}
	return v.liqFakeVenue.CancelOrder(ctx, symbol, id)
}

func (v *uncertainLiquidationVenue) GetOrderState(ctx context.Context, symbol string, id int64) (LiquidationOrderState, error) {
	if v.stateOverride != nil {
		return *v.stateOverride, nil
	}
	if v.queryFailure {
		return LiquidationOrderState{}, errors.New("query timed out")
	}
	return v.liqFakeVenue.GetOrderState(ctx, symbol, id)
}

func TestLiquidationCannotReplaceUnverifiedExistingOrder(t *testing.T) {
	base := newLiqFakeVenue(1)
	venue := &uncertainLiquidationVenue{liqFakeVenue: base, ackOnly: true}
	spm, _ := newLiqTestSPM(t, "LONG", base)
	slot := fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	slot.OrderID = base.restingOrder()
	slot.OrderSide = "SELL"
	slot.ClientOID = spm.generateClientOrderID(liqTestLast, "SELL", "")
	slot.OrderStatus = OrderStatusConfirmed
	if err := spm.LiquidateAllVerified(t.Context(), venue, time.Second); err == nil {
		t.Fatal("an ACK without terminal state allowed replacement")
	}
	if len(base.limitReqs) != 0 || len(base.marketReqs) != 0 {
		t.Fatal("submitted a close while the old close may still execute")
	}
	spm.ResumeOpening()
	if !spm.IsOpeningPaused() || !spm.liquidationNeedsReconciliation.Load() {
		t.Fatal("manual resume removed unverified liquidation hold")
	}
	before := venue.cancelCalls
	if err := spm.LiquidateAllVerified(t.Context(), venue, time.Second); err == nil || venue.cancelCalls != before {
		t.Fatal("unreconciled failure was blindly retried")
	}
}

func TestLiquidationUncertainLimitCannotFallBackToMarket(t *testing.T) {
	for _, market := range []string{"spot", "futures"} {
		for _, mode := range []string{"cancel_ack_only", "query_unavailable"} {
			t.Run(market+"/"+mode, func(t *testing.T) {
				base := newLiqFakeVenue(1)
				venue := &uncertainLiquidationVenue{liqFakeVenue: base, ackOnly: mode == "cancel_ack_only", queryFailure: mode == "query_unavailable"}
				spm, _ := newLiqTestSPM(t, "LONG", base)
				spm.config.Trading.MarketType = market
				fillSlot(spm, liqTestLast, 1, liqTestLast, "")
				if err := spm.LiquidateAllVerified(t.Context(), venue, time.Second); err == nil {
					t.Fatal("unknown order result reported as a completed liquidation")
				}
				if len(base.limitReqs) != 1 || len(base.marketReqs) != 0 {
					t.Fatalf("unsafe fallback: limits=%d markets=%d", len(base.limitReqs), len(base.marketReqs))
				}
				if !spm.IsOpeningPaused() {
					t.Fatal("unverified liquidation released opening gate")
				}
			})
		}
	}
}

func TestLiquidationCleanupNeverSweepsForeignOrders(t *testing.T) {
	venue := newLiqFakeVenue()
	owned, foreign := venue.restingOrder(), venue.restingOrder()
	if problems := cleanupLiquidationOpenOrders(t.Context(), venue, liqTestSymbol, []int64{owned, owned}); len(problems) != 0 {
		t.Fatal(problems)
	}
	if venue.orders[owned].status != OrderStatusCanceled || venue.orders[foreign].status != "NEW" {
		t.Fatal("cleanup did not respect explicit ownership")
	}
}

func TestLiquidationInvalidTerminalCannotReleaseRisk(t *testing.T) {
	for _, qty := range []float64{math.NaN(), math.Inf(1), -1, 0, 2} {
		base := newLiqFakeVenue(1)
		venue := &uncertainLiquidationVenue{liqFakeVenue: base, stateOverride: &LiquidationOrderState{Status: "FILLED", ExecutedQty: qty}}
		spm, _ := newLiqTestSPM(t, "LONG", base)
		fillSlot(spm, liqTestLast, 1, liqTestLast, "")
		if err := spm.LiquidateAllVerified(t.Context(), venue, time.Second); err == nil {
			t.Fatalf("invalid terminal quantity %v was accepted", qty)
		}
		if len(base.marketReqs) != 0 || !spm.IsOpeningPaused() {
			t.Fatalf("invalid terminal quantity %v released risk or caused fallback", qty)
		}
	}
}

func TestLiquidationInvalidPositionIsNotProofOfFlat(t *testing.T) {
	for _, qty := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		base := newLiqFakeVenue(qty)
		spm, _ := newLiqTestSPM(t, "LONG", base)
		fillSlot(spm, liqTestLast, 1, liqTestLast, "")
		if err := runVerified(t, spm, base); err == nil {
			t.Fatalf("invalid account quantity %v was reported flat", qty)
		}
		if len(base.marketReqs) != 0 {
			t.Fatal("invalid account position was used to size a market order")
		}
	}
}

func TestLiquidationCancelledContextCannotSubmit(t *testing.T) {
	base := newLiqFakeVenue(1)
	spm, _ := newLiqTestSPM(t, "LONG", base)
	fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := spm.LiquidateAllVerified(ctx, base, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled liquidation: %v", err)
	}
	if len(base.limitReqs) != 0 || len(base.marketReqs) != 0 {
		t.Fatal("cancelled operation reached venue")
	}
}

type blockingLiquidationVenue struct {
	*liqFakeVenue
	once            sync.Once
	started, resume chan struct{}
}

func (v *blockingLiquidationVenue) GetOrderState(ctx context.Context, symbol string, id int64) (LiquidationOrderState, error) {
	v.once.Do(func() {
		close(v.started)
		select {
		case <-v.resume:
		case <-ctx.Done():
		}
	})
	return v.liqFakeVenue.GetOrderState(ctx, symbol, id)
}

func TestLiquidationSerializesTriggersAndGridAdjust(t *testing.T) {
	base := newLiqFakeVenue(1)
	base.limitFillRatio = 1
	venue := &blockingLiquidationVenue{liqFakeVenue: base, started: make(chan struct{}), resume: make(chan struct{})}
	spm, _ := newLiqTestSPM(t, "LONG", base)
	fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	done := make(chan error, 1)
	go func() { done <- spm.LiquidateAllVerified(t.Context(), venue, time.Second) }()
	<-venue.started
	if err := spm.LiquidateAllVerified(t.Context(), venue, time.Second); err == nil {
		t.Fatal("duplicate concurrent liquidation was accepted")
	}
	if err := spm.AdjustOrders(liqTestLast + 100); err != nil {
		t.Fatal(err)
	}
	spm.LiquidateAll()
	if len(base.limitReqs) != 1 || !spm.IsOpeningPaused() {
		t.Fatal("concurrent tick placed orders or cleared the in-flight hold")
	}
	close(venue.resume)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLiquidationPreflightIncludesLateOpeningFill(t *testing.T) {
	base := newLiqFakeVenue(1.4)
	base.limitFillRatio = 1
	spm, _ := newLiqTestSPM(t, "LONG", base)
	slot := fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	id := base.restingOrder()
	base.orders[id].status, base.orders[id].executed, base.orders[id].price = "CANCELED", 0.4, liqTestLast
	slot.OrderID, slot.OrderSide, slot.OrderPrice = id, "BUY", liqTestLast
	slot.ClientOID = spm.generateClientOrderID(liqTestLast, "BUY", "")
	slot.OrderStatus = OrderStatusConfirmed
	if err := runVerified(t, spm, base); err != nil {
		t.Fatal(err)
	}
	if len(base.limitReqs) != 1 || math.Abs(base.limitReqs[0].Quantity-1.4) > liqTestEps {
		t.Fatalf("late opening fill missing from close plan: %+v", base.limitReqs)
	}
}

func TestLiquidationDeadlineIncludesWaitingForGridLock(t *testing.T) {
	base := newLiqFakeVenue(1)
	spm, _ := newLiqTestSPM(t, "LONG", base)
	fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	spm.mu.Lock()
	defer spm.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- spm.LiquidateAllVerified(ctx, base, time.Second) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("grid-lock deadline: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("liquidation ignored its deadline while waiting for the grid lock")
	}
	if len(base.limitReqs) != 0 || len(base.marketReqs) != 0 {
		t.Fatal("deadline-expired liquidation submitted orders")
	}
}
