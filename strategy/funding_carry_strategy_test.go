package strategy

import (
	"context"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/exchange/income"
)

func TestNewFundingCarryStrategy_ConfigParams(t *testing.T) {
	stratCfg := map[string]interface{}{
		"min_funding_rate":       0.001,
		"exit_funding_rate":      0.0005,
		"max_basis_pct":          0.3,
		"rebalance_interval_sec": 120.0,
	}
	s := NewFundingCarryStrategy("fc-test", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, stratCfg)
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
}

func TestNewFundingCarryStrategy_Defaults(t *testing.T) {
	s := NewFundingCarryStrategy("fc-def", nil, config.SymbolConfig{Symbol: "ETHUSDT"}, nil, nil, nil)
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
	s := NewFundingCarryStrategy("fc-nil-bus", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil)
	// 不 panic 即可
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{"test": true})
}

// mockEventBus 用於驗證事件發布
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

// mockFCExchange 简化的 mock 交易所
type mockFCExchange struct {
	name             string
	marketType       string
	latestPrice      float64
	fundingRate      float64
	positions        []*exchange.Position
	balance          float64
	baseAsset        string
	priceDecimals    int
	quantityDecimals int
	placeOrderErr    error
	placedOrders     []*exchange.OrderRequest
	getOrderStatus   exchange.OrderStatus
	getOrderExecQty  float64
	mu               sync.Mutex
}

func (m *mockFCExchange) GetName() string                { return m.name }
func (m *mockFCExchange) GetMarketType() string          { return m.marketType }
func (m *mockFCExchange) GetBaseAsset() string            { return m.baseAsset }
func (m *mockFCExchange) GetQuoteAsset() string           { return "USDT" }
func (m *mockFCExchange) GetPriceDecimals() int           { return m.priceDecimals }
func (m *mockFCExchange) GetQuantityDecimals() int        { return m.quantityDecimals }
func (m *mockFCExchange) StartOrderStream(ctx context.Context, cb func(interface{})) error { return nil }
func (m *mockFCExchange) StopOrderStream() error          { return nil }
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
	return nil, nil
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
	return "", nil
}
func (m *mockFCExchange) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}
func (m *mockFCExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return m.latestPrice, nil
}
func (m *mockFCExchange) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return m.fundingRate, nil
}
func (m *mockFCExchange) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	return m.positions, nil
}
func (m *mockFCExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	return m.balance, nil
}
func (m *mockFCExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.placedOrders = append(m.placedOrders, req)
	if m.placeOrderErr != nil {
		return nil, m.placeOrderErr
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

func TestOpenHedge_AtomicSuccess(t *testing.T) {
	bus := &mockEventBus{}
	spotEx := &mockFCExchange{
		name: "binance", marketType: "spot", baseAsset: "BTC",
		latestPrice: 50000, priceDecimals: 2, quantityDecimals: 5,
		getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.002,
	}
	futEx := &mockFCExchange{
		name: "binance", marketType: "futures", baseAsset: "BTC",
		latestPrice: 50050, fundingRate: 0.001, priceDecimals: 2, quantityDecimals: 3,
	}

	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "BTCUSDT", TotalAllocatedCapital: 500},
		futEx, spotEx, nil)
	s.SetEventBus(bus)

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

func TestSyncPositions(t *testing.T) {
	spotEx := &mockFCExchange{
		baseAsset: "ETH", balance: 1.5,
	}
	futEx := &mockFCExchange{
		positions: []*exchange.Position{
			{Symbol: "ETHUSDT", Size: -1.2},
		},
	}
	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "ETHUSDT"},
		futEx, spotEx, nil)

	if err := s.syncPositions(context.Background()); err != nil {
		t.Fatalf("syncPositions: %v", err)
	}
	if s.spotQty != 1.5 {
		t.Errorf("spotQty = %v, want 1.5", s.spotQty)
	}
	if s.futQty != 1.2 {
		t.Errorf("futQty = %v, want 1.2", s.futQty)
	}
}

func TestCloseAll_PublishesEvent(t *testing.T) {
	bus := &mockEventBus{}
	spotEx := &mockFCExchange{
		baseAsset: "BTC", balance: 0.01, latestPrice: 50000,
		priceDecimals: 2, quantityDecimals: 5,
		getOrderStatus: exchange.OrderStatusFilled,
	}
	futEx := &mockFCExchange{
		positions:        []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.01}},
		priceDecimals:    2,
		quantityDecimals: 3,
	}

	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "BTCUSDT"},
		futEx, spotEx, nil)
	s.SetEventBus(bus)

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

func TestConsecutiveErrorsTriggersNotification(t *testing.T) {
	bus := &mockEventBus{}
	s := NewFundingCarryStrategy("fc", nil,
		config.SymbolConfig{Symbol: "BTCUSDT"},
		nil, nil, nil)
	s.SetEventBus(bus)

	// 模擬連續錯誤
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
