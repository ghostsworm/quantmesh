package position

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
)

// ========== 核實型全平倉（退出 / 熔斷 / 緊急平倉） ==========
//
// LiquidateAll 只提交限價平倉單即返回，適合持有 spm.mu 的熱路徑（AdjustOrders 止損）。
// LiquidateAllVerified 在其基礎上等待成交、撤銷未成交剩餘、按交易所持倉以市價 ReduceOnly 補平，
// 最後確保本交易對無殘留掛單；有任何殘留時返回帶數量/訂單號的錯誤。
// 它會阻塞最多 timeout（按注入時鐘輪詢），調用方不得持有 spm.mu 或任何槽位鎖。

const (
	// liquidationVerifyDefaultTimeout 未指定超時時的默認總等待時長
	liquidationVerifyDefaultTimeout = 20 * time.Second
	// liquidationVerifyPollInterval 輪詢訂單狀態 / 持倉的間隔（按注入時鐘）
	liquidationVerifyPollInterval = 250 * time.Millisecond
	// liquidationLimitPhaseDivisor 限價階段可用的等待時長 = timeout / 該值，其餘留給市價補平與核實
	liquidationLimitPhaseDivisor = 2
	// liquidationVenueCallTimeout 撤單 / 市價補平 / 清理掛單的單次網絡超時（不受調用方 ctx 取消影響，保證收尾）
	liquidationVenueCallTimeout = 5 * time.Second
	// liquidationQtyEpsilon 數量比較容差
	liquidationQtyEpsilon = 1e-9
)

// IOCOrderExecutor 可選能力：執行器支持以 IOC 批量提交限價單（語義同 BatchPlaceOrdersWithDetails）。
// 未實現時核實型全平倉使用普通穿價限價單，等待超時後撤銷剩餘。
type IOCOrderExecutor interface {
	BatchPlaceIOCOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult
}

// LiquidationOrderState 核實型全平倉查詢到的訂單狀態
type LiquidationOrderState struct {
	Status      string  // NEW / PARTIALLY_FILLED / FILLED / CANCELED / REJECTED / EXPIRED（大小寫不敏感）
	ExecutedQty float64 // 累計成交數量
}

// LiquidationVenue 核實型全平倉需要的交易所能力（position.IExchange 不含下單/單筆撤單，故單獨定義）
type LiquidationVenue interface {
	// GetOrderState 查詢訂單狀態與累計成交
	GetOrderState(ctx context.Context, symbol string, orderID int64) (LiquidationOrderState, error)
	// CancelOrder 撤銷單筆訂單
	CancelOrder(ctx context.Context, symbol string, orderID int64) error
	// PlaceMarketOrder 提交市價單，返回訂單號
	PlaceMarketOrder(ctx context.Context, symbol, side string, qty float64, reduceOnly bool) (int64, error)
	// GetPositionSizes 交易所持倉（帶符號：正=多、負=空），每個持倉條目一項，不做合併
	GetPositionSizes(ctx context.Context, symbol string) ([]float64, error)
	// GetOpenOrderIDs 本交易對未完成訂單號
	GetOpenOrderIDs(ctx context.Context, symbol string) ([]int64, error)
	// CancelAllOrders 撤銷本交易對全部掛單
	CancelAllOrders(ctx context.Context, symbol string) error
}

// exchangeLiquidationVenue 以 exchange.IExchange 實現 LiquidationVenue
type exchangeLiquidationVenue struct {
	ex exchange.IExchange
}

// NewExchangeLiquidationVenue 以交易所適配器創建核實型全平倉所需能力；ex 為 nil 時返回 nil
func NewExchangeLiquidationVenue(ex exchange.IExchange) LiquidationVenue {
	if ex == nil {
		return nil
	}
	return &exchangeLiquidationVenue{ex: ex}
}

