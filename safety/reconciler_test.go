package safety

import (
	"context"
	"errors"
	"math"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"sync"
	"testing"
	"time"
)

type blockingReconcileExchange struct {
	MockReconcileExchange
	blockPositions bool
	blockOrders    bool
	positionsRead  chan struct{}
	ordersRead     chan struct{}
}

func (m *blockingReconcileExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	if m.blockPositions {
		close(m.positionsRead)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return m.MockReconcileExchange.GetPositions(ctx, symbol)
}

func (m *blockingReconcileExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	if m.blockOrders {
		close(m.ordersRead)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return m.MockReconcileExchange.GetOpenOrders(ctx, symbol)
}

type contextRecordingLock struct {
	*lock.NopLock
	unlockSawCanceled bool
}

func (m *contextRecordingLock) Unlock(ctx context.Context, key string) error {
	m.unlockSawCanceled = ctx.Err() != nil
	return nil
}

func TestReconcilerContextCancellationReachesVenueAndReleasesLock(t *testing.T) {
	for _, stage := range []string{"positions", "open_orders"} {
		t.Run(stage, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			ex := &blockingReconcileExchange{positionsRead: make(chan struct{}), ordersRead: make(chan struct{})}
			if stage == "positions" {
				ex.blockPositions = true
			} else {
				ex.blockOrders = true
			}
			pm := &MockPositionManager{Symbol: "BTCUSDT", Slots: make(map[float64]interface{})}
			distLock := &contextRecordingLock{NopLock: lock.NewNopLock()}
			r := NewReconciler(cfg, ex, pm, distLock)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- r.ReconcileContext(ctx) }()

			started := ex.positionsRead
			if stage == "open_orders" {
				started = ex.ordersRead
			}
			select {
			case <-started:
			case <-time.After(time.Second):
				cancel()
				t.Fatal("reconciliation did not reach the expected venue request")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("ReconcileContext() error=%v, want context.Canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatal("venue request ignored reconciliation cancellation")
			}
			if distLock.unlockSawCanceled {
				t.Fatal("distributed lock cleanup used the canceled operation context")
			}
			if pm.ReconcileCount != 0 {
				t.Fatalf("canceled reconciliation must not publish a completed sample, count=%d", pm.ReconcileCount)
			}
		})
	}
}

func TestReconcilerContextCancellationInterruptsThrottle(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30
	ex := &MockReconcileExchange{}
	pm := &MockPositionManager{Symbol: "BTCUSDT", Slots: make(map[float64]interface{})}
	r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
	r.lastReconcileTime = time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	err := r.ReconcileContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReconcileContext() error=%v, want context.Canceled", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("canceled reconciliation waited for the throttle interval")
	}
}

func TestReconcilerFailsClosedWhenDistributedLockCannotBeAcquired(t *testing.T) {
	wantErr := errors.New("distributed lock backend unavailable")
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30
	pm := &MockPositionManager{Symbol: "BTCUSDT"}
	ex := &MockReconcileExchange{Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0}}}
	r := NewReconciler(cfg, ex, pm, &failingReconcileLock{NopLock: lock.NewNopLock(), err: wantErr})
	err := r.Reconcile()
	if !errors.Is(err, wantErr) {
		t.Fatalf("Reconcile() error = %v, want wrapped %v", err, wantErr)
	}
	if pm.FailReconcileErr == nil {
		t.Fatal("lock failure did not engage fail-closed order gate")
	}
	if pm.ReconcileCount != 0 || pm.ForceSyncCount != 0 {
		t.Fatalf("lock failure changed reconciliation state: count=%d sync=%d", pm.ReconcileCount, pm.ForceSyncCount)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := execution.AcquireLocalPositionCoordination(ctx, execution.PositionReconciliationLockKey("unknown", "BTCUSDT"))
	if err != nil {
		t.Fatalf("local coordination barrier remained held after lock failure: %v", err)
	}
	release()
}

