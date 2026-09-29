package position

import (
	"testing"
	"time"
)

func TestUnknownRetainsSlotAndReservationUntilAuthoritativeUpdate(t *testing.T) {
	for _, tc := range []struct{ direction, side string }{{"LONG", "BUY"}, {"SHORT", "SELL"}, {"BOTH", "BUY"}, {"BOTH", "SELL"}} {
		t.Run(tc.direction+tc.side, func(t *testing.T) {
			spm, _ := newReservationTestSPM(t, tc.direction)
			cid := spm.generateClientOrderID(reservationTestPrice, tc.side, "")
			req := &OrderRequest{Symbol: "BTCUSDT", Side: tc.side, Price: reservationTestPrice, Quantity: reservationTestQty, ClientOrderID: cid}
			if _, err := spm.reserveOrderAllocation(req, 1, 0); err != nil {
				t.Fatal(err)
			}
			slot := spm.getOrCreateSlot(reservationTestPrice)
			slot.SlotStatus = SlotStatusPending
			spm.retainUnknownOrders([]*OrderRequest{req}, map[string]bool{cid: true})
			if slot.SlotStatus != SlotStatusLocked || slot.OrderStatus != OrderStatusUnknown || slot.ClientOID != cid || !spm.IsOpeningPaused() {
				t.Fatalf("unknown slot released: %+v", slot)
			}
			assertUsed(t, spm, 500)
			spm.OnOrderUpdate(OrderUpdate{OrderID: 12, ClientOrderID: cid, Symbol: "BTCUSDT", Side: tc.side, Status: "PARTIALLY_FILLED", ExecutedQty: 0.004, AvgPrice: reservationTestPrice})
			spm.OnOrderUpdate(OrderUpdate{OrderID: 12, ClientOrderID: cid, Symbol: "BTCUSDT", Side: tc.side, Status: "CANCELED", ExecutedQty: 0.004, AvgPrice: reservationTestPrice})
			assertUsed(t, spm, 200) // actual filled inventory still consumes capital
		})
	}
}

type unknownBatchExecutor struct{ MockExecutor }

func (e *unknownBatchExecutor) BatchPlaceOrdersWithDetails(reqs []*OrderRequest) *BatchPlaceOrdersResult {
	e.PlacedOrders = append(e.PlacedOrders, reqs...)
	r := &BatchPlaceOrdersResult{UnknownOrders: make(map[string]bool)}
	for _, req := range reqs {
		r.UnknownOrders[req.ClientOrderID] = true
	}
	return r
}

func TestGridBatchUnknownDoesNotFreeSlots(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT", "BOTH"} {
		t.Run(direction, func(t *testing.T) {
			spm, _ := newR5bSPM(t, direction, 2)
			exec := &unknownBatchExecutor{}
			spm.executor = exec
			if err := spm.AdjustOrders(100); err != nil {
				t.Fatal(err)
			}
			if len(exec.PlacedOrders) == 0 {
				t.Fatal("fixture did not attempt orders")
			}
			for _, req := range exec.PlacedOrders {
				price, _, ok := spm.parseClientOrderID(req.ClientOrderID)
				if !ok {
					t.Fatal("invalid test CID")
				}
				slot := spm.getOrCreateSlot(price)
				if slot.SlotStatus != SlotStatusLocked || slot.OrderStatus != OrderStatusUnknown {
					t.Fatalf("batch rollback freed UNKNOWN: %+v", slot)
				}
			}
		})
	}
}

func TestUnknownLiquidationIsNotRetriedOrOverwrittenBySnapshot(t *testing.T) {
	exec := &unknownBatchExecutor{}
	spm := newDirectionTestSPM(t, "LONG", exec)
	spm.lastMarketPrice.Store(100.0)
	slot := fillSlot(spm, 100, 1, 100, "")
	spm.LiquidateAll()
	if slot.OrderStatus != OrderStatusUnknown || slot.SlotStatus != SlotStatusLocked || len(exec.PlacedOrders) != 1 {
		t.Fatalf("unknown close released: status=%s slot=%s calls=%d", slot.OrderStatus, slot.SlotStatus, len(exec.PlacedOrders))
	}
	if err := spm.ForceSyncPositions(0); err == nil { // a temporarily empty snapshot is not order evidence
		t.Fatal("ForceSyncPositions() should report the UNKNOWN-order rejection")
	}
	if slot.PositionQty != 1 || slot.OrderStatus != OrderStatusUnknown {
		t.Fatal("snapshot erased uncertain inventory/identity")
	}
	spm.ResumeOpening()
	if !spm.IsOpeningPaused() || spm.GetOpeningPauseReason() == "" {
		t.Fatal("manual resume hid the independent UNKNOWN block")
	}
	spm.LiquidateAll()
	// No venue method may be called for a market fallback while the prior
	// close outcome is unknown; nil embedded methods would panic if reached.
	if err := spm.LiquidateAllVerified(t.Context(), struct{ LiquidationVenue }{}, time.Second); err == nil {
		t.Fatal("unknown close was reported verified")
	}
	if len(exec.PlacedOrders) != 1 {
		t.Fatal("unknown close was blindly resubmitted")
	}
}

func TestBothCancellationIncludesPartialOpeningButNotProtectiveClose(t *testing.T) {
	exec := &MockExecutor{}
	spm := newDirectionTestSPM(t, "BOTH", exec)
	opening := fillSlot(spm, 100, 0.4, 100, PositionLegLong)
	opening.OrderID, opening.OrderSide, opening.OrderStatus = 1, "BUY", OrderStatusPartiallyFilled
	protective := fillSlot(spm, 110, 1, 110, PositionLegLong)
	protective.OrderID, protective.OrderSide, protective.OrderStatus = 2, "SELL", OrderStatusPlaced
	spm.CancelAllOpenOrders()
	if len(exec.CancelledOrderIDs) != 1 || exec.CancelledOrderIDs[0] != 1 {
		t.Fatalf("partial opening/protective close classification: %v", exec.CancelledOrderIDs)
	}
}
