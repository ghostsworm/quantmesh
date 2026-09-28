package optimrun

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

var taskIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func IsValidTaskID(id string) bool { return taskIDPattern.MatchString(id) }

type taskRun struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func (m *OptimTaskManager) beginRun(id string) (*taskRun, error) {
	if !taskIDPattern.MatchString(id) {
		return nil, fmt.Errorf("invalid optimizer task id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.running[id]; exists || m.deleting[id] {
		return nil, fmt.Errorf("task %s already running or deleting", id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	run := &taskRun{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	m.running[id] = run
	return run, nil
}

func (m *OptimTaskManager) finishRun(id string, run *taskRun) {
	run.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.running, id)
	close(run.done)
}

func (m *OptimTaskManager) executeRun(id string, run *taskRun) error {
	defer m.finishRun(id, run)
	err := m.runTask(run.ctx, id)
	if run.ctx.Err() != nil {
		now := time.Now()
		if statusErr := m.store.UpdateOptimTaskStatus(id, "stopped", nil, &now, run.ctx.Err().Error(), ""); statusErr != nil {
			return statusErr
		}
		return run.ctx.Err()
	}
	return err
}

// DeleteTask cancels and waits before touching persistence. A timeout never
// implies the worker stopped; the task and result are retained for a retry.
func (m *OptimTaskManager) DeleteTask(ctx context.Context, id string) error {
	if !taskIDPattern.MatchString(id) {
		return fmt.Errorf("invalid optimizer task id")
	}
	m.mu.Lock()
	if m.deleting[id] {
		m.mu.Unlock()
		return fmt.Errorf("task %s already deleting", id)
	}
	m.deleting[id] = true
	run := m.running[id]
	if run != nil {
		run.cancel()
	}
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.deleting, id); m.mu.Unlock() }()
	if run != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-run.done:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Keep the record if result cleanup fails, so the user can retry deletion.
	if err := os.Remove(filepath.Join(m.resultsDir, id+".json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return m.store.DeleteOptimTask(id)
}
