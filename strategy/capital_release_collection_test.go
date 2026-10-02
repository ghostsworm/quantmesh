package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

func TestCapitalReleaseStrategyCollectionWaitCancels(t *testing.T) {
	manager := NewStrategyManager(&config.Config{}, 1000)
	allocator := capitalReleaseFixture(t)
	mse := NewMultiStrategyExecutor(nil, allocator)
	releaseBarrier, err := mse.BeginCapitalReconciliation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseBarrier()
	manager.mu.Lock()
	manager.strategies["dca"] = &DCAEnhancedStrategy{}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := allocator.ReleaseVerified(ctx, "dca", func(proofCtx context.Context) error {
			_, err := manager.GetAllStrategiesContext(proofCtx)
			return err
		})
		done <- err
	}()
	select {
	case err = <-done:
		manager.mu.Unlock()
	case <-time.After(time.Second):
		manager.mu.Unlock()
		err = <-done
		t.Error("strategy collection lookup remained blocked after proof deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) || allocator.GetUsed("dca") != 200 {
		t.Fatalf("cancelled collection proof changed capital: err=%v used=%v", err, allocator.GetUsed("dca"))
	}
	releaseBarrier()
	finish, err := mse.beginSubmission(&position.OrderRequest{ClientOrderID: "after-collection-timeout"})
	if err != nil {
		t.Fatal(err)
	}
	finish()
	releaseRetry, err := mse.BeginCapitalReconciliation(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRetry()
	released, err := allocator.ReleaseVerified(t.Context(), "dca", func(proofCtx context.Context) error {
		rows, err := manager.GetAllStrategiesContext(proofCtx)
		if err == nil && rows["dca"] == nil {
			return errors.New("registered strategy missing from snapshot")
		}
		return err
	})
	if err != nil || released["dca"] != 200 {
		t.Fatalf("legitimate retry failed: %v, %v", released, err)
	}
}

func TestCapitalReleaseStrategyCollectionContextAndCopy(t *testing.T) {
	manager := NewStrategyManager(&config.Config{}, 1000)
	if rows, err := manager.GetAllStrategiesContext(nil); rows != nil || err == nil {
		t.Fatalf("nil context: %v, %v", rows, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rows, err := manager.GetAllStrategiesContext(ctx); rows != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context: %v, %v", rows, err)
	}
	manager.RegisterStrategy("dca", &DCAEnhancedStrategy{}, 1, 0)
	rows, err := manager.GetAllStrategiesContext(t.Context())
	if err != nil || len(rows) != 1 || rows["dca"] != manager.GetStrategy("dca") {
		t.Fatalf("snapshot mismatch: %v, %v", rows, err)
	}
	delete(rows, "dca")
	if manager.GetStrategy("dca") == nil {
		t.Fatal("snapshot map aliases manager registry")
	}
}
