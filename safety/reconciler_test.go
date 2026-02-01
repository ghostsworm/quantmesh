package safety

import (
	"context"
	"quantmesh/config"
	"quantmesh/lock"
	"testing"
	"time"
)

// MockPositionManager 模拟仓位管理器
type MockPositionManager struct {
	Slots          map[float64]interface{}
	TotalBuyQty    float64
	TotalSellQty   float64
	ReconcileCount int64
	Symbol         string
	PriceInterval  float64
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

// TestSlot 用于对账反射
type TestSlot struct {
	PositionStatus string
	PositionQty    float64
	OrderSide      string
	OrderStatus    string
}

// MockReconcileExchange 专门用于对账测试的 Mock
type MockReconcileExchange struct{}

func (m *MockReconcileExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (m *MockReconcileExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (m *MockReconcileExchange) GetBaseAsset() string { return "BTC" }

func TestReconciler_Reconcile(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.ReconcileInterval = 30

	ex := &MockReconcileExchange{}

	pm := &MockPositionManager{
		Symbol:        "BTCUSDT",
		PriceInterval: 100.0,
		Slots:         make(map[float64]interface{}),
	}

	// 构造本地数据
	// 槽位 1: 已成交持仓，有卖单挂单
	pm.Slots[50000.0] = TestSlot{
		PositionStatus: "FILLED",
		PositionQty:    0.1,
		OrderSide:      "SELL",
		OrderStatus:    "PLACED",
	}
	// 槽位 2: 无持仓，有买单挂单
	pm.Slots[49900.0] = TestSlot{
		PositionStatus: "EMPTY",
		PositionQty:    0.0,
		OrderSide:      "BUY",
		OrderStatus:    "PLACED",
	}

	// 创建一个 mock 分布式锁
	mockLock := lock.NewNopLock() // 使用无操作锁用于测试
	r := NewReconciler(cfg, ex, pm, mockLock)

	// 模拟执行对账
	err := r.Reconcile()
	if err != nil {
		t.Fatalf("对账执行失败: %v", err)
	}

	// 验证对账次数增加
	if pm.ReconcileCount != 1 {
		t.Errorf("对账次数应为 1, 得到 %d", pm.ReconcileCount)
	}
}
