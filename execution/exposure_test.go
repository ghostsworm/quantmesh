package execution

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func exposureFixture(t *testing.T, limits ExposureLimits, positions ...ExposurePosition) (*ExposureBook, time.Time) {
	t.Helper()
	now := time.Now()
	b, err := NewExposureBook(limits, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = b.Seed(positions); err != nil {
		t.Fatal(err)
	}
	if err = b.SetMark(100, now); err != nil {
		t.Fatal(err)
	}
	return b, now
}

func exposureOpen(id string, quantity float64) ExposureRequest {
	return ExposureRequest{ID: id, Group: "grid", Lot: id, Leg: "LONG", Opening: true, Quantity: quantity, Price: 100}
}

func TestExposureConcurrentAdmissionIncludesPending(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Quantity: 3, Notional: 300, Layers: 3})
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 30; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			err := b.Reserve(exposureOpen(fmt.Sprint(n), 1), now)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrExposureLimit) {
				t.Errorf("admission: %v", err)
			}
		}(n)
	}
	wg.Wait()
	s := b.Snapshot(now)
	if accepted.Load() != 3 || s.PendingQuantity != 3 || s.PositionQuantity != 0 || s.ProjectedNotional != 300 || s.Layers != 3 {
		t.Fatalf("over-admission: %+v accepted=%d", s, accepted.Load())
	}
}

func TestExposurePartialCancelAndOwnedCloseLifecycle(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Quantity: 1, Layers: 2})
	if err := b.Reserve(exposureOpen("one", 1), now); err != nil {
		t.Fatal(err)
	}
	for _, fill := range []float64{.4, .7, .7, .6} {
		if err := b.Observe("one", ExposureUpdate{CumulativeQty: fill, OrderQty: 1, Status: "PARTIALLY_FILLED"}); err != nil {
			t.Fatal(err)
		}
	}
	if s := b.Snapshot(now); s.PositionQuantity != .7 || s.PendingQuantity != .3 || s.ProjectedQuantity != 1 || s.Layers != 1 {
		t.Fatalf("fill double-counted: %+v", s)
	}
	if err := b.Observe("one", ExposureUpdate{CumulativeQty: .7, OrderQty: 1, Status: "CANCEL_REQUESTED"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Reserve(exposureOpen("two", .3), now); !errors.Is(err, ErrExposureLimit) {
		t.Fatalf("cancel ACK freed quota: %v", err)
	}
	if err := b.Observe("one", ExposureUpdate{CumulativeQty: .7, OrderQty: 1, Status: "CANCELED"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Reserve(exposureOpen("two", .3), now); err != nil {
		t.Fatal(err)
	}
	close := ExposureRequest{ID: "close", Group: "grid", Lot: "one", Leg: "LONG", Quantity: .4}
	if err := b.Reserve(close, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := b.Observe("close", ExposureUpdate{CumulativeQty: .4, OrderQty: .4, Status: "FILLED"}); err != nil {
			t.Fatal(err)
		}
	}
	if s := b.Snapshot(now); s.PositionQuantity != .3 || s.PendingQuantity != .3 || s.ProjectedQuantity != .6 || s.Layers != 2 {
		t.Fatalf("close/duplicate accounting: %+v", s)
	}
	if err := b.Reject("two"); err != nil {
		t.Fatal(err)
	}
	if s := b.Snapshot(now); s.PendingQuantity != 0 || s.ProjectedQuantity != .3 || s.Layers != 1 {
		t.Fatalf("rejection: %+v", s)
	}
}

func TestExposureDecimalBoundaryAndGrossLegs(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Quantity: .3})
	if err := b.Reserve(exposureOpen("long", .1), now); err != nil {
		t.Fatal(err)
	}
	short := exposureOpen("short", .2)
	short.Leg = "SHORT"
	if err := b.Reserve(short, now); err != nil {
		t.Fatal(err)
	}
	if err := b.Reserve(exposureOpen("tiny", .000000000000000001), now); !errors.Is(err, ErrExposureLimit) {
		t.Fatalf("tiny overage allowed: %v", err)
	}
	if s := b.Snapshot(now); s.ProjectedQuantity != .3 {
		t.Fatalf("legs netted: %+v", s)
	}
}

func TestExposureLayersCountLotsAcrossReplacement(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Layers: 1})
	if err := b.Reserve(exposureOpen("slot", 1), now); err != nil {
		t.Fatal(err)
	}
	if err := b.Observe("slot", ExposureUpdate{CumulativeQty: .4, Status: "CANCELED"}); err != nil {
		t.Fatal(err)
	}
	replacement := exposureOpen("replacement", .6)
	replacement.Lot = "slot"
	if err := b.Reserve(replacement, now); err != nil {
		t.Fatal(err)
	}
	if err := b.Reserve(exposureOpen("other-slot", .1), now); !errors.Is(err, ErrExposureLimit) {
		t.Fatal("layer limit ignored")
	}
	if s := b.Snapshot(now); s.Layers != 1 || s.ProjectedQuantity != 1 {
		t.Fatalf("replacement double layer: %+v", s)
	}
}

