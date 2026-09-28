package position

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"
)

const liquidationBlockSource = "liquidation_unverified"

const liquidationGridLockPoll = 10 * time.Millisecond

// Do not start the timeout after acquiring the grid lock: a prior slow tick can
// hold it across venue calls. Waiting for that tick is part of this operation.
func (spm *SuperPositionManager) awaitLiquidationGridIdle(ctx context.Context) error {
	ticker := time.NewTicker(liquidationGridLockPoll)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("等待網格在途操作超時/取消: %w", err)
		}
		if spm.mu.TryLock() {
			spm.mu.Unlock()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待網格在途操作超時/取消: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

type liquidationExistingOrder struct {
	id     int64
	cid    string
	side   string
	filled float64
	price  float64
}

// Before replacing any opening/protective order, establish its terminal state
// and apply late fills. Cancel acknowledgement alone cannot free its inventory.
func (spm *SuperPositionManager) prepareLiquidationSlots(ctx context.Context, venue LiquidationVenue) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := spm.openingGate.Drain(ctx); err != nil {
		return fmt.Errorf("開倉請求尚未排空: %w", err)
	}
	var owned []liquidationExistingOrder
	var unknown bool
	spm.slots.Range(func(_, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		defer slot.mu.RUnlock()
		if slot.OrderStatus == OrderStatusUnknown || (slot.SlotStatus == SlotStatusPending && slot.OrderID == 0) {
			unknown = true
		}
		if slot.OrderID > 0 {
			owned = append(owned, liquidationExistingOrder{id: slot.OrderID, cid: slot.ClientOID,
				side: slot.OrderSide, filled: slot.OrderFilledQty, price: slot.OrderPrice})
		}
		return true
	})
	if unknown {
		return fmt.Errorf("存在 UNKNOWN/在途槽位，核實前不能覆蓋訂單歸屬")
	}
	if owner, ok := spm.executor.(interface{ CancelOwnedOpeningOrders(context.Context) error }); ok {
		if err := owner.CancelOwnedOpeningOrders(ctx); err != nil {
			return fmt.Errorf("本 Bot 開倉單未確認終止: %w", err)
		}
	}
	symbol := spm.config.Trading.Symbol
	for _, o := range owned {
		qctx, cancel := venueCallContext(ctx)
		st, queryErr := venue.GetOrderState(qctx, symbol, o.id)
		cancel()
		if queryErr != nil || !isLiquidationTerminalStatus(st.Status) {
			cctx, ccancel := venueCallContext(ctx)
			cancelErr := venue.CancelOrder(cctx, symbol, o.id)
			ccancel()
			qctx, cancel = venueCallContext(ctx)
			st, queryErr = venue.GetOrderState(qctx, symbol, o.id)
			cancel()
			if queryErr != nil || !isLiquidationTerminalStatus(st.Status) {
				return fmt.Errorf("替換舊單 #%d 前未確認撤單終態（%s）: %w", o.id, st.Status,
					errors.Join(cancelErr, queryErr, fmt.Errorf("needs reconciliation")))
			}
		}
		if math.IsNaN(st.ExecutedQty) || math.IsInf(st.ExecutedQty, 0) || st.ExecutedQty < o.filled || st.ExecutedQty < 0 ||
			(normalizeOrderStatus(st.Status) == OrderStatusFilled && st.ExecutedQty == 0) {
			return fmt.Errorf("舊單 #%d 終態成交量無效或倒退，禁止補單", o.id)
		}
		if st.ExecutedQty > o.filled && !positiveFinite(st.AvgPrice) {
			return fmt.Errorf("舊單 #%d 有新增成交但缺少實際均價，先核賬再補單", o.id)
		}
		spm.OnOrderUpdate(OrderUpdate{OrderID: o.id, ClientOrderID: o.cid, Symbol: symbol, Side: o.side,
			Status: st.Status, ExecutedQty: st.ExecutedQty, AvgPrice: st.AvgPrice, Price: o.price})
	}
	return nil
}
