package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
)

type fundingSpreadCoordinationLock struct {
	mu           sync.Mutex
	held         map[string]bool
	order        []string
	extendCalls  map[string]int
	extendErr    error
	waitKey      string
	waitEntered  chan struct{}
	continueWait chan struct{}
}

func TestFundingPerpSpreadOrderQuantityNeverExceedsPerLegNotional(t *testing.T) {
	qty, err := fundingPerpSpreadOrderQuantity(100, 200, 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	if qty != 0.5 || qty*200 > 100 || qty*100 > 100 {
		t.Fatalf("quantity = %v, leg notionals = %v/%v, want <= 100 each", qty, qty*200, qty*100)
	}

	if _, err := fundingPerpSpreadOrderQuantity(100, math.NaN(), 100, 3); err == nil {
		t.Fatal("NaN leg price unexpectedly produced an order quantity")
	}
}

func TestNormalizeFundingRateToEightHours(t *testing.T) {
	tests := []struct {
		name    string
		info    *exchange.FundingInfo
		symbol  string
		want    float64
		wantErr bool
	}{
		{name: "eight hour", info: &exchange.FundingInfo{Symbol: "BTCUSDT", Rate: 0.0008, FundingInterval: 8 * time.Hour}, symbol: "BTCUSDT", want: 0.0008},
		{name: "hourly normalized", info: &exchange.FundingInfo{Symbol: "BTCUSDT", Rate: 0.0001, FundingInterval: time.Hour}, symbol: "BTCUSDT", want: 0.0008},
		{name: "negative hourly normalized", info: &exchange.FundingInfo{Symbol: "BTCUSDT", Rate: -0.0001, FundingInterval: time.Hour}, symbol: "BTCUSDT", want: -0.0008},
		{name: "unknown interval", info: &exchange.FundingInfo{Symbol: "BTCUSDT", Rate: 0.0001}, symbol: "BTCUSDT", wantErr: true},
		{name: "mismatched symbol", info: &exchange.FundingInfo{Symbol: "ETHUSDT", Rate: 0.0001, FundingInterval: time.Hour}, symbol: "BTCUSDT", wantErr: true},
		{name: "non-finite rate", info: &exchange.FundingInfo{Symbol: "BTCUSDT", Rate: math.NaN(), FundingInterval: time.Hour}, symbol: "BTCUSDT", wantErr: true},
		{name: "interval over one day", info: &exchange.FundingInfo{Symbol: "BTCUSDT", Rate: 0.0001, FundingInterval: 25 * time.Hour}, symbol: "BTCUSDT", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeFundingRateToEightHours(tc.info, tc.symbol)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, tc.wantErr)
			}
			if err == nil && math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("normalized rate = %.12g, want %.12g", got, tc.want)
			}
		})
	}
}

