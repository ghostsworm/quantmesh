package strategy

import (
	"context"
	"errors"
	"testing"
)

type failedRollbackStrategy struct {
	routingTestStrategy
	startErr, stopErr error
	stops             int
	onStop            func()
	received          context.Context
}

func (s *failedRollbackStrategy) Start(ctx context.Context) error {
	s.received = ctx
	return s.startErr
}
func (s *failedRollbackStrategy) Stop() error {
	s.stops++
	if s.onStop != nil {
		s.onStop()
	}
	return s.stopErr
}

func TestStrategyManagerStartupFreezesAllLoopsBeforeRollback(t *testing.T) {
	manager := startupContextManager("a", "b")
	first := &failedRollbackStrategy{}
	failing := &failedRollbackStrategy{startErr: errors.New("startup failed")}
	activeDuringRollback := false
	failing.onStop = func() { activeDuringRollback = first.received.Err() == nil }
	manager.RegisterStrategy("a", first, 1, 0)
	manager.RegisterStrategy("b", failing, 1, 0)
	if err := manager.StartAll(); err == nil {
		t.Fatal("startup failure ignored")
	}
	if activeDuringRollback {
		t.Fatal("earlier trading loop remained active while failed strategy was rolling back")
	}
}

func TestStrategyManagerStartupPreservesEveryRollbackFailure(t *testing.T) {
	startup := errors.New("startup rejected")
	firstStop, failedStop := errors.New("first close unverified"), errors.New("failed strategy stop unverified")
	manager := startupContextManager("a", "b", "c")
	first := &failedRollbackStrategy{stopErr: firstStop}
	failing := &failedRollbackStrategy{startErr: startup, stopErr: failedStop}
	unstarted := &failedRollbackStrategy{}
	manager.RegisterStrategy("a", first, 1, 0)
	manager.RegisterStrategy("b", failing, 1, 0)
	manager.RegisterStrategy("c", unstarted, 1, 0)
	err := manager.StartAll()
	var rollback *StrategyStartupRollbackError
	if !errors.As(err, &rollback) {
		t.Fatalf("missing explicit unverified rollback state: %v", err)
	}
	if !errors.Is(err, startup) || !errors.Is(err, firstStop) || !errors.Is(err, failedStop) {
		t.Fatalf("startup lost unresolved rollback causes: %v", err)
	}
	if first.stops != 1 || failing.stops != 1 || unstarted.stops != 0 {
		t.Fatal("rollback omitted a started strategy or stopped an unstarted strategy")
	}
}

type cancelledUnverifiedFundingStartup struct {
	Strategy
	carry  *FundingCarryStrategy
	cancel context.CancelFunc
}

func (s cancelledUnverifiedFundingStartup) Start(ctx context.Context) error {
	if err := s.carry.Start(ctx); err != nil {
		return err
	}
	s.carry.MarkExecutionUnknown(errors.New("injected ambiguous execution"))
	s.cancel()
	return nil
}

func TestFundingCarryStartupCancellationPreservesUnverifiedCloseFailure(t *testing.T) {
	carry, margin, _ := newFundingCarryRepayIntentFixture()
	margin.positions = nil
	carry.direction, carry.marginDebt, carry.marginBorrowTransferID, carry.strategySpotKnown = DirectionNone, 0, 0, true
	if err := carry.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := startupContextManager("funding_carry")
	manager.RegisterStrategy("funding_carry", cancelledUnverifiedFundingStartup{Strategy: carry, carry: carry, cancel: cancel}, 1, 0)
	err := manager.StartAllContext(ctx)
	var rollback *StrategyStartupRollbackError
	if !errors.Is(err, context.Canceled) || !errors.As(err, &rollback) || carry.stopErr == nil || !errors.Is(err, carry.stopErr) {
		t.Fatalf("lost real unverified close after cancelled startup: %v", err)
	}
	if carry.IsRunning() || carry.stopCompleted || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
		t.Fatal("unverified rollback falsely closed or resumed trading")
	}
}
