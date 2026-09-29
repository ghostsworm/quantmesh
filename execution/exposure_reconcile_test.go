package execution

import (
	"testing"
	"time"
)

func TestReconcileGroupPositionsReplacesOnlyReconciledOwnerGroup(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Quantity: 3},
		ExposurePosition{Key: "grid:old", Group: "grid", Leg: "LONG", Quantity: 1},
		ExposurePosition{Key: "dca:owned", Group: "dca", Leg: "LONG", Quantity: 0.5},
	)
	if err := b.SetMark(100, now); err != nil {
		t.Fatal(err)
	}
	if err := b.ReconcileGroupPositions("grid", []ExposurePosition{
		{Key: "grid:adopted", Group: "grid", Leg: "LONG", Quantity: 1.25},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := b.Snapshot(now.Add(time.Second))
	if snapshot.PositionQuantity != 1.75 || snapshot.ProjectedNotional != 175 {
		t.Fatalf("reconciled exposure = %+v, want grid 1.25 + preserved DCA 0.5", snapshot)
	}
	if snapshot.Layers != 2 {
		t.Fatalf("reconciled layers = %d, want both owner groups preserved", snapshot.Layers)
	}
}

func TestReconcileGroupPositionsRejectsUnresolvedOrdersAndBlocksOpening(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Quantity: 3},
		ExposurePosition{Key: "grid:slot", Group: "grid", Leg: "LONG", Quantity: 1},
	)
	if err := b.Reserve(ExposureRequest{ID: "pending", Group: "grid", Lot: "grid:slot", Leg: "LONG", Opening: true, Quantity: 1, Price: 100}, now); err != nil {
		t.Fatal(err)
	}
	if err := b.ReconcileGroupPositions("grid", []ExposurePosition{
		{Key: "grid:adopted", Group: "grid", Leg: "LONG", Quantity: 2},
	}); err == nil {
		t.Fatal("reconciliation replaced inventory with an unresolved opening intent")
	}
	if err := b.Reserve(ExposureRequest{ID: "after", Group: "grid", Lot: "next", Leg: "LONG", Opening: true, Quantity: 0.1, Price: 100}, now); err == nil {
		t.Fatal("failed inventory reconciliation left opening admission enabled")
	}
}
