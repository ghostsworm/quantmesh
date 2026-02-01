package strategy

import (
	"quantmesh/position"
)

// MultiStrategyExecutorAdapter 适配器，将 MultiStrategyExecutor 转换为 position.OrderExecutorInterface
type MultiStrategyExecutorAdapter struct {
	executor     *MultiStrategyExecutor
	strategyName string
}

// NewMultiStrategyExecutorAdapter 创建适配器
func NewMultiStrategyExecutorAdapter(executor *MultiStrategyExecutor, strategyName string) *MultiStrategyExecutorAdapter {
	return &MultiStrategyExecutorAdapter{
		executor:     executor,
		strategyName: strategyName,
	}
}

// PlaceOrder 下单
func (a *MultiStrategyExecutorAdapter) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	return a.executor.PlaceOrder(a.strategyName, req)
}

// BatchPlaceOrders 批量下单
func (a *MultiStrategyExecutorAdapter) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	return a.executor.BatchPlaceOrders(a.strategyName, orders)
}

// BatchPlaceOrdersWithDetails 批量下单（返回详细结果）
func (a *MultiStrategyExecutorAdapter) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return a.executor.BatchPlaceOrdersWithDetails(a.strategyName, orders)
}

// BatchCancelOrders 批量撤单
func (a *MultiStrategyExecutorAdapter) BatchCancelOrders(orderIDs []int64) error {
	return a.executor.BatchCancelOrders(orderIDs)
}
