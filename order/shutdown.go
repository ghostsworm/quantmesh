package order

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"quantmesh/execution"
	"quantmesh/logger"
)

const RuntimeShutdownBlock = "runtime_shutdown"
const positionReconciliationBlockPrefix = "position_reconciliation_"

var ErrRuntimeStopping = errors.New("runtime is stopping")

type shutdownCloseKey struct{}

type shutdownCloseOwners map[*ExchangeOrderExecutor]struct{}

// BeginPositionReconciliation prevents new physical order submissions and
// waits for admitted submissions to finish before the caller reads account
// snapshots. The returned release is safe to call more than once.
func (oe *ExchangeOrderExecutor) BeginPositionReconciliation(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	block := fmt.Sprintf("%s%d", positionReconciliationBlockPrefix, oe.reconciliationSequence.Add(1))
	oe.submissionGate.Block(block)
	if err := oe.submissionGate.Drain(ctx); err != nil {
		oe.submissionGate.Unblock(block)
		return nil, fmt.Errorf("drain order submissions before reconciliation: %w", err)
	}
	var once sync.Once
	return func() { once.Do(func() { oe.submissionGate.Unblock(block) }) }, nil
}

// BeginPositionSnapshot holds the same local and distributed coordination
// lease used by order submissions while an account snapshot is collected.
// Losing the lease cancels the returned context and blocks this executor.
func (oe *ExchangeOrderExecutor) BeginPositionSnapshot(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshotCtx, releaseLease, err := oe.acquirePositionSubmissionLock(ctx, oe.exchange.GetName(), oe.symbol)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire position snapshot coordination lease: %w", err)
	}
	releaseBarrier, err := oe.BeginPositionReconciliation(snapshotCtx)
	if err != nil {
		releaseLease()
		return nil, nil, err
	}
	var once sync.Once
	return snapshotCtx, func() {
		once.Do(func() {
			releaseBarrier()
			releaseLease()
		})
	}, nil
}

// FailPositionReconciliation permanently blocks this executor for the current
// process when reconciliation cannot establish trustworthy position evidence.
func (oe *ExchangeOrderExecutor) FailPositionReconciliation(err error) {
	oe.submissionGate.Block(execution.PositionCoordinationLockLostBlock)
	logger.ErrorCtx(oe.logCtx(), "持倉對账失败，执行器保持关闭: %v", err)
}

func (oe *ExchangeOrderExecutor) IsShutdownCloseContext(ctx context.Context) bool {
	owners, ok := ctx.Value(shutdownCloseKey{}).(shutdownCloseOwners)
	_, authorized := owners[oe]
	return ok && authorized && oe.submissionGate.HasBlock(RuntimeShutdownBlock)
}

// BeginShutdown is irreversible for this executor. Block every runtime before
// waiting for any one of them; otherwise later Bots could keep adding risk.
func (oe *ExchangeOrderExecutor) BeginShutdown() {
	oe.openingGate.Block(RuntimeShutdownBlock)
	oe.submissionGate.Block(RuntimeShutdownBlock)
}

func (oe *ExchangeOrderExecutor) DrainShutdown(ctx context.Context) error {
	if !oe.submissionGate.HasBlock(RuntimeShutdownBlock) {
		return fmt.Errorf("shutdown has not begun")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return oe.submissionGate.Drain(ctx)
}

// ShutdownCloseContext grants only this executor's shutdown coordinator a
// close-only path after ordinary submissions drain. It does not bypass intent
// persistence, UNKNOWN checks, exposure limits or the opening gate.
func (oe *ExchangeOrderExecutor) ShutdownCloseContext(ctx context.Context) (context.Context, error) {
	if err := oe.DrainShutdown(ctx); err != nil {
		return nil, err
	}
	owners := make(shutdownCloseOwners)
	if existing, ok := ctx.Value(shutdownCloseKey{}).(shutdownCloseOwners); ok {
		for owner := range existing {
			owners[owner] = struct{}{}
		}
	}
	owners[oe] = struct{}{}
	return context.WithValue(ctx, shutdownCloseKey{}, owners), nil
}

func (oe *ExchangeOrderExecutor) admitSubmission(ctx context.Context, req *OrderRequest) (func(), error) {
	if owners, ok := ctx.Value(shutdownCloseKey{}).(shutdownCloseOwners); ok {
		_, authorized := owners[oe]
		if authorized &&
			oe.submissionGate.HasBlock(RuntimeShutdownBlock) && !oe.isOpeningOrder(req) {
			return func() {}, nil
		}
	}
	release, err := oe.submissionGate.Begin()
	if errors.Is(err, execution.ErrOpeningPaused) {
		return nil, ErrRuntimeStopping
	}
	return release, err
}
