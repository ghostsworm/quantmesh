package optimrun

import (
	"context"
	"errors"
	"quantmesh/backtest"
	"testing"
	"time"
)

func TestDeleteWaitsForWorkerAndBlocksRestart(t *testing.T) {
	store := &fakeOptimStore{}
	m := NewOptimTaskManager(store, nil)
	m.resultsDir = t.TempDir()
	run, err := m.beginRun("task-one")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.DeleteTask(context.Background(), "task-one") }()
	select {
	case <-run.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("delete did not cancel worker")
	}
	select {
	case err := <-done:
		t.Fatalf("delete returned before worker exit: %v", err)
	default:
	}
	if _, err := m.beginRun("task-one"); err == nil {
		t.Fatal("restart allowed during deletion")
	}
	m.finishRun("task-one", run)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if store.deleteCalls != 1 {
		t.Fatalf("delete calls=%d", store.deleteCalls)
	}
}

func TestDeleteTimeoutRetainsTaskUntilWorkerActuallyExits(t *testing.T) {
	store := &fakeOptimStore{}
	m := NewOptimTaskManager(store, nil)
	m.resultsDir = t.TempDir()
	run, err := m.beginRun("task-one")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.DeleteTask(ctx, "task-one"); err != context.Canceled {
		t.Fatalf("delete error=%v", err)
	}
	if store.deleteCalls != 0 {
		t.Fatal("timeout removed record")
	}
	if _, err := m.beginRun("task-one"); err == nil {
		t.Fatal("timeout treated live worker as stopped")
	}
	m.finishRun("task-one", run)
	if err := m.DeleteTask(context.Background(), "task-one"); err != nil {
		t.Fatal(err)
	}
	if store.deleteCalls != 1 {
		t.Fatal("retry did not delete")
	}
}

func TestOptimizerTaskIDsCannotEscapeResultDirectory(t *testing.T) {
	m := NewOptimTaskManager(&fakeOptimStore{}, nil)
	for _, id := range []string{"", "../other", "/tmp/other", "a/b", "a\\b"} {
		if _, err := m.beginRun(id); err == nil {
			t.Fatalf("accepted %q", id)
		}
		if err := m.DeleteTask(context.Background(), id); err == nil {
			t.Fatalf("deleted %q", id)
		}
		if _, err := LoadOptimResult(t.TempDir(), id); err == nil {
			t.Fatalf("loaded %q", id)
		}
	}
}

func TestReservedTaskCanceledBeforeExecutionBecomesStopped(t *testing.T) {
	store := &fakeOptimStore{}
	m := NewOptimTaskManager(store, nil)
	run, err := m.beginRun("task-one")
	if err != nil {
		t.Fatal(err)
	}
	run.cancel()
	if err := m.executeRun("task-one", run); err != context.Canceled {
		t.Fatalf("execution=%v", err)
	}
	select {
	case <-run.done:
	default:
		t.Fatal("worker termination not published")
	}
	if len(m.running) != 0 || len(store.statusCalls) != 1 || store.statusCalls[0] != "stopped" {
		t.Fatalf("incorrect canceled state: running=%d status=%v", len(m.running), store.statusCalls)
	}
}

func TestFailedTaskCreationReleasesReservationAndUsesUniqueIDs(t *testing.T) {
	store := &fakeOptimStore{createErr: errors.New("store unavailable")}
	m := NewOptimTaskManager(store, nil)
	ids := map[string]bool{}
	for i := 0; i < 20; i++ {
		task := &backtest.OptimTask{}
		if err := m.CreateAndRun(task); !errors.Is(err, store.createErr) {
			t.Fatalf("creation=%v", err)
		}
		if !IsValidTaskID(task.ID) || ids[task.ID] {
			t.Fatalf("invalid or reused id %q", task.ID)
		}
		ids[task.ID] = true
		if len(m.running) != 0 {
			t.Fatal("failed creation leaked reservation")
		}
	}
}
