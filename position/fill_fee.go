package position

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/storage"
)

const (
	// qtyFloorEpsilon 浮點取整容差：避免 0.009 被表示為 0.00899999... 時被多砍一檔精度
	qtyFloorEpsilon = 1e-9
	// feeSupplementCloseHold 現貨開倉買單 REST 補查手續費在途時，暫緩掛平倉單的最長時間（牆鐘）
	feeSupplementCloseHold = 10 * time.Second
	// feeSupplementTimeout 單次 REST 補查成交明細的超時
	feeSupplementTimeout = 8 * time.Second
	// tradeFeeCorrectionEvent 補查手續費無法寫回已保存交易記錄時，寫入事件表的更正記錄類型
	tradeFeeCorrectionEvent = "trade_fee_correction"
	// defaultFeeAsset 補查結果未給出手續費幣種時的默認口徑
	defaultFeeAsset = "USDT"
)

// addOrderCommissionLocked 累計某訂單（按 ClientOrderID）已由推送攜帶的手續費與基礎幣手續費數量；換訂單時重新計數。
// 調用方需持有 slot.mu。
func (slot *InventorySlot) addOrderCommissionLocked(clientOID string, commission, baseFeeQty float64) {
	if slot.feeClientOID != clientOID {
		slot.feeClientOID = clientOID
		slot.orderCommission = 0
		slot.orderBaseFeeQty = 0
	}
	slot.orderCommission += commission
	if baseFeeQty > 0 {
		slot.orderBaseFeeQty += baseFeeQty
	}
}

// orderFeeState 訂單結束時的手續費推送狀態
type orderFeeState struct {
	// missing 所有成交推送都未攜帶手續費，需要 REST 補查
	missing bool
	// wsBaseFeeQty 推送已攜帶（並已從持倉扣除）的基礎幣手續費數量
	wsBaseFeeQty float64
}

// takeOrderFeeStateLocked 訂單結束時取出其推送手續費狀態並清空累計，保證同一訂單最多觸發一次補查。
// 調用方需持有 slot.mu。
func (slot *InventorySlot) takeOrderFeeStateLocked(clientOID string) orderFeeState {
	matched := slot.feeClientOID == clientOID
	st := orderFeeState{missing: !matched || slot.orderCommission == 0}
	if matched {
		st.wsBaseFeeQty = slot.orderBaseFeeQty
	}
	slot.feeClientOID = ""
	slot.orderCommission = 0
	slot.orderBaseFeeQty = 0
	return st
}

// resetPositionCycleLocked 槽位持倉清空：結束當前持倉週期。
// 遞增週期代號使在途的手續費補查失效，並清掉只屬於本週期的費用/取整/補查狀態。調用方需持有 slot.mu。
func (slot *InventorySlot) resetPositionCycleLocked() {
	slot.cycleGen++
	slot.BuyFee = 0
	slot.AvgBuyPrice = 0
	slot.CostBasisUnverified = false
	slot.PositionEntryOrderID = 0
	slot.PositionEntryOrderAmbiguous = false
	slot.feeValuationUnknown = false
	slot.baseFeeUnfloored = false
	slot.feeSupplementUntil = time.Time{}
}

// closeOrderHeldForFeeLocked 槽位是否因現貨開倉手續費補查在途而暫緩掛平倉單。調用方需持有 slot.mu。
func (spm *SuperPositionManager) closeOrderHeldForFeeLocked(slot *InventorySlot) bool {
	if slot.feeSupplementUntil.IsZero() {
		return false
	}
	if time.Now().Before(slot.feeSupplementUntil) {
		return true
	}
	logger.Warn("⚠️ [手續費補充] 槽位 %s 補查超過 %v 未返回，按當前持倉 %.8f 掛平倉單",
		formatPrice(slot.Price, spm.priceDecimals), feeSupplementCloseHold, slot.PositionQty)
	slot.feeSupplementUntil = time.Time{}
	return false
}

// netReceivedQty 開倉成交的實際到帳數量：僅現貨 BUY 且手續費以基礎幣扣收時扣除 baseFeeQty，其餘返回成交增量。
func (spm *SuperPositionManager) netReceivedQty(side string, deltaQty, baseFeeQty float64) float64 {
	if deltaQty <= 0 || baseFeeQty <= 0 || side != "BUY" || !spm.isSpot() {
		return deltaQty
	}
	return math.Max(deltaQty-baseFeeQty, 0)
}