func TestExposureUnknownRetainsQuotaAndAllowsProtection(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Quantity: 2}, ExposurePosition{Key: "held", Group: "grid", Leg: "LONG", Quantity: 1})
	if err := b.Reserve(exposureOpen("uncertain", 1), now); err != nil {
		t.Fatal(err)
	}
	b.MarkUnknown("uncertain")
	if err := b.Reject("uncertain"); !errors.Is(err, ErrExposureUnverified) {
		t.Fatal("unknown quota released")
	}
	if err := b.Reserve(exposureOpen("another", .1), now); !errors.Is(err, ErrExposureUnverified) {
		t.Fatal("unknown permits new exposure")
	}
	if err := b.Reserve(ExposureRequest{ID: "protect", Group: "grid", Lot: "held", Leg: "LONG", Quantity: 1}, now); err != nil {
		t.Fatal(err)
	}
	if err := b.Observe("protect", ExposureUpdate{CumulativeQty: 1, OrderQty: 1, Status: "FILLED"}); err != nil {
		t.Fatal(err)
	}
	if s := b.Snapshot(now); s.Ready || s.PendingQuantity != 1 || s.PositionQuantity != 0 {
		t.Fatalf("protection/unknown: %+v", s)
	}
}

func TestExposureMalformedReportsNeverFreeQuota(t *testing.T) {
	for _, update := range []ExposureUpdate{{Status: "FILLED"}, {Status: "CANCELED", CumulativeQty: math.NaN()}, {Status: "mystery"}, {Status: "FILLED", OrderQty: 1, CumulativeQty: .5}, {Status: "NEW", OrderQty: math.Inf(1)}} {
		b, now := exposureFixture(t, ExposureLimits{Quantity: 1})
		if err := b.Reserve(exposureOpen("open", 1), now); err != nil {
			t.Fatal(err)
		}
		if err := b.Observe("open", update); !errors.Is(err, ErrExposureUnverified) {
			t.Fatalf("invalid report accepted: %v", err)
		}
		if err := b.Reject("open"); !errors.Is(err, ErrExposureUnverified) {
			t.Fatal("bad report then refusal released quota")
		}
		if s := b.Snapshot(now); s.PendingQuantity != 1 || s.Ready {
			t.Fatalf("invalid report freed quota: %+v", s)
		}
	}
}

func TestExposureOversizedVenueAckBlocksNewRisk(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Quantity: 1})
	if err := b.Reserve(exposureOpen("open", 1), now); err != nil {
		t.Fatal(err)
	}
	if err := b.Observe("open", ExposureUpdate{Status: "PARTIALLY_FILLED", OrderQty: 2, CumulativeQty: .5}); !errors.Is(err, ErrExposureUnverified) {
		t.Fatal("venue enlargement ignored")
	}
	if s := b.Snapshot(now); s.ProjectedQuantity != 2 || s.PositionQuantity != .5 || s.Ready {
		t.Fatalf("venue risk missing: %+v", s)
	}
}

func TestExposurePriceFreshnessRepriceAndLimitChanges(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{Notional: 100})
	req := exposureOpen("short", 1)
	req.Leg = "SHORT"
	if err := b.Reserve(req, now); err != nil {
		t.Fatal(err)
	}
	if err := b.Reprice("short", 100.01, now); !errors.Is(err, ErrExposureLimit) {
		t.Fatal("repricing exceeded cap")
	}
	if s := b.Snapshot(now); s.ProjectedNotional != 100 {
		t.Fatal("failed reprice mutated reservation")
	}
	if err := b.SetLimits(ExposureLimits{Quantity: .5}); err != nil {
		t.Fatal(err)
	}
	if err := b.Reserve(exposureOpen("new", .1), now); !errors.Is(err, ErrExposureLimit) {
		t.Fatal("lowered limit ignored")
	}
	if s := b.Snapshot(now); s.PendingQuantity != 1 {
		t.Fatal("lowering limits cleared commitments")
	}
	if err := b.Reserve(exposureOpen("stale", .1), now.Add(2*time.Minute)); !errors.Is(err, ErrExposureUnverified) {
		t.Fatal("stale mark used")
	}
	if err := b.Seed(nil); err == nil {
		t.Fatal("seed cleared existing quota")
	}
}

func TestExposureCloseCannotConsumeOtherOwnerOrLeg(t *testing.T) {
	for _, req := range []ExposureRequest{{ID: "close", Group: "other", Leg: "LONG", Quantity: 1}, {ID: "close", Group: "grid", Leg: "SHORT", Quantity: 1}, {ID: "close", Group: "grid", Leg: "LONG", Lot: "missing", Quantity: 1}} {
		b, now := exposureFixture(t, ExposureLimits{}, ExposurePosition{Key: "held", Group: "grid", Leg: "LONG", Quantity: 1})
		if err := b.Reserve(req, now); !errors.Is(err, ErrExposureLimit) {
			t.Fatalf("foreign close admitted: %v", err)
		}
		if s := b.Snapshot(now); s.PositionQuantity != 1 || !s.Ready {
			t.Fatalf("foreign close freed quota: %+v", s)
		}
	}
}
