package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
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

func TestFundingPerpSpreadNetPositionRejectsUnscopedRows(t *testing.T) {
	for _, symbol := range []string{"", "ETHUSDT"} {
		ex := &fundingSpreadTestExchange{name: "test", positions: []*exchange.Position{{Symbol: symbol, Size: 0}}}
		if _, err := netFutSize(context.Background(), ex, "BTCUSDT"); err == nil {
			t.Fatalf("net position accepted zero row for symbol %q", symbol)
		}
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

func TestFundingPerpSpreadProcessOwnershipLossBlocksStrategy(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT", ownershipReady: true,
	}
	st.SetRuntimeStateStore(store)
	if err := st.MarkOwnershipUnverified(); err != nil {
		t.Fatalf("MarkOwnershipUnverified() error = %v", err)
	}
	if err := st.verifyOwnedExposure(0, 0); err == nil {
		t.Fatal("strategy accepted exposure actions after process ownership was lost")
	}
	if _, err := decodeFundingPerpSpreadRuntimeState(store.version, store.payload, "a", "BTCUSDT", "b", "ETHUSDT"); err == nil {
		t.Fatal("persisted ownership-loss state was accepted as recoverable without reconciliation")
	}
}

func TestFundingPerpSpreadAccountOrderCheckRejectsOtherSymbol(t *testing.T) {
	ex := &fundingSpreadTestExchange{name: "binance", orders: []*exchange.Order{{OrderID: 99, Symbol: "ETHUSDT"}}}
	if err := requireAccountHasNoOpenOrders(t.Context(), ex, "funding_perp_spread futures"); err == nil {
		t.Fatal("accepted an account-wide open order on an unrelated symbol")
	}
}

type fundingSpreadOrderCountVerifier struct {
	exchange.IExchange
	err error
}

func (v fundingSpreadOrderCountVerifier) VerifyAccountHasNoOpenOrders(context.Context) error {
	return v.err
}

func TestFundingPerpSpreadAccountOrderCheckUsesCountVerifier(t *testing.T) {
	ex := fundingSpreadOrderCountVerifier{IExchange: &fundingSpreadTestExchange{name: "mexc"}}
	if err := requireAccountHasNoOpenOrders(t.Context(), ex, "funding_perp_spread futures"); err != nil {
		t.Fatalf("empty account verifier rejected account: %v", err)
	}
	ex.err = errors.New("MEXC account has 2 open orders")
	if err := requireAccountHasNoOpenOrders(t.Context(), ex, "funding_perp_spread futures"); err == nil || !strings.Contains(err.Error(), "2 open orders") {
		t.Fatalf("open account orders were not rejected: %v", err)
	}
}

type fundingSpreadOtherSymbolOrderExchange struct{ *fundingSpreadTestExchange }

func (e fundingSpreadOtherSymbolOrderExchange) GetOpenOrders(ctx context.Context, symbol string) ([]*exchange.Order, error) {
	if symbol == "BTCUSDT" {
		return []*exchange.Order{}, nil
	}
	return e.fundingSpreadTestExchange.GetOpenOrders(ctx, symbol)
}

func TestFundingPerpSpreadCurrentLegSnapshotRemainsUsableWithForeignSymbolOrder(t *testing.T) {
	ex := fundingSpreadOtherSymbolOrderExchange{&fundingSpreadTestExchange{
		name: "binance", orders: []*exchange.Order{{OrderID: 100, Symbol: "ETHUSDT"}},
	}}
	strategy := &FundingPerpSpreadStrategy{symA: "BTCUSDT", symB: "SOLUSDT"}
	if _, err := strategy.readLegSnapshot(t.Context(), ex, "BTCUSDT"); err != nil {
		t.Fatalf("foreign ETHUSDT order prevented a BTCUSDT leg snapshot needed for exposure management: %v", err)
	}
}

type emptyFundingSpreadExchange struct{ exchange.IExchange }

func (emptyFundingSpreadExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return []*exchange.Position{}, nil
}

func (emptyFundingSpreadExchange) GetName() string { return "test" }

func (emptyFundingSpreadExchange) GetQuantityDecimals() int { return 3 }

func (emptyFundingSpreadExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return []*exchange.Order{}, nil
}

func (emptyFundingSpreadExchange) GetAccountOpenOrders(context.Context) ([]*exchange.Order, error) {
	return []*exchange.Order{}, nil
}

type fundingSpreadTestExchange struct {
	exchange.IExchange
	name                string
	positions           []*exchange.Position
	orders              []*exchange.Order
	returnNilOrders     bool
	nilOrdersAfterPlace bool
	placed              int
	clientOrderIDs      []string
	residual            float64
}

type fundingPerpSpreadOpeningExchange struct {
	exchange.IExchange
	name     string
	symbol   string
	position float64
	placed   int
}

func (e *fundingPerpSpreadOpeningExchange) GetName() string          { return e.name }
func (e *fundingPerpSpreadOpeningExchange) GetQuantityDecimals() int { return 3 }
func (e *fundingPerpSpreadOpeningExchange) GetPriceDecimals() int    { return 2 }
func (e *fundingPerpSpreadOpeningExchange) GetLatestPrice(context.Context, string) (float64, error) {
	return 100, nil
}
func (e *fundingPerpSpreadOpeningExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	if e.position == 0 {
		return []*exchange.Position{}, nil
	}
	return []*exchange.Position{{Symbol: e.symbol, Size: e.position}}, nil
}
func (e *fundingPerpSpreadOpeningExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return []*exchange.Order{}, nil
}
func (e *fundingPerpSpreadOpeningExchange) GetAccountOpenOrders(context.Context) ([]*exchange.Order, error) {
	return []*exchange.Order{}, nil
}
func (e *fundingPerpSpreadOpeningExchange) PlaceOrder(_ context.Context, request *exchange.OrderRequest) (*exchange.Order, error) {
	e.placed++
	if request.Side == exchange.SideSell {
		e.position -= request.Quantity
	} else {
		e.position += request.Quantity
	}
	return &exchange.Order{OrderID: int64(e.placed), ClientOrderID: request.ClientOrderID, Symbol: request.Symbol,
		Side: request.Side, Quantity: request.Quantity, ExecutedQty: request.Quantity, Status: exchange.OrderStatusFilled}, nil
}

type fundingSpreadOrderLookupExchange struct {
	*fundingSpreadTestExchange
	order         *exchange.Order
	terminalOrder *exchange.Order
}

func (e *fundingSpreadOrderLookupExchange) GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error) {
	return e.order, nil
}