// floorBaseFeePositionLocked 開倉訂單結束（FILLED / 部分成交後撤單）時，若本單扣過基礎幣手續費，
// 把持倉向下取整到數量精度，確保後續平倉單數量不超過實際可用餘額。調用方需持有 slot.mu。
func (spm *SuperPositionManager) floorBaseFeePositionLocked(slot *InventorySlot) {
	if !slot.baseFeeUnfloored {
		return
	}
	slot.baseFeeUnfloored = false
	slot.PositionQty = floorToDecimals(slot.PositionQty, spm.quantityDecimals)
}

// floorToDecimals 按小數位向下取整（decimals<0 時原樣返回）
func floorToDecimals(v float64, decimals int) float64 {
	if decimals < 0 {
		return v
	}
	scale := math.Pow10(decimals)
	return math.Floor(v*scale+qtyFloorEpsilon) / scale
}

// feeSupplementTag 異步手續費補查的上下文：發起時的槽位週期身份與推送已處理的基礎幣手續費
type feeSupplementTag struct {
	orderID      int64
	symbol       string
	clientOID    string
	side         string
	openLeg      bool
	cycleGen     uint64
	wsBaseFeeQty float64
}

type pendingFeeSupplement struct {
	slot *InventorySlot
	tag  feeSupplementTag
}

// startPendingFeeSupplements releases queued REST work only after the grid
// snapshot containing its pending marker has been durably saved.
func (spm *SuperPositionManager) startPendingFeeSupplements() {
	spm.feeSupplementQueueMu.Lock()
	queued := spm.pendingFeeSupplements
	spm.pendingFeeSupplements = nil
	spm.feeSupplementQueueMu.Unlock()
	for _, task := range queued {
		go spm.supplementCommission(context.Background(), task.slot, task.tag)
	}
}

// startFeeSupplementLocked 訂單結束且推送未帶手續費時發起一次異步補查。
// 現貨開倉買單在補查返回（或超時）前暫緩掛平倉單，保證平倉數量使用扣除基礎幣手續費後的淨持倉。
// 調用方需持有 slot.mu。
func (spm *SuperPositionManager) startFeeSupplementLocked(slot *InventorySlot, update OrderUpdate, clientOID, side string, openLeg bool) {
	st := slot.takeOrderFeeStateLocked(clientOID)
	if !st.missing {
		return
	}
	if update.OrderID <= 0 || spm.exchange == nil {
		if openLeg {
			slot.feeValuationUnknown = true
		}
		spm.requireTradeLedgerReconciliation(update, fmt.Errorf("execution fee is missing and cannot be queried"))
		return
	}
	tag := feeSupplementTag{
		orderID:      update.OrderID,
		symbol:       update.Symbol,
		clientOID:    clientOID,
		side:         side,
		openLeg:      openLeg,
		cycleGen:     slot.cycleGen,
		wsBaseFeeQty: st.wsBaseFeeQty,
	}
	slot.pendingFeeSupplementCount++
	spm.feeSupplementQueueMu.Lock()
	spm.pendingFeeSupplements = append(spm.pendingFeeSupplements, pendingFeeSupplement{slot: slot, tag: tag})
	spm.feeSupplementQueueMu.Unlock()
	// 回放時鐘下不暫緩：補查協程與 tick 推進的先後不確定，暫緩會破壞回測可重現性（模擬交易所回報已帶手續費）
	if _, real := spm.clk.get().(realClock); real && openLeg && side == "BUY" && spm.isSpot() {
		slot.feeSupplementUntil = time.Now().Add(feeSupplementCloseHold)
	}
}

// fillFeeSummary 成交明細匯總
type fillFeeSummary struct {
	commission     float64 // 計價幣口徑手續費合計
	asset          string
	valuationKnown bool
	valid          bool    // 匯總、成交名義金額及基礎幣手續費均在有限有效範圍內
	baseFeeQty     float64 // 基礎幣扣收的手續費數量合計
	notional       float64 // Σ price×qty
	qty            float64 // Σ qty
}

