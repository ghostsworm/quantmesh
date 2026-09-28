package execution

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExposureConcurrentCloseReservation(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{}, ExposurePosition{Key: "held", Group: "grid", Leg: "LONG", Quantity: 1})
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 20; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := b.Reserve(ExposureRequest{ID: fmt.Sprint(n), Group: "grid", Leg: "LONG", Quantity: .1}, now.Add(time.Hour))
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrExposureLimit) {
				t.Errorf("reserve: %v", err)
			}
		}(n)
	}
	wg.Wait()
	if accepted.Load() != 10 || b.Snapshot(now).PositionQuantity != 1 {
		t.Fatalf("close admission changed or oversubscribed inventory: accepted=%d", accepted.Load())
	}
}

func TestExposureCloseReservationReleaseRequiresTerminal(t *testing.T) {
	for _, status := range []string{"PARTIALLY_FILLED", "CANCEL_REQUESTED", "CANCELED", "EXPIRED"} {
		t.Run(status, func(t *testing.T) {
			b, now := exposureFixture(t, ExposureLimits{}, ExposurePosition{Key: "held", Group: "grid", Leg: "LONG", Quantity: 1})
			req := ExposureRequest{ID: "first", Group: "grid", Leg: "LONG", Quantity: 1}
			if err := b.Reserve(req, now); err != nil {
				t.Fatal(err)
			}
			if err := b.Observe(req.ID, ExposureUpdate{Status: status, OrderQty: 1, CumulativeQty: .4}); err != nil {
				t.Fatal(err)
			}
			req.ID, req.Quantity = "next", .6
			err := b.Reserve(req, now)
			if exposureTerminal(status) {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrExposureLimit) {
				t.Fatalf("unconfirmed cancellation freed inventory: %v", err)
			}
			if b.Snapshot(now).PositionQuantity != .6 {
				t.Fatal("reserved close was treated as executed")
			}
		})
	}
}

func TestExposureCloseAllocationsDoNotStealSpecificLots(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{},
		ExposurePosition{Key: "a", Group: "grid", Leg: "LONG", Quantity: 1},
		ExposurePosition{Key: "b", Group: "grid", Leg: "LONG", Quantity: 1})
	for _, req := range []ExposureRequest{
		{ID: "specific", Group: "grid", Leg: "LONG", Lot: "a", Quantity: 1},
		{ID: "generic", Group: "grid", Leg: "LONG", Quantity: 1},
	} {
		if err := b.Reserve(req, now); err != nil {
			t.Fatal(err)
		}
	}
	// The later generic close fills first; it must consume b, not FIFO a.
	for _, id := range []string{"generic", "specific"} {
		if err := b.Observe(id, ExposureUpdate{Status: "FILLED", OrderQty: 1, CumulativeQty: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if s := b.Snapshot(now); s.PositionQuantity != 0 || !s.Ready {
		t.Fatalf("allocation corruption: %+v", s)
	}
}

func TestExposureUnknownCloseRetainsAllocation(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{}, ExposurePosition{Key: "held", Group: "grid", Leg: "LONG", Quantity: 1})
	req := ExposureRequest{ID: "unknown", Group: "grid", Leg: "LONG", Quantity: 1}
	if err := b.Reserve(req, now); err != nil {
		t.Fatal(err)
	}
	b.MarkUnknown(req.ID)
	if err := b.Observe(req.ID, ExposureUpdate{Status: "CANCELED", OrderQty: 1, CumulativeQty: .4}); err != nil {
		t.Fatal(err)
	}
	req.ID, req.Quantity = "second", .6
	if err := b.Reserve(req, now); !errors.Is(err, ErrExposureUnverified) {
		t.Fatalf("unknown close released inventory: %v", err)
	}
}

func TestExposureRejectedCloseReleasesAllocation(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{}, ExposurePosition{Key: "held", Group: "grid", Leg: "LONG", Quantity: 1})
	req := ExposureRequest{ID: "refused", Group: "grid", Leg: "LONG", Quantity: 1}
	if err := b.Reserve(req, now); err != nil {
		t.Fatal(err)
	}
	if err := b.Reject(req.ID); err != nil {
		t.Fatal(err)
	}
	req.ID = "replacement"
	if err := b.Reserve(req, now); err != nil {
		t.Fatal(err)
	}
}
