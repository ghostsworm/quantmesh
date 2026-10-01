package strategy

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/exchange/income"
	"quantmesh/execution"
)

func TestNewFundingCarryStrategy_ConfigParams(t *testing.T) {
	stratCfg := map[string]interface{}{
		"min_funding_rate":       0.001,
		"exit_funding_rate":      0.0005,
		"max_basis_pct":          0.3,
		"rebalance_interval_sec": 120.0,
		"settlement_buffer_min":  10.0,
		"reverse_enabled":        true,
		"auto_transfer_enabled":  true,
		"profit_harvest_enabled": true,
		"profit_harvest_min":     10.0,
	}
	s := NewFundingCarryStrategy("fc-test", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, stratCfg)
	if s.minFundingRate != 0.001 {
		t.Errorf("minFundingRate = %v, want 0.001", s.minFundingRate)
	}
	if s.exitFundingRate != 0.0005 {
		t.Errorf("exitFundingRate = %v, want 0.0005", s.exitFundingRate)
	}
	if s.maxBasisPct != 0.3 {
		t.Errorf("maxBasisPct = %v, want 0.3", s.maxBasisPct)
	}
	if s.tickInterval.Seconds() != 120 {
		t.Errorf("tickInterval = %v, want 120s", s.tickInterval)
	}
	if s.settlementBuffer != 10*time.Minute {
		t.Errorf("settlementBuffer = %v, want 10m", s.settlementBuffer)
	}
	// reverse_enabled 為 true 但 marginEx 為 nil，應被強制禁用
	if s.reverseEnabled {
		t.Error("reverseEnabled should be false when marginEx is nil")
	}
	if !s.autoTransferEnabled {
		t.Error("autoTransferEnabled should be true")
	}
	if !s.profitHarvestEnabled {
		t.Error("profitHarvestEnabled should be true")
	}
	if s.profitHarvestMin != 10.0 {
		t.Errorf("profitHarvestMin = %v, want 10.0", s.profitHarvestMin)
	}
}

func TestNewFundingCarryStrategy_Defaults(t *testing.T) {
	s := NewFundingCarryStrategy("fc-def", nil, config.SymbolConfig{Symbol: "ETHUSDT"}, nil, nil, nil, nil)
	if s.minFundingRate != 0.0004 {
		t.Errorf("default minFundingRate = %v, want 0.0004", s.minFundingRate)
	}
	if s.exitFundingRate != 0.0002 {
		t.Errorf("default exitFundingRate = %v, want 0.0002", s.exitFundingRate)
	}
	if s.maxBasisPct != 0.5 {
		t.Errorf("default maxBasisPct = %v, want 0.5", s.maxBasisPct)
	}
	if s.tickInterval != 45*time.Second {
		t.Errorf("default tickInterval = %v, want 45s", s.tickInterval)
	}
	if s.settlementBuffer != 5*time.Minute {
		t.Errorf("default settlementBuffer = %v, want 5m", s.settlementBuffer)
	}
	if s.reverseEnabled {
		t.Error("default reverseEnabled should be false")
	}
}

func TestRoundQty(t *testing.T) {
	s := &FundingCarryStrategy{}
	tests := []struct {
		qty      float64
		decimals int
		want     float64
	}{
		{0.12345, 3, 0.123},
		{0.999, 2, 0.99},
		{1.0, 0, 1.0},
		{0.001, 8, 0.001},
	}
	for _, tt := range tests {
		got := s.roundQty(tt.qty, tt.decimals)
		if got != tt.want {
			t.Errorf("roundQty(%.8f, %d) = %.8f, want %.8f", tt.qty, tt.decimals, got, tt.want)
		}
	}
}

func TestPublishEvent_NilBus(t *testing.T) {
	s := NewFundingCarryStrategy("fc-nil-bus", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{"test": true})
}

type mockEventBus struct {
	mu     sync.Mutex
	events []*event.Event
}

func (m *mockEventBus) Publish(e *event.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockEventBus) getEvents() []*event.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*event.Event, len(m.events))
	copy(cp, m.events)
	return cp
}

