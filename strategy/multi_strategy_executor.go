package strategy

import (
	"fmt"
	"sync"

	"quantmesh/order"
	"quantmesh/position"
)

// MultiStrategyExecutor 多策略订单执行器
type MultiStrategyExecutor struct {
	executor   *order.ExchangeOrderExecutor
	allocator  *CapitalAllocator
	strategies map[string]string // orderID -> strategyName
	mu         sync.RWMutex
}

// NewMultiStrategyExecutor 创建多策略订单执行器
func NewMultiStrategyExecutor(
	executor *order.ExchangeOrderExecutor,
	allocator *CapitalAllocator,
) *MultiStrategyExecutor {
	return &MultiStrategyExecutor{
		executor:   executor,
		allocator:  allocator,
		strategies: make(map[string]string),
		mu:         sync.RWMutex{},
	}
}

// PlaceOrder 下单（带策略标记）
func (mse *MultiStrategyExecutor) PlaceOrder(strategyName string, req *position.OrderRequest) (*position.Order, error) {
	// 计算订单金额
	orderAmount := req.Quantity * req.Price

	// 检查策略资金是否充足
	if !mse.allocator.CheckAvailable(strategyName, orderAmount) {
		return nil, fmt.Errorf("策略 %s 资金不足: 需要 %.2f, 可用 %.2f",
			strategyName, orderAmount, mse.allocator.GetAvailable(strategyName))
	}

	// 预留资金
	if !mse.allocator.Reserve(strategyName, orderAmount) {
		return nil, fmt.Errorf("策略 %s 资金预留失败", strategyName)
	}

	// 执行订单
	orderReq := &order.OrderRequest{
		Symbol:        req.Symbol,
		Side:          req.Side,
		Price:         req.Price,
		Quantity:      req.Quantity,
		PriceDecimals: req.PriceDecimals,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		ClientOrderID: req.ClientOrderID,
	}

	ord, err := mse.executor.PlaceOrder(orderReq)
	if err != nil {
		// 下单失败，释放资金
		mse.allocator.Release(strategyName, orderAmount)
		return nil, fmt.Errorf("下单失败: %w", err)
	}

	// 标记订单所属策略
	mse.mu.Lock()
	mse.strategies[fmt.Sprintf("%d", ord.OrderID)] = strategyName
	mse.mu.Unlock()

	// 转换为 position.Order
	return &position.Order{
		OrderID:       ord.OrderID,
		ClientOrderID: ord.ClientOrderID,
		Symbol:        ord.Symbol,
		Side:          ord.Side,
		Price:         ord.Price,
		Quantity:      ord.Quantity,
		Status:        ord.Status,
		CreatedAt:     ord.CreatedAt,
	}, nil
}

// BatchPlaceOrders 批量下单
func (mse *MultiStrategyExecutor) BatchPlaceOrders(strategyName string, orders []*position.OrderRequest) ([]*position.Order, bool) {
	result := mse.BatchPlaceOrdersWithDetails(strategyName, orders)
	return result.PlacedOrders, result.HasMarginError
}

// BatchPlaceOrdersWithDetails 批量下单（返回详细结果）
func (mse *MultiStrategyExecutor) BatchPlaceOrdersWithDetails(strategyName string, orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	result := &position.BatchPlaceOrdersResult{
		PlacedOrders:     make([]*position.Order, 0),
		HasMarginError:   false,
		ReduceOnlyErrors: make(map[string]bool),
	}

	// 转换为 order.OrderRequest
	orderReqs := make([]*order.OrderRequest, 0, len(orders))
	orderAmounts := make(map[string]float64) // ClientOrderID -> orderAmount

	for _, req := range orders {
		orderAmount := req.Quantity * req.Price

		// 检查资金
		if !mse.allocator.CheckAvailable(strategyName, orderAmount) {
			continue
		}

		// 预留资金
		if !mse.allocator.Reserve(strategyName, orderAmount) {
			continue
		}

		orderReq := &order.OrderRequest{
			Symbol:        req.Symbol,
			Side:          req.Side,
			Price:         req.Price,
			Quantity:      req.Quantity,
			PriceDecimals: req.PriceDecimals,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,
			ClientOrderID: req.ClientOrderID,
		}
		orderReqs = append(orderReqs, orderReq)
		orderAmounts[req.ClientOrderID] = orderAmount
	}

	// 批量下单
	batchResult := mse.executor.BatchPlaceOrdersWithDetails(orderReqs)
	result.HasMarginError = batchResult.HasMarginError
	result.ReduceOnlyErrors = batchResult.ReduceOnlyErrors

	// 处理成功的订单
	for _, ord := range batchResult.PlacedOrders {
		// 标记订单
		mse.mu.Lock()
		mse.strategies[fmt.Sprintf("%d", ord.OrderID)] = strategyName
		mse.mu.Unlock()

		result.PlacedOrders = append(result.PlacedOrders, &position.Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID,
			Symbol:        ord.Symbol,
			Side:          ord.Side,
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			Status:        ord.Status,
			CreatedAt:     ord.CreatedAt,
		})
	}

	// 释放失败订单的资金
	placedClientOIDs := make(map[string]bool)
	for _, ord := range batchResult.PlacedOrders {
		placedClientOIDs[ord.ClientOrderID] = true
	}
	for clientOID, amount := range orderAmounts {
		if !placedClientOIDs[clientOID] {
			mse.allocator.Release(strategyName, amount)
		}
	}

	return result
}

// BatchCancelOrders 批量撤单
func (mse *MultiStrategyExecutor) BatchCancelOrders(orderIDs []int64) error {
	// 获取订单ID对应的策略，释放资金
	// TODO: 需要知道订单金额才能释放资金
	// 实际释放应该在订单更新时处理（订单取消时）
	mse.mu.RLock()
	_ = mse.strategies // 暂时保留，后续实现资金释放
	mse.mu.RUnlock()

	return mse.executor.BatchCancelOrders(orderIDs)
}

// ReleaseOrderCapital 释放订单资金（订单成交或取消时调用）
func (mse *MultiStrategyExecutor) ReleaseOrderCapital(strategyName string, amount float64) {
	mse.allocator.Release(strategyName, amount)
}

// GetStrategyByOrderID 根据订单ID获取策略名称
func (mse *MultiStrategyExecutor) GetStrategyByOrderID(orderID int64) string {
	mse.mu.RLock()
	defer mse.mu.RUnlock()
	return mse.strategies[fmt.Sprintf("%d", orderID)]
}