func (e *fundingSpreadOrderLookupExchange) GetOrder(context.Context, string, int64) (*exchange.Order, error) {
	return e.terminalOrder, nil
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
	if e.returnNilOrders || e.nilOrdersAfterPlace && e.placed > 0 {
		return nil, nil
	}
	if e.orders == nil {
		return []*exchange.Order{}, nil
	}
	return e.orders, nil
}

func (e *fundingSpreadTestExchange) GetAccountOpenOrders(ctx context.Context) ([]*exchange.Order, error) {
	return e.GetOpenOrders(ctx, "")
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
			st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
				return true, nil
			})
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

func TestFundingPerpSpreadOpeningRespectsSharedRuntimeGate(t *testing.T) {
	gate := &execution.OpeningGate{}
	gate.Block("manual")
	a := &fundingSpreadTestExchange{name: "a"}
	b := &fundingSpreadTestExchange{name: "b"}
	st := &FundingPerpSpreadStrategy{
		openingGate: gate, legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT",
		ownershipReady: true, symCfg: config.SymbolConfig{TotalAllocatedCapital: 400},
	}
	err := st.openSpreadCoordinated(context.Background(), a, "BTCUSDT", b, "BTCUSDT", 100, 100, 0.001, 0)
	if !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("openSpread() error = %v, want ErrOpeningPaused", err)
	}
	if a.placed != 0 || b.placed != 0 {
		t.Fatalf("blocked paired opening reached the venues: legA=%d legB=%d", a.placed, b.placed)
	}
}

