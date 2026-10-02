package position

import (
	"context"
	"fmt"
	"time"
)

const gridFlatnessSlotLockRetryInterval = 5 * time.Millisecond

// GridRuntimeStateIsVerifiedEmptyContext reads the same inventory/cost/fee
// cursors as the legacy predicate. It never restores or clears any slot.
// Callers still need submission barriers and authoritative venue evidence.
func (spm *SuperPositionManager) GridRuntimeStateIsVerifiedEmptyContext(ctx context.Context) (bool, error) {
	if ctx == nil || spm == nil {
		return false, fmt.Errorf("grid flatness verification requires context and manager")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !spm.gridRuntimeStateRestored.Load() && !spm.gridRuntimeVenueFlatVerified.Load() {
		return false, nil
	}
	empty := true
	var proofErr error
	spm.slots.Range(func(_, value any) bool {
		if proofErr = ctx.Err(); proofErr != nil {
			return false
		}
		slot, ok := value.(*InventorySlot)
		if !ok || slot == nil {
			proofErr = fmt.Errorf("grid flatness slot identity is invalid")
			return false
		}
		if proofErr = lockGridFlatnessSlot(ctx, slot); proofErr != nil {
			return false
		}
		empty = gridSlotIsVerifiedEmpty(slot)
		slot.mu.RUnlock()
		return empty
	})
	if proofErr != nil {
		return false, proofErr
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return empty, nil
}

func lockGridFlatnessSlot(ctx context.Context, slot *InventorySlot) error {
	var retry *time.Ticker
	defer func() {
		if retry != nil {
			retry.Stop()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if slot.mu.TryRLock() {
			if err := ctx.Err(); err != nil {
				slot.mu.RUnlock()
				return err
			}
			return nil
		}
		if retry == nil {
			retry = time.NewTicker(gridFlatnessSlotLockRetryInterval)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-retry.C:
		}
	}
}

// Caller holds slot.mu.RLock.
func gridSlotIsVerifiedEmpty(slot *InventorySlot) bool {
	return slot.PositionStatus == PositionStatusEmpty && slot.PositionQty == 0 &&
		slot.SlotStatus == SlotStatusFree &&
		slot.OrderID == 0 && slot.ClientOID == "" && slot.OrderFilledQty == 0 && slot.OrderFilledNotional == 0 &&
		(slot.OrderStatus == OrderStatusNotPlaced || slot.OrderStatus == OrderStatusCanceled) &&
		slot.BuyFee == 0 && slot.AllocatedMargin == 0 && slot.AvgBuyPrice == 0 &&
		slot.orderCommission == 0 && slot.orderBaseFeeQty == 0 && !slot.orderFeeIncomplete && !slot.feeValuationUnknown &&
		slot.feeSupplementUntil.IsZero() && slot.pendingFeeSupplementCount == 0 &&
		!slot.baseFeeUnfloored && !slot.baseFeeReconciliationRequired && !slot.CostBasisUnverified && slot.PositionLeg == PositionLegNone
}