func TestReconcilerFailsClosedWhenPositionSyncIsRejected(t *testing.T) {
	syncErr := errors.New("UNKNOWN order blocks position sync")
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30
	cfg.Trading.MarketType = "futures"
	pm := &MockPositionManager{
		Symbol: "BTCUSDT",
		Slots: map[float64]interface{}{
			50000: TestSlot{PositionStatus: "FILLED", PositionQty: 0.2, OrderSide: "SELL", OrderStatus: "NOT_PLACED"},
		},
		ForceSyncErr: syncErr,
	}
	ex := &MockReconcileExchange{Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0.1}}}
	r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
	err := r.Reconcile()
	if !errors.Is(err, syncErr) {
		t.Fatalf("Reconcile() error = %v, want wrapped %v", err, syncErr)
	}
	if pm.FailReconcileErr == nil {
		t.Fatal("rejected position sync did not engage fail-closed order gate")
	}
	if pm.ReconcileCount != 0 {
		t.Fatalf("rejected position sync was counted as successful, count=%d", pm.ReconcileCount)
	}
}

// mockExchangePositionRow 與 reconciler 反射解析一致（Symbol + Size）
type mockExchangePositionRow struct {
	Symbol string
	Size   float64
}

// MockPositionManager 模拟倉位管理器
type MockPositionManager struct {
	Slots            map[float64]interface{}
	TotalBuyQty      float64
	TotalSellQty     float64
	ReconcileCount   int64
	Symbol           string
	PriceInterval    float64
	ForceSyncCount   int
	LastForceSync    float64
	BeginReconcile   func(context.Context) (func(), error)
	BarrierActive    bool
	ForceSyncInGate  bool
	ForceSyncHook    func()
	ForceSyncErr     error
	FailReconcileErr error
}

func (m *MockPositionManager) IterateSlots(fn func(price float64, slot interface{}) bool) {
	for price, slot := range m.Slots {
		if !fn(price, slot) {
			break
		}
	}
}
func (m *MockPositionManager) GetTotalBuyQty() float64             { return m.TotalBuyQty }
func (m *MockPositionManager) GetTotalSellQty() float64            { return m.TotalSellQty }
func (m *MockPositionManager) GetReconcileCount() int64            { return m.ReconcileCount }
func (m *MockPositionManager) IncrementReconcileCount()            { m.ReconcileCount++ }
func (m *MockPositionManager) UpdateLastReconcileTime(t time.Time) {}
func (m *MockPositionManager) GetSymbol() string                   { return m.Symbol }
func (m *MockPositionManager) GetPriceInterval() float64           { return m.PriceInterval }
func (m *MockPositionManager) GetProfitSpread() float64            { return m.PriceInterval }
func (m *MockPositionManager) ForceSyncPositions(exchangePosition float64) error {
	m.ForceSyncCount++
	m.LastForceSync = exchangePosition
	m.ForceSyncInGate = m.BarrierActive
	if m.ForceSyncHook != nil {
		m.ForceSyncHook()
	}
	return m.ForceSyncErr
}

func (m *MockPositionManager) BeginReconciliation(ctx context.Context) (func(), error) {
	if m.BeginReconcile != nil {
		return m.BeginReconcile(ctx)
	}
	return func() {}, nil
}
func (m *MockPositionManager) FailReconciliation(err error) { m.FailReconcileErr = err }

