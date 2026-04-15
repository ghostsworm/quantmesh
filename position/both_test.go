package position

import (
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
