package position

import (
	"context"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// leverageCacheRefreshInterval 槓桿倍數緩存的刷新間隔。
// 只有下單調整路徑（不持有任何槽位鎖）才會在緩存過期時發起 REST 刷新；
// WS 成交/撤單回調一律只讀緩存，避免持 slot.mu 做網絡請求（E4）。
const leverageCacheRefreshInterval = 5 * time.Minute

// reduceOnlyCooldownDuration reduce-only 被拒後該槽位暫停下平倉單的冷卻期
const reduceOnlyCooldownDuration = 2 * time.Minute

// allocationFillEpsilon 判斷數量已完全成交/已清空的容差
const allocationFillEpsilon = 1e-9

// orderReservation 單筆開倉訂單的資金預留（按 ClientOrderID 記賬）
type orderReservation struct {
	amount    float64 // 下單時預留的保證金（USDT）
	quantity  float64 // 訂單數量
	converted float64 // 已隨成交轉為持倉占用的金額
}

// allocationReservations 以去掉券商前綴後的 ClientOrderID 為 key 的預留表
type allocationReservations struct {
	mu          sync.Mutex
	byClientOID map[string]*orderReservation
}

// leverageCache 槓桿倍數緩存
type leverageCache struct {
	mu        sync.RWMutex
	value     int
	updatedAt time.Time
}

// reservationKey 統一 ClientOrderID（下單請求、下單回執與 WS 推送可能帶不同的券商前綴）
func (spm *SuperPositionManager) reservationKey(clientOrderID string) string {
	if clientOrderID == "" || spm.exchange == nil {
		return clientOrderID
	}
	return utils.RemoveBrokerPrefix(strings.ToLower(spm.exchange.GetName()), clientOrderID)
}

// isOpenLegOrderSide 判斷訂單方向是否為開倉腿（調用方持有 slot.mu 讀鎖或寫鎖）。
// LONG: BUY=開倉；SHORT: SELL=開倉；BOTH: 依槽位 PositionLeg 判斷。
func (spm *SuperPositionManager) isOpenLegOrderSide(side string, slot *InventorySlot) bool {
	if spm.isBoth() {
		return bothSideIsOpen(side, slot)
	}
	openSide := "BUY"
	if spm.isShort() {
		openSide = "SELL"
	}
	return side == openSide
}

// isOpeningOrderRequest 判斷下單請求是否為開倉腿（需要占用資金分配）。
// ReduceOnly 一律視為平倉；單向模式按方向判斷開倉方向；BOTH 按槽位當前腿判斷。
func (spm *SuperPositionManager) isOpeningOrderRequest(req *OrderRequest) bool {
	if req == nil || req.ReduceOnly {
		return false
	}
	if spm.isBoth() {
		price, side, valid := spm.parseClientOrderID(req.ClientOrderID)
		if !valid {
			return true // 無法識別時保守按開倉處理（占用額度）
		}
		if side == "" {
			side = req.Side
		}
		slot := spm.getOrCreateSlot(price)
		slot.mu.RLock()
		isOpen := bothSideIsOpen(side, slot)
		slot.mu.RUnlock()
		return isOpen
	}
	openSide := "BUY"
	if spm.isShort() {
		openSide = "SELL"
	}
	return req.Side == openSide
}

// reserveOrderAllocation 為開倉單預留資金並按 ClientOrderID 記賬；平倉單不受資金分配限制。
// 返回 (本次預留金額, error)。調用方不得持有槽位鎖。
func (spm *SuperPositionManager) reserveOrderAllocation(req *OrderRequest, leverage int, accountBalance float64) (float64, error) {
	if !spm.isOpeningOrderRequest(req) {
		return 0, nil
	}
	if leverage <= 0 || spm.isSpot() {
		leverage = 1
	}
	amount := req.Quantity * req.Price / float64(leverage)
	if err := spm.allocationManager.CheckAndReserve(spm.exchangeName, spm.config.Trading.Symbol, amount, accountBalance); err != nil {
		return amount, err
	}
	key := spm.reservationKey(req.ClientOrderID)
	if key == "" {
		return amount, nil
	}
	spm.allocReservations.mu.Lock()
	if spm.allocReservations.byClientOID == nil {
		spm.allocReservations.byClientOID = make(map[string]*orderReservation)
	}
	if old, ok := spm.allocReservations.byClientOID[key]; ok {
		// 同一 ClientOrderID 重複預留：先歸還舊的未轉換部分，避免重複占用
		spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, old.amount-old.converted)
	}
	spm.allocReservations.byClientOID[key] = &orderReservation{amount: amount, quantity: req.Quantity}
	spm.allocReservations.mu.Unlock()
	return amount, nil
}

// releaseOrderReservation 撤單/拒單/過期/下單失敗時，歸還該訂單尚未轉為持倉的預留金額。
// 可在持有槽位鎖時調用（只涉及內存鎖）。返回歸還金額。
func (spm *SuperPositionManager) releaseOrderReservation(clientOrderID string) float64 {
	key := spm.reservationKey(clientOrderID)
	if key == "" {
		return 0
	}
	spm.allocReservations.mu.Lock()
	res, ok := spm.allocReservations.byClientOID[key]
	if ok {
		delete(spm.allocReservations.byClientOID, key)
	}
	spm.allocReservations.mu.Unlock()
	if !ok {
		return 0
	}
	remaining := res.amount - res.converted
	if remaining > 0 {
		spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, remaining)
	}
	return remaining
}

