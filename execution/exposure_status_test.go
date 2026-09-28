package execution

import (
	"errors"
	"testing"
	"time"
)

func TestExposureStatusIncludesPendingExactCapacity(t *testing.T) {
	for _, limits := range []ExposureLimits{{Quantity: .3}, {Notional: 30}} {
		b, now := exposureFixture(t, limits)
		for _, req := range []ExposureRequest{exposureOpen("a", .1), exposureOpen("b", .2)} {
			if err := b.Reserve(req, now); err != nil {
				t.Fatal(err)
			}
		}
		s := b.Snapshot(now)
		if !s.Ready || s.OpeningAvailable || s.NewLotAvailable || s.PositionQuantity != 0 || s.PendingQuantity != .3 {
			t.Fatalf("pending capacity hidden: %+v", s)
		}
	}
}

func TestExposureStatusDistinguishesExistingLotFromNewLayer(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Layers: 1, Quantity: 2})
	if err := b.Reserve(exposureOpen("a", 1), now); err != nil {
		t.Fatal(err)
	}
	s := b.Snapshot(now)
	if !s.OpeningAvailable || s.NewLotAvailable {
		t.Fatalf("layer boundary conflated: %+v", s)
	}
	if err := b.Reserve(exposureOpen("new", .1), now); !errors.Is(err, ErrExposureLimit) {
		t.Fatal(err)
	}
	same := exposureOpen("replacement", .1)
	same.Lot = "a"
	if err := b.Reserve(same, now); err != nil {
		t.Fatal(err)
	}
}

func TestExposureStatusReasonCodes(t *testing.T) {
	b, err := NewExposureBook(ExposureLimits{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if s := b.Snapshot(now); s.ReasonCode != "not_initialized" || s.OpeningAvailable {
		t.Fatal(s)
	}
	if err := b.Seed(nil); err != nil {
		t.Fatal(err)
	}
	if s := b.Snapshot(now); s.ReasonCode != "quote_unavailable" || s.OpeningAvailable {
		t.Fatal(s)
	}
	if err := b.SetMark(100, now); err != nil {
		t.Fatal(err)
	}
	if s := b.Snapshot(now); s.ReasonCode != "ready" || !s.NewLotAvailable {
		t.Fatal(s)
	}
	b.RequireReconciliation("internal diagnostic")
	if s := b.Snapshot(now); s.ReasonCode != "reconciliation_required" || s.OpeningAvailable {
		t.Fatal(s)
	}
}