func TestValidateFundingPerpSpreadNotional(t *testing.T) {
	tests := []struct {
		name    string
		posA    float64
		posB    float64
		priceA  float64
		priceB  float64
		maxPct  float64
		wantErr bool
	}{
		{name: "balanced notional", posA: 1, posB: -2, priceA: 100, priceB: 50, maxPct: 1},
		{name: "within tolerance", posA: 1, posB: -1, priceA: 100, priceB: 99.5, maxPct: 1},
		{name: "imbalanced restored positions", posA: 1, posB: -0.8, priceA: 100, priceB: 100, maxPct: 1, wantErr: true},
		{name: "invalid mark price", posA: 1, posB: -1, priceA: math.NaN(), priceB: 100, maxPct: 1, wantErr: true},
		{name: "negative threshold", posA: 1, posB: -1, priceA: 100, priceB: 100, maxPct: -1, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateFundingPerpSpreadNotional(tc.posA, tc.posB, tc.priceA, tc.priceB, tc.maxPct)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}

func TestFundingPerpSpreadCarryDirectionFollowsCurrentFundingRanking(t *testing.T) {
	tests := []struct {
		name  string
		posA  float64
		posB  float64
		rateA float64
		rateB float64
		want  bool
	}{
		{name: "short A positive rates favorable", posA: -1, posB: 1, rateA: 0.001, rateB: 0.0002, want: true},
		{name: "short A negative rates favorable", posA: -1, posB: 1, rateA: -0.001, rateB: -0.002, want: true},
		{name: "short A ranking reversed", posA: -1, posB: 1, rateA: 0.0001, rateB: 0.0008},
		{name: "short B positive rates favorable", posA: 1, posB: -1, rateA: 0.0002, rateB: 0.001, want: true},
		{name: "short B negative rates favorable", posA: 1, posB: -1, rateA: -0.002, rateB: -0.001, want: true},
		{name: "short B ranking reversed", posA: 1, posB: -1, rateA: 0.0008, rateB: 0.0001},
		{name: "unhedged is not favorable", posA: -1, rateA: 0.001, rateB: 0.0001},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := fundingPerpSpreadCarryDirectionFavorable(tc.posA, tc.posB, tc.rateA, tc.rateB); got != tc.want {
				t.Fatalf("favorable = %t, want %t", got, tc.want)
			}
		})
	}
}

func (l *fundingSpreadCoordinationLock) Lock(ctx context.Context, key string, _ time.Duration) error {
	if key == l.waitKey {
		close(l.waitEntered)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-l.continueWait:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held == nil {
		l.held = make(map[string]bool)
	}
	if l.held[key] {
		return fmt.Errorf("already held: %s", key)
	}
	l.held[key] = true
	l.order = append(l.order, key)
	return nil
}

func (l *fundingSpreadCoordinationLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := l.Lock(ctx, key, ttl); err != nil {
		return false, err
	}
	return true, nil
}

func (l *fundingSpreadCoordinationLock) Unlock(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.held[key] {
		return fmt.Errorf("not held: %s", key)
	}
	delete(l.held, key)
	return nil
}

func (l *fundingSpreadCoordinationLock) Extend(ctx context.Context, key string, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.extendCalls == nil {
		l.extendCalls = make(map[string]int)
	}
	l.extendCalls[key]++
	return l.extendErr
}

func (*fundingSpreadCoordinationLock) Close() error { return nil }

func (l *fundingSpreadCoordinationLock) allHeld(keys []string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys {
		if !l.held[key] {
			return false
		}
	}
	return true
}

func (l *fundingSpreadCoordinationLock) extensions(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.extendCalls[key]
}

type coordinationCheckingRuntimeStateStore struct {
	coordinator *fundingSpreadCoordinationLock
	keys        []string
	savedLocked bool
}

func (s *coordinationCheckingRuntimeStateStore) LoadRuntimeState(string) (int, string, bool, error) {
	return 0, "", false, nil
}

func (s *coordinationCheckingRuntimeStateStore) SaveRuntimeState(_ string, _ int, _ string) error {
	s.savedLocked = s.coordinator.allHeld(s.keys)
	return nil
}

func TestFundingPerpSpreadCoordinatesBothLegsInStableOrder(t *testing.T) {
	coordinator := &fundingSpreadCoordinationLock{}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "z"}, legB: &fundingSpreadTestExchange{name: "a"},
		symA: "BTCUSDT", symB: "ETHUSDT", coordinationLock: coordinator,
	}
	keys := []string{
		execution.PositionReconciliationLockKey("z", "BTCUSDT"),
		execution.PositionReconciliationLockKey("a", "ETHUSDT"),
	}
	called := false
	if err := st.withLegCoordination(context.Background(), func(context.Context) error {
		called = true
		if !coordinator.allHeld(keys) {
			t.Fatal("operation ran without holding both leg locks")
		}
		return nil
	}); err != nil {
		t.Fatalf("withLegCoordination() error = %v", err)
	}
	if !called {
		t.Fatal("coordinated operation was not called")
	}
	coordinator.mu.Lock()
	gotOrder := append([]string(nil), coordinator.order...)
	coordinator.mu.Unlock()
	if gotOrder[0] > gotOrder[1] {
		t.Fatalf("lock order is not stable: %v", gotOrder)
	}
	if coordinator.allHeld(keys) {
		t.Fatal("coordination locks were not released")
	}
}

