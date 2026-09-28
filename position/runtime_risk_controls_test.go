package position

import "testing"

func TestGetPositionLegQuantitiesPreservesOpposingInventory(t *testing.T) {
	spm := newDirectionTestSPM(t, "BOTH", nil)
	fillSlot(spm, 100, 2, 100, PositionLegLong)
	fillSlot(spm, 200, 3, 200, PositionLegShort)
	long, short := spm.GetPositionLegQuantities()
	if long != 2 || short != 3 {
		t.Fatalf("gross legs were netted: long=%v short=%v", long, short)
	}
}
