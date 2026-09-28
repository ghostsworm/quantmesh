package execution

import (
	"errors"
	"testing"
)

func TestExposureIncompleteFilledReportRetainsReservation(t *testing.T) {
	for _, opening := range []bool{true, false} {
		b, now := exposureFixture(t, ExposureLimits{Quantity: 2}, ExposurePosition{Key: "held", Group: "grid", Leg: "LONG", Quantity: 1})
		req := exposureOpen("one", 1)
		req.Opening = opening
		if !opening {
			req.Lot = "held"
		}
		if err := b.Reserve(req, now); err != nil {
			t.Fatal(err)
		}
		if err := b.Observe(req.ID, ExposureUpdate{Status: "FILLED", CumulativeQty: .4}); !errors.Is(err, ErrExposureUnverified) {
			t.Fatalf("opening=%v incomplete terminal accepted: %v", opening, err)
		}
		s := b.Snapshot(now)
		if s.Ready || s.PositionQuantity != 1 || (opening && s.PendingQuantity != 1) {
			t.Fatalf("incomplete terminal consumed inventory or freed quota: %+v", s)
		}
	}
}

func TestExposureFilledWithoutQuantityUsesVerifiedOriginalQuantity(t *testing.T) {
	for _, venueQty := range []float64{0, .9, 1} {
		b, now := exposureFixture(t, ExposureLimits{Quantity: 1})
		if err := b.Reserve(exposureOpen("one", 1), now); err != nil {
			t.Fatal(err)
		}
		if err := b.Observe("one", ExposureUpdate{Status: "NEW", OrderQty: venueQty}); err != nil {
			t.Fatal(err)
		}
		expected := venueQty
		if expected == 0 {
			expected = 1
		}
		if err := b.Observe("one", ExposureUpdate{Status: "FILLED", CumulativeQty: expected}); err != nil {
			t.Fatal(err)
		}
		s := b.Snapshot(now)
		if !s.Ready || s.PendingQuantity != 0 || s.PositionQuantity != expected {
			t.Fatalf("valid terminal: %+v", s)
		}
	}
}

func TestExposureConflictingOriginalQuantityCannotReleaseReservation(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Quantity: 1})
	if err := b.Reserve(exposureOpen("one", 1), now); err != nil {
		t.Fatal(err)
	}
	if err := b.Observe("one", ExposureUpdate{Status: "NEW", OrderQty: 1}); err != nil {
		t.Fatal(err)
	}
	if err := b.Observe("one", ExposureUpdate{Status: "FILLED", OrderQty: .4, CumulativeQty: .4}); !errors.Is(err, ErrExposureUnverified) {
		t.Fatalf("conflicting report: %v", err)
	}
	if s := b.Snapshot(now); s.Ready || s.PendingQuantity != 1 {
		t.Fatalf("released inconsistent order: %+v", s)
	}
}