func TestReconcilerRejectsInvalidLocalPositionLedger(t *testing.T) {
	tests := []struct {
		name  string
		slots map[float64]interface{}
	}{
		{name: "NaN", slots: map[float64]interface{}{50000: TestSlot{PositionStatus: "FILLED", PositionQty: math.NaN()}}},
		{name: "positive infinity", slots: map[float64]interface{}{50000: TestSlot{PositionStatus: "FILLED", PositionQty: math.Inf(1)}}},
		{name: "negative quantity", slots: map[float64]interface{}{50000: TestSlot{PositionStatus: "FILLED", PositionQty: -0.1}}},
		{name: "aggregate overflow", slots: map[float64]interface{}{
			50000: TestSlot{PositionStatus: "FILLED", PositionQty: math.MaxFloat64},
			50001: TestSlot{PositionStatus: "FILLED", PositionQty: math.MaxFloat64},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			cfg.Trading.MarketType = "spot"
			cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyAdoptAll
			pm := &MockPositionManager{Symbol: "BTCUSDT", Slots: tt.slots}
			ex := &MockReconcileExchange{Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0}}}
			r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
			if err := r.Reconcile(); err == nil {
				t.Fatal("Reconcile() succeeded with an invalid local position ledger")
			}
			if pm.FailReconcileErr == nil {
				t.Fatal("invalid local ledger did not engage the reconciliation fail-closed gate")
			}
			if pm.ForceSyncCount != 0 || pm.ReconcileCount != 0 {
				t.Fatalf("invalid local ledger changed state: sync=%d reconcile=%d", pm.ForceSyncCount, pm.ReconcileCount)
			}
		})
	}
}

func TestReconcilerRejectsMalformedLocalSlotEvidence(t *testing.T) {
	tests := []struct {
		name  string
		price float64
		slot  interface{}
	}{
		{name: "non-struct slot", price: 50000, slot: "corrupt"},
		{name: "nil slot pointer", price: 50000, slot: (*TestSlot)(nil)},
		{name: "missing order evidence", price: 50000, slot: struct {
			PositionStatus string
			PositionQty    float64
		}{PositionStatus: "FILLED", PositionQty: 0.1}},
		{name: "unknown position status", price: 50000, slot: TestSlot{PositionStatus: "CORRUPT", PositionQty: 0.1}},
		{name: "non-finite slot price", price: math.NaN(), slot: TestSlot{PositionStatus: "FILLED", PositionQty: 0.1}},
		{name: "unknown order status", price: 50000, slot: TestSlot{PositionStatus: "EMPTY", OrderSide: "BUY", OrderStatus: "PENDING_SUBMIT"}},
		{name: "invalid order side", price: 50000, slot: TestSlot{PositionStatus: "EMPTY", OrderSide: "BID", OrderStatus: "UNKNOWN"}},
		{name: "active order missing side", price: 50000, slot: TestSlot{PositionStatus: "EMPTY", OrderStatus: "UNKNOWN"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			cfg.Trading.MarketType = "spot"
			cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyAdoptAll
			pm := &MockPositionManager{Symbol: "BTCUSDT", Slots: map[float64]interface{}{tt.price: tt.slot}}
			ex := &MockReconcileExchange{Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0}}}
			r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
			if err := r.Reconcile(); err == nil {
				t.Fatal("Reconcile() succeeded with malformed local slot evidence")
			}
			if pm.FailReconcileErr == nil {
				t.Fatal("malformed slot evidence did not engage the fail-closed gate")
			}
			if pm.ForceSyncCount != 0 || pm.ReconcileCount != 0 {
				t.Fatalf("malformed slot evidence changed state: sync=%d reconcile=%d", pm.ForceSyncCount, pm.ReconcileCount)
			}
		})
	}
}

type barrierObservingReconcileExchange struct {
	MockReconcileExchange
	manager          *MockPositionManager
	positionsInGate  bool
	openOrdersInGate bool
}

type trackingReconcileLock struct {
	*lock.NopLock
	mu   sync.Mutex
	held bool
}

type failingReconcileLock struct {
	*lock.NopLock
	err error
}

func (m *failingReconcileLock) Lock(context.Context, string, time.Duration) error {
	return m.err
}

func (m *trackingReconcileLock) Lock(context.Context, string, time.Duration) error {
	m.mu.Lock()
	m.held = true
	m.mu.Unlock()
	return nil
}

func (m *trackingReconcileLock) Unlock(context.Context, string) error {
	m.mu.Lock()
	m.held = false
	m.mu.Unlock()
	return nil
}

