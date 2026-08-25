package optimrun

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/backtest"
)

type fakeOptimStore struct {
	task         *backtest.OptimTask
	createErr    error
	statusCalls  []string
	progressDone int
}

func (s *fakeOptimStore) CreateOptimTask(task *backtest.OptimTask) error {
	if s.createErr != nil {
		return s.createErr
	}
	copied := *task
	s.task = &copied
	return nil
}
func (s *fakeOptimStore) GetOptimTask(id string) (*backtest.OptimTask, error) {
	if s.task == nil || s.task.ID != id {
		return nil, nil
	}
	return s.task, nil
}
func (s *fakeOptimStore) ListOptimTasks(limit, offset int) ([]*backtest.OptimTask, error) {
	if s.task == nil {
		return nil, nil
	}
	return []*backtest.OptimTask{s.task}, nil
}
func (s *fakeOptimStore) UpdateOptimTaskProgress(id string, completed int, progress int) error {
	s.progressDone = completed
	return nil
}
func (s *fakeOptimStore) UpdateOptimTaskStatus(id, status string, startedAt, completedAt *time.Time, errMsg, resultPath string) error {
	s.statusCalls = append(s.statusCalls, status)
	return nil
}
func (s *fakeOptimStore) DeleteOptimTask(id string) error { return nil }

func TestOptimTaskManagerHelpersAndFailurePaths(t *testing.T) {
	store := &fakeOptimStore{}
	manager := NewOptimTaskManager(store, map[string]string{"api_key": "k"})
	if manager.GetStore() != store {
		t.Fatal("GetStore did not return original store")
	}
	if manager.resultsDir != filepath.Join("backtest", "optim_results") {
		t.Fatalf("unexpected results dir: %s", manager.resultsDir)
	}

	space := toOptimizerSearchSpace(backtest.OptimSearchSpace{
		Strategy: "grid",
		Ranges: map[string]backtest.OptimParamRange{
			"grid_count": {Min: 2, Max: 4, Step: 1},
		},
	})
	if space.Strategy != "grid" || space.Ranges["grid_count"].Max != 4 {
		t.Fatalf("unexpected converted search space: %+v", space)
	}

	createErr := errors.New("store down")
	store.createErr = createErr
	err := manager.CreateAndRun(&backtest.OptimTask{
		ID:       "task-1",
		Strategy: "grid",
		Symbol:   "BTCUSDT",
		Interval: "1m",
		SearchSpace: backtest.OptimSearchSpace{Strategy: "grid", Ranges: map[string]backtest.OptimParamRange{
			"grid_count": {Min: 1, Max: 1, Step: 1},
		}},
	})
	if !errors.Is(err, createErr) {
		t.Fatalf("CreateAndRun error = %v", err)
	}

	store.createErr = nil
	if err := manager.RunTask("missing"); err != nil {
		t.Fatalf("RunTask missing should not return error: %v", err)
	}
	if len(store.statusCalls) == 0 || store.statusCalls[len(store.statusCalls)-1] != "failed" {
		t.Fatalf("missing task should be marked failed: %+v", store.statusCalls)
	}

	manager.running["busy"] = struct{}{}
	if err := manager.RunTask("busy"); err == nil {
		t.Fatal("RunTask should reject already running task")
	}
	delete(manager.running, "busy")
}

func TestLoadOptimResult(t *testing.T) {
	dir := t.TempDir()
	resultPath := filepath.Join(dir, "task-1.json")
	body := []byte(`{"task_id":"task-1","strategy":"grid","symbol":"BTCUSDT","total_combos":3,"completed":2}`)
	if err := os.WriteFile(resultPath, body, 0600); err != nil {
		t.Fatalf("write result: %v", err)
	}
	result, err := LoadOptimResult(dir, "task-1")
	if err != nil {
		t.Fatalf("LoadOptimResult: %v", err)
	}
	if result.TaskID != "task-1" || result.Strategy != "grid" || result.Symbol != "BTCUSDT" {
		t.Fatalf("unexpected result: %+v", result)
	}

	if _, err := LoadOptimResult(dir, "missing"); err == nil {
		t.Fatal("missing result should return error")
	}

	invalidPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(invalidPath, []byte(`{`), 0600); err != nil {
		t.Fatalf("write invalid result: %v", err)
	}
	if _, err := LoadOptimResult(dir, "bad"); err == nil {
		t.Fatal("invalid result JSON should return error")
	}
}
