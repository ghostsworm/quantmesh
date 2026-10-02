package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/position"
)

func TestCapitalReconciliationAccountingLockWaitDeadline(t *testing.T) {
	allocator := capitalReleaseFixture(t)
	mse := NewMultiStrategyExecutor(nil, allocator)
	releaseBarrier, err := mse.BeginCapitalReconciliation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseBarrier()
	mse.mu.Lock()
	mse.ordersByClient["pending"] = &orderCapital{}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := allocator.ReleaseVerified(ctx, "dca", func(proofCtx context.Context) error {
			return mse.VerifyCapitalReleaseAccounting(proofCtx)
		})
		done <- err
	}()
	select {
	case err = <-done:
		mse.mu.Unlock()
	case <-time.After(time.Second):
		mse.mu.Unlock()
		err = <-done
		t.Error("dispatcher proof lock wait outlived request deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) || allocator.GetUsed("dca") != 200 {
		t.Errorf("timeout proof changed capital: err=%v used=%v", err, allocator.GetUsed("dca"))
	}
	if err := mse.VerifyCapitalReleaseAccounting(t.Context()); err == nil {
		t.Fatal("timeout erased unresolved order ownership")
	}
	releaseBarrier()
	finish, err := mse.beginSubmission(&position.OrderRequest{ClientOrderID: "after-timeout"})
	if err != nil {
		t.Fatalf("cancelled proof left trading blocked: %v", err)
	}
	finish()
	mse.mu.Lock()
	delete(mse.ordersByClient, "pending") // Only the fixture reconciles the order.
	mse.mu.Unlock()
	releaseRetry, err := mse.BeginCapitalReconciliation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRetry()
	released, err := allocator.ReleaseVerified(t.Context(), "dca", func(proofCtx context.Context) error {
		return mse.VerifyCapitalReleaseAccounting(proofCtx)
	})
	if err != nil || released["dca"] != 200 {
		t.Fatalf("legitimate recovery failed: released=%v err=%v", released, err)
	}
}

func TestCapitalReconciliationAccountingRejectsMissingOrCancelledContext(t *testing.T) {
	mse := NewMultiStrategyExecutor(nil, nil)
	if err := mse.VerifyCapitalReleaseAccounting(nil); err == nil {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := mse.VerifyCapitalReleaseAccounting(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled proof: %v", err)
	}
	if !mse.mu.TryLock() {
		t.Fatal("cancelled proof retained dispatcher lock")
	}
	mse.mu.Unlock()
	if err := mse.VerifyCapitalReleaseAccounting(t.Context()); err != nil {
		t.Fatalf("valid proof after cancellation: %v", err)
	}
}