func (m *trackingReconcileLock) IsHeld() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.held
}

type lockObservingReconciliationStorage struct {
	lock               *trackingReconcileLock
	called             bool
	savedWhileLockHeld bool
}

func (m *lockObservingReconciliationStorage) SaveReconciliationHistory(string, time.Time, float64, float64, float64,
	int, int, float64, float64, float64, float64) error {
	m.called = true
	m.savedWhileLockHeld = m.lock.IsHeld()
	return nil
}

func (m *barrierObservingReconcileExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	m.positionsInGate = m.manager.BarrierActive
	return m.MockReconcileExchange.GetPositions(ctx, symbol)
}

func (m *barrierObservingReconcileExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	m.openOrdersInGate = m.manager.BarrierActive
	return m.MockReconcileExchange.GetOpenOrders(ctx, symbol)
}

// TestSlot 用於對账反射
type TestSlot struct {
	PositionStatus string
	PositionQty    float64
	OrderSide      string
	OrderStatus    string
}

// MockReconcileExchange 专门用於對账测試的 Mock
type MockReconcileExchange struct {
	Positions  []mockExchangePositionRow
	OpenOrders []*exchange.Order
}

func (m *MockReconcileExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	if m.Positions == nil {
		return []mockExchangePositionRow{}, nil
	}
	return m.Positions, nil
}
func (m *MockReconcileExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	if m.OpenOrders == nil {
		return []*exchange.Order{}, nil
	}
	return m.OpenOrders, nil
}
func (m *MockReconcileExchange) GetBaseAsset() string { return "BTC" }

type rawOpenOrdersReconcileExchange struct {
	MockReconcileExchange
	raw interface{}
}

func (m *rawOpenOrdersReconcileExchange) GetOpenOrders(context.Context, string) (interface{}, error) {
	return m.raw, nil
}

type rawPositionReconcileExchange struct {
	MockReconcileExchange
	raw interface{}
}

func (m *rawPositionReconcileExchange) GetPositions(context.Context, string) (interface{}, error) {
	return m.raw, nil
}

type failingReconcileExchange struct {
	MockReconcileExchange
	positionsErr error
	ordersErr    error
}

func (m *failingReconcileExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	if m.positionsErr != nil {
		return nil, m.positionsErr
	}
	return m.MockReconcileExchange.GetPositions(ctx, symbol)
}

func (m *failingReconcileExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	if m.ordersErr != nil {
		return nil, m.ordersErr
	}
	return m.MockReconcileExchange.GetOpenOrders(ctx, symbol)
}

func TestReconcilerRejectsUnverifiedPositionSnapshot(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
	}{
		{name: "nil response", raw: nil},
		{name: "typed nil slice", raw: []mockExchangePositionRow(nil)},
		{name: "wrong response type", raw: []string{"BTCUSDT"}},
		{name: "nil row", raw: []*mockExchangePositionRow{nil}},
		{name: "non-finite quantity", raw: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: math.NaN()}}},
		{name: "duplicate symbol", raw: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0.1}, {Symbol: "BTCUSDT", Size: 0.2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			cfg.Trading.MarketType = "spot"
			cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyAdoptAll
			ex := &rawPositionReconcileExchange{raw: tt.raw}
			pm := &MockPositionManager{
				Symbol: "BTCUSDT",
				Slots: map[float64]interface{}{
					50000: TestSlot{PositionStatus: "FILLED", PositionQty: 0.1, OrderSide: "SELL"},
				},
			}
			r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
			if err := r.Reconcile(); err == nil {
				t.Fatal("Reconcile() succeeded with an unverified position snapshot")
			}
			if pm.FailReconcileErr == nil {
				t.Fatal("unverified position snapshot did not engage the fail-closed gate")
			}
			if pm.ForceSyncCount != 0 {
				t.Fatalf("unverified snapshot triggered ForceSyncPositions(%v)", pm.LastForceSync)
			}
			if pm.ReconcileCount != 0 {
				t.Fatalf("unverified snapshot published a completed sample, count=%d", pm.ReconcileCount)
			}
		})
	}
}

