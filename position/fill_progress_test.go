package position

import (
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/strategy/regime"
)

func TestFillProgressCumulativeAndOutOfOrder(t *testing.T) {
	f := FillProgress{}
	for _, tc := range []struct{ qty, avg, delta, price float64 }{
		{0.5, 100, 0.5, 100}, {0.5, 100, 0, 0}, {0.2, 90, 0, 0},
		{1, 110, 0.5, 120}, {math.NaN(), 100, 0, 0}, {1.5, 50, 0, 0},
		{2, 120, 1, 130},
	} {
		delta, price := f.Advance(tc.qty, tc.avg, 100)
		if delta != tc.delta || price != tc.price {
			t.Fatalf("input=%+v delta=%v price=%v cursor=%+v", tc, delta, price, f)
		}
	}
	if f.Quantity != 2 || f.Notional != 240 {
		t.Fatalf("cursor=%+v", f)
	}
}

func TestGridCancelAccountsForNewFillExactlyOnce(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT", "BOTH"} {
		t.Run(direction, func(t *testing.T) {
			spm := newDirectionTestSPM(t, direction, nil)
			short := direction != "LONG"
			leg, side, price := "", "SELL", 110.0
			if short {
				leg, side, price = PositionLegShort, "BUY", 90
			}
			fillSlot(spm, 100, 2, 100, leg)
			st := &auditTradeRecorder{}
			spm.SetTradeStorage(st)
			cid := spm.generateClientOrderID(100, side, "")
			update := OrderUpdate{OrderID: 999, ClientOrderID: cid, Symbol: "BTCUSDT", Status: "PARTIALLY_FILLED", Side: side, ExecutedQty: 0.5, AvgPrice: price, Commission: 0.01, CommissionAsset: "USDT"}
			spm.OnOrderUpdate(update)
			spm.OnOrderUpdate(update)
			stale := update
			stale.ExecutedQty = 0.2
			spm.OnOrderUpdate(stale)
			update.Status, update.ExecutedQty = "CANCELED", 1
			if short {
				update.AvgPrice = 80
			} else {
				update.AvgPrice = 120
			}
			spm.OnOrderUpdate(update)
			spm.OnOrderUpdate(update)
			if len(st.pnl) != 2 || math.Abs(st.pnl[0]+st.pnl[1]-20) > 1e-9 {
				t.Fatalf("direction=%s pnl=%v", direction, st.pnl)
			}
			if slot := spm.getOrCreateSlot(100); slot.PositionQty != 1 || slot.OrderFilledNotional != 0 {
				t.Fatalf("remaining=%v cursor=%v", slot.PositionQty, slot.OrderFilledNotional)
			}
		})
	}
}

func TestGridMissingCumulativeAveragePriceRetainsFillForReconciliation(t *testing.T) {
	spm := newFillFeeSPM(t, "futures", nil)
	cid := openBuy(spm, 404)
	spm.OnOrderUpdate(OrderUpdate{OrderID: 404, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "FILLED", Side: "BUY", ExecutedQty: 1, Price: fillFeeTestPrice})
	slot := spm.getOrCreateSlot(fillFeeTestPrice)
	slot.mu.RLock()
	if slot.PositionQty != 0 || slot.OrderFilledQty != 0 || slot.OrderStatus != OrderStatusUnknown || slot.SlotStatus != SlotStatusLocked {
		t.Fatalf("missing average price was used to book a fill: qty=%v cursor=%v order_status=%q slot_status=%q", slot.PositionQty, slot.OrderFilledQty, slot.OrderStatus, slot.SlotStatus)
	}
	slot.mu.RUnlock()
	if !spm.openingGate.HasBlock("unknown_orders") {
		t.Fatal("missing fill price did not hold new openings")
	}
	spm.OnOrderUpdate(OrderUpdate{OrderID: 404, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "FILLED", Side: "BUY", ExecutedQty: 1, AvgPrice: fillFeeTestPrice})
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if slot.PositionQty != 1 || slot.OrderStatus != OrderStatusNotPlaced {
		t.Fatalf("corrected cumulative-average report did not settle locally: qty=%v status=%q", slot.PositionQty, slot.OrderStatus)
	}
}

func TestFundingTrendPreservesEveryHardOpeningConstraint(t *testing.T) {
	for _, kind := range []string{"pause", "price_low", "price_high", "quantity", "value", "layers", "bot_pause", "bot_quantity", "bot_value", "bot_layers"} {
		t.Run(kind, func(t *testing.T) {
			spm, exec := newR5bSPM(t, "LONG", 6)
			control := &spm.config.Trading.OpenPositionControl
			switch kind {
			case "pause":
				spm.isOpeningPaused.Store(true)
			case "price_low":
				spm.config.Trading.PriceLow = 101
			case "price_high":
				spm.config.Trading.PriceHigh = 99
			case "quantity":
				control.MaxPositionQuantity = 1
			case "value":
				control.MaxPositionValue = 100
			case "layers":
				control.MaxPositionLayers = 1
			case "bot_pause":
				control.BotRiskControl = &config.BotRiskControl{Enabled: true, PauseOpening: true}
				// Initial flags are consumed by the constructor into the shared gate.
				spm = NewSuperPositionManager(spm.config, exec, &MockExchange{}, 2, 3)
				spm.setAnchorPrice(100)
			case "bot_quantity":
				control.BotRiskControl = &config.BotRiskControl{Enabled: true, MaxPositionQuantity: 1}
			case "bot_value":
				control.BotRiskControl = &config.BotRiskControl{Enabled: true, MaxPositionValue: 100}
			case "bot_layers":
				control.BotRiskControl = &config.BotRiskControl{Enabled: true, MaxPositionLayers: 1}
			}
			fillSlot(spm, 110, 1, 110, "")
			spm.config.FundingRate.BiasEnabled, spm.config.FundingRate.TrendSyncEnabled = true, true
			spm.SetFundingMonitor(&auditFavorableFunding{})
			spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: readySnap(regime.TrendUp)}, RegimeControlOptions{FilterEnabled: true})
			if got := ordersBy(adjustAt(t, spm, exec, 100), "BUY", false); len(got) != 0 {
				t.Fatalf("hard constraint %s allowed %d opens", kind, len(got))
			}
		})
	}
}

func TestFundingPauseAcrossDirections(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT", "BOTH"} {
		t.Run(direction, func(t *testing.T) {
			spm, exec := newR5bSPM(t, direction, 6)
			spm.isOpeningPaused.Store(true)
			spm.config.FundingRate.BiasEnabled, spm.config.FundingRate.TrendSyncEnabled = true, true
			spm.SetFundingMonitor(&auditFavorableFunding{})
			trend := regime.TrendUp
			if direction == "SHORT" {
				trend = regime.TrendDown
			}
			spm.ConfigureRegimeControl(&fakeRegimeProvider{snap: readySnap(trend)}, RegimeControlOptions{FilterEnabled: true})
			for _, order := range adjustAt(t, spm, exec, 100) {
				if !order.ReduceOnly {
					t.Fatalf("%s paused strategy opened order", direction)
				}
			}
		})
	}
}