func TestFundingPerpSpreadRejectsMissingCoordinationLock(t *testing.T) {
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT",
	}
	if err := st.withLegCoordination(context.Background(), func(context.Context) error { return nil }); err == nil {
		t.Fatal("operation should fail closed without configured coordination lock")
	}
}

func TestFundingPerpSpreadRenewsFirstLegWhileWaitingForSecondLock(t *testing.T) {
	firstKey := execution.PositionReconciliationLockKey("a", "BTCUSDT")
	secondKey := execution.PositionReconciliationLockKey("z", "ETHUSDT")
	coordinator := &fundingSpreadCoordinationLock{
		waitKey: secondKey, waitEntered: make(chan struct{}), continueWait: make(chan struct{}),
	}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "z"},
		symA: "BTCUSDT", symB: "ETHUSDT", coordinationLock: coordinator, coordinationTTL: 60 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() {
		done <- st.withLegCoordination(context.Background(), func(context.Context) error { return nil })
	}()
	<-coordinator.waitEntered
	deadline := time.After(time.Second)
	for coordinator.extensions(firstKey) == 0 {
		select {
		case <-deadline:
			close(coordinator.continueWait)
			t.Fatal("first leg lock was not renewed while acquiring the second lock")
		case <-time.After(time.Millisecond):
		}
	}
	close(coordinator.continueWait)
	if err := <-done; err != nil {
		t.Fatalf("withLegCoordination() error = %v", err)
	}
}

func TestFundingPerpSpreadPersistsStartupOwnershipBeforeReleasingLegLocks(t *testing.T) {
	coordinator := &fundingSpreadCoordinationLock{}
	keys := []string{
		execution.PositionReconciliationLockKey("a", "BTCUSDT"),
		execution.PositionReconciliationLockKey("b", "ETHUSDT"),
	}
	store := &coordinationCheckingRuntimeStateStore{coordinator: coordinator, keys: keys}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT", tickInt: time.Hour, coordinationLock: coordinator,
	}
	st.SetRuntimeStateStore(store)
	if err := st.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !store.savedLocked {
		t.Fatal("startup ownership state was not persisted while both leg locks were held")
	}
	if err := st.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestFundingPerpSpreadLeaseLossLatchesUnknownExposure(t *testing.T) {
	coordinator := &fundingSpreadCoordinationLock{extendErr: errors.New("lease expired")}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT", coordinationLock: coordinator,
		coordinationTTL: 30 * time.Millisecond, ownershipReady: true,
	}
	st.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	started := make(chan struct{})
	err := st.withLegCoordination(context.Background(), func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	if err == nil {
		t.Fatal("coordination lease loss did not fail operation")
	}
	st.mu.RLock()
	unknown := st.exposureUnknown
	st.mu.RUnlock()
	if !unknown {
		t.Fatal("coordination lease loss did not latch exposure as unknown")
	}
	if coordinator.allHeld([]string{
		execution.PositionReconciliationLockKey("a", "BTCUSDT"),
		execution.PositionReconciliationLockKey("b", "ETHUSDT"),
	}) {
		t.Fatal("locks remained held after lease loss")
	}
}

type emptyFundingSpreadExchange struct{ exchange.IExchange }

func (emptyFundingSpreadExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return []*exchange.Position{}, nil
}

func (emptyFundingSpreadExchange) GetName() string { return "test" }

func (emptyFundingSpreadExchange) GetQuantityDecimals() int { return 3 }

func (emptyFundingSpreadExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return nil, nil
}