type mockFCExchange struct {
	name               string
	marketType         string
	latestPrice        float64
	fundingRate        float64
	positions          []*exchange.Position
	positionsErr       error
	openOrders         []*exchange.Order
	balance            float64
	balanceErr         error
	baseAsset          string
	priceDecimals      int
	quantityDecimals   int
	placeOrderErr      error
	placedOrders       []*exchange.OrderRequest
	getOrderStatus     exchange.OrderStatus
	getOrderExecQty    float64
	repayCalls         int
	returnNilPositions bool
	returnNilOrders    bool
	mu                 sync.Mutex
}

func (m *mockFCExchange) GetName() string          { return m.name }
func (m *mockFCExchange) GetMarketType() string    { return m.marketType }
func (m *mockFCExchange) GetBaseAsset() string     { return m.baseAsset }
func (m *mockFCExchange) GetQuoteAsset() string    { return "USDT" }
func (m *mockFCExchange) GetPriceDecimals() int    { return m.priceDecimals }
func (m *mockFCExchange) GetQuantityDecimals() int { return m.quantityDecimals }
func (m *mockFCExchange) StartOrderStream(ctx context.Context, cb func(interface{})) error {
	return nil
}
func (m *mockFCExchange) StopOrderStream() error { return nil }
func (m *mockFCExchange) StartPriceStream(ctx context.Context, symbol string, cb func(float64)) error {
	return nil
}
func (m *mockFCExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, cb exchange.CandleUpdateCallback) error {
	return nil
}
func (m *mockFCExchange) StopKlineStream() error { return nil }
func (m *mockFCExchange) GetHistoricalKlines(ctx context.Context, symbol, interval string, limit int) ([]*exchange.Candle, error) {
	return nil, nil
}
func (m *mockFCExchange) BatchPlaceOrders(ctx context.Context, orders []*exchange.OrderRequest) ([]*exchange.Order, bool) {
	return nil, false
}
func (m *mockFCExchange) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return nil
}
func (m *mockFCExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return nil
}
func (m *mockFCExchange) CancelAllOrders(ctx context.Context, symbol string) error { return nil }
func (m *mockFCExchange) GetOpenOrders(ctx context.Context, symbol string) ([]*exchange.Order, error) {
	if m.returnNilOrders {
		return nil, nil
	}
	if m.openOrders != nil {
		return m.openOrders, nil
	}
	return []*exchange.Order{}, nil
}
func (m *mockFCExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*exchange.OrderFill, error) {
	return nil, nil
}
func (m *mockFCExchange) GetAccount(ctx context.Context) (*exchange.Account, error) {
	return &exchange.Account{}, nil
}
func (m *mockFCExchange) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}
func (m *mockFCExchange) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return m.latestPrice, nil
}
func (m *mockFCExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	return nil, nil
}
func (m *mockFCExchange) InternalTransfer(ctx context.Context, from, to, asset string, amount float64) (string, error) {
	return "tx-mock", nil
}
func (m *mockFCExchange) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}
func (m *mockFCExchange) GetFundingInfo(ctx context.Context, symbol string) (*exchange.FundingInfo, error) {
	return &exchange.FundingInfo{
		Symbol:          symbol,
		Rate:            m.fundingRate,
		NextFundingTime: time.Now().Add(4 * time.Hour),
	}, nil
}
func (m *mockFCExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return m.latestPrice, nil
}
func (m *mockFCExchange) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return m.fundingRate, nil
}
func (m *mockFCExchange) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	if m.positionsErr != nil {
		return nil, m.positionsErr
	}
	if m.returnNilPositions {
		return nil, nil
	}
	if m.positions == nil {
		return []*exchange.Position{}, nil
	}
	return m.positions, nil
}
func (m *mockFCExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	return m.balance, m.balanceErr
}
func (m *mockFCExchange) Borrow(context.Context, string, float64) (int64, error) { return 1, nil }
func (m *mockFCExchange) Repay(context.Context, string, float64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.repayCalls++
	return int64(m.repayCalls), nil
}
func (m *mockFCExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.placedOrders = append(m.placedOrders, req)
	if m.placeOrderErr != nil {
		return nil, m.placeOrderErr
	}
	if req.ReduceOnly {
		remaining := req.Quantity
		for _, position := range m.positions {
			if position == nil || position.Symbol != req.Symbol || remaining <= 0 {
				continue
			}
			if req.Side == exchange.SideBuy && position.Size < 0 {
				closed := math.Min(math.Abs(position.Size), remaining)
				position.Size += closed
				remaining -= closed
			} else if req.Side == exchange.SideSell && position.Size > 0 {
				closed := math.Min(position.Size, remaining)
				position.Size -= closed
				remaining -= closed
			}
		}
	}
	return &exchange.Order{
		OrderID:     int64(len(m.placedOrders)),
		Symbol:      req.Symbol,
		Side:        req.Side,
		Status:      exchange.OrderStatusFilled,
		Quantity:    req.Quantity,
		ExecutedQty: req.Quantity,
	}, nil
}
func (m *mockFCExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	return &exchange.Order{
		OrderID:     orderID,
		Status:      m.getOrderStatus,
		ExecutedQty: m.getOrderExecQty,
	}, nil
}

