package position

import (
	"math"
	"testing"

	"quantmesh/config"
)

func TestAdjustOrdersRejectsNonFiniteMarketPrices(t *testing.T) {
	for _, tc := range []struct {
		name  string
		price float64
	}{
		{name: "nan", price: math.NaN()},
		{name: "positive_infinity", price: math.Inf(1)},
		{name: "negative_infinity", price: math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec := &MockExecutor{}
			spm := newDirectionTestSPM(t, "LONG", exec)
			if err := spm.AdjustOrders(tc.price); err != nil {
				t.Fatalf("AdjustOrders(%v): %v", tc.price, err)
			}
			if len(exec.PlacedOrders) != 0 {
				t.Fatalf("invalid market price produced orders: %+v", exec.PlacedOrders)
			}
		})
	}
}

func TestTriggerPricePreservesProtectiveCloses(t *testing.T) {
	for _, tc := range []struct {
		name, direction, leg, closeSide string
		entry, price, trigger           float64
	}{
		{"long stop", "LONG", "", "SELL", 120, 110, 100},
		{"short stop", "SHORT", "", "BUY", 80, 90, 100},
		{"both long stop", "BOTH", PositionLegLong, "SELL", 120, 110, 100},
		{"both short stop", "BOTH", PositionLegShort, "BUY", 80, 110, 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			net := 1.0
			if tc.closeSide == "BUY" {
				net = -1
			}
			exec := newLiqFakeVenue(net)
			exec.bid, exec.ask, exec.limitFillRatio = tc.price-0.01, tc.price+0.01, 1
			spm, _ := newLiqTestSPM(t, tc.direction, exec)
			configureTestProtective(t, spm, exec, nil)
			spm.config.Trading.TriggerPrice = tc.trigger
			spm.config.Trading.GridRiskControl = config.GridRiskControl{Enabled: true, StopLossRatio: 0.05}
			fillSlot(spm, tc.entry, 1, tc.entry, tc.leg)
			if !spm.triggerPricePending(tc.price) {
				t.Fatal("fixture must wait for trigger")
			}
			if err := spm.AdjustOrders(tc.price); err != nil {
				t.Fatal(err)
			}
			waitTestProtective(t, spm)
			if len(exec.limitReqs) != 1 {
				t.Fatalf("expected one protective close: %+v", exec.limitReqs)
			}
			req := exec.limitReqs[0]
			if req.Side != tc.closeSide || !req.ReduceOnly {
				t.Fatalf("wrong protective close: %+v", req)
			}
		})
	}
}

func TestTriggerPriceSuppressesOnlyOpeningOrders(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT", "BOTH"} {
		t.Run(direction, func(t *testing.T) {
			spm, exec := newR5bSPM(t, direction, 4)
			spm.config.Trading.TriggerPrice = 90
			if direction == "SHORT" {
				spm.config.Trading.TriggerPrice = 110
			}
			leg, closeSide := "", "SELL"
			if direction == "SHORT" {
				closeSide = "BUY"
			} else if direction == "BOTH" {
				leg = PositionLegLong
			}
			fillSlot(spm, 100, 1, 100, leg)
			orders := adjustAt(t, spm, exec, 100)
			if len(orders) == 0 {
				t.Fatal("waiting for trigger suppressed ordinary inventory closes")
			}
			for _, req := range orders {
				if !req.ReduceOnly || req.Side != closeSide {
					t.Fatalf("waiting for trigger created exposure: %+v", req)
				}
			}
		})
	}
}
