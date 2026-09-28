package strategy

import (
	"context"
	"quantmesh/position"
)

// MultiStrategyExecutorAdapter 适配器，將 MultiStrategyExecutor 轉换為 position.OrderExecutorInterface
type MultiStrategyExecutorAdapter struct {
	executor     *MultiStrategyExecutor
	strategyName string
}

func (a *MultiStrategyExecutorAdapter) IsOpeningPaused() bool {
	return a.executor.executor.IsOpeningPaused()
}

// NewMultiStrategyExecutorAdapter 創建适配器
func NewMultiStrategyExecutorAdapter(executor *MultiStrategyExecutor, strategyName string) *MultiStrategyExecutorAdapter {
	return &MultiStrategyExecutorAdapter{
		executor:     executor,
		strategyName: strategyName,
	}
}

// PlaceOrder 下單
func (a *MultiStrategyExecutorAdapter) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	return a.executor.PlaceOrder(a.strategyName, req)
}

func (a *MultiStrategyExecutorAdapter) PlaceOrderContext(ctx context.Context, req *position.OrderRequest) (*position.Order, error) {
	return a.executor.PlaceOrderContext(ctx, a.strategyName, req)
}

func (a *MultiStrategyExecutorAdapter) BatchPlaceOrdersWithDetailsContext(ctx context.Context, orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return a.executor.BatchPlaceOrdersWithDetailsContext(ctx, a.strategyName, orders)
}

// BatchPlaceOrders 批量下單
func (a *MultiStrategyExecutorAdapter) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	return a.executor.BatchPlaceOrders(a.strategyName, orders)
}

// BatchPlaceOrdersWithDetails 批量下單（回傳詳細結果）
func (a *MultiStrategyExecutorAdapter) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return a.executor.BatchPlaceOrdersWithDetails(a.strategyName, orders)
}

// BatchCancelOrders 批量撤單
func (a *MultiStrategyExecutorAdapter) BatchCancelOrders(orderIDs []int64) error {
	return a.executor.BatchCancelOrders(orderIDs)
}
