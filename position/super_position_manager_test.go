package position

import (
	"context"
	"testing"
	"quantmesh/config"
)

// MockExecutor 模拟订单执行器
type MockExecutor struct {
	PlacedOrders []*OrderRequest
}

func (m *MockExecutor) PlaceOrder(req *OrderRequest) (*Order, error) {
	m.PlacedOrders = append(m.PlacedOrders, req)
	return &Order{
		OrderID:       12345,
		ClientOrderID: req.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatusPlaced,
	}, nil
}

func (m *MockExecutor) BatchPlaceOrders(orders []*OrderRequest) ([]*Order, bool) {
	var results []*Order
	for _, req := range orders {
		order, _ := m.PlaceOrder(req)
		results = append(results, order)
	}
	return results, false
}

func (m *MockExecutor) BatchPlaceOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult {
	placed, hasError := m.BatchPlaceOrders(orders)
	return &BatchPlaceOrdersResult{
		PlacedOrders:   placed,
		HasMarginError: hasError,
	}
}

func (m *MockExecutor) BatchCancelOrders(orderIDs []int64) error {
	return nil
}

// MockExchange 模拟交易所
type MockExchange struct{}

func (m *MockExchange) GetName() string { return "mock" }
func (m *MockExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (m *MockExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (m *MockExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}
func (m *MockExchange) GetBaseAsset() string { return "BTC" }
func (m *MockExchange) CancelAllOrders(ctx context.Context, symbol string) error { return nil }

func TestSuperPositionManager_Initialize(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.BuyWindowSize = 5
	cfg.Trading.OrderQuantity = 100.0

	executor := &MockExecutor{}
	ex := &MockExchange{}

	// 价格精度2，数量精度3
	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)

	initialPrice := 50000.0
	err := spm.Initialize(initialPrice, "50000.00")
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	// 验证锚点价格
	if spm.anchorPrice != initialPrice {
		t.Errorf("锚点价格错误: 期望 %.2f, 得到 %.2f", initialPrice, spm.anchorPrice)
	}

	// 验证初始化是否成功
	if !spm.isInitialized.Load() {
		t.Error("初始化标志未设置")
	}

	// 验证槽位数量（BuyWindowSize = 5，初始化会创建5个买单槽位）
	count := 0
	spm.slots.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	if count != 5 {
		t.Errorf("槽位数量错误: 期望 5, 得到 %d", count)
	}
}

func TestSuperPositionManager_OnOrderUpdate(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.OrderQuantity = 100.0

	executor := &MockExecutor{}
	ex := &MockExchange{}

	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)
	spm.Initialize(50000.0, "50000.00")

	// 模拟价格变化触发下单
	spm.AdjustOrders(49950.0)

	// 获取一个已下单的槽位
	var testSlot *InventorySlot
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		if slot.SlotStatus == SlotStatusLocked {
			testSlot = slot
			return false
		}
		return true
	})

	if testSlot == nil {
		t.Fatal("未找到已锁定的槽位")
	}

	// 模拟订单成交
	update := OrderUpdate{
		OrderID:       testSlot.OrderID,
		ClientOrderID: testSlot.ClientOID,
		Symbol:        "BTCUSDT",
		Status:        OrderStatusFilled,
		ExecutedQty:   testSlot.OrderFilledQty,
		Price:         testSlot.OrderPrice,
		Side:          testSlot.OrderSide,
	}

	spm.OnOrderUpdate(update)

	// 验证槽位状态转为有持仓
	testSlot.mu.RLock()
	defer testSlot.mu.RUnlock()
	if testSlot.PositionStatus != PositionStatusFilled {
		t.Errorf("槽位持仓状态错误: 期望 FILLED, 得到 %s", testSlot.PositionStatus)
	}
}

