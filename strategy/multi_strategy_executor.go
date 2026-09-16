package strategy

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"quantmesh/config"
	"quantmesh/order"
	"quantmesh/position"
)

// 訂單回報狀態（與 position.OrderUpdate.Status 歸一化後的取值一致）
const (
	orderStatusPartiallyFilled = "PARTIALLY_FILLED"
	orderStatusFilled          = "FILLED"
	orderStatusCanceled        = "CANCELED"
	orderStatusExpired         = "EXPIRED"
	orderStatusRejected        = "REJECTED"
)

// capitalQtyEpsilon 數量比較容差
const capitalQtyEpsilon = 1e-12

// orderCapital 單筆策略訂單的資金記賬
type orderCapital struct {
	strategy      string
	leg           string  // position.PositionSideLong / PositionSideShort
	opening       bool    // 開倉單占用資金；平倉單成交時釋放持倉占用
	reserved      float64 // 下單時預留金額（僅開倉）
	converted     float64 // 已隨成交轉為持倉占用的金額
	quantity      float64 // 下單數量
	filledQty     float64 // 已處理的累計成交數量
	orderID       int64
	clientOrderID string
}

// legUsage 策略某條腿上已成交持倉占用的資金
type legUsage struct {
	amount float64
	qty    float64
}

// MultiStrategyExecutor 多策略订單執行器
//
// 資金生命週期（D5）：開倉單下單時 Reserve → 成交後轉為「持倉占用」（仍計入 used）
// → 平倉單成交時按比例 Release；開倉單撤單/拒單/過期時 Release 未成交部分。
type MultiStrategyExecutor struct {
	executor             *order.ExchangeOrderExecutor
	allocator            *CapitalAllocator
	strategies           map[string]string        // orderID -> strategyName
	clientStrategies     map[string]string        // clientOrderID -> strategyName
	ordersByClient       map[string]*orderCapital // clientOrderID -> 資金記賬
	ordersByID           map[int64]*orderCapital  // orderID -> 資金記賬
	positionUsage        map[string]*legUsage     // strategy|leg -> 持倉占用
	strategyPositionSide map[string]string        // strategyName -> 固定持倉腿（如 spot_short=SHORT）
	mu                   sync.RWMutex
}

// NewMultiStrategyExecutor 創建多策略订單執行器
func NewMultiStrategyExecutor(
	executor *order.ExchangeOrderExecutor,
	allocator *CapitalAllocator,
) *MultiStrategyExecutor {
	return &MultiStrategyExecutor{
		executor:             executor,
		allocator:            allocator,
		strategies:           make(map[string]string),
		clientStrategies:     make(map[string]string),
		ordersByClient:       make(map[string]*orderCapital),
		ordersByID:           make(map[int64]*orderCapital),
		positionUsage:        make(map[string]*legUsage),
		strategyPositionSide: make(map[string]string),
	}
}

// SetStrategyPositionSide 顯式聲明某策略只操作一條持倉腿（LONG/SHORT），
// 用於在未設置 OrderRequest.PositionSide 時判斷開/平倉，替代按策略名稱猜測（S9）。
func (mse *MultiStrategyExecutor) SetStrategyPositionSide(strategyName, positionSide string) error {
	side := strings.ToUpper(strings.TrimSpace(positionSide))
	if side != position.PositionSideLong && side != position.PositionSideShort {
		return fmt.Errorf("策略 %s 的持倉方向 %q 無效，應為 LONG 或 SHORT", strategyName, positionSide)
	}
	mse.mu.Lock()
	mse.strategyPositionSide[strategyName] = side
	mse.mu.Unlock()
	return nil
}

func (mse *MultiStrategyExecutor) bindOrderRouteLocked(orderID int64, clientOrderID, strategyName string) {
	if strategyName != "" {
		if orderID > 0 {
			mse.strategies[fmt.Sprintf("%d", orderID)] = strategyName
		}
		if clientOrderID != "" {
			mse.clientStrategies[clientOrderID] = strategyName
		}
	}
}

// trackOrderLocked 記錄訂單資金記賬（ClientOrderID 或 OrderID 任一可用即可查到）
func (mse *MultiStrategyExecutor) trackOrderLocked(rec *orderCapital, orderID int64, clientOrderID string) {
	if rec == nil {
		return
	}
	if orderID > 0 {
		rec.orderID = orderID
		mse.ordersByID[orderID] = rec
	}
	if clientOrderID != "" {
		if rec.clientOrderID == "" {
			rec.clientOrderID = clientOrderID
		}
		mse.ordersByClient[clientOrderID] = rec
	}
}

