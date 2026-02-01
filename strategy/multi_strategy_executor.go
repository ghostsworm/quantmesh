package strategy

import (
	"fmt"
	"strings"
	"sync"

	"quantmesh/order"
	"quantmesh/position"
)

// MultiStrategyExecutor 多策略订單執行器
type MultiStrategyExecutor struct {
	executor            *order.ExchangeOrderExecutor
	allocator           *CapitalAllocator
	strategies          map[string]string  // orderID -> strategyName
	orderReservedAmount map[int64]float64   // orderID -> 下單時預留的金額（僅開倉買單）
	mu                  sync.RWMutex
}

// NewMultiStrategyExecutor 創建多策略订單執行器
func NewMultiStrategyExecutor(
	executor *order.ExchangeOrderExecutor,
	allocator *CapitalAllocator,
) *MultiStrategyExecutor {
	return &MultiStrategyExecutor{
		executor:            executor,
		allocator:           allocator,
		strategies:          make(map[string]string),
		orderReservedAmount: make(map[int64]float64),
		mu:                  sync.RWMutex{},
	}
}

// extractStrategyType 從策略名称中提取策略類型
// 策略名称格式可能是: "Grid-BTCUSDT-1", "DCA-ETHUSDT", "Martingale-BTCUSDT", "combo" 等
func extractStrategyType(strategyName string) string {
	if strategyName == "" {
		return ""
	}

	// 轉换為小写以便匹配
	nameLower := strings.ToLower(strategyName)

	// 检查常见的策略類型前缀
	if strings.HasPrefix(nameLower, "grid") {
		return "grid"
	}
	if strings.HasPrefix(nameLower, "dca") {
		return "dca"
	}
	if strings.HasPrefix(nameLower, "martingale") {
		return "martingale"
	}
	if strings.HasPrefix(nameLower, "trend") {
		return "trend"
	}
	if strings.HasPrefix(nameLower, "mean") || strings.HasPrefix(nameLower, "mean_reversion") {
		return "mean_reversion"
	}
	if strings.HasPrefix(nameLower, "combo") {
		return "combo"
	}
	if strings.HasPrefix(nameLower, "momentum") {
		return "momentum"
	}

	// 如果無法识别，返回空字符串
	return ""
}

