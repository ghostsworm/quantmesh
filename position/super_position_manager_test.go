package position

import (
	"context"
	"quantmesh/config"
	"testing"
	"time"
)

// MockExecutor 模拟订單執行器
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
func (m *MockExchange) GetAccount(ctx context.Context) (interface{}, error)       { return nil, nil }
func (m *MockExchange) GetPriceDecimals() int                                     { return 2 }
func (m *MockExchange) GetQuantityDecimals() int                                  { return 3 }
func (m *MockExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return nil, nil
}
func (m *MockExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}
func (m *MockExchange) GetBaseAsset() string { return "BTC" }
func (m *MockExchange) CancelAllOrders(ctx context.Context, symbol string) error {
	return nil
}

func TestSuperPositionManager_Initialize(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.BuyWindowSize = 5
	cfg.Trading.OrderQuantity = 100.0

	executor := &MockExecutor{}
	ex := &MockExchange{}

	// 價格精度2，數量精度3
	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)

	initialPrice := 50000.0
	err := spm.Initialize(initialPrice, "50000.00")
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	// 驗证锚点價格
	if spm.anchorPrice != initialPrice {
		t.Errorf("锚点價格錯误: 期望 %.2f, 得到 %.2f", initialPrice, spm.anchorPrice)
	}

	// 驗证初始化是否成功
	if !spm.isInitialized.Load() {
		t.Error("初始化標志未設置")
	}

	// 驗证槽位數量（BuyWindowSize = 5，初始化會創建5個買單槽位）
	count := 0
	spm.slots.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	if count != 5 {
		t.Errorf("槽位數量錯误: 期望 5, 得到 %d", count)
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

	// 模拟價格變化触发下單
	spm.AdjustOrders(49950.0)

	// 獲取一個已下單的槽位
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
		t.Fatal("未找到已鎖定的槽位")
	}

	// 模拟订單成交
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

	// 驗证槽位状態轉為有持倉
	testSlot.mu.RLock()
	defer testSlot.mu.RUnlock()
	if testSlot.PositionStatus != PositionStatusFilled {
		t.Errorf("槽位持倉状態錯误: 期望 FILLED, 得到 %s", testSlot.PositionStatus)
	}
}

func TestSuperPositionManager_ReduceOnlyCooldown(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.OrderQuantity = 100.0

	executor := &MockExecutor{}
	ex := &MockExchange{}
	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)
	spm.Initialize(50000.0, "50000.00")

	slotPrice := 49900.0
	// 初始不在冷却期
	if spm.isReduceOnlyCooldown(slotPrice) {
		t.Error("新槽位不應在冷却期")
	}
	// 記錄冷却
	spm.reduceOnlyCooldown.Store(slotPrice, time.Now())
	// 應在冷却期
	if !spm.isReduceOnlyCooldown(slotPrice) {
		t.Error("剛記錄的槽位應在冷却期")
	}
	// 記錄 3 分鐘前，應已過期
	spm.reduceOnlyCooldown.Store(slotPrice, time.Now().Add(-3*time.Minute))
	if spm.isReduceOnlyCooldown(slotPrice) {
		t.Error("3 分鐘前的冷却應已過期")
	}
}
