package execution

import (
	"errors"
	"math"
	"testing"
	"time"
)

func TestExposureMarkInvalidationCannotBeWashedByOldQuote(t *testing.T) {
	for _, price := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		b, now := exposureFixture(t, ExposureLimits{})
		later := now.Add(time.Second)
		if err := b.ObserveMark(price, later, later); err == nil {
			t.Fatal("invalid quote accepted")
		}
		if err := b.ObserveMark(100, now, later); err == nil {
			t.Fatal("old quote restored readiness")
		}
		if b.Snapshot(later).Ready {
			t.Fatal("invalid evidence lost")
		}
		if err := b.ObserveMark(101, later.Add(time.Second), later.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if !b.Snapshot(later.Add(time.Second)).Ready {
			t.Fatal("new valid quote failed recovery")
		}
	}
}

func TestExposureMarkStaleFutureAndExternalReconciliation(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{})
	if err := b.ObserveMark(100, now.Add(time.Hour), now); err == nil || b.Snapshot(now).Ready {
		t.Fatal("future quote accepted")
	}
	if err := b.ObserveMark(100, now.Add(time.Second), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if b.Snapshot(now.Add(2 * time.Minute)).Ready {
		t.Fatal("stale mark accepted")
	}
	b.RequireReconciliation("external inventory mutation")
	if err := b.ObserveMark(100, now.Add(3*time.Minute), now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := b.Seed(nil); err == nil {
		t.Fatal("reconciliation erased by empty seed")
	}
	if b.Snapshot(now.Add(3 * time.Minute)).Ready {
		t.Fatal("quote repaired unknown inventory")
	}
}

func TestExposureBotWideCloseAllocatesOnlyUnreservedSameLeg(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{},
		ExposurePosition{Key: "grid", Group: "grid", Leg: "LONG", Quantity: 1},
		ExposurePosition{Key: "dca", Group: "dca", Leg: "LONG", Quantity: 2},
		ExposurePosition{Key: "short", Group: "dca", Leg: "SHORT", Quantity: 3})
	if err := b.Reserve(ExposureRequest{ID: "protect", Group: "grid", Leg: "LONG", Lot: "grid", Quantity: 1}, now); err != nil {
		t.Fatal(err)
	}
	req := ExposureRequest{ID: "manual", Group: "manual_close", Leg: "LONG", Quantity: 2, BotWideClose: true}
	if err := b.Reserve(req, now); err != nil {
		t.Fatal(err)
	}
	if err := b.Observe(req.ID, ExposureUpdate{Status: "FILLED", OrderQty: 2, CumulativeQty: 2}); err != nil {
		t.Fatal(err)
	}
	if err := b.Observe("protect", ExposureUpdate{Status: "FILLED", OrderQty: 1, CumulativeQty: 1}); err != nil {
		t.Fatal(err)
	}
	if s := b.Snapshot(now); s.PositionQuantity != 3 || !s.Ready {
		t.Fatalf("wrong leg consumed: %+v", s)
	}
	req.ID, req.Quantity = "no-long", 1
	if err := b.Reserve(req, now); !errors.Is(err, ErrExposureLimit) {
		t.Fatalf("borrowed short inventory: %v", err)
	}
}

func TestExposureBotWideCloseCannotOpenOrBypassUnknown(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{}, ExposurePosition{Key: "dca", Group: "dca", Leg: "LONG", Quantity: 1})
	req := ExposureRequest{ID: "manual", Group: "manual_close", Leg: "LONG", Quantity: 1, BotWideClose: true, Opening: true, Price: 100}
	if err := b.Reserve(req, now); err == nil {
		t.Fatal("bot-wide open accepted")
	}
	req.Opening = false
	if err := b.Reserve(req, now); err != nil {
		t.Fatal(err)
	}
	b.MarkUnknown(req.ID)
	if err := b.Reserve(ExposureRequest{ID: "strategy", Group: "dca", Leg: "LONG", Quantity: 1}, now); !errors.Is(err, ErrExposureUnverified) {
		t.Fatalf("unknown manual close bypassed: %v", err)
	}
}
