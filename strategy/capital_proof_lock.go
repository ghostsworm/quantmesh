package strategy

import (
	"context"
	"fmt"
	"time"
)

const capitalProofLockRetryInterval = 5 * time.Millisecond

// Wait without launching an uncancellable locker goroutine. No waiter can
// acquire and leak a lock after the proof/request has already returned.
func acquireCapitalProofLock(ctx context.Context, tryLock func() bool, unlock func()) error {
	if ctx == nil {
		return fmt.Errorf("capital proof lock requires context")
	}
	var retry *time.Ticker
	defer func() {
		if retry != nil {
			retry.Stop()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if tryLock() {
			if err := ctx.Err(); err != nil {
				unlock()
				return err
			}
			return nil
		}
		if retry == nil {
			retry = time.NewTicker(capitalProofLockRetryInterval)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-retry.C:
		}
	}
}
