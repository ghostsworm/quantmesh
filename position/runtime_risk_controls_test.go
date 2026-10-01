package position

import (
	"math"
	"testing"

	"quantmesh/config"
)

func TestGetPositionLegQuantitiesPreservesOpposingInventory(t *testing.T) {
	spm := newDirectionTestSPM(t, "BOTH", nil)
	fillSlot(spm, 100, 2, 100, PositionLegLong)
	fillSlot(spm, 200, 3, 200, PositionLegShort)
	long, short, err := spm.GetPositionLegQuantities()
	if err != nil || long != 2 || short != 3 {
		t.Fatalf("gross legs were netted: long=%v short=%v", long, short)
	}
}

func TestInvalidGridRiskUpdateRetainsLastGoodSnapshotAndBlocksOpening(t *testing.T) {
	spm := newDirectionTestSPM(t, "LONG", nil)
	initial := config.GridRiskControl{Enabled: true, StopLossRatio: 0.1}
	spm.SetGridRiskControl(initial)
	spm.SetGridRiskControl(config.GridRiskControl{Enabled: true, StopLossRatio: math.NaN()})
	if got := spm.GetRiskControls().Grid; got != initial {
		t.Fatalf("invalid grid controls replaced last good snapshot: got %+v", got)
	}
	if !spm.OpeningGate().HasBlock("invalid_grid_risk_control") {
		t.Fatal("invalid grid controls did not block new exposure")
	}
	spm.SetGridRiskControl(config.GridRiskControl{Enabled: true, StopLossRatio: 0.05})
	if got := spm.GetRiskControls().Grid.StopLossRatio; got != 0.05 {
		t.Fatalf("valid retry did not apply: stop-loss ratio=%v", got)
	}
	if spm.OpeningGate().HasBlock("invalid_grid_risk_control") {
		t.Fatal("valid retry did not release the owned validation block")
	}
}

func TestGetPositionLegQuantitiesFailsClosedOnInvalidInventory(t *testing.T) {
	tests := []struct {
		name string
		qtys []float64
		leg  string
		mode string
	}{
		{name: "nan", qtys: []float64{math.NaN()}, leg: PositionLegLong, mode: "BOTH"},
		{name: "infinity", qtys: []float64{math.Inf(1)}, leg: PositionLegLong, mode: "BOTH"},
		{name: "negative", qtys: []float64{-1}, leg: PositionLegLong, mode: "BOTH"},
		{name: "aggregate overflow", qtys: []float64{math.MaxFloat64, math.MaxFloat64}, leg: PositionLegLong, mode: "BOTH"},
		{name: "unknown hedge leg", qtys: []float64{1}, leg: "", mode: "BOTH"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spm := newDirectionTestSPM(t, tt.mode, nil)
			for i, qty := range tt.qtys {
				spm.slots.Store(float64(100+i), &InventorySlot{Price: float64(100 + i), PositionQty: qty, PositionLeg: tt.leg})
			}
			if long, short, err := spm.GetPositionLegQuantities(); err == nil || long != 0 || short != 0 {
				t.Fatalf("invalid exposure was treated as reconciled: long=%v short=%v err=%v", long, short, err)
			}
		})
	}
}

