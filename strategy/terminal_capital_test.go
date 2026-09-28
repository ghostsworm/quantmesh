package strategy

import (
	"testing"

	"quantmesh/position"
)

func TestTerminalOnlyFillRetainsCapitalAndReleasesClosedPortion(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT"} {
		for _, status := range []string{"CANCELED", "CANCELLED", "EXPIRED", "REJECTED"} {
			t.Run(direction+"/"+status, func(t *testing.T) {
				mse, allocator := newCapitalTestExecutor(t, direction)
				openSide, closeSide := "BUY", "SELL"
				if direction == "SHORT" {
					openSide, closeSide = closeSide, openSide
				}
				trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: openSide, Quantity: 2, ClientOrderID: "open"}, 1, 200)
				update := &position.OrderUpdate{OrderID: 1, ClientOrderID: "open", Status: status, ExecutedQty: 1}
				mse.OnOrderUpdate(update)
				mse.OnOrderUpdate(update)
				assertStrategyUsed(t, allocator, 100)
				if got := mse.GetPositionCapital("dca", direction); got != 100 {
					t.Fatalf("terminal-only opening fill lost: %v", got)
				}
				trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: closeSide, Quantity: 1, ReduceOnly: true, ClientOrderID: "close"}, 2, 0)
				update = &position.OrderUpdate{OrderID: 2, ClientOrderID: "close", Status: status, ExecutedQty: 0.4}
				mse.OnOrderUpdate(update)
				mse.OnOrderUpdate(update)
				assertStrategyUsed(t, allocator, 60)
			})
		}
	}
}
