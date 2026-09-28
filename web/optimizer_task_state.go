package web

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"quantmesh/backtest/optimizer"
)

func newOptimizerTaskID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("opt_%x", id), nil
}

func optimizerTerminal(status string) bool {
	return status == "completed" || status == "failed" || status == "stopped"
}

func snapshotOptimizerTask(id string) (optimizerTask, bool) {
	optimizerTasksMu.RLock()
	defer optimizerTasksMu.RUnlock()
	task, ok := optimizerTasks[id]
	if !ok {
		return optimizerTask{}, false
	}
	return *task, true
}

func beginOptimizerPhase(ctx context.Context, id, phase string) bool {
	optimizerTasksMu.Lock()
	defer optimizerTasksMu.Unlock()
	task, ok := optimizerTasks[id]
	if !ok || optimizerTerminal(task.Status) {
		return false
	}
	task.UpdatedAt = time.Now()
	if ctx.Err() != nil || task.Status == "stopping" {
		task.Status = "stopped"
		return false
	}
	task.Status = phase
	return true
}

// Stop requests and result publication share the same lock. A stop accepted
// before publication wins; stopping is only acknowledged as stopped by a worker.
func finishOptimizerTask(ctx context.Context, id string, result *optimizer.OptimResult, runErr error) bool {
	optimizerTasksMu.Lock()
	defer optimizerTasksMu.Unlock()
	task, ok := optimizerTasks[id]
	if !ok || optimizerTerminal(task.Status) {
		return false
	}
	task.UpdatedAt = time.Now()
	if ctx.Err() != nil || task.Status == "stopping" {
		task.Status, task.Result, task.Error = "stopped", nil, ""
		return false
	}
	if runErr != nil || result == nil {
		task.Status = "failed"
		if runErr != nil {
			task.Error = runErr.Error()
		} else {
			task.Error = "optimizer returned no result"
		}
		return false
	}
	task.Status, task.Progress, task.Result = "completed", 100, result
	return true
}
