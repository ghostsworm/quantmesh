package position

import (
	"context"
	"math"
	"reflect"
	"strconv"
	"time"

	"quantmesh/logger"
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

// startFeeSupplementLocked 訂單結束且推送未帶手續費時發起一次異步補查。
// 現貨開倉買單在補查返回（或超時）前暫緩掛平倉單，保證平倉數量使用扣除基礎幣手續費後的淨持倉。
// 調用方需持有 slot.mu。
func (spm *SuperPositionManager) startFeeSupplementLocked(slot *InventorySlot, update OrderUpdate, clientOID, side string, openLeg bool) {
	st := slot.takeOrderFeeStateLocked(clientOID)
	if !st.missing || update.OrderID <= 0 || spm.exchange == nil {
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
	// 回放時鐘下不暫緩：補查協程與 tick 推進的先後不確定，暫緩會破壞回測可重現性（模擬交易所回報已帶手續費）
	if _, real := spm.clk.get().(realClock); real && openLeg && side == "BUY" && spm.isSpot() {
		slot.feeSupplementUntil = time.Now().Add(feeSupplementCloseHold)
	}
	go spm.supplementCommission(context.Background(), slot, tag)
}

// fillFeeSummary 成交明細匯總
type fillFeeSummary struct {
	commission float64 // 計價幣口徑手續費合計
	asset      string
	baseFeeQty float64 // 基礎幣扣收的手續費數量合計
	notional   float64 // Σ price×qty
	qty        float64 // Σ qty
}

// summarizeFills 解析適配層返回的成交明細（[]*exchange.OrderFill 等具體類型切片或 []map）並匯總手續費
func summarizeFills(fillsRaw interface{}) (fillFeeSummary, int) {
	sum := fillFeeSummary{asset: defaultFeeAsset}
	// 適配層返回的是具體類型切片（如 []*exchange.OrderFill），不能直接斷言為 []interface{}，用反射展開
	fills := interfaceSliceOf(fillsRaw)
	for _, fillRaw := range fills {
		var price, qty float64
		if fillMap, ok := fillRaw.(map[string]interface{}); ok {
			sum.commission += mapFloat(fillMap, "Commission")
			if asset, ok := fillMap["CommissionAsset"].(string); ok && asset != "" {
				sum.asset = asset
			}
			if v := mapFloat(fillMap, "BaseFeeQty"); v > 0 {
				sum.baseFeeQty += v
			}
			price, qty = mapFloat(fillMap, "Price"), mapFloat(fillMap, "Quantity")
		} else {
			rv := reflect.ValueOf(fillRaw)
			if rv.Kind() == reflect.Ptr {
				rv = rv.Elem()
			}
			if rv.Kind() != reflect.Struct {
				continue
			}
			sum.commission += structFloat(rv, "Commission")
			if f := rv.FieldByName("CommissionAsset"); f.IsValid() && f.Kind() == reflect.String && f.String() != "" {
				sum.asset = f.String()
			}
			if v := structFloat(rv, "BaseFeeQty"); v > 0 {
				sum.baseFeeQty += v
			}
			price, qty = structFloat(rv, "Price"), structFloat(rv, "Quantity")
		}
		if price > 0 && qty > 0 {
			sum.notional += price * qty
			sum.qty += qty
		}
	}
	return sum, len(fills)
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
		if tag.openLeg {
			slot.mu.Lock()
			if slot.cycleGen == tag.cycleGen {
				slot.feeSupplementUntil = time.Time{}
			}
			slot.mu.Unlock()
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, feeSupplementTimeout)
	defer cancel()
	fillsRaw, err := spm.exchange.GetOrderFills(ctx, tag.symbol, tag.orderID)
	if err != nil || fillsRaw == nil {
		logger.Debug("🔍 [手續費補充] 訂單 %d 查詢成交記錄失敗或不支援: %v", tag.orderID, err)
		return
	}
	sum, n := summarizeFills(fillsRaw)
	if n == 0 {
		logger.Debug("🔍 [手續費補充] 訂單 %d 無成交記錄", tag.orderID)
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
	slot.BuyFee += sum.commission
	if sum.commission != 0 && sum.asset != "" {
		slot.FeeAsset = sum.asset
	}
	spm.applySupplementBaseFeeLocked(slot, tag, sum)
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
	leg := "close"
	if tag.openLeg {
		leg = "open"
	}
	data := map[string]interface{}{
		"bot_id":          spm.botID,
		"exchange":        spm.exchangeName,
		"symbol":          tag.symbol,
		"order_id":        tag.orderID,
		"client_order_id": tag.clientOID,
		"side":            tag.side,
		"leg":             leg,
		"fee":             sum.commission,
		"fee_asset":       sum.asset,
		"base_fee_qty":    sum.baseFeeQty,
		"reason":          reason,
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
