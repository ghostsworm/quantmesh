package strategy

import (
	"context"
	"quantmesh/config"
	"quantmesh/position"
	"testing"
)

// MockGridExecutor 模拟订单执行器
type MockGridExecutor struct {
	position.OrderExecutorInterface
}

func (m *MockGridExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	return &position.Order{
		OrderID:       12345,
		ClientOrderID: req.ClientOrderID,
		Status:        position.OrderStatusPlaced,
	}, nil
}

func (m *MockGridExecutor) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	var results []*position.Order
	for _, req := range orders {
		order, _ := m.PlaceOrder(req)
		results = append(results, order)
	}
	return results, false
}

func (m *MockGridExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	placed, hasError := m.BatchPlaceOrders(orders)
	return &position.BatchPlaceOrdersResult{
		PlacedOrders:   placed,
		HasMarginError: hasError,
	}
}

// MockGridExchange 模拟交易所
type MockGridExchange struct {
	position.IExchange
}

func (m *MockGridExchange) GetName() string      { return "mock" }
func (m *MockGridExchange) GetBaseAsset() string { return "BTC" }
func (m *MockGridExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (m *MockGridExchange) GetQuoteAsset() string { return "USDT" }

func TestGridStrategy_Delegation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.OrderQuantity = 30.0

	executor := &MockGridExecutor{}
	ex := &MockGridExchange{}

	// 创建 SuperPositionManager
	spm := position.NewSuperPositionManager(cfg, executor, ex, 2, 3)
	spm.Initialize(50000.0, "50000.00")

	// 创建 GridStrategy
	gs := NewGridStrategy("test_grid", cfg, executor, ex, spm)

	// 测试价格变化触发下单
	err := gs.OnPriceChange(49950.0)
	if err != nil {
		t.Fatalf("OnPriceChange failed: %v", err)
	}

	// 测试订单更新回调
	update := &position.OrderUpdate{
		OrderID: 12345,
		Status:  position.OrderStatusFilled,
	}
	err = gs.OnOrderUpdate(update)
	if err != nil {
		t.Fatalf("OnOrderUpdate failed: %v", err)
	}
}
