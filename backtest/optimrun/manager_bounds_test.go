package optimrun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/visionfeed"
	"quantmesh/exchange"
)

func TestCreateAndRunRejectsOversizedSpaceBeforePersistence(t *testing.T) {
	store := &fakeOptimStore{}
	manager := NewOptimTaskManager(store, nil)
	task := validReplayTask()
	task.SearchSpace.Ranges["grid_spacing"] = backtest.OptimParamRange{Min: 1, Max: 1e9, Step: 0.01}
	if err := manager.CreateAndRun(task); err == nil {
		t.Fatal("oversized task accepted")
	}
	if store.task != nil || len(manager.running) != 0 || len(store.statusCalls) != 0 {
		t.Fatal("invalid task reached persistence or execution")
	}
}

func TestCreateAndRunRejectsConcurrentOptimizerBeforePersistence(t *testing.T) {
	store := &fakeOptimStore{}
	manager := NewOptimTaskManager(store, nil)
	active, err := manager.beginRun("active")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.finishRun("active", active)

	task := validReplayTask()
	manager.replayVision = func(context.Context, visionfeed.LoadRequest) (visionfeed.LoadResult, error) {
		return visionResult(task, false), nil
	}
	if err := manager.CreateAndRun(task); !errors.Is(err, ErrOptimizerCapacity) {
		t.Fatalf("CreateAndRun error=%v, want capacity error", err)
	}
	if store.task != nil || len(manager.running) != 1 {
		t.Fatalf("over-capacity task was persisted or reserved: task=%v running=%d", store.task, len(manager.running))
	}
}

func TestCreateAndRunRejectsMissingReplayDataBeforePersistence(t *testing.T) {
	store := &fakeOptimStore{}
	restCalled := false
	manager := NewOptimTaskManager(store, nil, WithHistoricalDataLoader(func(context.Context, string, string, time.Time, time.Time) ([]*exchange.Candle, error) {
		restCalled = true
		return nil, nil
	}), WithReplayVisionLoader(func(context.Context, visionfeed.LoadRequest) (visionfeed.LoadResult, error) {
		return visionfeed.LoadResult{}, errors.New("visionfeed: coverage: missing 1m file for 2025-01-02")
	}))
	task := validReplayTask()
	if err := manager.CreateAndRun(task); !errors.Is(err, ErrInvalidOptimTask) {
		t.Fatalf("CreateAndRun error = %v, want invalid task", err)
	}
	if store.task != nil || len(manager.running) != 0 || len(store.statusCalls) != 0 {
		t.Fatal("unreadable replay data reached persistence or reserved capacity")
	}
	if task.ID != "" {
		t.Fatalf("rejected task was assigned an id %q", task.ID)
	}
	if restCalled {
		t.Fatal("rejected replay task reached the live exchange loader")
	}
}

func TestCreateAndRunDoesNotReportTheDataDirectory(t *testing.T) {
	root := t.TempDir()
	store := &fakeOptimStore{}
	manager := NewOptimTaskManager(store, nil, WithVisionRoot(root))
	task := validReplayTask()
	err := manager.CreateAndRun(task)
	if !errors.Is(err, ErrInvalidOptimTask) {
		t.Fatalf("CreateAndRun error = %v", err)
	}
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "no such file") {
		t.Fatalf("directory leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
	if store.task != nil || len(manager.running) != 0 || len(store.statusCalls) != 0 {
		t.Fatal("missing data was stored or kept the slot")
	}
}
