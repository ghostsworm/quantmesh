package processor

import (
	"testing"
	"time"
)

func TestTaskProcessorLifecycleGuards(t *testing.T) {
	p := NewTaskProcessor(nil, nil)
	if p == nil {
		t.Fatal("NewTaskProcessor returned nil")
	}
	if p.checkInterval != 2*time.Second {
		t.Fatalf("check interval = %v", p.checkInterval)
	}
	if cap(p.workerPool) != 10 {
		t.Fatalf("worker pool capacity = %d", cap(p.workerPool))
	}

	p.Stop()
	if p.isRunning {
		t.Fatal("Stop on a non-running processor should keep it stopped")
	}

	p.isRunning = true
	p.Stop()
	if p.isRunning {
		t.Fatal("Stop should mark processor as stopped")
	}

	p.Stop()
}
