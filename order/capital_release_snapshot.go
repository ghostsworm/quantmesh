package order

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"quantmesh/execution"
)

// Capital proof must report lease cleanup failures alongside actual committed
// amounts. The normal snapshot API retains its existing void cleanup contract.
func (oe *ExchangeOrderExecutor) BeginCapitalReleaseSnapshot(ctx context.Context) (context.Context, func() error, error) {
	if ctx == nil || oe == nil || oe.exchange == nil {
		return nil, nil, fmt.Errorf("capital snapshot requires context and executor")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	snapshotCtx, releaseLease, err := oe.acquirePositionSubmissionLease(ctx, oe.exchange.GetName(), oe.symbol, execution.PositionReconciliationLockTTL)
	if err != nil {
		return nil, nil, err
	}
	releaseBarrier, err := oe.BeginPositionReconciliation(snapshotCtx)
	if err != nil {
		return nil, nil, errors.Join(err, releaseLease())
	}
	var once sync.Once
	var releaseErr error
	release := func() error {
		once.Do(func() {
			releaseBarrier()
			releaseErr = releaseLease()
		})
		return releaseErr
	}
	if err := snapshotCtx.Err(); err != nil {
		return nil, nil, errors.Join(err, release())
	}
	return snapshotCtx, release, nil
}
