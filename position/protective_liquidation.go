package position

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/event"
	"quantmesh/logger"
)

const protectiveLiquidationBlock = "protective_liquidation"

type ProtectiveLiquidationStatus struct {
	State      string    `json:"state"`
	Reason     string    `json:"reason,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type protectiveLiquidation struct {
	mu       sync.Mutex
	queued   atomic.Bool
	ctx      context.Context
	venue    LiquidationVenue
	schedule func(func())
	stopped  bool
	done     chan struct{}
	cancel   context.CancelFunc
	err      error
	status   ProtectiveLiquidationStatus
}

// ConfigureProtectiveLiquidation is called before price delivery. A nil
// scheduler runs in a goroutine; replay supplies an after-tick queue so the
// same verified workflow runs outside AdjustOrders' lock deterministically.
// A custom scheduler must enqueue the work, never execute it inline.
func (spm *SuperPositionManager) ConfigureProtectiveLiquidation(ctx context.Context, venue LiquidationVenue, schedule func(func())) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p := &spm.protective
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.queued.Load() || p.stopped {
		return fmt.Errorf("protective liquidation is active or stopped")
	}
	if venue == nil {
		return fmt.Errorf("protective liquidation requires a verification venue")
	}
	if _, ok := spm.executor.(ContextBatchOrderExecutor); !ok {
		return fmt.Errorf("protective liquidation requires a context batch executor")
	}
	if _, ok := spm.executor.(ContextOrderExecutor); !ok {
		return fmt.Errorf("protective liquidation requires a context order executor")
	}
	p.ctx, p.venue, p.schedule = ctx, venue, schedule
	return nil
}

// Called under spm.mu: reserve/deduplicate before scheduling, never run network
// verification under that lock. Failure retains a separate non-resumable hold.
func (spm *SuperPositionManager) scheduleProtectiveLiquidation(reason string, stopAfter bool) error {
	p := &spm.protective
	p.mu.Lock()
	if p.queued.Load() {
		p.mu.Unlock()
		return nil
	}
	if p.stopped {
		p.mu.Unlock()
		return fmt.Errorf("protective liquidation lifecycle has stopped")
	}
	spm.openingGate.Block(protectiveLiquidationBlock)
	p.status = ProtectiveLiquidationStatus{State: "queued", Reason: reason, StartedAt: spm.now()}
	if p.venue == nil {
		p.err = fmt.Errorf("protective liquidation has no configured verification venue")
		p.status.State, p.status.Error, p.status.FinishedAt = "failed", p.err.Error(), spm.now()
		spm.liquidationNeedsReconciliation.Store(true)
		err := p.err
		p.mu.Unlock()
		spm.publishProtectiveLiquidation("failed", reason, err)
		return err
	}
	p.queued.Store(true)
	p.done = make(chan struct{})
	done := p.done
	ctx, cancel := context.WithCancel(p.ctx)
	p.cancel = cancel
	venue, schedule := p.venue, p.schedule
	callback := spm.requestStopFunc
	p.err = nil
	p.mu.Unlock()
	work := func() {
		p.mu.Lock()
		p.status.State = "running"
		p.mu.Unlock()
		err := spm.LiquidateAllVerified(ctx, venue, 0)
		cancel()
		if err != nil {
			spm.liquidationNeedsReconciliation.Store(true)
		}
		p.mu.Lock()
		p.err = err
		p.status.State, p.status.FinishedAt = "completed", spm.now()
		if err != nil {
			p.status.State, p.status.Error = "failed", err.Error()
		}
		p.cancel = nil
		p.queued.Store(false)
		close(done) // callback may stop the Bot and wait; never wait on ourselves
		p.mu.Unlock()
		spm.publishProtectiveLiquidation("completed", reason, err)
		if err == nil && stopAfter && callback != nil {
			callback()
		}
	}
	if schedule == nil {
		go work()
	} else {
		schedule(work)
	}
	return nil
}

func (spm *SuperPositionManager) publishProtectiveLiquidation(state, reason string, err error) {
	if err != nil {
		state = "failed"
		logger.Error("[%s] 保護性平倉核實失敗: %v", spm.logPrefix(), err)
	}
	if spm.eventBus != nil {
		data := map[string]interface{}{"bot_id": spm.botID, "symbol": spm.config.Trading.Symbol, "exchange": spm.exchangeName,
			"reason": "protective_liquidation_" + state, "trigger": reason, "state": state, "requires_reconciliation": err != nil}
		if err != nil {
			data["error"] = err.Error()
		}
		spm.eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Timestamp: spm.now(), Data: data})
	}
}

func (spm *SuperPositionManager) GetProtectiveLiquidationStatus() ProtectiveLiquidationStatus {
	p := &spm.protective
	p.mu.Lock()
	defer p.mu.Unlock()
	status := p.status
	if status.State == "" {
		status.State = "idle"
	}
	return status
}

func (spm *SuperPositionManager) WaitProtectiveLiquidation(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	p := &spm.protective
	p.mu.Lock()
	done, err := p.done, p.err
	p.mu.Unlock()
	if done == nil {
		return err
	}
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Stop cancels an owned worker and waits before order streams are torn down.
// Cancelled/failed liquidation cannot be followed by blind close_on_stop retry.
func (spm *SuperPositionManager) StopProtectiveLiquidation(ctx context.Context) error {
	p := &spm.protective
	p.mu.Lock()
	p.stopped = true
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()
	return spm.WaitProtectiveLiquidation(ctx)
}

// ResumeOpeningManually releases successful protection only on explicit user
// action. Automatic recovery must use ResumeOpening and retain this hold.
func (spm *SuperPositionManager) ResumeOpeningManually() error {
	p := &spm.protective
	p.mu.Lock()
	if spm.openingGate.HasBlock(protectiveLiquidationBlock) {
		if p.queued.Load() || p.stopped || p.status.State != "completed" || spm.liquidationNeedsReconciliation.Load() {
			p.mu.Unlock()
			return fmt.Errorf("protective liquidation is not verified or lifecycle has stopped")
		}
		spm.openingGate.Unblock(protectiveLiquidationBlock)
	}
	p.mu.Unlock()
	spm.ResumeOpening()
	return nil
}
