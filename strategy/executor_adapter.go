package strategy

import (
	"context"
	"fmt"
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

func (a *MultiStrategyExecutorAdapter) MarkOrderReconciliationRequired(orderID int64, clientOrderID, reason string) error {
	return a.executor.executor.MarkOrderReconciliationRequired(orderID, clientOrderID, reason)
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

// SettleRecoveredIntent is called only after this strategy durably reconciles
// its own recovered order. The physical executor revalidates the order and
// keeps the Bot recovery gate closed until owner-wide exposure bootstrap.
func (a *MultiStrategyExecutorAdapter) SettleRecoveredIntent(ctx context.Context, clientOrderID string) error {
	if a == nil || a.executor == nil || a.executor.executor == nil || a.strategyName == "" {
		return fmt.Errorf("recovered intent settlement adapter is unavailable")
	}
	owner, _, found := a.executor.executor.IntentStrategyType(clientOrderID)
	if !found || owner != a.strategyName {
		return fmt.Errorf("recovered intent strategy does not match adapter owner")
	}
	return a.executor.executor.SettleRecoveredIntent(ctx, clientOrderID, a.strategyName)
}

type recoveredIntentSettler interface {
	SettleRecoveredIntent(context.Context, string) error
}

// settleRecoveredIntentAfterAccounting releases only a terminal order whose
// owning strategy has already durably applied its exact venue fill evidence.
// The physical executor keeps the owner-wide recovery gate closed until the
// subsequent exposure bootstrap succeeds.
func settleRecoveredIntentAfterAccounting(ctx context.Context, executor position.OrderExecutorInterface, clientOrderID, status string) error {
	if !signalOrderStatusFilled(status) && !signalOrderStatusTerminal(status) || clientOrderID == "" {
		return nil
	}
	settler, ok := executor.(recoveredIntentSettler)
	if !ok {
		return nil
	}
	return settler.SettleRecoveredIntent(ctx, clientOrderID)
}

func (a *MultiStrategyExecutorAdapter) classifyComboOrder(req *position.OrderRequest) (bool, error) {
	if a == nil || a.executor == nil || req == nil {
		return false, fmt.Errorf("combo order classification evidence unavailable")
	}
	leg, opening := a.executor.classifyOrder(a.strategyName, req)
	if leg == "" {
		return false, fmt.Errorf("combo order side or position leg is invalid")
	}
	return opening, nil
}

func (a *MultiStrategyExecutorAdapter) estimateComboOrderNotional(req *position.OrderRequest) (float64, error) {
	if a == nil || a.executor == nil || a.executor.executor == nil || req == nil {
		return 0, fmt.Errorf("combo order notional estimator unavailable")
	}
	return a.executor.executor.EstimateFinalOrderAmount(req.Symbol, req.Price, req.Quantity, req.ReduceOnly), nil
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
