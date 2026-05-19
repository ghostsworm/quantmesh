package position

import (
	"quantmesh/config"
	"testing"
)

func TestBothSideIsOpen(t *testing.T) {
	empty := &InventorySlot{PositionStatus: PositionStatusEmpty, PositionQty: 0, PositionLeg: PositionLegNone}
	if !bothSideIsOpen("BUY", empty) || !bothSideIsOpen("SELL", empty) {
		t.Fatal("empty slot should allow both open sides")
	}
	long := &InventorySlot{PositionStatus: PositionStatusFilled, PositionQty: 1, PositionLeg: PositionLegLong}
	if !bothSideIsOpen("BUY", long) || bothSideIsOpen("SELL", long) {
		t.Fatal("long leg: only BUY is open")
	}
	sh := &InventorySlot{PositionStatus: PositionStatusFilled, PositionQty: 1, PositionLeg: PositionLegShort}
	if !bothSideIsOpen("SELL", sh) || bothSideIsOpen("BUY", sh) {
		t.Fatal("short leg: only SELL is open")
	}
}

func TestAdjustOrdersBothRespectsOrderCleanupThresholdAcrossBothOpenSides(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = "BOTH"
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.ProfitSpread = 100
	cfg.Trading.BuyWindowSize = 4
	cfg.Trading.SellWindowSize = 4
	cfg.Trading.ShortOpenWindowSize = 4
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.OrderCleanupThreshold = 1

	executor := &MockExecutor{}
	ex := &MockExchange{}
	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)
	if err := spm.Initialize(50000, "50000.00"); err != nil {
		t.Fatal(err)
	}

	executor.PlacedOrders = nil
	spm.AdjustOrders(50000)

	if len(executor.PlacedOrders) > 1 {
		t.Fatalf("BOTH mode should not place more orders than threshold=1, got %d", len(executor.PlacedOrders))
	}
	for _, req := range executor.PlacedOrders {
		if req.ReduceOnly {
			t.Fatalf("initial BOTH open order must not be reduce-only: %+v", req)
		}
		if req.Side != "BUY" && req.Side != "SELL" {
			t.Fatalf("unexpected BOTH open side %q", req.Side)
		}
	}
}

func TestAdjustOrdersBothCloseSidesMatchPositionLegs(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = "BOTH"
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.ProfitSpread = 100
	cfg.Trading.BuyWindowSize = 1
	cfg.Trading.SellWindowSize = 4
	cfg.Trading.ShortOpenWindowSize = 1
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.OrderCleanupThreshold = 10

	executor := &MockExecutor{}
	ex := &MockExchange{}
	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)
	if err := spm.Initialize(50000, "50000.00"); err != nil {
		t.Fatal(err)
	}

	longSlot := spm.getOrCreateSlot(49900)
	longSlot.mu.Lock()
	longSlot.PositionStatus = PositionStatusFilled
	longSlot.PositionQty = 0.01
	longSlot.AvgBuyPrice = 49900
	longSlot.PositionLeg = PositionLegLong
	longSlot.SlotStatus = SlotStatusFree
	longSlot.mu.Unlock()

	shortSlot := spm.getOrCreateSlot(50100)
	shortSlot.mu.Lock()
	shortSlot.PositionStatus = PositionStatusFilled
	shortSlot.PositionQty = 0.01
	shortSlot.AvgBuyPrice = 50100
	shortSlot.PositionLeg = PositionLegShort
	shortSlot.SlotStatus = SlotStatusFree
	shortSlot.mu.Unlock()

	executor.PlacedOrders = nil
	spm.AdjustOrders(50000)

	var longCloseFound, shortCloseFound bool
	for _, req := range executor.PlacedOrders {
		slotPrice, oidSide, valid := spm.parseClientOrderID(req.ClientOrderID)
		if !valid {
			continue
		}
		switch {
		case slotPrice == 49900 && oidSide == "SELL":
			longCloseFound = true
			if req.Side != "SELL" || !req.ReduceOnly {
				t.Fatalf("long leg close must be reduce-only SELL, got %+v", req)
			}
		case slotPrice == 50100 && oidSide == "BUY":
			shortCloseFound = true
			if req.Side != "BUY" || !req.ReduceOnly {
				t.Fatalf("short leg close must be reduce-only BUY, got %+v", req)
			}
		}
	}
	if !longCloseFound {
		t.Fatal("missing reduce-only SELL close order for long leg")
	}
	if !shortCloseFound {
		t.Fatal("missing reduce-only BUY close order for short leg")
	}
}

func TestSmartOrderManagerBothTracksShortOpenOrders(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = "BOTH"
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.SmartOrder.Enabled = true
	cfg.Trading.SmartOrder.MaxOpenOrders = 3
	cfg.Trading.SmartOrder.OpenOrderDistance = 2

	executor := &MockExecutor{}
	ex := &MockExchange{}
	spm := NewSuperPositionManager(cfg, executor, ex, 2, 3)
	if err := spm.Initialize(50000, "50000.00"); err != nil {
		t.Fatal(err)
	}

	farShort := spm.getOrCreateSlot(50600)
	farShort.mu.Lock()
	farShort.PositionStatus = PositionStatusEmpty
	farShort.PositionQty = 0
	farShort.OrderID = 9001
	farShort.OrderSide = "SELL"
	farShort.OrderStatus = OrderStatusPlaced
	farShort.SlotStatus = SlotStatusLocked
	farShort.mu.Unlock()

	som := NewSmartOrderManager(spm, &cfg.Trading.SmartOrder)
	if !som.shouldAdjustOrders(50000) {
		t.Fatal("BOTH smart order manager should treat far SELL as an open short order")
	}

	som.adjustOrders(50000)
	if len(executor.CancelledOrderIDs) != 1 || executor.CancelledOrderIDs[0] != 9001 {
		t.Fatalf("expected far short open order 9001 to be cancelled, got %v", executor.CancelledOrderIDs)
	}
}