type fundingSpreadTestExchange struct {
	exchange.IExchange
	name           string
	positions      []*exchange.Position
	orders         []*exchange.Order
	placed         int
	clientOrderIDs []string
	residual       float64
}

type fundingSpreadOrderLookupExchange struct {
	*fundingSpreadTestExchange
	order *exchange.Order
}

func (e *fundingSpreadOrderLookupExchange) GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error) {
	return e.order, nil
}

func (e *fundingSpreadTestExchange) GetName() string { return e.name }

func (e *fundingSpreadTestExchange) GetQuantityDecimals() int { return 3 }

func (e *fundingSpreadTestExchange) GetPriceDecimals() int { return 2 }

func (e *fundingSpreadTestExchange) GetLatestPrice(context.Context, string) (float64, error) {
	return 100, nil
}

func (e *fundingSpreadTestExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	if e.positions == nil {
		return []*exchange.Position{}, nil
	}
	return e.positions, nil
}

func (e *fundingSpreadTestExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return e.orders, nil
}

func (e *fundingSpreadTestExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	e.placed++
	e.clientOrderIDs = append(e.clientOrderIDs, req.ClientOrderID)
	if req.ReduceOnly {
		if e.residual != 0 {
			e.positions = []*exchange.Position{{Symbol: req.Symbol, Size: e.residual}}
		} else {
			e.positions = []*exchange.Position{}
		}
	}
	return nil, nil
}

func TestFundingPerpSpreadShutdownClosesAndVerifiesBothLegs(t *testing.T) {
	for _, failSecondary := range []bool{false, true} {
		t.Run(fmt.Sprintf("secondary_residual_%t", failSecondary), func(t *testing.T) {
			a := &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}}}
			b := &fundingSpreadTestExchange{name: "b", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.01}}}
			if failSecondary {
				b.residual = 0.005
			}
			done := make(chan struct{})
			close(done)
			st := &FundingPerpSpreadStrategy{legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT",
				cancel: func() {}, runDone: done, ownershipReady: true, ownedA: -0.01, ownedB: 0.01}
			st.SetRuntimeStateStore(&memoryRuntimeStateStore{})
			st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
			err := st.CloseForShutdown(context.Background())
			if failSecondary {
				if err == nil || st.stopped || a.placed != 1 || b.placed != 1 {
					t.Fatalf("secondary residual was reported closed: err=%v stopped=%t placements=(%d,%d)", err, st.stopped, a.placed, b.placed)
				}
				if err := st.VerifyFlat(context.Background()); err == nil {
					t.Fatal("VerifyFlat accepted residual exposure on leg B")
				}
				return
			}
			if err != nil || !st.stopped || a.placed != 1 || b.placed != 1 {
				t.Fatalf("both-leg close not confirmed: err=%v stopped=%t placements=(%d,%d)", err, st.stopped, a.placed, b.placed)
			}
			if len(a.clientOrderIDs) != 1 || a.clientOrderIDs[0] == "" || len(b.clientOrderIDs) != 1 || b.clientOrderIDs[0] == "" || a.clientOrderIDs[0] == b.clientOrderIDs[0] {
				t.Fatalf("each close leg needs a unique ClientOrderID: A=%v B=%v", a.clientOrderIDs, b.clientOrderIDs)
			}
			if err := st.VerifyFlat(context.Background()); err != nil {
				t.Fatalf("VerifyFlat() after close: %v", err)
			}
		})
	}
}