// summarizeFills 解析適配層返回的成交明細（[]*exchange.OrderFill 等具體類型切片或 []map）並匯總手續費
func summarizeFills(fillsRaw interface{}, quoteAndBase ...string) (fillFeeSummary, int) {
	quoteAsset, baseAsset := defaultFeeAsset, ""
	if len(quoteAndBase) > 0 && quoteAndBase[0] != "" {
		quoteAsset = quoteAndBase[0]
	}
	if len(quoteAndBase) > 1 {
		baseAsset = quoteAndBase[1]
	}
	quoteAsset, baseAsset = strings.ToUpper(quoteAsset), strings.ToUpper(baseAsset)
	sum := fillFeeSummary{asset: quoteAsset, valuationKnown: true, valid: true}
	// 適配層返回的是具體類型切片（如 []*exchange.OrderFill），不能直接斷言為 []interface{}，用反射展開
	fills := interfaceSliceOf(fillsRaw)
	for _, fillRaw := range fills {
		var price, qty float64
		if fillMap, ok := fillRaw.(map[string]interface{}); ok {
			asset, _ := fillMap["CommissionAsset"].(string)
			price, qty = mapFloat(fillMap, "Price"), mapFloat(fillMap, "Quantity")
			converted, known := mapFloat(fillMap, "CommissionQuote"), false
			known, _ = fillMap["CommissionQuoteKnown"].(bool)
			commission := mapFloat(fillMap, "Commission")
			if commission == 0 && asset == "" && !known {
				sum.valuationKnown = false
			}
			addFillCommission(&sum, commission, asset, price, quoteAsset, baseAsset, converted, known)
			addFillBaseFee(&sum, mapFloat(fillMap, "BaseFeeQty"), qty)
		} else {
			rv := reflect.ValueOf(fillRaw)
			if rv.Kind() == reflect.Ptr {
				rv = rv.Elem()
			}
			if rv.Kind() != reflect.Struct {
				continue
			}
			asset := ""
			if f := rv.FieldByName("CommissionAsset"); f.IsValid() && f.Kind() == reflect.String {
				asset = f.String()
			}
			price, qty = structFloat(rv, "Price"), structFloat(rv, "Quantity")
			commission := structFloat(rv, "Commission")
			knownField := rv.FieldByName("CommissionQuoteKnown")
			converted := structFloat(rv, "CommissionQuote")
			convertedKnown := knownField.IsValid() && knownField.Kind() == reflect.Bool && knownField.Bool()
			if commission == 0 && asset == "" && !convertedKnown {
				sum.valuationKnown = false
			}
			addFillCommission(&sum, commission, asset, price, quoteAsset, baseAsset, converted, convertedKnown)
			addFillBaseFee(&sum, structFloat(rv, "BaseFeeQty"), qty)
		}
		if math.IsNaN(price) || math.IsInf(price, 0) || math.IsNaN(qty) || math.IsInf(qty, 0) || price < 0 || qty < 0 {
			sum.valid = false
		} else if price > 0 && qty > 0 {
			notional := price * qty
			nextNotional, nextQty := sum.notional+notional, sum.qty+qty
			if math.IsNaN(notional) || math.IsInf(notional, 0) || math.IsNaN(nextNotional) || math.IsInf(nextNotional, 0) ||
				math.IsNaN(nextQty) || math.IsInf(nextQty, 0) {
				sum.valid = false
			} else {
				sum.notional, sum.qty = nextNotional, nextQty
			}
		}
	}
	return sum, len(fills)
}

func addFillCommission(sum *fillFeeSummary, amount float64, asset string, price float64, quoteAsset, baseAsset string, converted float64, convertedKnown bool) {
	if amount == 0 {
		return
	}
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		sum.valuationKnown = false
		sum.asset = asset
		return
	}
	valued := 0.0
	switch {
	case convertedKnown && !math.IsNaN(converted) && !math.IsInf(converted, 0):
		valued = converted
	case asset == quoteAsset:
		valued = amount
	case asset == baseAsset && baseAsset != "" && price > 0:
		valued = amount * price
	default:
		sum.valuationKnown = false
		sum.asset = asset
		return
	}
	nextCommission := sum.commission + valued
	if math.IsNaN(valued) || math.IsInf(valued, 0) || math.IsNaN(nextCommission) || math.IsInf(nextCommission, 0) {
		sum.valuationKnown = false
		sum.asset = asset
		return
	}
	sum.commission = nextCommission
	sum.asset = quoteAsset
}