// takeOrderReservationForFill 開倉單成交 deltaQty 時，把對應比例的預留轉為持倉占用；
// final=true（FILLED）時轉換全部剩餘預留並刪除記錄。
// 返回 (轉換金額, 是否存在預留記錄)。
func (spm *SuperPositionManager) takeOrderReservationForFill(clientOrderID string, deltaQty float64, final bool) (float64, bool) {
	key := spm.reservationKey(clientOrderID)
	if key == "" {
		return 0, false
	}
	spm.allocReservations.mu.Lock()
	defer spm.allocReservations.mu.Unlock()
	res, ok := spm.allocReservations.byClientOID[key]
	if !ok {
		return 0, false
	}
	remaining := res.amount - res.converted
	share := remaining
	if !final && res.quantity > 0 {
		share = math.Min(remaining, res.amount*deltaQty/res.quantity)
	}
	if share < 0 {
		share = 0
	}
	res.converted += share
	if final {
		delete(spm.allocReservations.byClientOID, key)
	}
	return share, true
}

// applyOpeningFillAllocationLocked 開倉成交後更新槽位資金占用（調用方持有 slot.mu）。
// 有預留記錄時轉換預留；無預留記錄（如非網格路徑下的單）時按緩存槓桿補記占用，保證平倉時可對稱釋放。
func (spm *SuperPositionManager) applyOpeningFillAllocationLocked(slot *InventorySlot, clientOrderID string, deltaQty, fillPrice float64, final bool) {
	share, tracked := spm.takeOrderReservationForFill(clientOrderID, deltaQty, final)
	if !tracked && deltaQty > 0 && fillPrice > 0 {
		share = deltaQty * fillPrice / float64(spm.cachedLeverage())
		spm.allocationManager.Consume(spm.exchangeName, spm.config.Trading.Symbol, share)
	}
	if share > 0 {
		slot.AllocatedMargin += share
	}
}

// releaseSlotAllocationLocked 平倉成交 reducedQty 後按比例釋放槽位持倉占用（調用方持有 slot.mu）。
// qtyBefore 為減倉前持倉；持倉清空時釋放全部。
func (spm *SuperPositionManager) releaseSlotAllocationLocked(slot *InventorySlot, reducedQty, qtyBefore float64) float64 {
	if slot.AllocatedMargin <= 0 {
		return 0
	}
	release := slot.AllocatedMargin
	if qtyBefore > allocationFillEpsilon && reducedQty < qtyBefore-allocationFillEpsilon {
		release = slot.AllocatedMargin * (reducedQty / qtyBefore)
	}
	if release <= 0 {
		return 0
	}
	slot.AllocatedMargin -= release
	if slot.AllocatedMargin < allocationFillEpsilon {
		slot.AllocatedMargin = 0
	}
	spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, release)
	return release
}

// cachedLeverage 返回緩存的槓桿倍數（不發起網絡請求；未初始化時為 1）
func (spm *SuperPositionManager) cachedLeverage() int {
	if spm.isSpot() {
		return 1
	}
	spm.leverage.mu.RLock()
	v := spm.leverage.value
	spm.leverage.mu.RUnlock()
	if v <= 0 {
		return 1
	}
	return v
}

func (spm *SuperPositionManager) storeLeverage(v int) {
	if v <= 0 {
		return
	}
	spm.leverage.mu.Lock()
	spm.leverage.value = v
	spm.leverage.updatedAt = time.Now()
	spm.leverage.mu.Unlock()
}

func (spm *SuperPositionManager) leverageCacheFresh() bool {
	spm.leverage.mu.RLock()
	defer spm.leverage.mu.RUnlock()
	return spm.leverage.value > 0 && time.Since(spm.leverage.updatedAt) < leverageCacheRefreshInterval
}

// resolveLeverage 下單路徑使用：優先解析已取得的帳戶信息；緩存未過期時直接用緩存；
// 否則查詢持倉刷新緩存。調用方不得持有槽位鎖。
func (spm *SuperPositionManager) resolveLeverage(ctx context.Context, accountResult interface{}) int {
	if spm.isSpot() {
		return 1
	}
	if lev := leverageFromAccount(accountResult); lev > 1 {
		spm.storeLeverage(lev)
		return lev
	}
	if spm.leverageCacheFresh() {
		return spm.cachedLeverage()
	}
	lev := 1
	if spm.exchange != nil {
		positions, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol)
		if err != nil {
			logger.Debug("🔍 [%s] [杠杆检测] 查詢持倉獲取杠杆失敗，沿用緩存: %v", spm.logPrefix(), err)
			return spm.cachedLeverage()
		}
		if posLev := leverageFromPositions(positions); posLev > 0 {
			lev = posLev
		}
	}
	spm.storeLeverage(lev)
	return lev
}

// leverageFromAccount 從帳戶結構的 AccountLeverage 字段解析槓桿（反射兼容多交易所類型）
func leverageFromAccount(accountResult interface{}) int {
	if accountResult == nil {
		return 0
	}
	v := reflect.ValueOf(accountResult)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return 0
	}
	if f := v.FieldByName("AccountLeverage"); f.IsValid() && f.CanInterface() {
		if lev, ok := f.Interface().(int); ok && lev > 0 {
			return lev
		}
	}
	return 0
}

// leverageFromPositions 從持倉列表的 Leverage 字段解析槓桿
func leverageFromPositions(positions interface{}) int {
	if positions == nil {
		return 0
	}
	v := reflect.ValueOf(positions)
	if v.Kind() != reflect.Slice {
		return 0
	}
	for i := 0; i < v.Len(); i++ {
		pv := v.Index(i)
		for pv.Kind() == reflect.Interface || pv.Kind() == reflect.Ptr {
			if pv.IsNil() {
				break
			}
			pv = pv.Elem()
		}
		if pv.Kind() != reflect.Struct {
			continue
		}
		if f := pv.FieldByName("Leverage"); f.IsValid() && f.CanInterface() {
			if lev, ok := f.Interface().(int); ok && lev > 0 {
				return lev
			}
		}
	}
	return 0
}
