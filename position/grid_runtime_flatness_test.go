package position

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestGridFlatnessSlotLockWaitRespectsDeadline(t *testing.T) {
	spm := &SuperPositionManager{}
	spm.MarkGridRuntimeVenueFlatVerified()
	slot := &InventorySlot{PositionStatus: PositionStatusEmpty, SlotStatus: SlotStatusFree, OrderStatus: OrderStatusNotPlaced, PositionQty: 1}
	spm.slots.Store(100.0, slot)
	slot.mu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := spm.GridRuntimeStateIsVerifiedEmptyContext(ctx)
		done <- err
	}()
	var err error
	select {
	case err = <-done:
		slot.mu.Unlock()
	case <-time.After(time.Second):
		slot.mu.Unlock()
		err = <-done
		t.Error("grid slot proof remained blocked after deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) || slot.PositionQty != 1 {
		t.Fatalf("cancelled grid proof: err=%v quantity=%v", err, slot.PositionQty)
	}
	if spm.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("occupied slot accepted as flat after cancellation")
	}
	slot.mu.Lock()
	slot.PositionQty = 0 // Test-only reconciliation; the proof must not clear it.
	slot.mu.Unlock()
	if !spm.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("legitimate empty state could not recover")
	}
}

func TestGridFlatnessRetainsAccountingGuards(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*InventorySlot)
	}{
		{"position status", func(s *InventorySlot) { s.PositionStatus = PositionStatusFilled }},
		{"quantity", func(s *InventorySlot) { s.PositionQty = 1 }},
		{"invalid quantity", func(s *InventorySlot) { s.PositionQty = math.NaN() }},
		{"slot status", func(s *InventorySlot) { s.SlotStatus = SlotStatusLocked }},
		{"order ID", func(s *InventorySlot) { s.OrderID = 1 }},
		{"client ID", func(s *InventorySlot) { s.ClientOID = "pending" }},
		{"fill quantity", func(s *InventorySlot) { s.OrderFilledQty = 1 }},
		{"fill notional", func(s *InventorySlot) { s.OrderFilledNotional = 1 }},
		{"order status", func(s *InventorySlot) { s.OrderStatus = "NEW" }},
		{"buy fee", func(s *InventorySlot) { s.BuyFee = 1 }},
		{"margin", func(s *InventorySlot) { s.AllocatedMargin = 1 }},
		{"cost", func(s *InventorySlot) { s.AvgBuyPrice = 1 }},
		{"invalid cost", func(s *InventorySlot) { s.AvgBuyPrice = math.Inf(1) }},
		{"commission", func(s *InventorySlot) { s.orderCommission = 1 }},
		{"base fee", func(s *InventorySlot) { s.orderBaseFeeQty = 1 }},
		{"incomplete fee", func(s *InventorySlot) { s.orderFeeIncomplete = true }},
		{"fee valuation", func(s *InventorySlot) { s.feeValuationUnknown = true }},
		{"fee window", func(s *InventorySlot) { s.feeSupplementUntil = time.Now() }},
		{"fee supplement", func(s *InventorySlot) { s.pendingFeeSupplementCount = 1 }},
		{"base fee unfloored", func(s *InventorySlot) { s.baseFeeUnfloored = true }},
		{"base fee reconciliation", func(s *InventorySlot) { s.baseFeeReconciliationRequired = true }},
		{"unverified cost", func(s *InventorySlot) { s.CostBasisUnverified = true }},
		{"position leg", func(s *InventorySlot) { s.PositionLeg = PositionLegLong }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spm := &SuperPositionManager{}
			spm.MarkGridRuntimeVenueFlatVerified()
			slot := &InventorySlot{PositionStatus: PositionStatusEmpty, SlotStatus: SlotStatusFree, OrderStatus: OrderStatusNotPlaced}
			tc.edit(slot)
			spm.slots.Store(100.0, slot)
			if empty, err := spm.GridRuntimeStateIsVerifiedEmptyContext(t.Context()); empty || err != nil {
				t.Fatalf("unsettled %s accepted or unreadable: empty=%v err=%v", tc.name, empty, err)
			}
			if spm.GridRuntimeStateIsVerifiedEmpty() {
				t.Fatalf("legacy predicate ignored %s", tc.name)
			}
		})
	}
}

func TestGridFlatnessContextRejectsInvalidInputsAndSlots(t *testing.T) {
	spm := &SuperPositionManager{}
	spm.MarkGridRuntimeVenueFlatVerified()
	if empty, err := spm.GridRuntimeStateIsVerifiedEmptyContext(nil); empty || err == nil {
		t.Fatalf("nil context: %v, %v", empty, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if empty, err := spm.GridRuntimeStateIsVerifiedEmptyContext(ctx); empty || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: %v, %v", empty, err)
	}
	var missing *SuperPositionManager
	if empty, err := missing.GridRuntimeStateIsVerifiedEmptyContext(t.Context()); empty || err == nil {
		t.Fatalf("nil manager: %v, %v", empty, err)
	}
	for _, value := range []any{nil, (*InventorySlot)(nil), "not a slot"} {
		spm.slots.Store(100.0, value)
		if empty, err := spm.GridRuntimeStateIsVerifiedEmptyContext(t.Context()); empty || err == nil {
			t.Fatalf("invalid slot %T: %v, %v", value, empty, err)
		}
		if spm.GridRuntimeStateIsVerifiedEmpty() {
			t.Fatal("legacy predicate accepted invalid slot")
		}
	}
}
