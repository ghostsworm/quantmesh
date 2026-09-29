package position

import (
	"math"
	"testing"
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