func TestFundingPerpSpreadOpeningFailsClosedOnWalletAdmissionError(t *testing.T) {
	a := &fundingSpreadTestExchange{name: "a"}
	b := &fundingSpreadTestExchange{name: "b"}
	st := &FundingPerpSpreadStrategy{
		openingGate: &execution.OpeningGate{}, legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT",
		ownershipReady: true, symCfg: config.SymbolConfig{TotalAllocatedCapital: 400},
	}
	st.SetOpeningAdmissionGuard(func(context.Context) error { return errors.New("stale wallet balance") })
	err := st.openSpreadCoordinated(context.Background(), a, "BTCUSDT", b, "BTCUSDT", 100, 100, 0.001, 0)
	if err == nil || !strings.Contains(err.Error(), "stale wallet balance") {
		t.Fatalf("wallet admission error = %v, want fail-closed stale evidence error", err)
	}
	if a.placed != 0 || b.placed != 0 {
		t.Fatalf("wallet admission failure reached venues: legA=%d legB=%d", a.placed, b.placed)
	}
}

func TestFundingPerpSpreadOpeningRejectsExistingOwnedAllocation(t *testing.T) {
	a := &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -1}}}
	b := &fundingSpreadTestExchange{name: "b", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 1}}}
	st := &FundingPerpSpreadStrategy{
		openingGate: &execution.OpeningGate{}, legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT",
		ownershipReady: true, ownedA: -1, ownedB: 1,
		symCfg: config.SymbolConfig{TotalAllocatedCapital: 400},
	}
	err := st.openSpreadCoordinated(context.Background(), a, "BTCUSDT", b, "BTCUSDT", 100, 100, 0.001, 0)
	if err == nil || !strings.Contains(err.Error(), "must be flat") {
		t.Fatalf("openSpreadCoordinated() error = %v, want existing-allocation rejection", err)
	}
	if a.placed != 0 || b.placed != 0 {
		t.Fatalf("existing allocation was increased: legA=%d legB=%d", a.placed, b.placed)
	}
}

func TestFundingPerpSpreadOpeningCompletesHedgeBeforeWaitingForExecutionLedger(t *testing.T) {
	short := &fundingPerpSpreadOpeningExchange{name: "short", symbol: "BTCUSDT"}
	long := &fundingPerpSpreadOpeningExchange{name: "long", symbol: "BTCUSDT"}
	st := &FundingPerpSpreadStrategy{
		cfg: &config.Config{}, symCfg: config.SymbolConfig{TotalAllocatedCapital: 200},
		legA: short, legB: long, symA: "BTCUSDT", symB: "BTCUSDT", maxBasis: 1,
		openingGate: &execution.OpeningGate{}, ownershipReady: true,
	}
	st.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
		if short.placed != 1 || long.placed != 1 || short.position >= 0 || long.position <= 0 {
			t.Fatalf("execution ledger ran before hedge completion: placements=(%d,%d), positions=(%v,%v)", short.placed, long.placed, short.position, long.position)
		}
		return true, nil
	})
	if err := st.openSpreadCoordinated(context.Background(), short, "BTCUSDT", long, "BTCUSDT", 100, 100, 0.001, 0); err != nil {
		t.Fatalf("openSpreadCoordinated() error = %v", err)
	}
	if short.placed != 1 || long.placed != 1 {
		t.Fatalf("spread legs placed = (%d,%d), want one order on each leg", short.placed, long.placed)
	}
}

func TestFundingPerpSpreadCloseRefusesUnownedExposure(t *testing.T) {
	ex := &fundingSpreadTestExchange{
		name:      "a",
		positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 2}},
	}
	st := &FundingPerpSpreadStrategy{legA: ex, legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT"}
	st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
		return true, nil
	})
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
	st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
		return true, nil
	})
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

func TestFundingPerpSpreadStartRejectsNilOpenOrderSnapshot(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	a := &fundingSpreadTestExchange{name: "a", returnNilOrders: true}
	b := &fundingSpreadTestExchange{name: "b"}
	st := &FundingPerpSpreadStrategy{legA: a, legB: b, symA: "BTCUSDT", symB: "ETHUSDT"}
	st.SetRuntimeStateStore(store)
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	if err := st.Start(context.Background()); err == nil {
		t.Fatal("startup accepted nil open-order evidence as an empty leg")
	}
	if st.started || st.cancel != nil || store.found {
		t.Fatalf("unverified startup changed state: started=%v cancel=%v persisted=%v", st.started, st.cancel != nil, store.found)
	}
}