func TestReconcilerRejectsUnverifiedOpenOrderSnapshot(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
	}{
		{name: "nil response", raw: nil},
		{name: "typed nil slice", raw: []*exchange.Order(nil)},
		{name: "wrong response type", raw: []string{"BTCUSDT"}},
		{name: "nil order", raw: []*exchange.Order{nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			cfg.Trading.MarketType = "spot"
			cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyAdoptAll
			ex := &rawOpenOrdersReconcileExchange{raw: tt.raw}
			pm := &MockPositionManager{
				Symbol: "BTCUSDT",
				Slots: map[float64]interface{}{
					50000: TestSlot{PositionStatus: "FILLED", PositionQty: 0.1, OrderSide: "SELL"},
				},
			}
			r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
			if err := r.Reconcile(); err == nil {
				t.Fatal("Reconcile() succeeded with an unverified open-order snapshot")
			}
			if pm.FailReconcileErr == nil {
				t.Fatal("unverified open-order snapshot did not engage the fail-closed gate")
			}
			if pm.ForceSyncCount != 0 || pm.ReconcileCount != 0 {
				t.Fatalf("unverified snapshot changed state: sync=%d reconcile=%d", pm.ForceSyncCount, pm.ReconcileCount)
			}
		})
	}
}

func TestReconcilerFailsClosedOnVenueQueryErrors(t *testing.T) {
	tests := []struct {
		name  string
		venue *failingReconcileExchange
	}{
		{name: "position query", venue: &failingReconcileExchange{positionsErr: errors.New("position API unavailable")}},
		{name: "open-order query", venue: &failingReconcileExchange{ordersErr: errors.New("open-order API unavailable")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			cfg.Trading.MarketType = "futures"
			pm := &MockPositionManager{Symbol: "BTCUSDT"}
			r := NewReconciler(cfg, test.venue, pm, lock.NewNopLock())
			if err := r.ReconcileContext(context.Background()); err == nil {
				t.Fatal("venue query error reported a successful reconciliation")
			}
			if pm.FailReconcileErr == nil || pm.ReconcileCount != 0 {
				t.Fatalf("query failure did not retain fail-closed state: failure=%v reconciled=%d", pm.FailReconcileErr, pm.ReconcileCount)
			}
		})
	}
}

func TestReconcilerSkipsPositionSyncWhileOrdersRemainOpen(t *testing.T) {
	tests := []struct {
		name       string
		openOrders []*exchange.Order
		orderState string
	}{
		{
			name:       "venue order not represented by local slot",
			openOrders: []*exchange.Order{{OrderID: 10, Symbol: "BTCUSDT", Status: exchange.OrderStatusNew}},
			orderState: "NOT_PLACED",
		},
		{
			name:       "local close order awaiting venue snapshot",
			openOrders: []*exchange.Order{},
			orderState: "PLACED",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			cfg.Trading.MarketType = "spot"
			cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyAdoptAll
			ex := &MockReconcileExchange{OpenOrders: tt.openOrders}
			pm := &MockPositionManager{
				Symbol: "BTCUSDT",
				Slots: map[float64]interface{}{
					50000: TestSlot{PositionStatus: "FILLED", PositionQty: 0.1, OrderSide: "SELL", OrderStatus: tt.orderState},
				},
			}
			r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
			if err := r.Reconcile(); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if pm.ForceSyncCount != 0 {
				t.Fatalf("open orders must prevent ForceSyncPositions, got %v", pm.LastForceSync)
			}
			if pm.ReconcileCount != 1 {
				t.Fatalf("valid snapshots should complete reconciliation, count=%d", pm.ReconcileCount)
			}
		})
	}
}

