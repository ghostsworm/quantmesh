package binance

import (
	"context"
	"fmt"
	"time"
)

// A timeout is not completion. Callers may retry waiting on the same generation;
// neither the running state nor the done channel is replaced before all workers exit.
func waitOrderStreamExit(done <-chan struct{}, timeout time.Duration) error {
	select {
	case <-done:
		return nil
	default:
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		return fmt.Errorf("order stream workers have not exited: %w", context.DeadlineExceeded)
	}
}

func (w *WebSocketManager) StopWithError() error {
	w.mu.Lock()
	if !w.isRunning {
		w.mu.Unlock()
		return nil
	}
	stop, done, cancel, timeout := w.stopC, w.doneC, w.cancelWorkers, w.closeTimeout
	select {
	case <-stop:
	default:
		close(stop)
	}
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if err := waitOrderStreamExit(done, timeout); err != nil {
		return err
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.stopC != stop {
		return fmt.Errorf("order stream generation changed during stop")
	}
	return nil
}

func (w *SpotUserDataWebSocketManager) StopWithError() error {
	w.mu.Lock()
	if !w.isRunning {
		w.mu.Unlock()
		return nil
	}
	stop, done, cancel, timeout := w.stopC, w.doneC, w.cancelWorkers, w.closeTimeout
	select {
	case <-stop:
	default:
		close(stop)
	}
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if err := waitOrderStreamExit(done, timeout); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopC != stop {
		return fmt.Errorf("spot order stream generation changed during stop")
	}
	return nil
}
