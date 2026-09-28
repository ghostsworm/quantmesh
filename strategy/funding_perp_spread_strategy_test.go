package strategy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"quantmesh/exchange"
)

type emptyFundingSpreadExchange struct{ exchange.IExchange }

func (emptyFundingSpreadExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return nil, nil
}

func (emptyFundingSpreadExchange) GetName() string { return "test" }

func (emptyFundingSpreadExchange) GetQuantityDecimals() int { return 3 }

func (emptyFundingSpreadExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return nil, nil
}

type fundingSpreadTestExchange struct {
	exchange.IExchange
	name      string
	positions []*exchange.Position
	orders    []*exchange.Order
	placed    int
}

func (e *fundingSpreadTestExchange) GetName() string { return e.name }

func (e *fundingSpreadTestExchange) GetQuantityDecimals() int { return 3 }

func (e *fundingSpreadTestExchange) GetPriceDecimals() int { return 2 }

func (e *fundingSpreadTestExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return e.positions, nil
}

func (e *fundingSpreadTestExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return e.orders, nil
}

func (e *fundingSpreadTestExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	e.placed++
	if req.ReduceOnly {
		e.positions = nil
	}
	return nil, nil
}

func (emptyFundingSpreadExchange) PlaceOrder(context.Context, *exchange.OrderRequest) (*exchange.Order, error) {
	return nil, nil
}

func TestFundingPerpSpreadStopWaitsForRunLoopBeforeClosing(t *testing.T) {
	runDone := make(chan struct{})
	started := make(chan struct{})
	st := &FundingPerpSpreadStrategy{
		legA:           emptyFundingSpreadExchange{},
		legB:           emptyFundingSpreadExchange{},
		symA:           "BTCUSDT",
		symB:           "BTCUSDT",
		cancel:         func() {},
		runDone:        runDone,
		ownershipReady: true,
	}

	go func() {
		close(started)
		time.Sleep(20 * time.Millisecond)
		close(runDone)
	}()
	<-started
	stopStarted := time.Now()
	if err := st.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if elapsed := time.Since(stopStarted); elapsed < 15*time.Millisecond {
		t.Fatalf("Stop() returned before run loop exited: %s", elapsed)
	}
}

func TestFundingPerpSpreadStopReturnsWhenNeverStarted(t *testing.T) {
	if err := (&FundingPerpSpreadStrategy{}).Stop(); err != nil {
		t.Fatalf("Stop() on an unstarted strategy error = %v", err)
	}
}

func TestFundingCarryStopCancelsAndWaitsBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	st := &FundingCarryStrategy{ctx: ctx, cancel: cancel, runDone: runDone}
	go func() {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		close(runDone)
	}()

	stopStarted := time.Now()
	if err := st.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if elapsed := time.Since(stopStarted); elapsed < 15*time.Millisecond {
		t.Fatalf("Stop() returned before run loop exited: %s", elapsed)
	}
}

func TestFundingPerpSpreadStartRejectsPreexistingRisk(t *testing.T) {
	cases := []struct {
		name      string
		positions []*exchange.Position
		orders    []*exchange.Order
	}{
		{name: "position", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.01}}},
		{name: "open order", orders: []*exchange.Order{{Symbol: "BTCUSDT", OrderID: 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &fundingSpreadTestExchange{name: "a", positions: tc.positions, orders: tc.orders}
			b := &fundingSpreadTestExchange{name: "b"}
			st := &FundingPerpSpreadStrategy{legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT"}
			st.SetRuntimeStateStore(&memoryRuntimeStateStore{})
			if err := st.Start(context.Background()); err == nil {
				t.Fatal("Start() succeeded despite preexisting positions/orders")
			}
			if st.cancel != nil || st.started {
				t.Fatal("failed startup left strategy marked as running")
			}
		})
	}
}

func TestFundingPerpSpreadCloseRefusesUnownedExposure(t *testing.T) {
	ex := &fundingSpreadTestExchange{
		name:      "a",
		positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 2}},
	}
	st := &FundingPerpSpreadStrategy{legA: ex, legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT"}
	if err := st.closeLeg(context.Background(), ex, "BTCUSDT", 1); err == nil {
		t.Fatal("closeLeg() succeeded when actual exposure did not match strategy ownership")
	}
	if ex.placed != 0 {
		t.Fatalf("closeLeg() placed %d orders for unowned exposure", ex.placed)
	}
}

func TestFundingPerpSpreadExposureMismatchLatchesRiskBlock(t *testing.T) {
	st := &FundingPerpSpreadStrategy{
		legA:           &fundingSpreadTestExchange{name: "a"},
		legB:           &fundingSpreadTestExchange{name: "b"},
		ownershipReady: true,
		ownedA:         1,
	}
	if err := st.verifyOwnedExposure(0, 0); err == nil {
		t.Fatal("verifyOwnedExposure() accepted changed exposure")
	}
	if !st.exposureUnknown {
		t.Fatal("exposure mismatch did not latch trading block")
	}
}

func TestFundingPerpSpreadRestoresPersistedLegOwnership(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "BTCUSDT", OwnershipReady: true,
		OwnedA: -0.01, OwnedB: 0.01,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true}
	a := &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}}}
	b := &fundingSpreadTestExchange{name: "b", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.01}}}
	st := &FundingPerpSpreadStrategy{legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT", tickInt: time.Hour}
	st.SetRuntimeStateStore(store)
	if err := st.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed to restore verified ownership: %v", err)
	}
	st.mu.RLock()
	ownedA, ownedB := st.ownedA, st.ownedB
	st.mu.RUnlock()
	if ownedA != -0.01 || ownedB != 0.01 {
		t.Fatalf("restored ownership = (%v, %v), want (-0.01, 0.01)", ownedA, ownedB)
	}
	if err := st.Stop(); err != nil {
		t.Fatalf("Stop() failed to close restored owned positions: %v", err)
	}
}

func TestFundingPerpSpreadStartRejectsUnresolvedPersistedIntent(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "BTCUSDT", OwnershipReady: true, IntentInFlight: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "BTCUSDT",
	}
	st.SetRuntimeStateStore(&memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true})
	if err := st.Start(context.Background()); err == nil {
		t.Fatal("Start() accepted an unresolved persisted order intent")
	}
}