func TestReconcilerDoesNotSyncWhileLocalOrderIsUnknown(t *testing.T) {
	tests := []struct {
		name         string
		direction    string
		position     string
		orderSide    string
		positionQty  float64
		exchangeSize float64
	}{
		{name: "LONG open", direction: "LONG", position: "EMPTY", orderSide: "BUY", exchangeSize: 0.1},
		{name: "LONG close", direction: "LONG", position: "FILLED", orderSide: "SELL", positionQty: 0.1, exchangeSize: 0},
		{name: "SHORT open", direction: "SHORT", position: "EMPTY", orderSide: "SELL", exchangeSize: -0.1},
		{name: "SHORT close", direction: "SHORT", position: "FILLED", orderSide: "BUY", positionQty: 0.1, exchangeSize: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			cfg.Trading.MarketType = "futures"
			cfg.Trading.Direction = tt.direction
			ex := &MockReconcileExchange{Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: tt.exchangeSize}}}
			if tt.exchangeSize == 0 {
				ex.Positions = []mockExchangePositionRow{}
			}
			pm := &MockPositionManager{
				Symbol: "BTCUSDT",
				Slots: map[float64]interface{}{
					50000: TestSlot{PositionStatus: tt.position, PositionQty: tt.positionQty, OrderSide: tt.orderSide, OrderStatus: "UNKNOWN"},
				},
			}
			r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
			if err := r.Reconcile(); err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if pm.ForceSyncCount != 0 {
				t.Fatalf("UNKNOWN local order must prevent ForceSyncPositions, got %v", pm.LastForceSync)
			}
			if pm.ReconcileCount != 1 {
				t.Fatalf("valid snapshots should count as reconciled without applying unsafe sync, count=%d", pm.ReconcileCount)
			}
		})
	}
}

func TestReconcilerHoldsSubmissionBarrierAcrossSnapshotsAndSync(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30
	cfg.Trading.MarketType = "spot"
	cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyAdoptAll
	pm := &MockPositionManager{
		Symbol: "BTCUSDT",
		Slots: map[float64]interface{}{
			50000: TestSlot{PositionStatus: "FILLED", PositionQty: 0.01, OrderStatus: "NOT_PLACED"},
		},
	}
	pm.BeginReconcile = func(context.Context) (func(), error) {
		pm.BarrierActive = true
		return func() { pm.BarrierActive = false }, nil
	}
	ex := &barrierObservingReconcileExchange{
		MockReconcileExchange: MockReconcileExchange{Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0.05}}},
		manager:               pm,
	}
	r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
	if err := r.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !ex.positionsInGate || !ex.openOrdersInGate || !pm.ForceSyncInGate {
		t.Fatalf("barrier not held across critical section: positions=%v orders=%v sync=%v",
			ex.positionsInGate, ex.openOrdersInGate, pm.ForceSyncInGate)
	}
	if pm.BarrierActive {
		t.Fatal("reconciliation did not release the submission barrier")
	}
}

func TestReconcilerReleasesCoordinationLockBeforeHistoryStorage(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30
	cfg.Trading.MarketType = "spot"
	cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyAdoptAll
	distLock := &trackingReconcileLock{NopLock: lock.NewNopLock()}
	pm := &MockPositionManager{
		Symbol: "BTCUSDT",
		Slots: map[float64]interface{}{
			50000: TestSlot{PositionStatus: "FILLED", PositionQty: 0.01, OrderStatus: "NOT_PLACED"},
		},
	}
	forceSyncHeldLock := false
	pm.ForceSyncHook = func() { forceSyncHeldLock = distLock.IsHeld() }
	ex := &MockReconcileExchange{Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0.05}}}
	storage := &lockObservingReconciliationStorage{lock: distLock}
	r := NewReconciler(cfg, ex, pm, distLock)
	r.SetStorage(storage)
	if err := r.Reconcile(); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !forceSyncHeldLock {
		t.Fatal("position synchronization ran after releasing the distributed lock")
	}
	if !storage.called || storage.savedWhileLockHeld {
		t.Fatalf("history storage lock state: called=%v held=%v, want called after release", storage.called, storage.savedWhileLockHeld)
	}
}

