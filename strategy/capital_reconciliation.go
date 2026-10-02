package strategy

import (
	"context"
	"fmt"
	"sync"

	"quantmesh/order"
)

const capitalReconciliationSourcePrefix = "strategy_capital_reconciliation_"

// Immutable constructor bindings must match the allocator being mutated and
// the physical executor whose coordination barrier supplies the proof.
func (mse *MultiStrategyExecutor) VerifyCapitalReleaseBinding(allocator *CapitalAllocator, executor *order.ExchangeOrderExecutor) error {
	if allocator == nil || executor == nil || mse.allocator != allocator || mse.executor != executor {
		return fmt.Errorf("strategy capital release components have mismatched owner bindings")
	}
	return nil
}

// BeginCapitalReconciliation blocks BEFORE reservation, and drains complete
// strategy dispatches, including work waiting for the physical submission lock.
// Acquire this barrier BEFORE the executor's position snapshot lease; doing it
// in the reverse order would deadlock against already reserved, queued work.
// This covers openings and closes because both mutate capital accounting.
func (mse *MultiStrategyExecutor) BeginCapitalReconciliation(ctx context.Context) (func(), error) {
	if ctx == nil {
		return nil, fmt.Errorf("capital reconciliation requires context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source := fmt.Sprintf("%s%d", capitalReconciliationSourcePrefix, mse.capitalReconciliationSequence.Add(1))
	mse.capitalSubmissionGate.Block(source)
	if err := mse.capitalSubmissionGate.Drain(ctx); err != nil {
		mse.capitalSubmissionGate.Unblock(source)
		return nil, fmt.Errorf("drain strategy capital submissions: %w", err)
	}
	if err := ctx.Err(); err != nil {
		mse.capitalSubmissionGate.Unblock(source)
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { mse.capitalSubmissionGate.Unblock(source) }) }, nil
}

// VerifyCapitalReleaseAccounting checks the dispatcher's ownership ledger, not
// merely the allocator's aggregate Used number. The caller must keep its
// reconciliation barrier held through live verification and allocator release.
// A residual lot is a reconciliation task, not permission to erase exposure.
func (mse *MultiStrategyExecutor) VerifyCapitalReleaseAccounting(ctx context.Context) error {
	if err := acquireCapitalProofLock(ctx, mse.mu.TryRLock, mse.mu.RUnlock); err != nil {
		return err
	}
	defer mse.mu.RUnlock()
	if len(mse.ordersByClient) != 0 || len(mse.ordersByID) != 0 {
		return fmt.Errorf("strategy order capital remains unsettled")
	}
	for _, usage := range mse.positionUsage {
		if usage == nil || !finiteNumber(usage.qty) || !finiteNumber(usage.amount) || usage.qty != 0 || usage.amount != 0 {
			return fmt.Errorf("strategy position capital requires reconciliation")
		}
	}
	return ctx.Err()
}