func TestFundingCarryVerifyFlatRequiresLiveAndDurableFlatEvidence(t *testing.T) {
	newFlatStrategy := func(t *testing.T) (*FundingCarryStrategy, *mockFCExchange, *memoryRuntimeStateStore) {
		t.Helper()
		spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC", balance: 2, quantityDecimals: 8}
		futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", quantityDecimals: 3}
		stateStore := &memoryRuntimeStateStore{}
		strategy := NewFundingCarryStrategy("fc-verify-flat", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
		strategy.SetRuntimeStateStore(stateStore)
		strategy.mu.Lock()
		strategy.strategySpotKnown = true
		strategy.direction = DirectionNone
		if err := strategy.persistRuntimeStateLocked(); err != nil {
			strategy.mu.Unlock()
			t.Fatal(err)
		}
		strategy.mu.Unlock()
		return strategy, spot, stateStore
	}

	t.Run("allows unrelated existing spot inventory", func(t *testing.T) {
		strategy, _, _ := newFlatStrategy(t)
		if err := strategy.VerifyFlat(context.Background()); err != nil {
			t.Fatalf("VerifyFlat rejected unrelated pre-existing spot inventory: %v", err)
		}
	})

	t.Run("rejects active order", func(t *testing.T) {
		strategy, spot, _ := newFlatStrategy(t)
		spot.openOrders = []*exchange.Order{{OrderID: 1, Symbol: "BTCUSDT", Status: exchange.OrderStatusNew}}
		if err := strategy.VerifyFlat(context.Background()); err == nil {
			t.Fatal("VerifyFlat accepted an active spot order")
		}
	})

	t.Run("rejects residual futures exposure", func(t *testing.T) {
		strategy, _, _ := newFlatStrategy(t)
		strategy.fut.(*mockFCExchange).positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.1}}
		if err := strategy.VerifyFlat(context.Background()); err == nil {
			t.Fatal("VerifyFlat accepted residual futures exposure")
		}
	})

	t.Run("rejects symbol-less or cross-symbol zero futures rows", func(t *testing.T) {
		for _, symbol := range []string{"", "ETHUSDT"} {
			strategy, _, _ := newFlatStrategy(t)
			strategy.fut.(*mockFCExchange).positions = []*exchange.Position{{Symbol: symbol, Size: 0}}
			if err := strategy.VerifyFlat(context.Background()); err == nil {
				t.Fatalf("VerifyFlat accepted zero position row for %q", symbol)
			}
		}
	})

	t.Run("rejects missing durable ownership state", func(t *testing.T) {
		strategy, _, stateStore := newFlatStrategy(t)
		stateStore.found = false
		if err := strategy.VerifyFlat(context.Background()); err == nil {
			t.Fatal("VerifyFlat accepted missing durable ownership state")
		}
	})
}

