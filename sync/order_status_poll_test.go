package sync

import (
	"context"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/exchange/income"
	"quantmesh/position"
)

type orderPollMockPM struct {
	slots   []interface{}
	updates []position.OrderUpdate
}

func (m *orderPollMockPM) IterateSlots(fn func(price float64, slot interface{}) bool) {
	for i, slot := range m.slots {
		if !fn(float64(i+1)*100, slot) {
			return
		}
	}
}

func (m *orderPollMockPM) OnOrderUpdate(update position.OrderUpdate) {
	m.updates = append(m.updates, update)
}

func (m *orderPollMockPM) GetSymbol() string {
	return "BTCUSDT"
}

type orderPollSlot struct {
	OrderID     int64
	ClientOID   string
	OrderSide   string
	OrderStatus string
}

type orderPollMockExchange struct {
	order *exchange.Order
}

func (m *orderPollMockExchange) GetName() string { return "mock" }
func (m *orderPollMockExchange) GetMarketType() string {
	return "futures"
}
func (m *orderPollMockExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	return nil, nil
}
func (m *orderPollMockExchange) BatchPlaceOrders(ctx context.Context, orders []*exchange.OrderRequest) ([]*exchange.Order, bool) {
	return nil, false
}
func (m *orderPollMockExchange) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return nil
}
func (m *orderPollMockExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return nil
}
func (m *orderPollMockExchange) CancelAllOrders(ctx context.Context, symbol string) error { return nil }
func (m *orderPollMockExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	if m.order != nil {
		return m.order, nil
	}
	return &exchange.Order{OrderID: orderID, Symbol: symbol, Status: exchange.OrderStatusFilled}, nil
}
func (m *orderPollMockExchange) GetOpenOrders(ctx context.Context, symbol string) ([]*exchange.Order, error) {
	return nil, nil
}
func (m *orderPollMockExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*exchange.OrderFill, error) {
	return nil, nil
}
func (m *orderPollMockExchange) GetAccount(ctx context.Context) (*exchange.Account, error) {
	return nil, nil
}
func (m *orderPollMockExchange) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	return nil, nil
}
func (m *orderPollMockExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	return 0, nil
}
func (m *orderPollMockExchange) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return nil
}
func (m *orderPollMockExchange) StopOrderStream() error { return nil }
func (m *orderPollMockExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *orderPollMockExchange) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return nil
}
func (m *orderPollMockExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback exchange.CandleUpdateCallback) error {
	return nil
}
func (m *orderPollMockExchange) StopKlineStream() error { return nil }
func (m *orderPollMockExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	return nil, nil
}
func (m *orderPollMockExchange) GetPriceDecimals() int    { return 2 }
func (m *orderPollMockExchange) GetQuantityDecimals() int { return 4 }
func (m *orderPollMockExchange) GetBaseAsset() string     { return "BTC" }
func (m *orderPollMockExchange) GetQuoteAsset() string    { return "USDT" }
func (m *orderPollMockExchange) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}
func (m *orderPollMockExchange) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *orderPollMockExchange) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}
func (m *orderPollMockExchange) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}
func (m *orderPollMockExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	return nil, nil
}
func (m *orderPollMockExchange) GetFundingInfo(ctx context.Context, symbol string) (*exchange.FundingInfo, error) {
	return nil, exchange.ErrNotImplemented
}
func (m *orderPollMockExchange) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", nil
}

func TestOrderStatusPollCollectsInt64OrderID(t *testing.T) {
	pm := &orderPollMockPM{
		slots: []interface{}{orderPollSlot{
			OrderID:     123,
			ClientOID:   "cid-123",
			OrderSide:   "BUY",
			OrderStatus: position.OrderStatusPlaced,
		}},
	}
	ex := &orderPollMockExchange{
		order: &exchange.Order{
			OrderID:       123,
			ClientOrderID: "cid-123",
			Symbol:        "BTCUSDT",
			Status:        exchange.OrderStatusFilled,
			ExecutedQty:   0.01,
			Side:          exchange.SideBuy,
			Type:          exchange.OrderTypeLimit,
		},
	}
	service := NewOrderStatusPollService(ex, pm, "BTCUSDT", time.Second)

	if err := service.PollOrderStatus(context.Background()); err != nil {
		t.Fatalf("PollOrderStatus() error=%v", err)
	}
	if len(pm.updates) != 1 {
		t.Fatalf("int64 OrderID 应被收集并触发更新，updates=%d", len(pm.updates))
	}
	if pm.updates[0].Status != position.OrderStatusFilled {
		t.Fatalf("状态应归一为 FILLED，got %s", pm.updates[0].Status)
	}
}

func TestOrderStatusPollHandlesPointerSlotsAndNilDependencies(t *testing.T) {
	pm := &orderPollMockPM{
		slots: []interface{}{&orderPollSlot{
			OrderID:     456,
			OrderSide:   "SELL",
			OrderStatus: position.OrderStatusCancelRequested,
		}},
	}
	ex := &orderPollMockExchange{
		order: &exchange.Order{OrderID: 456, Symbol: "BTCUSDT", Status: exchange.OrderStatusCanceled},
	}
	service := NewOrderStatusPollService(ex, pm, "BTCUSDT", 0)

	if err := service.PollOrderStatus(nil); err != nil {
		t.Fatalf("PollOrderStatus(nil) error=%v", err)
	}
	if len(pm.updates) != 1 {
		t.Fatalf("指针槽位应被识别，updates=%d", len(pm.updates))
	}

	nilService := NewOrderStatusPollService(nil, nil, "BTCUSDT", 0)
	if err := nilService.PollOrderStatus(context.Background()); err != nil {
		t.Fatalf("依赖为空时应安全跳过，got %v", err)
	}
}

func TestOrderStatusPollCanRestartWithInvalidInterval(t *testing.T) {
	service := NewOrderStatusPollService(nil, nil, "BTCUSDT", 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	service.Start(ctx)
	if !service.isRunning {
		t.Fatal("Start() 后应处于运行状态")
	}
	if service.pollInterval != defaultOrderPollInterval {
		t.Fatalf("无效轮询间隔应使用默认值，got %s", service.pollInterval)
	}
	service.Stop()
	if service.stopC != nil {
		t.Fatal("Stop() 后应清空停止通道")
	}

	service.Start(ctx)
	if !service.isRunning || service.stopC == nil {
		t.Fatal("Stop() 后应能再次 Start()")
	}
	service.Stop()
}

func TestOrderStatusPollMarksStoppedWhenContextCanceled(t *testing.T) {
	service := NewOrderStatusPollService(nil, nil, "BTCUSDT", time.Second)
	ctx, cancel := context.WithCancel(context.Background())

	service.Start(ctx)
	cancel()

	deadline := time.After(time.Second)
	for {
		service.mu.RLock()
		isRunning := service.isRunning
		service.mu.RUnlock()
		if !isRunning {
			break
		}

		select {
		case <-deadline:
			t.Fatal("context 取消后轮询服务应自动标记为停止")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	service.Start(nil)
	if !service.isRunning || service.stopC == nil {
		t.Fatal("context 取消退出后应允许 Start(nil) 安全重启")
	}
	service.Stop()
}