func TestFundingPerpSpreadTickSnapshotRejectsForeignOrderWithFlatPosition(t *testing.T) {
	ex := &fundingSpreadTestExchange{name: "a", orders: []*exchange.Order{{OrderID: 91, Symbol: "BTCUSDT"}}}
	strategy := &FundingPerpSpreadStrategy{legA: ex, symA: "BTCUSDT"}
	position, err := strategy.readLegSnapshot(context.Background(), ex, "BTCUSDT")
	if err == nil {
		t.Fatalf("flat position %.8f with an unrelated active order was accepted", position)
	}
}

func TestFundingPerpSpreadResolvedLedgerFailureBlocksNewOrdersWithoutLosingPositionOwnership(t *testing.T) {
	gate := &execution.OpeningGate{}
	store := &memoryRuntimeStateStore{}
	st := &FundingPerpSpreadStrategy{openingGate: gate, legA: &fundingSpreadTestExchange{name: "a"},
		legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT", ownershipReady: true}
	st.SetRuntimeStateStore(store)
	st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
		return true, errors.New("fill persistence failed")
	})
	resolved, err := st.recordOrderExecution(context.Background(), nil, nil, nil)
	var ledgerErr *fundingPerpSpreadExecutionLedgerError
	if !resolved || !errors.As(err, &ledgerErr) {
		t.Fatalf("resolved ledger failure = (%t, %v), want resolved ledger-only error", resolved, err)
	}
	if st.exposureUnknown {
		t.Fatal("known order identity incorrectly erased verified position ownership")
	}
	if _, err := gate.Begin(); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("opening gate error = %v, want permanent ledger block", err)
	}
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatalf("decode persisted ledger state: %v", err)
	}
	if !persisted.ExecutionLedgerUnverified {
		t.Fatal("ledger verification failure did not survive in durable runtime state")
	}
}

func TestFundingPerpSpreadRestoresLedgerBlockAcrossRestart(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, ExecutionLedgerUnverified: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gate := &execution.OpeningGate{}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT", tickInt: time.Hour, openingGate: gate,
	}
	st.SetRuntimeStateStore(&memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true})
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	if err := st.Start(context.Background()); err != nil {
		t.Fatalf("Start() rejected recoverable positions with a ledger block: %v", err)
	}
	if _, err := gate.Begin(); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("restored opening gate error = %v, want ledger block", err)
	}
	if err := st.Stop(); err != nil {
		t.Fatalf("Stop() after restored ledger block: %v", err)
	}
}

func TestFundingPerpSpreadReconcilesPendingExecutionAndClearsLedgerBlock(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, ExecutionLedgerUnverified: true,
		PendingExecutions: []fundingPerpSpreadPendingExecutionState{{
			Exchange: "a", Symbol: "BTCUSDT", ClientOrderID: "pending-cid", OrderID: 91, Side: "BUY", Quantity: 0.01,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gate := &execution.OpeningGate{}
	store := &memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT", tickInt: time.Hour, openingGate: gate,
	}
	st.SetRuntimeStateStore(store)
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	st.SetExecutionRecorder(func(_ context.Context, client exchange.IExchange, request *exchange.OrderRequest, order *exchange.Order) (bool, error) {
		if client.GetName() != "a" || request.ClientOrderID != "pending-cid" || order == nil || order.OrderID != 91 {
			t.Fatalf("unexpected pending execution recovery request: client=%s request=%+v order=%+v", client.GetName(), request, order)
		}
		return true, nil
	})
	if err := st.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed after successful execution recovery: %v", err)
	}
	if release, err := gate.Begin(); err != nil {
		t.Fatalf("opening gate remained blocked after exact pending execution recovery: %v", err)
	} else {
		release()
	}
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.ExecutionLedgerUnverified || len(persisted.PendingExecutions) != 0 {
		t.Fatalf("resolved pending execution was not cleared durably: %+v", persisted)
	}
	if err := st.Stop(); err != nil {
		t.Fatalf("Stop() after recovered ledger: %v", err)
	}
}