func (v *exchangeLiquidationVenue) GetOrderState(ctx context.Context, symbol string, orderID int64) (LiquidationOrderState, error) {
	ord, err := v.ex.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return LiquidationOrderState{}, fmt.Errorf("查詢訂單 %s#%d 失敗: %w", symbol, orderID, err)
	}
	if ord == nil {
		return LiquidationOrderState{}, fmt.Errorf("查詢訂單 %s#%d 未返回數據", symbol, orderID)
	}
	return LiquidationOrderState{Status: string(ord.Status), ExecutedQty: ord.ExecutedQty}, nil
}

func (v *exchangeLiquidationVenue) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return v.ex.CancelOrder(ctx, symbol, orderID)
}

func (v *exchangeLiquidationVenue) PlaceMarketOrder(ctx context.Context, symbol, side string, qty float64, reduceOnly bool) (int64, error) {
	ord, err := v.ex.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol:        symbol,
		Side:          exchange.Side(side),
		Type:          exchange.OrderTypeMarket,
		Quantity:      qty,
		ReduceOnly:    reduceOnly,
		PriceDecimals: v.ex.GetPriceDecimals(),
	})
	if err != nil {
		return 0, err
	}
	if ord == nil {
		return 0, nil
	}
	return ord.OrderID, nil
}

func (v *exchangeLiquidationVenue) GetPositionSizes(ctx context.Context, symbol string) ([]float64, error) {
	positions, err := v.ex.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}
	sizes := make([]float64, 0, len(positions))
	for _, p := range positions {
		if p == nil || (p.Symbol != "" && !strings.EqualFold(p.Symbol, symbol)) {
			continue
		}
		sizes = append(sizes, p.Size)
	}
	return sizes, nil
}

func (v *exchangeLiquidationVenue) GetOpenOrderIDs(ctx context.Context, symbol string) ([]int64, error) {
	orders, err := v.ex.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(orders))
	for _, o := range orders {
		if o != nil {
			ids = append(ids, o.OrderID)
		}
	}
	return ids, nil
}

func (v *exchangeLiquidationVenue) CancelAllOrders(ctx context.Context, symbol string) error {
	return v.ex.CancelAllOrders(ctx, symbol)
}

// isLiquidationTerminalStatus 訂單是否已終結
func isLiquidationTerminalStatus(status string) bool {
	switch normalizeOrderStatus(status) {
	case OrderStatusFilled, OrderStatusCanceled, "REJECTED", "EXPIRED":
		return true
	}
	return false
}