func TestRiskEconomicsOpeningGateHoldsInvalidStopLossInputs(t *testing.T) {
	tests := []struct {
		name   string
		mode   string
		mutate func(*InventorySlot)
	}{
		{name: "BOTH 缺持倉腿", mode: "BOTH", mutate: func(slot *InventorySlot) { slot.PositionLeg = PositionLegNone }},
		{name: "手續費估值未知", mode: "LONG", mutate: func(slot *InventorySlot) { slot.feeValuationUnknown = true }},
		{name: "手續費補查未完成", mode: "LONG", mutate: func(slot *InventorySlot) { slot.pendingFeeSupplementCount = 1 }},
		{name: "手續費計價資產不符", mode: "LONG", mutate: func(slot *InventorySlot) { slot.BuyFee, slot.FeeAsset = 1, "BTC" }},
		{name: "非法持倉數量", mode: "LONG", mutate: func(slot *InventorySlot) { slot.PositionQty = math.NaN() }},
		{name: "方向與模式不符", mode: "SHORT", mutate: func(slot *InventorySlot) { slot.PositionLeg = PositionLegLong }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spm := newDirectionTestSPM(t, tc.mode, nil)
			leg := PositionLegLong
			if tc.mode == "SHORT" {
				leg = PositionLegShort
			}
			slot := fillSlot(spm, 100, 1, 100, leg)
			slot.mu.Lock()
			tc.mutate(slot)
			slot.mu.Unlock()
			if _, verified := spm.calculateUnrealizedPnLVerified(90); verified {
				t.Fatal("invalid stop-loss economics were reported as verified")
			}
			spm.refreshCostBasisOpeningGate()
			if !spm.OpeningGate().HasBlock("grid_cost_basis_unverified") {
				t.Fatal("unverified stop-loss inputs did not block new exposure")
			}
			slot.mu.Lock()
			slot.PositionQty = 1
			slot.PositionLeg = leg
			slot.CostBasisUnverified = false
			slot.AvgBuyPrice = 100
			slot.BuyFee = 0
			slot.feeValuationUnknown = false
			slot.pendingFeeSupplementCount = 0
			slot.mu.Unlock()
			spm.refreshCostBasisOpeningGate()
			if spm.OpeningGate().HasBlock("grid_cost_basis_unverified") {
				t.Fatal("verified stop-loss inputs did not release their owned gate")
			}
		})
	}
}

func TestAdjustOrdersDoesNotAddExposureWithUnverifiedRiskEconomics(t *testing.T) {
	executor := &MockExecutor{}
	spm := newDirectionTestSPM(t, "BOTH", executor)
	spm.config.Trading.GridRiskControl.Enabled = true
	spm.config.Trading.GridRiskControl.StopLossRatio = 0.05
	spm.setAnchorPrice(100)
	slot := fillSlot(spm, 100, 1, 100, PositionLegNone)
	slot.mu.Lock()
	slot.feeValuationUnknown = true
	slot.mu.Unlock()

	if err := spm.AdjustOrders(90); err != nil {
		t.Fatal(err)
	}
	if !spm.OpeningGate().HasBlock("grid_cost_basis_unverified") {
		t.Fatal("AdjustOrders did not hold new exposure on unverified risk economics")
	}
	for _, order := range executor.PlacedOrders {
		if order != nil && !order.ReduceOnly {
			t.Fatalf("new exposure submitted with unverified stop-loss economics: %+v", order)
		}
	}
}

func TestAdjustOrdersBlocksWhenRiskNotionalOverflows(t *testing.T) {
	executor := &MockExecutor{}
	spm := newDirectionTestSPM(t, "LONG", executor)
	spm.config.Trading.GridRiskControl.Enabled = true
	spm.config.Trading.GridRiskControl.StopLossRatio = 0.05
	spm.setAnchorPrice(100)
	fillSlot(spm, 100, math.MaxFloat64, 100, PositionLegNone)

	if err := spm.AdjustOrders(100); err != nil {
		t.Fatal(err)
	}
	if !spm.OpeningGate().HasBlock(gridRiskNotionalBlock) {
		t.Fatal("overflowed position notional did not hold risk-dependent openings")
	}
	for _, order := range executor.PlacedOrders {
		if order != nil && !order.ReduceOnly {
			t.Fatalf("new exposure submitted with unverified risk notional: %+v", order)
		}
	}

	slot, _ := spm.slots.Load(100.0)
	slot.(*InventorySlot).mu.Lock()
	slot.(*InventorySlot).PositionQty = 1
	slot.(*InventorySlot).mu.Unlock()
	spm.refreshGridRiskNotionalGate(100)
	if spm.OpeningGate().HasBlock(gridRiskNotionalBlock) {
		t.Fatal("finite risk notional did not release its owned hold")
	}
}