func addFillBaseFee(sum *fillFeeSummary, baseFeeQty, fillQty float64) {
	if math.IsNaN(baseFeeQty) || math.IsInf(baseFeeQty, 0) || baseFeeQty < 0 ||
		baseFeeQty > 0 && (fillQty <= 0 || baseFeeQty > fillQty) {
		sum.valid = false
		return
	}
	nextBaseFeeQty := sum.baseFeeQty + baseFeeQty
	if math.IsNaN(nextBaseFeeQty) || math.IsInf(nextBaseFeeQty, 0) {
		sum.valid = false
		return
	}
	sum.baseFeeQty = nextBaseFeeQty
}

func structFloat(rv reflect.Value, name string) float64 {
	f := rv.FieldByName(name)
	if f.IsValid() && f.Kind() == reflect.Float64 {
		return f.Float()
	}
	return 0
}

func mapFloat(m map[string]interface{}, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}

// supplementCommission 補充手續費（當 WebSocket 未提供時）。
//   - 開倉腿：週期代號仍一致時累加進槽位 BuyFee；現貨買單另按 BaseFeeQty 扣減持倉（推送已扣過則跳過）。
//     週期已結束（期間已平倉/被清倉）時不寫入槽位，改記更正記錄。
//   - 平倉腿：交易記錄已保存且存儲不支持按訂單更新手續費，記更正記錄。
func (spm *SuperPositionManager) supplementCommission(ctx context.Context, slot *InventorySlot, tag feeSupplementTag) {
	defer func() {
		slot.mu.Lock()
		if tag.openLeg && slot.cycleGen == tag.cycleGen {
			slot.feeSupplementUntil = time.Time{}
		}
		if slot.pendingFeeSupplementCount <= 0 {
			spm.openingGate.Block("grid_fee_supplement_state_invalid")
			logger.Error("[%s] 手續費補查計數失配：order=%d cid=%s", spm.logPrefix(), tag.orderID, tag.clientOID)
		} else {
			slot.pendingFeeSupplementCount--
		}
		slot.mu.Unlock()
		spm.persistGridRuntimeStateOrHold(OrderUpdate{OrderID: tag.orderID, ClientOrderID: tag.clientOID, Symbol: tag.symbol})
	}()

	ctx, cancel := context.WithTimeout(ctx, feeSupplementTimeout)
	defer cancel()
	fillsRaw, err := spm.exchange.GetOrderFills(ctx, tag.symbol, tag.orderID)
	if err != nil || fillsRaw == nil {
		spm.markFeeSupplementUnverified(slot, tag)
		cause := fmt.Errorf("query execution fees for order %d: %w", tag.orderID, err)
		if err == nil {
			cause = fmt.Errorf("query execution fees for order %d returned no fill evidence", tag.orderID)
		}
		spm.requireTradeLedgerReconciliation(OrderUpdate{OrderID: tag.orderID, ClientOrderID: tag.clientOID, Symbol: tag.symbol}, cause)
		logger.Warn("⚠️ [手續費補充] 訂單 %d 查詢成交記錄失敗或不支援: %v", tag.orderID, cause)
		return
	}
	sum, n := summarizeFills(fillsRaw, spm.exchange.GetQuoteAsset(), spm.exchange.GetBaseAsset())
	if n == 0 {
		spm.markFeeSupplementUnverified(slot, tag)
		spm.requireTradeLedgerReconciliation(OrderUpdate{OrderID: tag.orderID, ClientOrderID: tag.clientOID, Symbol: tag.symbol}, fmt.Errorf("query execution fees for order %d returned an empty fill set", tag.orderID))
		logger.Warn("⚠️ [手續費補充] 訂單 %d 無成交記錄，费用保持未核实", tag.orderID)
		return
	}
	if !sum.valuationKnown {
		update := OrderUpdate{OrderID: tag.orderID, ClientOrderID: tag.clientOID, Symbol: tag.symbol}
		spm.markFeeSupplementUnverified(slot, tag)
		spm.requireTradeLedgerReconciliation(update, fmt.Errorf("execution fee in %s has no verified quote-asset conversion", sum.asset))
		spm.recordFeeCorrection(tag, sum, "成交手续费币种没有可验证的历史计价币换算")
		return
	}
	if !sum.valid {
		update := OrderUpdate{OrderID: tag.orderID, ClientOrderID: tag.clientOID, Symbol: tag.symbol}
		spm.markFeeSupplementUnverified(slot, tag)
		spm.requireTradeLedgerReconciliation(update, fmt.Errorf("execution fee or fill economics for order %d is outside finite limits", tag.orderID))
		spm.recordFeeCorrection(tag, sum, "成交费用或名义金额超出有限账务范围")
		return
	}
	if sum.commission == 0 && sum.baseFeeQty == 0 {
		logger.Debug("🔍 [手續費補充] 訂單 %d 手續費為 0", tag.orderID)
		return
	}

	logger.Info("💰 [手續費補充] 訂單 %d (開倉腿=%v) 補充手續費: %.8f %s, 基礎幣手續費: %.8f",
		tag.orderID, tag.openLeg, sum.commission, sum.asset, sum.baseFeeQty)

	if !tag.openLeg {
		spm.recordFeeCorrection(tag, sum, "平倉單推送未帶手續費，已保存的交易記錄手續費偏少")
		return
	}

	slot.mu.Lock()
	if slot.cycleGen != tag.cycleGen {
		slot.mu.Unlock()
		spm.recordFeeCorrection(tag, sum, "開倉單補查返回時該持倉週期已結束，買入手續費未計入已保存的交易記錄")
		return
	}
	nextBuyFee := slot.BuyFee + sum.commission
	if math.IsNaN(slot.BuyFee) || math.IsInf(slot.BuyFee, 0) || math.IsNaN(nextBuyFee) || math.IsInf(nextBuyFee, 0) {
		slot.feeValuationUnknown = true
		slot.mu.Unlock()
		update := OrderUpdate{OrderID: tag.orderID, ClientOrderID: tag.clientOID, Symbol: tag.symbol}
		spm.requireTradeLedgerReconciliation(update, fmt.Errorf("accumulated opening fee for order %d exceeds finite limits", tag.orderID))
		spm.recordFeeCorrection(tag, sum, "補查手續費加入持倉累計時溢出")
		return
	}
	slot.BuyFee = nextBuyFee
	if sum.commission != 0 && sum.asset != "" {
		slot.FeeAsset = sum.asset
	}
	spm.applySupplementBaseFeeLocked(slot, tag, sum)
	if slot.cycleGen == tag.cycleGen && slot.pendingFeeSupplementCount == 1 && slot.OrderID == 0 && slot.ClientOID == "" {
		slot.feeValuationUnknown = false
	}
	slot.mu.Unlock()
}