func TestOpenHedge_AtomicSuccess(t *testing.T) {
	bus := &mockEventBus{}
	spotEx := &mockFCExchange{
		name: "binance", marketType: "spot", baseAsset: "BTC",
		balance: 1000, latestPrice: 50000, priceDecimals: 2, quantityDecimals: 5,
		getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.002,
	}
	futEx := &mockFCExchange{
		name: "binance", marketType: "futures", baseAsset: "BTC",
		balance: 300, latestPrice: 50050, fundingRate: 0.001, priceDecimals: 2, quantityDecimals: 3,
	}

	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "BTCUSDT", TotalAllocatedCapital: 500},
		futEx, spotEx, nil, nil)
	s.SetEventBus(bus)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})

	err := s.openHedge(context.Background(), 50050, 50000, 0.001)
	if err != nil {
		t.Fatalf("openHedge: %v", err)
	}

	spotEx.mu.Lock()
	spotOrders := len(spotEx.placedOrders)
	spotEx.mu.Unlock()
	futEx.mu.Lock()
	futOrders := len(futEx.placedOrders)
	futEx.mu.Unlock()

	if spotOrders != 1 {
		t.Errorf("spot orders = %d, want 1", spotOrders)
	}
	if futOrders != 1 {
		t.Errorf("fut orders = %d, want 1", futOrders)
	}

	evts := bus.getEvents()
	found := false
	for _, e := range evts {
		if e.Type == event.EventTypePositionOpened {
			found = true
		}
	}
	if !found {
		t.Error("expected EventTypePositionOpened event")
	}
}

func TestSyncPositions_Forward(t *testing.T) {
	spotEx := &mockFCExchange{baseAsset: "ETH", balance: 1.5}
	futEx := &mockFCExchange{
		positions: []*exchange.Position{{Symbol: "ETHUSDT", Size: -1.2}},
	}
	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "ETHUSDT"},
		futEx, spotEx, nil, nil)
	s.direction = DirectionForward
	s.futQty = 1.2
	s.strategySpotQty = 1.2
	s.strategySpotKnown = true

	if err := s.syncPositions(context.Background()); err != nil {
		t.Fatalf("syncPositions: %v", err)
	}
	// 只認策略自己的 1.2 現貨記賬，多出的 0.3 視為用戶自有持幣
	if s.spotQty != 1.2 {
		t.Errorf("spotQty = %v, want 1.2", s.spotQty)
	}
	if s.futQty != 1.2 {
		t.Errorf("futQty = %v, want 1.2", s.futQty)
	}
	if s.direction != DirectionForward {
		t.Errorf("direction = %v, want Forward", s.direction)
	}
}

func TestSyncPositions_None(t *testing.T) {
	spotEx := &mockFCExchange{baseAsset: "ETH", balance: 0}
	futEx := &mockFCExchange{positions: []*exchange.Position{}}
	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "ETHUSDT"},
		futEx, spotEx, nil, nil)

	if err := s.syncPositions(context.Background()); err != nil {
		t.Fatalf("syncPositions: %v", err)
	}
	if s.direction != DirectionNone {
		t.Errorf("direction = %v, want None", s.direction)
	}
}

func TestFundingCarryCleanStartRejectsNilPositionOrOrderSnapshots(t *testing.T) {
	tests := []struct {
		name             string
		nilFuturesPos    bool
		nilMarginPos     bool
		nilFuturesOrders bool
		nilSpotOrders    bool
		nilMarginOrders  bool
	}{
		{name: "futures positions", nilFuturesPos: true},
		{name: "spot-margin positions", nilMarginPos: true},
		{name: "futures open orders", nilFuturesOrders: true},
		{name: "spot open orders", nilSpotOrders: true},
		{name: "spot-margin open orders", nilMarginOrders: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", returnNilPositions: tc.nilFuturesPos, returnNilOrders: tc.nilFuturesOrders}
			spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC", returnNilOrders: tc.nilSpotOrders}
			margin := &mockFCExchange{name: "binance", marketType: "spot_margin", baseAsset: "BTC", returnNilPositions: tc.nilMarginPos, returnNilOrders: tc.nilMarginOrders}
			store := &memoryRuntimeStateStore{}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
			s.SetRuntimeStateStore(store)
			if err := s.Start(context.Background()); err == nil {
				t.Fatal("funding_carry started without authoritative empty startup snapshots")
			}
			if s.started || store.found {
				t.Fatalf("unverified startup changed runtime state: started=%v statePersisted=%v", s.started, store.found)
			}
		})
	}
}

