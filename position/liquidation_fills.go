package position

import (
	"fmt"
	"math"
	"strings"

	"quantmesh/utils"
)

func (spm *SuperPositionManager) sameClientOrderID(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	venue := strings.ToLower(spm.exchangeName)
	return utils.AddBrokerPrefix(venue, a) == b || utils.AddBrokerPrefix(venue, b) == a
}

func (spm *SuperPositionManager) findSlotByClientOrderID(cid string) (*InventorySlot, float64, bool) {
	var found *InventorySlot
	var price float64
	spm.slots.Range(func(key, value interface{}) bool {
		s := value.(*InventorySlot)
		s.mu.RLock()
		matches := spm.sameClientOrderID(s.ClientOID, cid) || spm.sameClientOrderID(s.lastFilledClientOID, cid)
		s.mu.RUnlock()
		if matches {
			found, price = s, key.(float64)
		}
		return !matches
	})
	return found, price, found != nil
}

// Apply terminal cumulative fills through normal incremental accounting, never
// synthesizing an execution price from a limit or mark price.
func (spm *SuperPositionManager) applyLiquidationFill(p liquidationPlacedOrder, st LiquidationOrderState) error {
	if err := validateLiquidationFill(p, st); err != nil {
		return err
	}
	if st.ExecutedQty > 0 && !positiveFinite(st.AvgPrice) {
		return fmt.Errorf("訂單 #%d 已成交但缺少實際均價", p.orderID)
	}
	slot := spm.getOrCreateSlot(p.slotPrice)
	slot.mu.RLock()
	terminal := spm.sameClientOrderID(slot.lastFilledClientOID, p.clientOID)
	last := slot.lastTerminalFill
	active := spm.sameClientOrderID(slot.ClientOID, p.clientOID)
	filled := slot.OrderFilledQty
	slot.mu.RUnlock()
	if terminal {
		return validateLiquidationReceipt(p, st, last)
	}
	if !active {
		return fmt.Errorf("訂單 #%d 槽位歸屬已變更，禁止回填到其他訂單", p.orderID)
	}
	if st.ExecutedQty < filled-liquidationQtyEpsilon {
		return fmt.Errorf("訂單 #%d REST 成交量落後於已入賬推送", p.orderID)
	}
	spm.OnOrderUpdate(OrderUpdate{OrderID: p.orderID, ClientOrderID: p.clientOID,
		Symbol: spm.config.Trading.Symbol, Side: p.side, Status: st.Status,
		ExecutedQty: st.ExecutedQty, AvgPrice: st.AvgPrice})
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if !spm.sameClientOrderID(slot.lastFilledClientOID, p.clientOID) {
		return fmt.Errorf("訂單 #%d 終態未一致入賬", p.orderID)
	}
	return validateLiquidationReceipt(p, st, slot.lastTerminalFill)
}

func validateLiquidationReceipt(p liquidationPlacedOrder, st LiquidationOrderState, receipt FillProgress) error {
	if math.Abs(receipt.Quantity-st.ExecutedQty) > liquidationQtyEpsilon {
		return fmt.Errorf("訂單 #%d REST/已入賬終態成交量不一致", p.orderID)
	}
	if st.ExecutedQty > 0 && math.Abs(receipt.Notional-st.ExecutedQty*st.AvgPrice) > liquidationQtyEpsilon*math.Max(1, math.Abs(receipt.Notional)) {
		return fmt.Errorf("訂單 #%d REST/已入賬終態成交額不一致", p.orderID)
	}
	return nil
}

// A late REST acknowledgement cannot resurrect an early terminal WS or reset
// the partial-fill cursor that was advanced during the physical call.
func (spm *SuperPositionManager) acknowledgeLiquidationOrder(p liquidationPlacedOrder, ord *Order) error {
	slot := spm.getOrCreateSlot(p.slotPrice)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if spm.sameClientOrderID(slot.lastFilledClientOID, p.clientOID) {
		return nil
	}
	if !spm.sameClientOrderID(slot.ClientOID, p.clientOID) {
		return fmt.Errorf("訂單 #%d 回執與槽位意圖不匹配", ord.OrderID)
	}
	if slot.OrderID != 0 && slot.OrderID != ord.OrderID {
		return fmt.Errorf("訂單 #%d 回執與推送身份不匹配", ord.OrderID)
	}
	slot.OrderID = ord.OrderID
	if slot.OrderStatus != OrderStatusConfirmed && slot.OrderStatus != OrderStatusPartiallyFilled && slot.OrderStatus != OrderStatusUnknown {
		slot.OrderStatus = OrderStatusPlaced
	}
	slot.SlotStatus = SlotStatusLocked
	return nil
}
