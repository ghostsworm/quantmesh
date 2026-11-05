package optimrun

import (
	"errors"
	"quantmesh/backtest"
	"testing"
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
	if err := manager.CreateAndRun(task); !errors.Is(err, ErrOptimizerCapacity) {
		t.Fatalf("CreateAndRun error=%v, want capacity error", err)
	}
	if store.task != nil || len(manager.running) != 1 {
		t.Fatalf("over-capacity task was persisted or reserved: task=%v running=%d", store.task, len(manager.running))
	}
}