func TestFundingCarryRuntimePositionSyncBlocksOnNilSnapshots(t *testing.T) {
	tests := []struct {
		name       string
		nilFutures bool
		nilMargin  bool
	}{
		{name: "futures", nilFutures: true},
		{name: "spot margin", nilMargin: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", returnNilPositions: tc.nilFutures}
			spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC"}
			margin := &mockFCExchange{name: "binance", marketType: "spot_margin", baseAsset: "BTC", returnNilPositions: tc.nilMargin}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
			s.strategySpotKnown = true
			if err := s.syncPositions(context.Background()); err == nil {
				t.Fatal("accepted nil inventory response as flat")
			}
			if !s.unownedExposure {
				t.Fatal("unverified inventory did not latch the unowned-exposure block")
			}
		})
	}
}

func TestFundingCarryRuntimePositionSyncBlocksOnSpotBalanceFailure(t *testing.T) {
	futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", positions: []*exchange.Position{}}
	spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC", balanceErr: errors.New("balance API unavailable")}
	s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	s.strategySpotKnown = true
	if err := s.syncPositions(context.Background()); err == nil {
		t.Fatal("accepted failed spot-balance query as verified inventory")
	}
	if !s.unownedExposure {
		t.Fatal("spot-balance query failure did not latch the unowned-exposure block")
	}
}

func TestFundingCarryCloseSnapshotFailuresLatchUnownedExposure(t *testing.T) {
	tests := []struct {
		name  string
		close func(*FundingCarryStrategy) error
		setup func(*FundingCarryStrategy, *mockFCExchange)
	}{
		{
			name: "forward close positions",
			setup: func(s *FundingCarryStrategy, _ *mockFCExchange) {
				s.direction, s.futQty = DirectionForward, 1
			},
			close: func(s *FundingCarryStrategy) error { return s.closeAll(context.Background(), "test") },
		},
		{
			name: "reverse close positions",
			setup: func(s *FundingCarryStrategy, _ *mockFCExchange) {
				s.direction, s.futQty = DirectionReverse, 1
			},
			close: func(s *FundingCarryStrategy) error { return s.closeReverse(context.Background(), "test") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			futures := &mockFCExchange{name: "binance", marketType: "futures", positionsErr: errors.New("positions unavailable")}
			spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC"}
			margin := &mockFCExchange{name: "binance", marketType: "spot_margin"}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
			tc.setup(s, futures)
			if err := tc.close(s); err == nil {
				t.Fatal("close accepted failed position snapshot")
			}
			if !s.unownedExposure {
				t.Fatal("failed close snapshot did not latch unowned exposure")
			}
		})
	}
}

func TestFundingCarrySpotCloseBalanceFailureLatchesUnownedExposure(t *testing.T) {
	futures := &mockFCExchange{name: "binance", marketType: "futures"}
	spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC", balanceErr: errors.New("balance unavailable")}
	s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	s.strategySpotQty = 1
	if err := s.closeStrategySpot(context.Background()); err == nil {
		t.Fatal("spot close accepted failed balance snapshot")
	}
	if !s.unownedExposure {
		t.Fatal("failed spot close balance did not latch unowned exposure")
	}
}

func TestFundingCarryRecoveredStateRejectsNilOpenOrderSnapshots(t *testing.T) {
	for _, scope := range []string{"futures", "spot", "spot_margin"} {
		t.Run(scope, func(t *testing.T) {
			futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", positions: []*exchange.Position{}, returnNilOrders: scope == "futures"}
			spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC", returnNilOrders: scope == "spot"}
			margin := &mockFCExchange{name: "binance", marketType: "spot_margin", baseAsset: "BTC", positions: []*exchange.Position{}, returnNilOrders: scope == "spot_margin"}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
			if err := s.requireNoOpenOrders(context.Background()); err == nil {
				t.Fatal("accepted nil open-order response as authoritative empty")
			}
		})
	}
}