// LiquidateAllVerified 核實型全平倉：
//  1. 按方向撤銷開倉委託（同 LiquidateAll）；
//  2. 按槽位提交穿價限價平倉單（SELL ≤ 買一、BUY ≥ 賣一；執行器支持時 IOC）；
//  3. 按注入時鐘輪詢訂單狀態，最多 timeout/2，撤銷未終結訂單的剩餘數量；
//  4. 按腿別計算本 Bot 剩餘待平量（多腿 SELL、空腿 BUY 分開計），查詢交易所持倉，
//     對每個持倉條目按其方向以市價 ReduceOnly 補平（數量不超過本 Bot 該腿剩餘量，避免平掉同交易對其他 Bot 的倉位）；
//     現貨無持倉接口，按剩餘量市價 SELL；
//  5. 輪詢直到交易所持倉歸零或總超時；
//  6. 撤銷本交易對全部殘留掛單並核實。
//
// 返回 nil 表示持倉已核實為 0 且無殘留掛單；否則返回匯總錯誤（殘留數量、訂單號、查詢失敗原因）。
// timeout<=0 時使用 ctx 剩餘時間或默認 20s。會阻塞，調用方不得持有 spm.mu 或槽位鎖。
func (spm *SuperPositionManager) LiquidateAllVerified(ctx context.Context, venue LiquidationVenue, timeout time.Duration) error {
	if venue == nil {
		return fmt.Errorf("[%s] 核實型全平倉失敗: 交易所未初始化", spm.logPrefix())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout = liquidationEffectiveTimeout(ctx, timeout)
	symbol := spm.config.Trading.Symbol
	logger.Warn("🚨 [%s] [核實全平倉] 開始：撤銷開倉單 → 穿價限價平倉 → 撤剩餘 → 市價補平 → 清理掛單（超時 %s）",
		spm.logPrefix(), timeout)

	spm.CancelAllOpenOrders()

	clk := spm.clk.get()
	start := clk.Now()
	deadline := start.Add(timeout)
	limitDeadline := start.Add(timeout / liquidationLimitPhaseDivisor)

	sub := spm.submitLiquidationCloses(true)

	var problems []string
	filledSell, filledBuy := spm.settleLiquidationLimitOrders(ctx, venue, symbol, sub.placed, limitDeadline)

	residualSell := math.Max(0, sub.sellQty-filledSell)
	residualBuy := math.Max(0, sub.buyQty-filledBuy)

	if spm.isSpot() {
		problems = append(problems, spm.closeSpotLiquidationResidual(ctx, venue, symbol, residualSell, deadline)...)
	} else {
		problems = append(problems, spm.closeFuturesLiquidationResidual(ctx, venue, symbol, residualSell, residualBuy, deadline)...)
	}

	problems = append(problems, cleanupLiquidationOpenOrders(ctx, venue, symbol)...)

	if len(problems) > 0 {
		err := fmt.Errorf("[%s] 核實全平倉未完成: %s", spm.logPrefix(), strings.Join(problems, "; "))
		logger.Error("❌ %v", err)
		return err
	}
	logger.Info("✅ [%s] [核實全平倉] 完成：持倉已歸零，無殘留掛單（耗時 %s）", spm.logPrefix(), clk.Now().Sub(start))
	return nil
}

// liquidationEffectiveTimeout timeout<=0 或超過 ctx 剩餘時間時取 ctx 剩餘時間；兩者都沒有時用默認值
func liquidationEffectiveTimeout(ctx context.Context, timeout time.Duration) time.Duration {
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining > 0 && (timeout <= 0 || remaining < timeout) {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		timeout = liquidationVerifyDefaultTimeout
	}
	return timeout
}

// venueCallContext 收尾類網絡調用的 ctx：不隨調用方取消，單次限時
func venueCallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), liquidationVenueCallTimeout)
}

// liquidationWaitPoll 按注入時鐘等待一個輪詢間隔；ctx 取消或已到 deadline 時返回 false
func (spm *SuperPositionManager) liquidationWaitPoll(ctx context.Context, deadline time.Time) bool {
	clk := spm.clk.get()
	if !clk.Now().Before(deadline) {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case <-clk.After(liquidationVerifyPollInterval):
		return true
	}
}

// settleLiquidationLimitOrders 等待限價平倉單終結；到期後撤銷剩餘並再查一次成交量。
// 返回按方向累計的成交量（無法確認的按 0 計，後續市價補平以交易所持倉封頂）。
func (spm *SuperPositionManager) settleLiquidationLimitOrders(ctx context.Context, venue LiquidationVenue, symbol string,
	placed []liquidationPlacedOrder, deadline time.Time) (filledSell, filledBuy float64) {
	if len(placed) == 0 {
		return 0, 0
	}
	executed := make(map[int64]float64, len(placed))
	pending := append([]liquidationPlacedOrder(nil), placed...)
	for {
		next := pending[:0]
		for _, p := range pending {
			st, err := venue.GetOrderState(ctx, symbol, p.orderID)
			if err == nil && isLiquidationTerminalStatus(st.Status) {
				executed[p.orderID] = st.ExecutedQty
				continue
			}
			if err != nil && spm.liquidationSlotClosed(p) {
				executed[p.orderID] = p.qty
				continue
			}
			next = append(next, p)
		}
		pending = next
		if len(pending) == 0 || !spm.liquidationWaitPoll(ctx, deadline) {
			break
		}
	}

	for _, p := range pending {
		cctx, cancel := venueCallContext(ctx)
		if err := venue.CancelOrder(cctx, symbol, p.orderID); err != nil {
			logger.Warn("⚠️ [%s] [核實全平倉] 撤銷未成交平倉單 #%d 失敗（將在清理階段再撤）: %v", spm.logPrefix(), p.orderID, err)
		} else {
			logger.Info("🧹 [%s] [核實全平倉] 已撤銷未完全成交的平倉單 #%d", spm.logPrefix(), p.orderID)
		}
		if st, err := venue.GetOrderState(cctx, symbol, p.orderID); err == nil {
			executed[p.orderID] = st.ExecutedQty
		} else {
			logger.Warn("⚠️ [%s] [核實全平倉] 撤單後查詢平倉單 #%d 成交量失敗，按未成交處理: %v", spm.logPrefix(), p.orderID, err)
		}
		cancel()
	}

	for _, p := range placed {
		qty := math.Min(executed[p.orderID], p.qty)
		if p.side == "BUY" {
			filledBuy += qty
		} else {
			filledSell += qty
		}
	}
	return filledSell, filledBuy
}