func TestReconciler_Reconcile(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30

	ex := &MockReconcileExchange{}

	pm := &MockPositionManager{
		Symbol:        "BTCUSDT",
		PriceInterval: 100.0,
		Slots:         make(map[float64]interface{}),
	}

	// 構造本地數據
	// 槽位 1: 已成交持倉，有賣單挂單
	pm.Slots[50000.0] = TestSlot{
		PositionStatus: "FILLED",
		PositionQty:    0.1,
		OrderSide:      "SELL",
		OrderStatus:    "PLACED",
	}
	// 槽位 2: 無持倉，有買單挂單
	pm.Slots[49900.0] = TestSlot{
		PositionStatus: "EMPTY",
		PositionQty:    0.0,
		OrderSide:      "BUY",
		OrderStatus:    "PLACED",
	}

	// 創建一個 mock 分布式鎖
	mockLock := lock.NewNopLock() // 使用無操作鎖用於测試
	r := NewReconciler(cfg, ex, pm, mockLock)

	// 模拟執行對账
	err := r.Reconcile()
	if err != nil {
		t.Fatalf("對账執行失败: %v", err)
	}

	// 驗证對账次數增加
	if pm.ReconcileCount != 1 {
		t.Errorf("對账次數应為 1, 得到 %d", pm.ReconcileCount)
	}
}

// TestReconciler_SpotConservative_SkipsForceSyncWhenLocalLessThanExchange 現貨 conservative：本地小於交易所時不自動收編外部基礎幣
func TestReconciler_SpotConservative_SkipsForceSyncWhenLocalLessThanExchange(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30
	cfg.Trading.MarketType = "spot"
	cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyConservative

	ex := &MockReconcileExchange{
		Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0.05}},
	}

	pm := &MockPositionManager{
		Symbol:        "BTCUSDT",
		PriceInterval: 100.0,
		Slots: map[float64]interface{}{
			50000.0: TestSlot{
				PositionStatus: "FILLED",
				PositionQty:    0.01,
				OrderSide:      "SELL",
				OrderStatus:    "PLACED",
			},
		},
	}

	r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
	if err := r.Reconcile(); err != nil {
		t.Fatalf("對账執行失败: %v", err)
	}
	if pm.ForceSyncCount != 0 {
		t.Fatalf("conservative 現貨不應調用 ForceSyncPositions，得到調用次數 %d", pm.ForceSyncCount)
	}
}

// TestReconciler_SpotAdoptAll_ForceSyncWhenLocalLessThanExchange 現貨 adopt_all：本地小於交易所時以交易所為準補齊
func TestReconciler_SpotAdoptAll_ForceSyncWhenLocalLessThanExchange(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30
	cfg.Trading.MarketType = "spot"
	cfg.Trading.SpotInventoryPolicy = config.SpotInventoryPolicyAdoptAll

	ex := &MockReconcileExchange{
		Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: 0.05}},
	}

	pm := &MockPositionManager{
		Symbol:        "BTCUSDT",
		PriceInterval: 100.0,
		Slots: map[float64]interface{}{
			50000.0: TestSlot{
				PositionStatus: "FILLED",
				PositionQty:    0.01,
				OrderSide:      "SELL",
				OrderStatus:    "NOT_PLACED",
			},
		},
	}

	r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
	if err := r.Reconcile(); err != nil {
		t.Fatalf("對账執行失败: %v", err)
	}
	if pm.ForceSyncCount != 1 || pm.LastForceSync != 0.05 {
		t.Fatalf("adopt_all 應 ForceSyncPositions(0.05)，得到 count=%d last=%v", pm.ForceSyncCount, pm.LastForceSync)
	}
}