func TestFundingPerpSpreadRetainsPendingExecutionWhenStartupReconciliationFails(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, ExecutionLedgerUnverified: true,
		PendingExecutions: []fundingPerpSpreadPendingExecutionState{{
			Exchange: "a", Symbol: "BTCUSDT", ClientOrderID: "pending-cid", OrderID: 92, Side: "BUY", Quantity: 0.01,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	gate := &execution.OpeningGate{}
	store := &memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT", tickInt: time.Hour, openingGate: gate,
	}
	st.SetRuntimeStateStore(store)
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
		return true, errors.New("fill range incomplete")
	})
	if err := st.Start(context.Background()); err != nil {
		t.Fatalf("Start() should retain the active runtime with openings blocked: %v", err)
	}
	if _, err := gate.Begin(); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("opening gate error = %v, want retained pending-ledger block", err)
	}
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.ExecutionLedgerUnverified || len(persisted.PendingExecutions) != 1 || persisted.PendingExecutions[0].OrderID != 92 {
		t.Fatalf("failed recovery lost its durable marker: %+v", persisted)
	}
	if err := st.Stop(); err != nil {
		t.Fatalf("Stop() with a retained ledger block: %v", err)
	}
}

func TestFundingPerpSpreadUnresolvedOrderBlocksOwnershipAndProgress(t *testing.T) {
	st := &FundingPerpSpreadStrategy{}
	st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
		return false, errors.New("order identity unavailable")
	})
	resolved, err := st.recordOrderExecution(context.Background(), nil, nil, nil)
	if err == nil || resolved || !st.exposureUnknown {
		t.Fatalf("unresolved order state = resolved:%t err:%v exposureUnknown:%t", resolved, err, st.exposureUnknown)
	}
}

func TestFundingPerpSpreadCloseAttemptsBothLegsWhenResolvedFillPersistenceFails(t *testing.T) {
	a := &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}}}
	b := &fundingSpreadTestExchange{name: "b", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.01}}}
	st := &FundingPerpSpreadStrategy{
		legA: a, legB: b, symA: "BTCUSDT", symB: "BTCUSDT", ownershipReady: true,
		ownedA: -0.01, ownedB: 0.01, maxBasis: 1,
	}
	st.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
		if a.placed != 1 || b.placed != 1 || len(a.positions) != 0 || len(b.positions) != 0 {
			t.Fatalf("fill ledger ran before both risk exits: placements=(%d,%d), positions=(%v,%v)", a.placed, b.placed, a.positions, b.positions)
		}
		return true, errors.New("fill persistence failed")
	})
	err := st.closeAllCoordinated(context.Background(), "ledger_failure_test")
	if err == nil || a.placed != 1 || b.placed != 1 {
		t.Fatalf("close results: err=%v placements=(%d,%d); both risk-reducing closes must be attempted", err, a.placed, b.placed)
	}
	if st.ownedA != 0 || st.ownedB != 0 {
		t.Fatalf("verified closed ownership not cleared: (%v,%v)", st.ownedA, st.ownedB)
	}
}

