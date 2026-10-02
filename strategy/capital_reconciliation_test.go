package strategy

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/execution"
	"quantmesh/position"
)

func TestCapitalReconciliationDrainsPrePhysicalReservation(t *testing.T) {
	allocator := capitalReleaseFixture(t)
	mse := NewMultiStrategyExecutor(nil, allocator)
	finish, err := mse.beginSubmission(&position.OrderRequest{ClientOrderID: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a reservation waiting for the physical lock, with no venue intent.
	if !allocator.Reserve("dca", 100) {
		t.Fatal("queued reservation failed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if release, err := mse.BeginCapitalReconciliation(ctx); !errors.Is(err, context.DeadlineExceeded) || release != nil {
		t.Fatalf("undrained queued reservation authorized reconciliation: release=%v err=%v", release != nil, err)
	}
	if allocator.GetUsed("dca") != 300 {
		t.Fatal("drain failure cleared queued capital")
	}
	allocator.Release("dca", 100)
	finish()
	// A timed-out barrier must not permanently disable normal trading.
	finish, err = mse.beginSubmission(&position.OrderRequest{ClientOrderID: "after-timeout"})
	if err != nil {
		t.Fatal(err)
	}
	finish()
	release, err := mse.BeginCapitalReconciliation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, req := range []*position.OrderRequest{
		{ClientOrderID: "new-opening", Side: "BUY"},
		{ClientOrderID: "new-close", Side: "SELL", ReduceOnly: true},
	} {
		if _, err := mse.beginSubmission(req); !errors.Is(err, execution.ErrOpeningPaused) {
			t.Fatalf("submission bypassed capital barrier: %v", err)
		}
	}
	if err := mse.VerifyCapitalReleaseAccounting(t.Context()); err != nil {
		t.Fatal(err)
	}
	released, err := allocator.ReleaseVerified(t.Context(), "dca", func(context.Context) error { return nil })
	if err != nil || released["dca"] != 200 {
		t.Fatalf("stale capital did not recover: released=%v err=%v", released, err)
	}
	release()
	release()
	finish, err = mse.beginSubmission(&position.OrderRequest{ClientOrderID: "after-success"})
	if err != nil {
		t.Fatal(err)
	}
	finish()
}

func TestCapitalReconciliationIndependentSources(t *testing.T) {
	mse := NewMultiStrategyExecutor(nil, nil)
	first, err := mse.BeginCapitalReconciliation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := mse.BeginCapitalReconciliation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	first()
	if _, err := mse.beginSubmission(&position.OrderRequest{ClientOrderID: "blocked"}); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatal("first release removed second barrier")
	}
	second()
	finish, err := mse.beginSubmission(&position.OrderRequest{ClientOrderID: "normal"})
	if err != nil {
		t.Fatal(err)
	}
	finish()
}

func TestCapitalReleaseAccountingRejectsResidualOwnership(t *testing.T) {
	for _, scenario := range []string{"client order", "id order", "position quantity", "position amount", "invalid position", "nil position"} {
		t.Run(scenario, func(t *testing.T) {
			mse := NewMultiStrategyExecutor(nil, nil)
			switch scenario {
			case "client order":
				mse.ordersByClient["pending"] = &orderCapital{}
			case "id order":
				mse.ordersByID[17] = &orderCapital{}
			case "position quantity":
				mse.positionUsage["dca|LONG"] = &legUsage{qty: 1}
			case "position amount":
				mse.positionUsage["dca|LONG"] = &legUsage{amount: 200}
			case "invalid position":
				mse.positionUsage["dca|LONG"] = &legUsage{qty: math.NaN()}
			case "nil position":
				mse.positionUsage["dca|LONG"] = nil
			}
			if err := mse.VerifyCapitalReleaseAccounting(t.Context()); err == nil {
				t.Fatal("residual strategy ownership authorized capital release")
			}
		})
	}
}
