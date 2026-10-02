package strategy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"quantmesh/execution"
	"quantmesh/lock"
)

const (
	fundingCarryWalletLockTTL           = 60 * time.Second
	fundingCarryWalletUnlockBudget      = 5 * time.Second
	strategyWalletRuntimeOwnershipBlock = "runtime_ownership_unverified"
)

var fundingCarryWalletGates sync.Map // map[account scope]chan struct{}

// WithAccountWalletCoordination shares the exact lease used by borrowing,
// repayment and Funding Carry wallet mutations. Runtime verification must hold
// it before draining strategy dispatches and acquiring physical snapshot locks.
func WithAccountWalletCoordination(ctx context.Context, coordinator lock.DistributedLock, key string, operation func(context.Context) error) error {
	if ctx == nil || coordinator == nil || key == "" || operation == nil {
		return fmt.Errorf("wallet verification requires context, coordinator, owner key and operation")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return withAccountWalletCoordination(ctx, coordinator, key, operation)
}

func localFundingCarryWalletGate(key string) chan struct{} {
	gate, _ := fundingCarryWalletGates.LoadOrStore(key, func() chan struct{} {
		created := make(chan struct{}, 1)
		created <- struct{}{}
		return created
	}())
	return gate.(chan struct{})
}

// withAccountWalletCoordination first serializes this account in-process, then
// takes the configured distributed lease for other application instances.
// Losing the lease cancels every exchange call using the operation context.
func (s *FundingCarryStrategy) withAccountWalletCoordination(ctx context.Context, operation func(context.Context) error) error {
	s.mu.RLock()
	coordinator, key, gate := s.accountWalletLock, s.accountWalletLockKey, s.openingGate
	s.mu.RUnlock()
	if err := verifyStrategyWalletRuntimeOwner(gate); err != nil {
		return err
	}
	return withAccountWalletCoordination(ctx, coordinator, key, func(operationCtx context.Context) error {
		if err := verifyStrategyWalletRuntimeOwner(gate); err != nil {
			return err
		}
		return operation(operationCtx)
	})
}

func verifyStrategyWalletRuntimeOwner(gate *execution.OpeningGate) error {
	if gate != nil && gate.HasBlock(strategyWalletRuntimeOwnershipBlock) {
		return fmt.Errorf("strategy wallet operation refused because runtime ownership is unverified")
	}
	return nil
}

// withAccountWalletCoordination serializes every strategy that mutates the
// same exchange account wallet using one shared in-process gate and lease.
func withAccountWalletCoordination(ctx context.Context, coordinator lock.DistributedLock, key string, operation func(context.Context) error) error {
	if key == "" {
		return operation(ctx) // unit/legacy constructors without runtime wiring
	}

	localGate := localFundingCarryWalletGate(key)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-localGate:
	}
	defer func() { localGate <- struct{}{} }()

	if coordinator == nil {
		return fmt.Errorf("account wallet distributed coordinator is unavailable")
	}
	if err := coordinator.Lock(ctx, key, fundingCarryWalletLockTTL); err != nil {
		return fmt.Errorf("acquire account wallet coordination lock: %w", err)
	}

	operationCtx, cancelOperation := context.WithCancel(ctx)
	var lostMu sync.Mutex
	var leaseErr error
	stopRenewal := lock.StartAutoRenew(coordinator, key, fundingCarryWalletLockTTL, func(err error) {
		lostMu.Lock()
		leaseErr = err
		lostMu.Unlock()
		cancelOperation()
	})
	operationErr := operation(operationCtx)
	stopRenewal()
	cancelOperation()

	unlockCtx, cancelUnlock := context.WithTimeout(context.Background(), fundingCarryWalletUnlockBudget)
	unlockErr := coordinator.Unlock(unlockCtx, key)
	cancelUnlock()

	lostMu.Lock()
	leaseLost := leaseErr
	lostMu.Unlock()
	if leaseLost != nil {
		return errors.Join(fmt.Errorf("account wallet coordination lease was lost: %w", leaseLost), operationErr, unlockErr)
	}
	if operationErr != nil || unlockErr != nil {
		return errors.Join(operationErr, wrapWalletUnlockError(unlockErr))
	}
	return nil
}

func wrapWalletUnlockError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("release account wallet coordination lock: %w", err)
}
