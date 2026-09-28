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
	"quantmesh/utils"
)

// ========== 核實型全平倉（退出 / 熔斷 / 緊急平倉） ==========
//
// LiquidateAll 是只提交限價單的舊介面；內部止損通過異步任務使用核實流程。
// LiquidateAllVerified 在其基礎上等待成交、撤銷未成交剩餘、按交易所持倉以市價 ReduceOnly 補平，
// 最後核實本次自有平倉單終止；其他 Bot/手工掛單不在此操作的權限內。
// 它會阻塞最多 timeout（按注入時鐘輪詢），調用方不得持有 spm.mu 或任何槽位鎖。

const (
	// liquidationVerifyDefaultTimeout 未指定超時時的默認總等待時長
	liquidationVerifyDefaultTimeout = 20 * time.Second
	// liquidationVerifyPollInterval 輪詢訂單狀態 / 持倉的間隔（按注入時鐘）
	liquidationVerifyPollInterval = 250 * time.Millisecond
	// liquidationLimitPhaseDivisor 限價階段可用的等待時長 = timeout / 該值，其餘留給市價補平與核實
	liquidationLimitPhaseDivisor = 2
	// liquidationVenueCallTimeout 單次網絡超時，同時遵守整體 ctx 截止時間
	liquidationVenueCallTimeout = 5 * time.Second
	// liquidationQtyEpsilon 數量比較容差
	liquidationQtyEpsilon = 1e-9
)

// LiquidationOrderState 核實型全平倉查詢到的訂單狀態
type LiquidationOrderState struct {
	Status      string  // NEW / PARTIALLY_FILLED / FILLED / CANCELED / REJECTED / EXPIRED（大小寫不敏感）
	ExecutedQty float64 // 累計成交數量
	AvgPrice    float64 // 交易所實際累計成交均價，不是委託價格
	Price       float64
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
	ex       exchange.IExchange
	executor ContextOrderExecutor
}

// NewExchangeLiquidationVenue supplies read/cancel capabilities only. Market
// submission requires the Bot's owned executor via spm.NewLiquidationVenue.
func NewExchangeLiquidationVenue(ex exchange.IExchange) LiquidationVenue {
	if ex == nil {
		return nil
	}
	return &exchangeLiquidationVenue{ex: ex}
}

// NewLiquidationVenue binds market fallback to this Bot's normal intent ledger
// and opening gate, never to an untracked direct exchange.PlaceOrder call.
func (spm *SuperPositionManager) NewLiquidationVenue(ex exchange.IExchange) LiquidationVenue {
	if ex == nil {
		return nil
	}
	executor, _ := spm.executor.(ContextOrderExecutor)
	return &exchangeLiquidationVenue{ex: ex, executor: executor}
}

func (v *exchangeLiquidationVenue) GetOrderState(ctx context.Context, symbol string, orderID int64) (LiquidationOrderState, error) {
	ord, err := v.ex.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return LiquidationOrderState{}, fmt.Errorf("查詢訂單 %s#%d 失敗: %w", symbol, orderID, err)
	}
	if ord == nil {
		return LiquidationOrderState{}, fmt.Errorf("查詢訂單 %s#%d 未返回數據", symbol, orderID)
	}
	if ord.OrderID != orderID || (ord.Symbol != "" && ord.Symbol != symbol) {
		return LiquidationOrderState{}, fmt.Errorf("查詢訂單 %s#%d 返回不匹配的身份", symbol, orderID)
	}
	return LiquidationOrderState{Status: string(ord.Status), ExecutedQty: ord.ExecutedQty, AvgPrice: ord.AvgPrice, Price: ord.Price}, nil
}

func (v *exchangeLiquidationVenue) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return v.ex.CancelOrder(ctx, symbol, orderID)
}