func TestFundingPerpSpreadCloseKeepsIntentUnknownWhenPostCloseOrdersAreNil(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	ex := &fundingSpreadTestExchange{
		name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}}, nilOrdersAfterPlace: true,
	}
	st := &FundingPerpSpreadStrategy{legA: ex, legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT"}
	st.ownershipReady, st.ownedA = true, -0.01
	st.SetRuntimeStateStore(store)
	if err := st.closeLeg(context.Background(), ex, "BTCUSDT", -0.01); err == nil {
		t.Fatal("close was marked verified without an authoritative post-close order snapshot")
	}
	if !st.exposureUnknown || ex.placed != 1 || !store.found {
		t.Fatalf("uncertain close not retained: exposureUnknown=%v placed=%d stateSaved=%v", st.exposureUnknown, ex.placed, store.found)
	}
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.IntentInFlight || persisted.PendingOrder == nil {
		t.Fatalf("uncertain close intent was cleared: %+v", persisted)
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

func TestFundingPerpSpreadStartFlattensRecoveredFilledPendingOrder(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true, IntentInFlight: true,
		PendingOrder: &fundingPerpSpreadOrderIntent{ClientOrderID: "filled-order", LegExchange: "a", Symbol: "BTCUSDT", Side: "SELL", Quantity: 0.01},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := &fundingSpreadOrderLookupExchange{
		fundingSpreadTestExchange: &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}}},
		order:                     &exchange.Order{OrderID: 7654, ClientOrderID: "filled-order", Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 0.01, Status: exchange.OrderStatusNew},
		terminalOrder:             &exchange.Order{OrderID: 7654, ClientOrderID: "filled-order", Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 0.01, ExecutedQty: 0.01, Status: exchange.OrderStatusFilled},
	}
	store := &memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true}
	gate := &execution.OpeningGate{}
	st := &FundingPerpSpreadStrategy{legA: a, legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT", tickInt: time.Hour, openingGate: gate}
	st.SetRuntimeStateStore(store)
	st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
	recorded := 0
	st.SetExecutionRecorder(func(_ context.Context, client exchange.IExchange, request *exchange.OrderRequest, order *exchange.Order) (bool, error) {
		recorded++
		if recorded == 1 && (client != a || request.ClientOrderID != "filled-order" || request.Symbol != "BTCUSDT" || request.Side != exchange.SideSell || order.OrderID != 7654 || order.ExecutedQty != 0.01 || order.Status != exchange.OrderStatusFilled) {
			t.Fatalf("startup recorder received mismatched recovered order: client=%v request=%+v order=%+v", client.GetName(), request, order)
		}
		return true, nil
	})
	if err := st.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed to flatten the verified interrupted exposure: %v", err)
	}
	if recorded != 2 || a.placed != 1 {
		t.Fatalf("startup recovery should ledger both the recovered fill and reducing close: recorder calls=%d close orders=%d", recorded, a.placed)
	}
	if err := st.VerifyFlat(context.Background()); err != nil {
		t.Fatalf("startup recovery did not leave both legs flat: %v", err)
	}
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.EmergencyCloseRequired || persisted.OwnedA != 0 || persisted.OwnedB != 0 || persisted.IntentInFlight {
		t.Fatalf("successful startup recovery was not durably settled: %+v", persisted)
	}
	st.mu.RLock()
	cancel, done := st.cancel, st.runDone
	st.mu.RUnlock()
	cancel()
	<-done
	if unblock, err := gate.Begin(); err != nil {
		t.Fatalf("recovery gate remained blocked after verified flatness: %v", err)
	} else {
		unblock()
	}
}

func TestFundingPerpSpreadEmergencyCloseRecoveryPersistsAndRetries(t *testing.T) {
	state, err := json.Marshal(fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: "a", LegASymbol: "BTCUSDT",
		LegBExchange: "b", LegBSymbol: "ETHUSDT", OwnershipReady: true,
		EmergencyCloseRequired: true, OwnedA: -0.01,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: fundingPerpSpreadRuntimeStateVersion, payload: string(state), found: true}
	firstLegA := &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}}, residual: -0.005}
	newStrategy := func(legA exchange.IExchange, gate *execution.OpeningGate) *FundingPerpSpreadStrategy {
		st := &FundingPerpSpreadStrategy{legA: legA, legB: &fundingSpreadTestExchange{name: "b"}, symA: "BTCUSDT", symB: "ETHUSDT", tickInt: time.Hour, openingGate: gate}
		st.SetRuntimeStateStore(store)
		st.SetCoordinationLock(&fundingSpreadCoordinationLock{})
		st.SetExecutionRecorder(func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error) {
			return true, nil
		})
		return st
	}
	firstGate := &execution.OpeningGate{}
	first := newStrategy(firstLegA, firstGate)
	if err := first.Start(context.Background()); err == nil {
		t.Fatal("startup accepted emergency recovery while the reducing order left residual exposure")
	}
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.EmergencyCloseRequired || math.Abs(persisted.OwnedA-(-0.005)) > 1e-12 {
		t.Fatalf("failed recovery did not persist its remaining owned exposure for retry: %+v", persisted)
	}
	if unblock, err := firstGate.Begin(); !errors.Is(err, execution.ErrOpeningPaused) {
		if unblock != nil {
			unblock()
		}
		t.Fatalf("failed emergency recovery did not keep opening blocked: %v", err)
	}

	secondLegA := &fundingSpreadTestExchange{name: "a", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.005}}}
	second := newStrategy(secondLegA, &execution.OpeningGate{})
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("restart did not retry a durably recorded emergency close: %v", err)
	}
	if err := second.VerifyFlat(context.Background()); err != nil {
		t.Fatalf("retried emergency close did not verify flatness: %v", err)
	}
	persisted = fundingPerpSpreadRuntimeState{}
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.EmergencyCloseRequired || persisted.OwnedA != 0 || persisted.OwnedB != 0 {
		t.Fatalf("successful retry did not clear durable recovery state: %+v; payload=%s", persisted, store.payload)
	}
	second.mu.RLock()
	cancel, done := second.cancel, second.runDone
	second.mu.RUnlock()
	cancel()
	<-done
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