func (spm *SuperPositionManager) markFeeSupplementUnverified(slot *InventorySlot, tag feeSupplementTag) {
	if !tag.openLeg {
		return
	}
	slot.mu.Lock()
	if slot.cycleGen == tag.cycleGen && slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
		slot.feeValuationUnknown = true
	}
	slot.mu.Unlock()
}

// applySupplementBaseFeeLocked 現貨開倉買單：按補查得到的基礎幣手續費扣減持倉並向下取整，
// 均價按 fill_fee 口徑（成交價 × 淨到帳數量加權）修正。推送已扣過基礎幣手續費時跳過，避免重複扣減。
// 調用方需持有 slot.mu。
func (spm *SuperPositionManager) applySupplementBaseFeeLocked(slot *InventorySlot, tag feeSupplementTag, sum fillFeeSummary) {
	if sum.baseFeeQty <= 0 || tag.side != "BUY" || !spm.isSpot() {
		return
	}
	if tag.wsBaseFeeQty > 0 {
		logger.Debug("🔍 [手續費補充] 訂單 %d 推送已扣除基礎幣手續費 %.8f，跳過重複扣減", tag.orderID, tag.wsBaseFeeQty)
		return
	}
	qtyBefore := slot.PositionQty
	if qtyBefore <= 0 {
		return
	}
	fee := math.Min(sum.baseFeeQty, qtyBefore)
	netQty := qtyBefore - fee
	if netQty > 0 && slot.AvgBuyPrice > 0 && sum.qty > 0 {
		// 原均價按毛數量計入本單：(舊成本 + p×d)/(舊量 + d)；改為淨數量：減去 p×fee 並除以淨總量
		fillPx := sum.notional / sum.qty
		if adjusted := (slot.AvgBuyPrice*qtyBefore - fillPx*fee) / netQty; adjusted > 0 {
			slot.AvgBuyPrice = adjusted
		}
	}
	slot.PositionQty = math.Max(floorToDecimals(netQty, spm.quantityDecimals), 0)
	if slot.OrderID != 0 || slot.ClientOID != "" {
		logger.Warn("⚠️ [現貨基礎幣手續費] 槽位 %s 補查返回時已有在途訂單 %s，其數量可能按扣費前持倉 %.8f 計算",
			formatPrice(slot.Price, spm.priceDecimals), slot.ClientOID, qtyBefore)
	}
	logger.Info("🪙 [現貨基礎幣手續費] 槽位 %s 補查扣減: 持倉 %.8f → %.8f (基礎幣手續費 %.8f)",
		formatPrice(slot.Price, spm.priceDecimals), qtyBefore, slot.PositionQty, sum.baseFeeQty)
}