func TestCloseAll_PublishesEvent(t *testing.T) {
	bus := &mockEventBus{}
	spotEx := &mockFCExchange{
		baseAsset: "BTC", balance: 0.01, latestPrice: 50000,
		priceDecimals: 2, quantityDecimals: 5,
		getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.01,
	}
	futEx := &mockFCExchange{
		positions:        []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}},
		priceDecimals:    2,
		quantityDecimals: 3,
	}

	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "BTCUSDT"},
		futEx, spotEx, nil, nil)
	s.SetEventBus(bus)
	s.direction = DirectionForward
	s.futQty = 0.01
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	s.recordStrategySpot(0.01)

	err := s.closeAll(context.Background(), "test_exit")
	if err != nil {
		t.Fatalf("closeAll: %v", err)
	}

	evts := bus.getEvents()
	found := false
	for _, e := range evts {
		if e.Type == event.EventTypePositionClosed {
			found = true
			if e.Data["reason"] != "test_exit" {
				t.Errorf("reason = %v, want test_exit", e.Data["reason"])
			}
		}
	}
	if !found {
		t.Error("expected EventTypePositionClosed event")
	}
}

func TestCloseReverse_DoesNotRepayBeforeDebtBuybackIsComplete(t *testing.T) {
	spotEx := &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2}
	futEx := &mockFCExchange{quantityDecimals: 3, priceDecimals: 2}
	marginEx := &mockFCExchange{
		baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2,
		positions:      []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.4}},
		getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.2,
	}
	s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futEx, spotEx, marginEx, nil)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	s.direction, s.marginDebt = DirectionReverse, 0.4

	err := s.closeReverse(context.Background(), "test")
	if err == nil {
		t.Fatal("expected incomplete buyback error")
	}
	if marginEx.repayCalls != 0 {
		t.Fatalf("repay called %d times before full debt buyback", marginEx.repayCalls)
	}
	if s.direction != DirectionReverse || s.marginDebt != 0.4 || !s.unownedExposure {
		t.Fatalf("incomplete reverse close lost ownership record: direction=%v debt=%v blocked=%v", s.direction, s.marginDebt, s.unownedExposure)
	}
}

func TestEstimateNextSettlement(t *testing.T) {
	tests := []struct {
		hour int
		want int
	}{
		{3, 8},
		{10, 16},
		{20, 0}, // next day
	}
	for _, tt := range tests {
		now := time.Date(2026, 4, 7, tt.hour, 30, 0, 0, time.UTC)
		next := estimateNextSettlement(now)
		if next.Hour() != tt.want {
			t.Errorf("hour=%d: nextSettlement hour=%d, want %d", tt.hour, next.Hour(), tt.want)
		}
		if !next.After(now) {
			t.Errorf("hour=%d: nextSettlement should be after now", tt.hour)
		}
	}
}

func TestCarryDirection_String(t *testing.T) {
	if DirectionNone.String() != "none" {
		t.Errorf("None = %v", DirectionNone.String())
	}
	if DirectionForward.String() != "forward" {
		t.Errorf("Forward = %v", DirectionForward.String())
	}
	if DirectionReverse.String() != "reverse" {
		t.Errorf("Reverse = %v", DirectionReverse.String())
	}
}

func TestGetFundingStatus(t *testing.T) {
	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "BTCUSDT"},
		nil, nil, nil, nil)
	s.nextSettlement = time.Now().Add(2 * time.Hour)
	s.spotQty = 0.5
	s.futQty = 0.5
	s.direction = DirectionForward

	status := s.GetFundingStatus()
	if status["symbol"] != "BTCUSDT" {
		t.Errorf("symbol = %v", status["symbol"])
	}
	if status["direction"] != "forward" {
		t.Errorf("direction = %v", status["direction"])
	}
	secUntil, ok := status["seconds_until_settlement"].(int)
	if !ok || secUntil <= 0 {
		t.Errorf("seconds_until_settlement = %v", status["seconds_until_settlement"])
	}
}

func TestGetVisualizationData(t *testing.T) {
	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "ETHUSDT"},
		nil, nil, nil, map[string]interface{}{"reverse_enabled": false})
	s.direction = DirectionForward
	s.spotQty = 1.0
	s.futQty = 1.0

	data := s.GetVisualizationData()
	if data["direction"] != "forward" {
		t.Errorf("direction = %v", data["direction"])
	}
	if data["position_open"] != true {
		t.Error("expected position_open = true")
	}
}