func TestFundingPerpSpreadPersistsExecutionIdentityBeforeClearingOrderIntent(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	st := &FundingPerpSpreadStrategy{
		legA: &fundingSpreadTestExchange{name: "a"}, legB: &fundingSpreadTestExchange{name: "b"},
		symA: "BTCUSDT", symB: "ETHUSDT", ownershipReady: true,
	}
	st.SetRuntimeStateStore(store)
	request := &exchange.OrderRequest{ClientOrderID: "stable-order-id", Symbol: "ETHUSDT", Side: exchange.SideBuy, Quantity: 0.25}
	client := st.legB
	if err := st.beginOrderIntent(fundingPerpSpreadOrderIntent{
		ClientOrderID: request.ClientOrderID, LegExchange: "b", Symbol: request.Symbol,
		Side: string(request.Side), Quantity: request.Quantity,
	}); err != nil {
		t.Fatalf("beginOrderIntent() error = %v", err)
	}
	if err := st.persistPendingExecution(client, request, &exchange.Order{OrderID: 123, ClientOrderID: request.ClientOrderID}); err != nil {
		t.Fatalf("persistPendingExecution() error = %v", err)
	}
	var persisted fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.IntentInFlight || persisted.PendingOrder == nil || !persisted.ExecutionLedgerUnverified || len(persisted.PendingExecutions) != 1 {
		t.Fatalf("intent was cleared or execution identity was not durable before settlement: %+v", persisted)
	}
	if err := st.finishOrderIntent(); err != nil {
		t.Fatalf("finishOrderIntent() error = %v", err)
	}
	persisted = fundingPerpSpreadRuntimeState{}
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.IntentInFlight || persisted.PendingOrder != nil || !persisted.ExecutionLedgerUnverified || len(persisted.PendingExecutions) != 1 {
		t.Fatalf("settled intent lost its pending execution marker: %+v", persisted)
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

func TestFundingPerpSpreadStatusDoesNotReportUnimplementedStatistics(t *testing.T) {
	strategy := &FundingPerpSpreadStrategy{
		symA: "BTCUSDT", symB: "ETHUSDT",
		ownedA: 1.25, ownedB: -1.25,
		ownershipReady: true,
	}
	if stats := strategy.GetStatistics(); stats != nil {
		t.Fatalf("unimplemented statistics must be omitted, got %+v", stats)
	}
	data := strategy.GetVisualizationData()
	if data["ownership_verified"] != true || data["leg_a_owned_quantity"] != 1.25 || data["leg_b_owned_quantity"] != -1.25 {
		t.Fatalf("verified two-leg ownership missing from status: %+v", data)
	}
	strategy.exposureUnknown = true
	if verified := strategy.GetVisualizationData()["ownership_verified"]; verified != false {
		t.Fatalf("unknown exposure was reported as verified: %v", verified)
	}
}