func TestFundingPerpSpreadPrepareShutdownHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	st := &FundingPerpSpreadStrategy{cancel: cancel, runDone: done}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if err := st.PrepareShutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PrepareShutdown() error = %v, want deadline exceeded", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal("shutdown preparation did not cancel the strategy loop")
	}
	close(done)
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
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})

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
			st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
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
	st := &FundingPerpSpreadStrategy{legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT", tickInt: time.Hour, maxBasis: 1}
	st.SetRuntimeStateStore(store)
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
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

func TestFundingPerpSpreadStartRejectsImbalancedPersistedNotional(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "BTCUSDT", OwnershipReady: true,
		OwnedA: -0.01, OwnedB: 0.005,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true}
	a := &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}}}
	b := &fundingSpreadTestExchange{name: "b", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.005}}}
	st := &FundingPerpSpreadStrategy{legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT", maxBasis: 1}
	st.SetRuntimeStateStore(store)
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	if err := st.Start(context.Background()); err == nil {
		_ = st.Stop()
		t.Fatal("Start() accepted persisted hedge with a 50% notional imbalance")
	}
}

func TestFundingPerpSpreadStartRejectsPersistedUnhedgedExposure(t *testing.T) {
	cases := []struct {
		name string
		a    float64
		b    float64
	}{
		{name: "only short leg", a: -0.01},
		{name: "only long leg", b: 0.01},
		{name: "same direction", a: -0.01, b: -0.02},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, err := json.Marshal(fundingPerpSpreadRuntimeState{
				Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
				LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, OwnedA: tc.a, OwnedB: tc.b,
			})
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true}
			a := &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: tc.a}}}
			b := &fundingSpreadTestExchange{name: "b", positions: []*exchange.Position{{Symbol: "ETHUSDT", Size: tc.b}}}
			st := &FundingPerpSpreadStrategy{legA: a, legB: b, symA: "BTCUSDT", symB: "ETHUSDT"}
			st.SetRuntimeStateStore(store)
			st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
			if err := st.Start(context.Background()); err == nil {
				t.Fatal("Start() accepted a persisted single-leg or same-direction exposure")
			}
			if st.started || st.cancel != nil {
				t.Fatal("rejected unhedged startup left strategy active")
			}
		})
	}
}

func TestFundingPerpSpreadStartRejectsUnresolvedPersistedIntent(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "BTCUSDT", OwnershipReady: true, IntentInFlight: true,
		PendingOrder: &fundingPerpSpreadOrderIntent{ClientOrderID: "spread-order-1", LegExchange: "a", Symbol: "BTCUSDT", Side: "SELL", Quantity: 0.01},
	})
	if err != nil {
		t.Fatal(err)
	}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "BTCUSDT",
	}
	st.SetRuntimeStateStore(&memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true})
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	if err := st.Start(context.Background()); err == nil {
		t.Fatal("Start() accepted an unresolved persisted order intent")
	}
}

func TestFundingPerpSpreadStartRecoversExactZeroFillTerminalOrder(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, IntentInFlight: true, ExposureUnknown: true,
		PendingOrder: &fundingPerpSpreadOrderIntent{ClientOrderID: "recover-me", LegExchange: "a", Symbol: "BTCUSDT", Side: "SELL", Quantity: 0.01},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true}
	a := &fundingSpreadOrderLookupExchange{
		fundingSpreadTestExchange: &fundingSpreadTestExchange{name: "a"},
		order:                     &exchange.Order{ClientOrderID: "recover-me", Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 0.01, Status: exchange.OrderStatusCanceled},
	}
	st := &FundingPerpSpreadStrategy{legA: a, legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT", tickInt: time.Hour}
	st.SetRuntimeStateStore(store)
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	if err := st.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed to recover a provably unfilled terminal order: %v", err)
	}
	st.mu.RLock()
	cancel, done := st.cancel, st.runDone
	st.mu.RUnlock()
	cancel()
	<-done
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.IntentInFlight || persisted.PendingOrder != nil || persisted.ExposureUnknown {
		t.Fatalf("verified zero-fill order was not durably reconciled: %+v", persisted)
	}
}