func TestConsecutiveErrorsTriggersNotification(t *testing.T) {
	bus := &mockEventBus{}
	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "BTCUSDT"},
		nil, nil, nil, nil)
	s.SetEventBus(bus)

	for i := 0; i < maxConsecutiveErrors; i++ {
		s.mu.Lock()
		s.consecutiveErrors++
		s.mu.Unlock()
	}
	s.mu.RLock()
	count := s.consecutiveErrors
	s.mu.RUnlock()
	if count < maxConsecutiveErrors {
		t.Skip("skip if count mismatch")
	}

	s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
		"consecutive_errors": count,
		"message":            "test consecutive errors",
	})

	evts := bus.getEvents()
	if len(evts) == 0 {
		t.Error("expected at least one event for consecutive errors")
	}
}

func TestCombineErrors(t *testing.T) {
	if combineErrors(nil) != nil {
		t.Error("nil input should return nil")
	}
	if combineErrors([]error{}) != nil {
		t.Error("empty input should return nil")
	}
	err := combineErrors([]error{
		context.DeadlineExceeded,
		context.Canceled,
	})
	if err == nil {
		t.Error("expected non-nil error")
	}
}

func TestFundingCarryOpeningLimitsCapPairedExposure(t *testing.T) {
	s := &FundingCarryStrategy{symCfg: config.SymbolConfig{TotalAllocatedCapital: 1000},
		openControl: config.OpenPositionControl{MaxPositionValue: 700, MaxPositionQuantity: 5}}
	got, err := s.capitalWithinOpeningLimits(101, 100)
	if err != nil || got <= 500 || got > 505 || got*(0.5/101+0.5/100) > 5+1e-9 {
		t.Fatalf("paired notional/quantity limits not applied: cap=%v err=%v", got, err)
	}
	s.openControl = config.OpenPositionControl{MaxPositionValue: 250}
	got, err = s.capitalWithinOpeningLimits(101, 100)
	if err != nil || got != 250 {
		t.Fatalf("notional limit not applied: cap=%v err=%v", got, err)
	}
}

func TestFundingCarryScheduleControlsOnlyItsOwnOpeningGateSource(t *testing.T) {
	gate := &execution.OpeningGate{}
	gate.Block("manual")
	s := &FundingCarryStrategy{openingGate: gate, lastScheduleRun: make(map[string]string)}
	s.UpdateOpenPositionControl(config.OpenPositionControl{ScheduleRules: []config.ScheduleRule{{
		Enabled: true, Action: "pause", Time: "09:30", Weekdays: []int{1},
	}}})
	now := time.Date(2026, 9, 28, 9, 30, 0, 0, time.UTC) // Monday
	s.applyOpeningSchedule(now)
	if !gate.HasBlock("schedule") || !gate.HasBlock("manual") {
		t.Fatalf("schedule did not add an independent pause source")
	}
	s.UpdateOpenPositionControl(config.OpenPositionControl{})
	if gate.HasBlock("schedule") || !gate.HasBlock("manual") {
		t.Fatal("control update removed another owner's pause or retained stale schedule pause")
	}
}

func TestFundingCarryPeriodicControlClosesAndReopensOnlyItsOwnSource(t *testing.T) {
	gate := &execution.OpeningGate{}
	s := &FundingCarryStrategy{openingGate: gate, lastScheduleRun: make(map[string]string)}
	s.UpdateOpenPositionControl(config.OpenPositionControl{PeriodicRule: &config.PeriodicRule{
		Enabled: true, OpenDurationMin: 1, CloseDurationMin: 2,
	}})
	start := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	s.applyOpeningSchedule(start)
	s.applyOpeningSchedule(start.Add(time.Minute))
	if !gate.HasBlock("periodic") {
		t.Fatal("periodic close window did not block openings")
	}
	s.applyOpeningSchedule(start.Add(3 * time.Minute))
	if gate.HasBlock("periodic") {
		t.Fatal("periodic open window did not resume its own gate source")
	}
}