// liquidationSlotClosed 訂單查詢失敗時以槽位狀態兜底：該槽位持倉已清空視為已成交
func (spm *SuperPositionManager) liquidationSlotClosed(p liquidationPlacedOrder) bool {
	v, ok := spm.slots.Load(p.slotPrice)
	if !ok {
		return false
	}
	slot := v.(*InventorySlot)
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	return slot.PositionStatus == PositionStatusEmpty && slot.PositionQty < liquidationQtyEpsilon
}

// closeFuturesLiquidationResidual 合約：按交易所每個持倉條目的方向分別市價 ReduceOnly 補平，並核實歸零
func (spm *SuperPositionManager) closeFuturesLiquidationResidual(ctx context.Context, venue LiquidationVenue, symbol string,
	residualSell, residualBuy float64, deadline time.Time) []string {
	cctx, cancel := venueCallContext(ctx)
	sizes, err := venue.GetPositionSizes(cctx, symbol)
	cancel()
	if err != nil {
		return []string{fmt.Sprintf("查詢 %s 持倉失敗，無法核實（本 Bot 剩餘 多腿=%.8f 空腿=%.8f）: %v", symbol, residualSell, residualBuy, err)}
	}
	if len(nonZeroSizes(sizes)) == 0 {
		return nil
	}

	var problems []string
	for _, size := range nonZeroSizes(sizes) {
		side, qty := "SELL", 0.0
		if size > 0 {
			qty = math.Min(size, residualSell)
			residualSell -= qty
		} else {
			side = "BUY"
			qty = math.Min(-size, residualBuy)
			residualBuy -= qty
		}
		if qty < liquidationQtyEpsilon {
			logger.Warn("⚠️ [%s] [核實全平倉] 交易所持倉 %.8f 超出本 Bot 槽位剩餘量，不代為平倉", spm.logPrefix(), size)
			continue
		}
		logger.Warn("⚠️ [%s] [核實全平倉] 交易所持倉 %.8f 未平，市價 ReduceOnly %s %.8f 補平", spm.logPrefix(), size, side, qty)
		mctx, mcancel := venueCallContext(ctx)
		if _, placeErr := venue.PlaceMarketOrder(mctx, symbol, side, qty, true); placeErr != nil {
			problems = append(problems, fmt.Sprintf("市價補平 %s %.8f 失敗: %v", side, qty, placeErr))
		}
		mcancel()
	}

	// 輪詢核實：直到所有持倉條目歸零或超時（至少再查一次）
	var last []float64
	var lastErr error
	for {
		qctx, qcancel := venueCallContext(ctx)
		sizes, err = venue.GetPositionSizes(qctx, symbol)
		qcancel()
		if err != nil {
			lastErr = err
		} else {
			lastErr = nil
			last = nonZeroSizes(sizes)
			if len(last) == 0 {
				return problems
			}
		}
		if !spm.liquidationWaitPoll(ctx, deadline) {
			break
		}
	}
	if lastErr != nil {
		problems = append(problems, fmt.Sprintf("核實 %s 持倉失敗: %v", symbol, lastErr))
	}
	for _, size := range last {
		problems = append(problems, fmt.Sprintf("%s 殘留持倉 %.8f", symbol, size))
	}
	return problems
}