// TestReconciler_DirectionAwareSync 交易所持倉帶符號：SHORT 按絕對值比較、BOTH 不按淨值同步、方向不符跳過
func TestReconciler_DirectionAwareSync(t *testing.T) {
	filled := func(qty float64, closeSide string) TestSlot {
		return TestSlot{PositionStatus: "FILLED", PositionQty: qty, OrderSide: closeSide, OrderStatus: "NOT_PLACED"}
	}
	tests := []struct {
		name          string
		direction     string
		exchangeSize  float64
		localQty      float64
		closeSide     string
		wantSyncCount int
		wantSyncValue float64
		wantErr       bool
	}{
		{name: "SHORT 本地超出交易所空倉時修剪到絕對值", direction: "SHORT", exchangeSize: -0.03, localQty: 0.05, closeSide: "BUY", wantSyncCount: 1, wantSyncValue: 0.03},
		{name: "SHORT 本地少於交易所空倉時拒絕收編無歸屬差額", direction: "SHORT", exchangeSize: -0.05, localQty: 0.03, closeSide: "BUY", wantErr: true},
		{name: "SHORT 數量一致不同步", direction: "SHORT", exchangeSize: -0.02, localQty: 0.02, closeSide: "BUY", wantSyncCount: 0},
		{name: "SHORT 交易所已平倉且無挂單時清空", direction: "SHORT", exchangeSize: 0, localQty: 0.02, closeSide: "BUY", wantSyncCount: 1, wantSyncValue: 0},
		{name: "SHORT 交易所出現多倉方向不符跳過", direction: "SHORT", exchangeSize: 0.02, localQty: 0.05, closeSide: "BUY", wantSyncCount: 0},
		{name: "BOTH 淨持倉為 0 不清空本地", direction: "BOTH", exchangeSize: 0, localQty: 0.02, closeSide: "SELL", wantSyncCount: 0},
		{name: "BOTH 淨持倉小於本地不修剪", direction: "BOTH", exchangeSize: 0.01, localQty: 0.05, closeSide: "SELL", wantSyncCount: 0},
		{name: "LONG 交易所出現空倉方向不符跳過", direction: "LONG", exchangeSize: -0.02, localQty: 0.05, closeSide: "SELL", wantSyncCount: 0},
		{name: "LONG 本地超出交易所時修剪", direction: "LONG", exchangeSize: 0.03, localQty: 0.05, closeSide: "SELL", wantSyncCount: 1, wantSyncValue: 0.03},
		{name: "LONG 本地少於交易所多倉時拒絕收編無歸屬差額", direction: "LONG", exchangeSize: 0.05, localQty: 0.03, closeSide: "SELL", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.ReconcileInterval = 30
			cfg.Trading.MarketType = "futures"
			cfg.Trading.Direction = tt.direction

			ex := &MockReconcileExchange{Positions: []mockExchangePositionRow{{Symbol: "BTCUSDT", Size: tt.exchangeSize}}}
			if tt.exchangeSize == 0 {
				ex.Positions = []mockExchangePositionRow{}
			}
			pm := &MockPositionManager{
				Symbol:        "BTCUSDT",
				PriceInterval: 100,
				Slots:         map[float64]interface{}{50000.0: filled(tt.localQty, tt.closeSide)},
			}
			r := NewReconciler(cfg, ex, pm, lock.NewNopLock())
			err := r.Reconcile()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Reconcile() error = %v, wantErr %v", err, tt.wantErr)
			}
			if pm.ForceSyncCount != tt.wantSyncCount {
				t.Fatalf("ForceSyncCount = %d, want %d", pm.ForceSyncCount, tt.wantSyncCount)
			}
			if tt.wantSyncCount > 0 && pm.LastForceSync != tt.wantSyncValue {
				t.Fatalf("ForceSyncPositions(%v), want %v", pm.LastForceSync, tt.wantSyncValue)
			}
			if tt.wantErr {
				if pm.FailReconcileErr == nil {
					t.Fatal("unowned futures position difference did not engage fail-closed gate")
				}
				if pm.ReconcileCount != 0 {
					t.Fatalf("unowned futures position difference was counted as reconciled: %d", pm.ReconcileCount)
				}
			}
		})
	}
}