func (v *exchangeLiquidationVenue) PlaceMarketOrder(ctx context.Context, symbol, side string, qty float64, reduceOnly bool) (int64, error) {
	if v.executor == nil {
		return 0, fmt.Errorf("market liquidation requires the Bot's context-aware intent executor")
	}
	leg := PositionLegLong
	if side == "BUY" {
		leg = PositionLegShort
	}
	ord, err := v.executor.PlaceOrderContext(ctx, &OrderRequest{
		Symbol:        symbol,
		Side:          side,
		Type:          string(exchange.OrderTypeMarket),
		Quantity:      qty,
		ReduceOnly:    reduceOnly,
		PriceDecimals: v.ex.GetPriceDecimals(),
		PositionSide:  leg,
		ClientOrderID: utils.NewCompactOrderID(),
		OrderSource:   "liquidation",
		StrategyType:  "grid",
	})
	if ord == nil {
		if err == nil {
			err = fmt.Errorf("market liquidation returned no order acknowledgement")
		}
		return 0, err
	}
	return ord.OrderID, err
}

func (v *exchangeLiquidationVenue) GetPositionSizes(ctx context.Context, symbol string) ([]float64, error) {
	positions, err := v.ex.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}
	return positionSizesFromExchange(positions, symbol)
}

func positionSizesFromExchange(positions []*exchange.Position, symbol string) ([]float64, error) {
	sizes := make([]float64, 0, len(positions))
	for index, p := range positions {
		if p == nil {
			return nil, fmt.Errorf("交易所持仓响应第 %d 项为空，不能核实清仓状态", index)
		}
		if p.Symbol != "" && !strings.EqualFold(p.Symbol, symbol) {
			continue
		}
		if math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return nil, fmt.Errorf("交易所持仓响应包含非法数量，不能核实清仓状态")
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
//  1. 關閉開倉准入，排空在途請求，核實本 Bot 舊單終態並處理遲到成交；
//  2. 按槽位提交穿價限價平倉單（SELL ≤ 買一、BUY ≥ 賣一；執行器支持時 IOC）；
//  3. 按注入時鐘輪詢訂單狀態，最多 timeout/2，撤銷未終結訂單的剩餘數量；
//  4. 按腿別計算本 Bot 剩餘待平量（多腿 SELL、空腿 BUY 分開計），查詢交易所持倉，
//     對每個持倉條目按其方向以市價 ReduceOnly 補平（數量不超過本 Bot 該腿剩餘量，避免平掉同交易對其他 Bot 的倉位）；
//     現貨無持倉接口，按剩餘量市價 SELL；
//  5. 輪詢直到交易所持倉歸零或總超時；
//  6. 僅清理本次操作明確擁有的平倉單並核實；失敗保留暫停與待對賬狀態。
//
// 返回 nil 表示此核驗鏈路通過；不表示策略/成交帳本的重啟恢復也已完成。
// timeout<=0 時使用 ctx 剩餘時間或默認 20s。會阻塞，調用方不得持有 spm.mu 或槽位鎖。
func (spm *SuperPositionManager) LiquidateAllVerified(ctx context.Context, venue LiquidationVenue, timeout time.Duration) (resultErr error) {
	if venue == nil {
		return fmt.Errorf("[%s] 核實型全平倉失敗: 交易所未初始化", spm.logPrefix())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout = liquidationEffectiveTimeout(ctx, timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if !spm.liquidationMu.TryLock() {
		return fmt.Errorf("[%s] 已有全平倉正在核實，禁止重複觸發", spm.logPrefix())
	}
	defer spm.liquidationMu.Unlock()
	if spm.liquidationNeedsReconciliation.Load() {
		return fmt.Errorf("[%s] 上次全平倉尚待對賬，禁止重新提交平倉", spm.logPrefix())
	}
	spm.openingGate.Block(liquidationBlockSource)
	spm.liquidationActive.Store(true)
	defer func() {
		if resultErr != nil {
			spm.liquidationNeedsReconciliation.Store(true)
		} else {
			spm.openingGate.Unblock(liquidationBlockSource)
		}
		spm.liquidationActive.Store(false)
	}()
	// Wait for a tick already holding the grid lock before snapshotting its
	// orders. Later ticks see liquidationActive and cannot submit competing work.
	if err := spm.awaitLiquidationGridIdle(ctx); err != nil {
		return err
	}
	symbol := spm.config.Trading.Symbol
	logger.Warn("🚨 [%s] [核實全平倉] 開始：撤銷開倉單 → 穿價限價平倉 → 撤剩餘 → 市價補平 → 清理掛單（超時 %s）",
		spm.logPrefix(), timeout)

	if err := spm.prepareLiquidationSlots(ctx, venue); err != nil {
		return fmt.Errorf("[%s] 全平倉前置核實失敗: %w", spm.logPrefix(), err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	clk := spm.clk.get()
	start := clk.Now()
	deadline := start.Add(timeout)
	limitDeadline := start.Add(timeout / liquidationLimitPhaseDivisor)

	sub := spm.submitLiquidationClosesContext(ctx, true)
	if sub.err != nil {
		return fmt.Errorf("平倉限價提交未完成: %w", sub.err)
	}
	if sub.unknown {
		return fmt.Errorf("[%s] 平倉結果 UNKNOWN，保留持倉與委託歸屬；核實前禁止重複市價補平", spm.logPrefix())
	}

	var problems []string
	filledSell, filledBuy, settleErr := spm.settleLiquidationLimitOrders(ctx, venue, symbol, sub.placed, limitDeadline)
	if settleErr != nil {
		spm.openingGate.Block(liquidationBlockSource)
		return fmt.Errorf("[%s] 平倉單終態未核實，禁止市價重複補平: %w", spm.logPrefix(), settleErr)
	}
	ownedIDs := make([]int64, 0, len(sub.placed))
	for _, p := range sub.placed {
		ownedIDs = append(ownedIDs, p.orderID)
	}

	residualSell := math.Max(0, sub.sellQty-filledSell)
	residualBuy := math.Max(0, sub.buyQty-filledBuy)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("平倉期限已到，不再提交補單: %w", err)
	}

	problems = append(problems, spm.closeOwnedLiquidationResidual(ctx, venue, symbol, residualSell, residualBuy, deadline, &ownedIDs)...)

	problems = append(problems, cleanupLiquidationOpenOrders(ctx, venue, symbol, ownedIDs)...)

	if len(problems) > 0 {
		err := fmt.Errorf("[%s] 核實全平倉未完成: %s", spm.logPrefix(), strings.Join(problems, "; "))
		spm.openingGate.Block(liquidationBlockSource)
		logger.Error("❌ %v", err)
		return err
	}
	logger.Info("✅ [%s] [核實全平倉] 本次核驗完成：持倉查詢通過、自有平倉單已終止（耗時 %s）", spm.logPrefix(), clk.Now().Sub(start))
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

// venueCallContext 每次調用有上限，並遵守整體取消/截止時間。
func venueCallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, liquidationVenueCallTimeout)
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
// 只有確認所有舊單終止才返回可補平數量；查不到不是未成交證明。
func (spm *SuperPositionManager) settleLiquidationLimitOrders(ctx context.Context, venue LiquidationVenue, symbol string,
	placed []liquidationPlacedOrder, deadline time.Time) (filledSell, filledBuy float64, resultErr error) {
	if len(placed) == 0 {
		return 0, 0, nil
	}
	executed := make(map[int64]float64, len(placed))
	observed := make(map[int64]float64, len(placed))
	var problems []error
	pending := append([]liquidationPlacedOrder(nil), placed...)
	for {
		next := pending[:0]
		for _, p := range pending {
			qctx, qcancel := context.WithTimeout(ctx, liquidationVenueCallTimeout)
			st, err := venue.GetOrderState(qctx, symbol, p.orderID)
			qcancel()
			if err == nil {
				if st.ExecutedQty < observed[p.orderID] {
					problems = append(problems, fmt.Errorf("訂單 #%d 成交量倒退，禁止補單", p.orderID))
					continue
				}
				observed[p.orderID] = st.ExecutedQty
			}
			if err == nil && isLiquidationTerminalStatus(st.Status) {
				if err := spm.applyLiquidationFill(p, st); err != nil {
					problems = append(problems, err)
				} else {
					executed[p.orderID] = st.ExecutedQty
				}
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
		cancelErr := venue.CancelOrder(cctx, symbol, p.orderID)
		cancel()
		qctx, qcancel := venueCallContext(ctx)
		st, queryErr := venue.GetOrderState(qctx, symbol, p.orderID)
		qcancel()
		if queryErr == nil && st.ExecutedQty < observed[p.orderID] {
			problems = append(problems, fmt.Errorf("訂單 #%d 撤單後成交量倒退，禁止補單", p.orderID))
			continue
		}
		if queryErr != nil || !isLiquidationTerminalStatus(st.Status) {
			problems = append(problems, fmt.Errorf("訂單 #%d 撤單後終態未確認（status=%s）: %w", p.orderID, st.Status,
				errors.Join(cancelErr, queryErr, fmt.Errorf("needs reconciliation"))))
			continue
		}
		if err := spm.applyLiquidationFill(p, st); err != nil {
			problems = append(problems, err)
			continue
		}
		executed[p.orderID] = st.ExecutedQty
	}

	for _, p := range placed {
		qty := math.Min(executed[p.orderID], p.qty)
		if p.side == "BUY" {
			filledBuy += qty
		} else {
			filledSell += qty
		}
	}
	return filledSell, filledBuy, errors.Join(problems...)
}

func validateLiquidationFill(p liquidationPlacedOrder, st LiquidationOrderState) error {
	if math.IsNaN(st.ExecutedQty) || math.IsInf(st.ExecutedQty, 0) || st.ExecutedQty < 0 || st.ExecutedQty > p.qty+liquidationQtyEpsilon {
		return fmt.Errorf("訂單 #%d 成交數量無效: %.8f（委託 %.8f）", p.orderID, st.ExecutedQty, p.qty)
	}
	if normalizeOrderStatus(st.Status) == OrderStatusFilled && st.ExecutedQty < p.qty-liquidationQtyEpsilon {
		return fmt.Errorf("訂單 #%d FILLED 但累計成交不足，需核實", p.orderID)
	}
	return nil
}

// cleanupLiquidationOpenOrders only owns IDs from this liquidation. Never infer
// ownership from symbol/direction or sweep manual/other-bot orders on the account.
func cleanupLiquidationOpenOrders(ctx context.Context, venue LiquidationVenue, symbol string, ownedIDs []int64) []string {
	var problems []string
	seen := make(map[int64]bool)
	for _, id := range ownedIDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		qctx, qcancel := venueCallContext(ctx)
		st, queryErr := venue.GetOrderState(qctx, symbol, id)
		qcancel()
		if queryErr == nil && isLiquidationTerminalStatus(st.Status) {
			continue
		}
		cctx, cancel := venueCallContext(ctx)
		cancelErr := venue.CancelOrder(cctx, symbol, id)
		cancel()
		qctx, qcancel = venueCallContext(ctx)
		st, queryErr = venue.GetOrderState(qctx, symbol, id)
		qcancel()
		if queryErr != nil || !isLiquidationTerminalStatus(st.Status) {
			problems = append(problems, fmt.Sprintf("%s 本次平倉單 #%d 未確認終止（%s）: %v", symbol, id, st.Status, errors.Join(cancelErr, queryErr)))
		}
	}
	return problems
}

// nonZeroSizes 過濾掉視為 0 的持倉
func validateLiquidationPositions(sizes []float64) error {
	for _, size := range sizes {
		if math.IsNaN(size) || math.IsInf(size, 0) {
			return fmt.Errorf("交易所持倉數量無效，不能當作零倉位")
		}
	}
	return nil
}

func nonZeroSizes(sizes []float64) []float64 {
	out := make([]float64, 0, len(sizes))
	for _, s := range sizes {
		if math.Abs(s) >= liquidationQtyEpsilon {
			out = append(out, s)
		}
	}
	return out
}