// closeSpotLiquidationResidual 現貨：無持倉接口，按本 Bot 剩餘量市價 SELL 並等待成交
func (spm *SuperPositionManager) closeSpotLiquidationResidual(ctx context.Context, venue LiquidationVenue, symbol string,
	residualSell float64, deadline time.Time) []string {
	if residualSell < liquidationQtyEpsilon {
		return nil
	}
	logger.Warn("⚠️ [%s] [核實全平倉] 現貨剩餘 %.8f 未賣出，市價 SELL 補平", spm.logPrefix(), residualSell)
	mctx, mcancel := venueCallContext(ctx)
	orderID, err := venue.PlaceMarketOrder(mctx, symbol, "SELL", residualSell, false)
	mcancel()
	if err != nil {
		return []string{fmt.Sprintf("現貨市價補平 SELL %.8f 失敗: %v", residualSell, err)}
	}
	if orderID == 0 {
		return []string{fmt.Sprintf("現貨市價補平 SELL %.8f 未返回訂單號，無法核實", residualSell)}
	}
	var last LiquidationOrderState
	var lastErr error
	for {
		qctx, qcancel := venueCallContext(ctx)
		last, lastErr = venue.GetOrderState(qctx, symbol, orderID)
		qcancel()
		if lastErr == nil && isLiquidationTerminalStatus(last.Status) {
			break
		}
		if !spm.liquidationWaitPoll(ctx, deadline) {
			break
		}
	}
	if lastErr != nil {
		return []string{fmt.Sprintf("核實現貨市價單 #%d 失敗: %v", orderID, lastErr)}
	}
	if left := residualSell - last.ExecutedQty; left > liquidationQtyEpsilon {
		return []string{fmt.Sprintf("%s 現貨殘留 %.8f（市價單 #%d 狀態 %s）", symbol, left, orderID, last.Status)}
	}
	return nil
}

// cleanupLiquidationOpenOrders 撤銷本交易對全部殘留掛單並核實；返回殘留訂單號描述
func cleanupLiquidationOpenOrders(ctx context.Context, venue LiquidationVenue, symbol string) []string {
	cctx, cancel := venueCallContext(ctx)
	defer cancel()
	ids, err := venue.GetOpenOrderIDs(cctx, symbol)
	if err == nil && len(ids) == 0 {
		return nil
	}
	if err != nil {
		logger.Warn("⚠️ [核實全平倉] 查詢 %s 殘留掛單失敗，仍嘗試全部撤銷: %v", symbol, err)
	}
	var cancelErr error
	if cancelErr = venue.CancelAllOrders(cctx, symbol); cancelErr != nil {
		logger.Error("❌ [核實全平倉] 撤銷 %s 全部掛單失敗: %v", symbol, cancelErr)
	}
	left, err := venue.GetOpenOrderIDs(cctx, symbol)
	if err != nil {
		return []string{fmt.Sprintf("核實 %s 殘留掛單失敗: %v", symbol, errors.Join(cancelErr, err))}
	}
	if len(left) == 0 {
		return nil
	}
	strs := make([]string, 0, len(left))
	for _, id := range left {
		strs = append(strs, fmt.Sprintf("%d", id))
	}
	return []string{fmt.Sprintf("%s 殘留掛單 %d 個，訂單號: %s", symbol, len(left), strings.Join(strs, ","))}
}

// nonZeroSizes 過濾掉視為 0 的持倉
func nonZeroSizes(sizes []float64) []float64 {
	out := make([]float64, 0, len(sizes))
	for _, s := range sizes {
		if math.Abs(s) >= liquidationQtyEpsilon {
			out = append(out, s)
		}
	}
	return out
}
