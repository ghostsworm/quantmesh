package optimrun

import (
	"quantmesh/backtest"
	"testing"
)

func TestCreateAndRunRejectsOversizedSpaceBeforePersistence(t *testing.T) {
	store := &fakeOptimStore{}
	manager := NewOptimTaskManager(store, nil)
	task := &backtest.OptimTask{SearchSpace: backtest.OptimSearchSpace{
		Strategy: "grid", Ranges: map[string]backtest.OptimParamRange{
			"grid_spacing": {Min: 1, Max: 1e9, Step: 0.01},
		},
	}}
	if err := manager.CreateAndRun(task); err == nil {
		t.Fatal("oversized task accepted")
	}
	if store.task != nil || len(manager.running) != 0 || len(store.statusCalls) != 0 {
		t.Fatal("invalid task reached persistence or execution")
	}
}