// untrackOrderLocked 刪除訂單的記賬與路由
func (mse *MultiStrategyExecutor) untrackOrderLocked(rec *orderCapital) {
	for clientOID, r := range mse.ordersByClient {
		if r == rec {
			delete(mse.ordersByClient, clientOID)
			delete(mse.clientStrategies, clientOID)
		}
	}
	if rec.orderID > 0 {
		delete(mse.ordersByID, rec.orderID)
		delete(mse.strategies, fmt.Sprintf("%d", rec.orderID))
	}
}

func (mse *MultiStrategyExecutor) lookupOrderLocked(orderID int64, clientOrderID string) *orderCapital {
	if clientOrderID != "" {
		if rec, ok := mse.ordersByClient[clientOrderID]; ok {
			return rec
		}
	}
	if orderID > 0 {
		if rec, ok := mse.ordersByID[orderID]; ok {
			return rec
		}
	}
	return nil
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

// classifyOrder 判斷訂單所屬持倉腿與開/平倉（S9：不再按策略名稱是否含 "short" 猜測）。
// 優先級：ReduceOnly（一定是平倉）> OrderRequest.PositionSide > 策略註冊的持倉腿 > 全局 direction。
// 全局 BOTH 且無顯式信息時，非 ReduceOnly 單一律按開倉處理（保守占用資金）。
func (mse *MultiStrategyExecutor) classifyOrder(strategyName string, req *position.OrderRequest) (leg string, opening bool) {
	if req == nil {
		return "", false
	}
	side := strings.ToUpper(strings.TrimSpace(req.Side))
	if side != "BUY" && side != "SELL" {
		return "", false
	}
	if req.ReduceOnly {
		if side == "SELL" {
			return position.PositionSideLong, false
		}
		return position.PositionSideShort, false
	}

	posSide := strings.ToUpper(strings.TrimSpace(req.PositionSide))
	if posSide != position.PositionSideLong && posSide != position.PositionSideShort {
		posSide = ""
		if mse != nil {
			mse.mu.RLock()
			posSide = mse.strategyPositionSide[strategyName]
			mse.mu.RUnlock()
		}
	}
	if posSide == "" {
		direction := "LONG"
		if mse != nil && mse.allocator != nil {
			if cfg := mse.allocator.GetConfig(); cfg != nil {
				direction = config.NormalizeDirection(cfg.Trading.Direction)
			}
		}
		switch direction {
		case "SHORT":
			posSide = position.PositionSideShort
		case "BOTH":
			if side == "BUY" {
				return position.PositionSideLong, true
			}
			return position.PositionSideShort, true
		default:
			posSide = position.PositionSideLong
		}
	}

	if posSide == position.PositionSideShort {
		return posSide, side == "SELL"
	}
	return posSide, side == "BUY"
}

func (mse *MultiStrategyExecutor) isReducePositionOrder(strategyName string, req *position.OrderRequest) bool {
	_, opening := mse.classifyOrder(strategyName, req)
	return !opening
}

// PlaceOrder 下單（带策略標記）
func (mse *MultiStrategyExecutor) PlaceOrder(strategyName string, req *position.OrderRequest) (*position.Order, error) {
	leg, opening := mse.classifyOrder(strategyName, req)

	var estimatedAmount float64

	if opening {
		// 🔥 使用交易所的 EstimateFinalOrderAmount 預估最终下單金額
		// 交易所可能因最小名义金額（如币安 100 USDT）、精度對齐等原因調整數量
		// 必須用預估的最终金額做 Reserve，否则會出現"預留 90 實際下 180"的穿透額度问题
		estimatedAmount = mse.executor.EstimateFinalOrderAmount(req.Symbol, req.Price, req.Quantity, req.ReduceOnly)
		if estimatedAmount <= 0 {
			return nil, fmt.Errorf("策略 %s 預估订單金額為 0 (價格: %.2f, 數量: %.8f)", strategyName, req.Price, req.Quantity)
		}

		// 检查策略资金是否充足
		if !mse.allocator.CheckAvailable(strategyName, estimatedAmount) {
			// 嘗試從配置中獲取 bot ID 以提供更好的錯誤信息
			botID := ""
			if cfg := mse.allocator.GetConfig(); cfg != nil && cfg.Trading.BotID != "" {
				botID = cfg.Trading.BotID + " "
			}
			return nil, fmt.Errorf("%s策略 %s 资金不足: 需要 %.2f, 可用 %.2f",
				botID, strategyName, estimatedAmount, mse.allocator.GetAvailable(strategyName))
		}

		// 預留资金（使用預估的最终金額）
		if !mse.allocator.Reserve(strategyName, estimatedAmount) {
			// 嘗試從配置中獲取 bot ID 以提供更好的錯誤信息
			botID := ""
			if cfg := mse.allocator.GetConfig(); cfg != nil && cfg.Trading.BotID != "" {
				botID = cfg.Trading.BotID + " "
			}
			return nil, fmt.Errorf("%s策略 %s 资金預留失败", botID, strategyName)
		}
	}

	rec := &orderCapital{strategy: strategyName, leg: leg, opening: opening, reserved: estimatedAmount, quantity: req.Quantity}
	if req.ClientOrderID != "" {
		// 下單前先記賬，處理「成交回報先於下單回執到達」
		mse.mu.Lock()
		mse.clientStrategies[req.ClientOrderID] = strategyName
		mse.trackOrderLocked(rec, 0, req.ClientOrderID)
		mse.mu.Unlock()
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
		if opening && estimatedAmount > 0 {
			mse.allocator.Release(strategyName, estimatedAmount)
		}
		mse.mu.Lock()
		mse.untrackOrderLocked(rec)
		mse.mu.Unlock()
		if errors.Is(err, order.ErrLockNotAcquired) {
			// 未提交到交易所：預留已歸還，調用方可用 errors.Is 識別並在下一輪重試，不應計為失敗
			return nil, fmt.Errorf("策略 %s 下單跳過（價格位被其他實例鎖定）: %w", strategyName, err)
		}
		return nil, fmt.Errorf("策略 %s 下單失败: %w", strategyName, err)
	}

	// 標記订單所属策略，並記錄資金記賬（成交/取消回報時用於轉換或釋放資金）
	mse.mu.Lock()
	mse.bindOrderRouteLocked(ord.OrderID, ord.ClientOrderID, strategyName)
	if req.ClientOrderID != "" && req.ClientOrderID != ord.ClientOrderID {
		mse.bindOrderRouteLocked(ord.OrderID, req.ClientOrderID, strategyName)
	}
	mse.trackOrderLocked(rec, ord.OrderID, ord.ClientOrderID)
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
	records := make(map[string]*orderCapital) // ClientOrderID -> 資金記賬

	for _, req := range orders {
		leg, opening := mse.classifyOrder(strategyName, req)

		var estimatedAmount float64

		if opening {
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

		rec := &orderCapital{strategy: strategyName, leg: leg, opening: opening, reserved: estimatedAmount, quantity: req.Quantity}
		if req.ClientOrderID != "" {
			records[req.ClientOrderID] = rec
			mse.mu.Lock()
			mse.clientStrategies[req.ClientOrderID] = strategyName
			mse.trackOrderLocked(rec, 0, req.ClientOrderID)
			mse.mu.Unlock()
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
		mse.bindOrderRouteLocked(ord.OrderID, ord.ClientOrderID, strategyName)
		if rec, ok := records[ord.ClientOrderID]; ok {
			mse.trackOrderLocked(rec, ord.OrderID, ord.ClientOrderID)
		}
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
	for clientOID, rec := range records {
		if !placedClientOIDs[clientOID] {
			if rec.opening && rec.reserved > 0 {
				mse.allocator.Release(strategyName, rec.reserved)
			}
			mse.mu.Lock()
			mse.untrackOrderLocked(rec)
			mse.mu.Unlock()
		}
	}

	return result
}

// BatchCancelOrders 批量撤單
// 資金釋放在撤單回報（CANCELED/EXPIRED/REJECTED）經 OnOrderUpdate 處理，這裡不提前釋放。
func (mse *MultiStrategyExecutor) BatchCancelOrders(orderIDs []int64) error {
	return mse.executor.BatchCancelOrders(orderIDs)
}

// ReleaseOrderCapital 释放订單资金（手動釋放指定金額）
func (mse *MultiStrategyExecutor) ReleaseOrderCapital(strategyName string, amount float64) {
	mse.allocator.Release(strategyName, amount)
}

// OnOrderUpdate 根據訂單回報維護策略資金（D5）：
//   - 開倉單成交：預留按成交比例轉為持倉占用（不釋放，DCA/馬丁累計敞口受分配額度約束）
//   - 平倉單成交：按比例釋放該策略該腿的持倉占用
//   - 開倉單撤單/拒單/過期：釋放未成交部分的預留
func (mse *MultiStrategyExecutor) OnOrderUpdate(update *position.OrderUpdate) {
	if mse == nil || update == nil {
		return
	}
	status := strings.ToUpper(strings.TrimSpace(update.Status))
	var releaseStrategy string
	var releaseAmount float64

	mse.mu.Lock()
	rec := mse.lookupOrderLocked(update.OrderID, update.ClientOrderID)
	if rec == nil {
		mse.mu.Unlock()
		return
	}
	if update.OrderID > 0 && rec.orderID == 0 {
		mse.trackOrderLocked(rec, update.OrderID, "")
	}

	switch status {
	case orderStatusPartiallyFilled, orderStatusFilled:
		final := status == orderStatusFilled
		delta := update.ExecutedQty - rec.filledQty
		if delta < 0 {
			delta = 0
		}
		if update.ExecutedQty > rec.filledQty {
			rec.filledQty = update.ExecutedQty
		}
		usageKey := rec.strategy + "|" + rec.leg
		usage := mse.positionUsage[usageKey]
		if usage == nil {
			usage = &legUsage{}
			mse.positionUsage[usageKey] = usage
		}
		if rec.opening {
			remaining := rec.reserved - rec.converted
			share := remaining
			if !final && rec.quantity > 0 {
				share = rec.reserved * delta / rec.quantity
				if share > remaining {
					share = remaining
				}
			}
			if share < 0 {
				share = 0
			}
			rec.converted += share
			usage.amount += share
			usage.qty += delta
		} else if delta > 0 && usage.amount > 0 {
			release := usage.amount
			if usage.qty > capitalQtyEpsilon && delta < usage.qty {
				release = usage.amount * delta / usage.qty
			}
			usage.amount -= release
			usage.qty -= delta
			if usage.qty <= capitalQtyEpsilon {
				usage.qty = 0
				// 持倉清空：歸還剩餘占用（處理精度殘留）
				release += usage.amount
				usage.amount = 0
			}
			releaseStrategy, releaseAmount = rec.strategy, release
		}
		if final {
			mse.untrackOrderLocked(rec)
		}
	case orderStatusCanceled, orderStatusExpired, orderStatusRejected:
		if rec.opening {
			releaseStrategy, releaseAmount = rec.strategy, rec.reserved-rec.converted
		}
		mse.untrackOrderLocked(rec)
	}
	mse.mu.Unlock()

	if releaseStrategy != "" && releaseAmount > 0 {
		mse.allocator.Release(releaseStrategy, releaseAmount)
	}
}

// GetPositionCapital 返回策略某條腿上已成交持倉占用的資金（供監控/測試）
func (mse *MultiStrategyExecutor) GetPositionCapital(strategyName, positionSide string) float64 {
	mse.mu.RLock()
	defer mse.mu.RUnlock()
	if usage, ok := mse.positionUsage[strategyName+"|"+strings.ToUpper(positionSide)]; ok {
		return usage.amount
	}
	return 0
}

// GetStrategyByOrderID 根據订單ID獲取策略名称
func (mse *MultiStrategyExecutor) GetStrategyByOrderID(orderID int64) string {
	mse.mu.RLock()
	defer mse.mu.RUnlock()
	return mse.strategies[fmt.Sprintf("%d", orderID)]
}

// GetStrategyByClientOrderID 根據 ClientOrderID 獲取策略名称
func (mse *MultiStrategyExecutor) GetStrategyByClientOrderID(clientOrderID string) string {
	mse.mu.RLock()
	defer mse.mu.RUnlock()
	return mse.clientStrategies[clientOrderID]
}

// RestoreOrderRoute 從持久化記錄恢復策略路由
func (mse *MultiStrategyExecutor) RestoreOrderRoute(orderID int64, clientOrderID, strategyName string) {
	if strategyName == "" {
		return
	}
	mse.mu.Lock()
	defer mse.mu.Unlock()
	mse.bindOrderRouteLocked(orderID, clientOrderID, strategyName)
}
