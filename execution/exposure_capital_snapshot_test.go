package execution

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestCapitalReleaseExposureSnapshotLock(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{})
	b.mu.Lock()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := b.CapitalReleaseSnapshot(ctx, now, &ExposureQuote{Price: -1, At: now})
		done <- err
	}()
	select {
	case err := <-done:
		b.mu.Unlock()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline: %v", err)
		}
	case <-time.After(time.Second):
		b.mu.Unlock()
		<-done
		t.Fatal("exposure snapshot waited past request deadline")
	}
	got, err := b.CapitalReleaseSnapshot(t.Context(), now, nil)
	if err != nil || !reflect.DeepEqual(got, b.Snapshot(now)) || !got.Ready {
		t.Fatalf("normal snapshot could not recover: %+v %v", got, err)
	}
}

type capitalSnapshotCancelContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int32
}

func (ctx *capitalSnapshotCancelContext) Err() error {
	if ctx.checks.Add(1) == 3 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestCapitalReleaseExposureSnapshotCalculationCancelReturnsNoPartial(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{}, ExposurePosition{Key: "lot", Group: "dca", Leg: "LONG", Quantity: 1})
	before := b.Snapshot(now)
	base, cancel := context.WithCancel(t.Context())
	defer cancel()
	ctx := &capitalSnapshotCancelContext{Context: base, cancel: cancel}
	got, err := b.CapitalReleaseSnapshot(ctx, now, nil)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, ExposureSnapshot{}) {
		t.Fatalf("partial snapshot escaped calculation cancel: %+v %v", got, err)
	}
	if got := b.Snapshot(now); !reflect.DeepEqual(got, before) {
		t.Fatalf("cancel cleared exposure or left lock held: %+v", got)
	}
}

func TestCapitalReleaseExposureSnapshotPreservesRules(t *testing.T) {
	for _, state := range []string{"flat", "positions", "pending", "unknown", "invalid_quote", "stale", "reconciliation", "superseded"} {
		t.Run(state, func(t *testing.T) {
			b, now := exposureFixture(t, ExposureLimits{Quantity: 2, Notional: 200, Layers: 2})
			if state == "positions" {
				b, now = exposureFixture(t, ExposureLimits{}, ExposurePosition{Key: "lot", Group: "dca", Leg: "LONG", Quantity: 1})
			}
			if state == "pending" || state == "unknown" {
				if err := b.Reserve(exposureOpen("pending", 1), now); err != nil {
					t.Fatal(err)
				}
				if state == "unknown" {
					b.MarkUnknown("pending")
				}
			}
			quote := &ExposureQuote{Price: 101, At: now}
			switch state {
			case "invalid_quote":
				quote.Price = -1
			case "stale":
				now = now.Add(2 * time.Minute)
			case "reconciliation":
				b.RequireReconciliation("unresolved inventory")
			case "superseded":
				quote.At = now.Add(-time.Second)
			}
			got, err := b.CapitalReleaseSnapshot(t.Context(), now, quote)
			if err != nil || !reflect.DeepEqual(got, b.Snapshot(now)) {
				t.Fatalf("normal snapshot rules changed: %+v %v", got, err)
			}
			if state == "positions" && (got.PositionQuantity != 1 || got.ProjectedQuantity != 1 || got.ProjectedNotional != 101) {
				t.Fatalf("position exposure disappeared: %+v", got)
			}
			if (state == "pending" || state == "unknown") && (got.PendingQuantity != 1 || got.ProjectedQuantity != 1 || got.ProjectedNotional != 101) {
				t.Fatalf("pending exposure disappeared: %+v", got)
			}
			if state == "invalid_quote" || state == "stale" || state == "reconciliation" || state == "unknown" {
				if got.Ready {
					t.Fatal("bad evidence became ready")
				}
			}
		})
	}
}

func TestCapitalReleaseExposureSnapshotInvalidContextDoesNotUpdateQuote(t *testing.T) {
	b, now := exposureFixture(t, ExposureLimits{})
	before := b.Snapshot(now)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, invalid := range []context.Context{nil, ctx} {
		if _, err := b.CapitalReleaseSnapshot(invalid, now, &ExposureQuote{Price: -1, At: now}); err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	if got := b.Snapshot(now); !reflect.DeepEqual(got, before) {
		t.Fatalf("cancelled snapshot modified evidence: %+v", got)
	}
	var missing *ExposureBook
	if _, err := missing.CapitalReleaseSnapshot(t.Context(), now, nil); err == nil {
		t.Fatal("nil book accepted")
	}
}
