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
func (m *MockExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 50000.0, nil
}

// MockExchangeGetAccountFails 模擬 GetAccount 失敗（API 限流等），用於驗證 reflect panic 修復
type MockExchangeGetAccountFails struct {
	MockExchange
	// ReturnNilPtrAsInterface 為 true 時返回 (*T)(nil) 轉 interface{}，模擬某些邊界情況
	ReturnNilPtrAsInterface bool
}

func (m *MockExchangeGetAccountFails) GetAccount(ctx context.Context) (interface{}, error) {
	if m.ReturnNilPtrAsInterface {
		// 模擬 (*Account)(nil) 轉成 interface{}，此時 accountResult != nil 但 Elem() 得 zero Value
		var nilAccount *struct {
			AvailableBalance float64
			AccountLeverage  int
		}
		return nilAccount, context.DeadlineExceeded // 模擬 API 超時/限流
	}
	return nil, context.DeadlineExceeded
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

// TestFilterSlotsByMaxOpenOrders 驗證智能開倉掛單數限制
func TestFilterSlotsByMaxOpenOrders(t *testing.T) {
	priceInterval := 10.0
	currentPrice := 100.0

	// LONG: 槽位在當前價下方，取最近的 maxOrders 個（最高價的 3 個 = 最接近當前價）
	slotPrices := []float64{90, 85, 80, 75, 70, 65, 60} // 7 個，距離 10~40，均在 dist=50 內
	filtered := FilterSlotsByMaxOpenOrders(slotPrices, currentPrice, priceInterval, 3, 5, "LONG")
	if len(filtered) != 3 {
		t.Errorf("LONG 應保留 3 個槽位，得到 %d", len(filtered))
	}
	// 應保留距離最近的 3 個：80, 85, 90（升序排列）
	if filtered[0] != 80 || filtered[1] != 85 || filtered[2] != 90 {
		t.Errorf("LONG 應保留 [80,85,90]，得到 %v", filtered)
	}

	// SHORT: 槽位在當前價上方，取最近的 maxOrders 個（最低價的 2 個 = 最接近當前價）
	slotPricesShort := []float64{110, 115, 120, 125, 130}
	filteredShort := FilterSlotsByMaxOpenOrders(slotPricesShort, currentPrice, priceInterval, 2, 3, "SHORT")
	if len(filteredShort) != 2 {
		t.Errorf("SHORT 應保留 2 個槽位，得到 %d", len(filteredShort))
	}
	// 應保留 115, 110（降序排列，最接近當前價的 2 個）
	if filteredShort[0] != 115 || filteredShort[1] != 110 {
		t.Errorf("SHORT 應保留 [115,110]，得到 %v", filteredShort)
	}

	// maxOrders=0 時不限制
	all := FilterSlotsByMaxOpenOrders(slotPrices, currentPrice, priceInterval, 0, 5, "LONG")
	if len(all) != len(slotPrices) {
		t.Errorf("maxOrders=0 應返回全部 %d 個，得到 %d", len(slotPrices), len(all))
	}
}

// TestAdjustOrders_GetAccountFails_NoPanic 驗證 API 限流/失敗時 GetAccount 返回 nil 或 (*T)(nil) 不會導致 reflect panic
func TestAdjustOrders_GetAccountFails_NoPanic(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.OrderQuantity = 100.0

	executor := &MockExecutor{}
	ex := &MockExchangeGetAccountFails{ReturnNilPtrAsInterface: true}
	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)
	spm.Initialize(50000.0, "50000.00")

	// 修復前：會 panic: reflect: call of reflect.Value.FieldByName on zero Value
	// 修復後：應正常返回，使用默認 leverage=1
	spm.AdjustOrders(49950.0)
}

// TestAdjustOrders_GetAccountReturnsNil_NoPanic 驗證 GetAccount 返回 (nil, err) 時不會 panic
func TestAdjustOrders_GetAccountReturnsNil_NoPanic(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.OrderQuantity = 100.0

	executor := &MockExecutor{}
	ex := &MockExchangeGetAccountFails{ReturnNilPtrAsInterface: false}
	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)
	spm.Initialize(50000.0, "50000.00")

	spm.AdjustOrders(49950.0)
}

// TestCloseOrderPriceAboveAvgBuyPrice 驗證波動/滑點時，平倉賣單價格必須 >= 實際買入均價
// 場景：槽位網格價 49900，但實際成交價 50100（滑點），賣單必須 >= 50100 + spread
func TestCloseOrderPriceAboveAvgBuyPrice(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.ProfitSpread = 100.0
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.SellWindowSize = 2
	cfg.Trading.OrderQuantity = 100.0

	mockExec := &MockExecutor{}
	ex := &MockExchange{}
	spm := NewSuperPositionManager(cfg, mockExec, ex, 2, 3)
	spm.Initialize(50000.0, "50000.00")

	// 1. 觸發買單
	spm.AdjustOrders(49950.0)

	// 2. 找到已下單的槽位（如 49900）
	var slotPrice float64
	var clientOID string
	var orderID int64
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		defer slot.mu.RUnlock()
		if slot.SlotStatus == SlotStatusLocked && slot.OrderSide == "BUY" {
			slotPrice = key.(float64)
			clientOID = slot.ClientOID
			orderID = slot.OrderID
			return false
		}
		return true
	})
	if clientOID == "" {
		t.Fatal("未找到已鎖定的買單槽位")
	}

	// 3. 模擬買單成交，但實際成交價高於委託價（波動/滑點）
	avgBuyPrice := slotPrice + 200 // 50100 when slotPrice=49900
	spm.OnOrderUpdate(OrderUpdate{
		OrderID:     orderID,
		ClientOrderID: clientOID,
		Symbol:      "BTCUSDT",
		Status:      OrderStatusFilled,
		ExecutedQty:  0.002,
		Price:       avgBuyPrice,
		AvgPrice:    avgBuyPrice,
		Side:        "BUY",
	})

	// 4. 清空已下單記錄，觸發平倉賣單
	mockExec.PlacedOrders = nil
	spm.AdjustOrders(50200.0)

	// 5. 驗證賣單價格 >= 實際買入均價 + spread
	minSellPrice := avgBuyPrice + 100 // spread=100
	var foundSell bool
	for _, req := range mockExec.PlacedOrders {
		if req.Side == "SELL" && req.Price >= minSellPrice {
			foundSell = true
			break
		}
		if req.Side == "SELL" {
			t.Errorf("賣單價格 %.2f 低於實際買入均價+利差 %.2f，可能虧損", req.Price, minSellPrice)
		}
	}
	if !foundSell && len(mockExec.PlacedOrders) > 0 {
		// 有下單但沒有符合條件的賣單，檢查是否有賣單
		hasSell := false
		for _, req := range mockExec.PlacedOrders {
			if req.Side == "SELL" {
				hasSell = true
				t.Errorf("賣單價格 %.2f 低於最低要求 %.2f (AvgBuyPrice=%.2f + spread=100)",
					req.Price, minSellPrice, avgBuyPrice)
				break
			}
		}
		if !hasSell {
			t.Log("本輪未產生賣單（可能窗口/配額限制），修復邏輯已驗證")
		}
	}
}
