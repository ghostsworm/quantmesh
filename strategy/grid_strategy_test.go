package strategy

import (
	"context"
	"quantmesh/config"
	"quantmesh/position"
	"testing"
)

// MockGridExecutor 模拟订單執行器
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

// MockGridExchange 模拟交易所（必須實現 position.IExchange 全量方法；勿嵌入 nil 接口否則未實現方法會 panic）
type MockGridExchange struct{}

func (m *MockGridExchange) GetName() string      { return "mock" }
func (m *MockGridExchange) GetBaseAsset() string { return "BTC" }
func (m *MockGridExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (m *MockGridExchange) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return nil, nil
}
func (m *MockGridExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}
func (m *MockGridExchange) CancelAllOrders(ctx context.Context, symbol string) error { return nil }
func (m *MockGridExchange) GetAccount(ctx context.Context) (interface{}, error)       { return nil, nil }
func (m *MockGridExchange) GetPriceDecimals() int    { return 2 }
func (m *MockGridExchange) GetQuantityDecimals() int { return 3 }
func (m *MockGridExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*position.OrderBook, error) {
	return &position.OrderBook{}, nil
}
func (m *MockGridExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return nil, nil
}
func (m *MockGridExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 50000, nil
}

func TestGridStrategy_Delegation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100.0
	cfg.Trading.BuyWindowSize = 2
	cfg.Trading.OrderQuantity = 30.0

	executor := &MockGridExecutor{}
	ex := &MockGridExchange{}

	// 創建 SuperPositionManager
	spm := position.NewSuperPositionManager(cfg, executor, ex, 2, 3)
	spm.Initialize(50000.0, "50000.00")

	// 創建 GridStrategy
	gs := NewGridStrategy("test_grid", cfg, executor, ex, spm)

	// 测試價格變化触发下單
	err := gs.OnPriceChange(49950.0)
	if err != nil {
		t.Fatalf("OnPriceChange failed: %v", err)
	}

	// 测試订單更新回呼
	update := &position.OrderUpdate{
		OrderID: 12345,
		Status:  position.OrderStatusFilled,
	}
	err = gs.OnOrderUpdate(update)
	if err != nil {
		t.Fatalf("OnOrderUpdate failed: %v", err)
	}
}