// recordFeeCorrection 補查手續費無法寫回已保存的交易記錄：存儲支持事件時寫入更正記錄，否則明確告警。
func (spm *SuperPositionManager) recordFeeCorrection(tag feeSupplementTag, sum fillFeeSummary, reason string) {
	// 事件記錄不是已實現交易帳本的修正。對帳完成前，若仍允許新增風險，
	// 熔斷器會按偏低的費用計算淨盈虧；因此持久化經濟對帳鎖，等待人工核驗。
	spm.requireTradeLedgerReconciliation(
		OrderUpdate{OrderID: tag.orderID, ClientOrderID: tag.clientOID, Symbol: tag.symbol},
		fmt.Errorf("unapplied execution fee correction (%s, %.8f %s): %s", tag.side, sum.commission, sum.asset, reason),
	)
	leg := "close"
	if tag.openLeg {
		leg = "open"
	}
	data := map[string]interface{}{
		"bot_id":          spm.botID,
		"exchange":        spm.exchangeName,
		"market_type":     spm.config.Trading.MarketType,
		"symbol":          tag.symbol,
		"order_id":        tag.orderID,
		"client_order_id": tag.clientOID,
		"side":            tag.side,
		"leg":             leg,
		"fee":             sum.commission,
		"fee_asset":       sum.asset,
		"base_fee_qty":    sum.baseFeeQty,
		"executed_qty":    sum.qty,
		"reason":          reason,
	}
	if writer, ok := spm.tradeStorage.(interface {
		SaveTradeFeeCorrection(*storage.TradeFeeCorrection) (string, error)
	}); ok {
		correction := &storage.TradeFeeCorrection{
			Exchange: spm.exchangeName, Symbol: tag.symbol, OrderID: tag.orderID,
			ClientOrderID: tag.clientOID, Leg: leg, Side: tag.side,
			Fee: sum.commission, FeeAsset: sum.asset, BaseFeeQty: sum.baseFeeQty, Reason: reason,
			ExecutedQty: sum.qty,
			CreatedAt:   spm.now(),
		}
		correctionID, err := writer.SaveTradeFeeCorrection(correction)
		if err != nil {
			logger.Error("🚨 [手續費更正] 訂單 %d 持久化核賬項失敗，重啟後風險鎖可能無法恢復: %v", tag.orderID, err)
		} else {
			data["correction_id"] = correctionID
		}
	}
	if st, ok := spm.tradeStorage.(interface {
		SaveEvent(eventType string, data map[string]interface{}) error
	}); ok {
		if err := st.SaveEvent(tradeFeeCorrectionEvent, data); err == nil {
			logger.Info("🧾 [手續費更正] 訂單 %d (%s) 手續費 %.8f %s 已記錄更正: %s", tag.orderID, leg, sum.commission, sum.asset, reason)
			return
		} else {
			logger.Warn("⚠️ [手續費更正] 訂單 %d 更正記錄保存失敗: %v", tag.orderID, err)
		}
	}
	logger.Warn("⚠️ [手續費更正] 訂單 %d (%s, %s) 手續費 %.8f %s 未計入交易記錄（存儲不支持按訂單更新）: %s",
		tag.orderID, leg, tag.symbol, sum.commission, sum.asset, reason)
}