func TestFundingPerpSpreadStartRejectsFilledPendingOrder(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, IntentInFlight: true,
		PendingOrder: &fundingPerpSpreadOrderIntent{ClientOrderID: "filled-order", LegExchange: "a", Symbol: "BTCUSDT", Side: "SELL", Quantity: 0.01},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &fundingSpreadOrderLookupExchange{
		fundingSpreadTestExchange: &fundingSpreadTestExchange{name: "a"},
		order:                     &exchange.Order{ClientOrderID: "filled-order", Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 0.01, ExecutedQty: 0.01, Status: exchange.OrderStatusFilled},
	}
	st := &FundingPerpSpreadStrategy{legA: a, legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT"}
	st.SetRuntimeStateStore(&memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true})
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	if err := st.Start(context.Background()); err == nil {
		t.Fatal("Start() released a filled pending order without economic replay")
	}
}

func TestFundingPerpSpreadStartRejectsZeroFillOrderWhenPositionChanged(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, IntentInFlight: true,
		PendingOrder: &fundingPerpSpreadOrderIntent{ClientOrderID: "zero-fill-position-changed", LegExchange: "a", Symbol: "BTCUSDT", Side: "SELL", Quantity: 0.01},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &fundingSpreadOrderLookupExchange{
		fundingSpreadTestExchange: &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}}},
		order:                     &exchange.Order{ClientOrderID: "zero-fill-position-changed", Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 0.01, Status: exchange.OrderStatusCanceled},
	}
	st := &FundingPerpSpreadStrategy{legA: a, legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT"}
	st.SetRuntimeStateStore(&memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true})
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	if err := st.Start(context.Background()); err == nil {
		t.Fatal("Start() accepted a zero-fill order despite an unexpected position change")
	}
}

func TestFundingPerpSpreadPersistsOrderIdentityBeforeSubmission(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT", ownershipReady: true,
	}
	st.SetRuntimeStateStore(store)
	intent := fundingPerpSpreadOrderIntent{
		ClientOrderID: "stable-order-id", LegExchange: "b", Symbol: "ETHUSDT", Side: "BUY", Quantity: 0.25,
	}
	if err := st.beginOrderIntent(intent); err != nil {
		t.Fatalf("beginOrderIntent() error = %v", err)
	}
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if store.version != fundingPerpSpreadRuntimeStateVersion || !persisted.IntentInFlight || persisted.PendingOrder == nil || *persisted.PendingOrder != intent {
		t.Fatalf("order identity was not durably persisted before submission: version=%d state=%+v", store.version, persisted)
	}
	if err := st.finishOrderIntent(); err != nil {
		t.Fatalf("finishOrderIntent() error = %v", err)
	}
	persisted = fundingPerpSpreadRuntimeState{}
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.IntentInFlight || persisted.PendingOrder != nil {
		t.Fatalf("settled intent retained pending identity: %+v", persisted)
	}
}

func TestFundingPerpSpreadRuntimeStateRejectsInvalidPendingOrderIdentity(t *testing.T) {
	state := fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, IntentInFlight: true,
		PendingOrder: &fundingPerpSpreadOrderIntent{ClientOrderID: "bad-scope", LegExchange: "a", Symbol: "ETHUSDT", Side: "BUY", Quantity: 1},
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFundingPerpSpreadRuntimeState(fundingPerpSpreadRuntimeStateVersion, string(payload), "a", "BTCUSDT", "b", "ETHUSDT"); err == nil {
		t.Fatal("accepted pending order identity outside both configured legs")
	}
}

func TestFundingPerpSpreadRuntimeStateMigratesResolvedV1AndRejectsUnknownV1Intent(t *testing.T) {
	state := fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true,
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFundingPerpSpreadRuntimeState(1, string(payload), "a", "BTCUSDT", "b", "ETHUSDT"); err != nil {
		t.Fatalf("resolved v1 state failed migration: %v", err)
	}
	state.IntentInFlight = true
	payload, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFundingPerpSpreadRuntimeState(1, string(payload), "a", "BTCUSDT", "b", "ETHUSDT"); err == nil {
		t.Fatal("legacy in-flight intent without a ClientOrderID was accepted")
	}
}