// PlaceOrder 下單（带策略標記）
func (mse *MultiStrategyExecutor) PlaceOrder(strategyName string, req *position.OrderRequest) (*position.Order, error) {
	// 🔥 判斷是否為平倉/減倉操作
	// ReduceOnly=true 或 賣出操作（平多倉）不需要檢查和預留資金
	isReducePosition := req.ReduceOnly || strings.ToUpper(req.Side) == "SELL"

	var estimatedAmount float64

	if !isReducePosition {
		// 🔥 使用交易所的 EstimateFinalOrderAmount 預估最终下單金額
		// 交易所可能因最小名义金額（如币安 100 USDT）、精度對齐等原因調整數量
		// 必須用預估的最终金額做 Reserve，否则會出現"預留 90 實際下 180"的穿透額度问题
		estimatedAmount = mse.executor.EstimateFinalOrderAmount(req.Symbol, req.Price, req.Quantity, req.ReduceOnly)
		if estimatedAmount <= 0 {
			return nil, fmt.Errorf("策略 %s 預估订單金額為 0 (價格: %.2f, 數量: %.8f)", strategyName, req.Price, req.Quantity)
		}

		// 检查策略资金是否充足
		if !mse.allocator.CheckAvailable(strategyName, estimatedAmount) {
			return nil, fmt.Errorf("策略 %s 资金不足: 需要 %.2f, 可用 %.2f",
				strategyName, estimatedAmount, mse.allocator.GetAvailable(strategyName))
		}

		// 預留资金（使用預估的最终金額）
		if !mse.allocator.Reserve(strategyName, estimatedAmount) {
			return nil, fmt.Errorf("策略 %s 资金預留失败", strategyName)
		}
	}

	// 執行订單
	orderReq := &order.OrderRequest{
		Symbol:        req.Symbol,
		Side:          req.Side,
		Price:         req.Price,
		Quantity:      req.Quantity, // 交易所會自动調整數量
		PriceDecimals: req.PriceDecimals,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		ClientOrderID: req.ClientOrderID,
		StrategyName:  strategyName,
		StrategyType:  extractStrategyType(strategyName),
	}

	ord, err := mse.executor.PlaceOrder(orderReq)
	if err != nil {
		// 下單失败，释放资金（僅對開倉操作）
		if !isReducePosition && estimatedAmount > 0 {
			mse.allocator.Release(strategyName, estimatedAmount)
		}
		return nil, fmt.Errorf("下單失败: %w", err)
	}

	// 標記订單所属策略，並記錄預留金額（訂單成交/取消時用於釋放資金）
	mse.mu.Lock()
	mse.strategies[fmt.Sprintf("%d", ord.OrderID)] = strategyName
	if !isReducePosition && estimatedAmount > 0 {
		mse.orderReservedAmount[ord.OrderID] = estimatedAmount
	}
	mse.mu.Unlock()

	// 轉换為 position.Order
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

// BatchPlaceOrders 批量下單
func (mse *MultiStrategyExecutor) BatchPlaceOrders(strategyName string, orders []*position.OrderRequest) ([]*position.Order, bool) {
	result := mse.BatchPlaceOrdersWithDetails(strategyName, orders)
	return result.PlacedOrders, result.HasMarginError
}

// BatchPlaceOrdersWithDetails 批量下單（回傳詳細結果）
func (mse *MultiStrategyExecutor) BatchPlaceOrdersWithDetails(strategyName string, orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	result := &position.BatchPlaceOrdersResult{
		PlacedOrders:     make([]*position.Order, 0),
		HasMarginError:   false,
		ReduceOnlyErrors: make(map[string]bool),
	}

	// 轉换為 order.OrderRequest
	orderReqs := make([]*order.OrderRequest, 0, len(orders))
	orderAmounts := make(map[string]float64) // ClientOrderID -> estimatedAmount

	for _, req := range orders {
		// 🔥 判斷是否為平倉/減倉操作
		// ReduceOnly=true 或 賣出操作（平多倉）不需要檢查和預留資金
		isReducePosition := req.ReduceOnly || strings.ToUpper(req.Side) == "SELL"

		var estimatedAmount float64

		if !isReducePosition {
			// 🔥 使用交易所的 EstimateFinalOrderAmount 預估最终下單金額
			estimatedAmount = mse.executor.EstimateFinalOrderAmount(req.Symbol, req.Price, req.Quantity, req.ReduceOnly)
			if estimatedAmount <= 0 {
				// 金額為 0，跳過此订單
				continue
			}

			// 检查资金
			if !mse.allocator.CheckAvailable(strategyName, estimatedAmount) {
				continue
			}

			// 預留资金（使用預估的最终金額）
			if !mse.allocator.Reserve(strategyName, estimatedAmount) {
				continue
			}
		}

		orderReq := &order.OrderRequest{
			Symbol:        req.Symbol,
			Side:          req.Side,
			Price:         req.Price,
			Quantity:      req.Quantity, // 交易所會自动調整數量
			PriceDecimals: req.PriceDecimals,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,
			ClientOrderID: req.ClientOrderID,
			StrategyName:  strategyName,
			StrategyType:  extractStrategyType(strategyName),
		}
		orderReqs = append(orderReqs, orderReq)

		// 僅記錄開倉操作的資金預留
		if !isReducePosition && estimatedAmount > 0 {
			orderAmounts[req.ClientOrderID] = estimatedAmount
		}
	}

	// 批量下單
	batchResult := mse.executor.BatchPlaceOrdersWithDetails(orderReqs)
	result.HasMarginError = batchResult.HasMarginError
	result.ReduceOnlyErrors = batchResult.ReduceOnlyErrors

	// 处理成功的订單
	for _, ord := range batchResult.PlacedOrders {
		// 標記订單
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

	// 释放失败订單的资金
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

// BatchCancelOrders 批量撤單
func (mse *MultiStrategyExecutor) BatchCancelOrders(orderIDs []int64) error {
	// 獲取訂單ID對应的策略，释放资金
	// TODO: 需要知道订單金額才能释放资金
	// 實際释放应該在订單更新時处理（订單取消時）
	mse.mu.RLock()
	_ = mse.strategies // 暂時保留，后续實現资金释放
	mse.mu.RUnlock()

	return mse.executor.BatchCancelOrders(orderIDs)
}

// ReleaseOrderCapital 释放订單资金（订單成交或取消時調用）
func (mse *MultiStrategyExecutor) ReleaseOrderCapital(strategyName string, amount float64) {
	mse.allocator.Release(strategyName, amount)
}

// ReleaseOrderCapitalByOrderID 根據訂單 ID 釋放當時預留的資金（訂單成交或取消時調用，避免 DCA 等策略「可用」只減不增）
func (mse *MultiStrategyExecutor) ReleaseOrderCapitalByOrderID(orderID int64) {
	mse.mu.Lock()
	strategyName, hasStrategy := mse.strategies[fmt.Sprintf("%d", orderID)]
	amount, hasAmount := mse.orderReservedAmount[orderID]
	if hasStrategy && hasAmount {
		delete(mse.strategies, fmt.Sprintf("%d", orderID))
		delete(mse.orderReservedAmount, orderID)
		mse.mu.Unlock()
		mse.allocator.Release(strategyName, amount)
		return
	}
	mse.mu.Unlock()
}

// GetStrategyByOrderID 根據订單ID獲取策略名称
func (mse *MultiStrategyExecutor) GetStrategyByOrderID(orderID int64) string {
	mse.mu.RLock()
	defer mse.mu.RUnlock()
	return mse.strategies[fmt.Sprintf("%d", orderID)]
}
