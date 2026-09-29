package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/execution"
	"quantmesh/lock"
)

const runtimeOwnershipLeaseTTL = 30 * time.Second

type runtimeOwnershipLease struct {
	distributedLock lock.DistributedLock
	key             string
	stopRenew       func()
	lost            atomic.Bool
	releaseOnce     sync.Once
	releaseErr      error
}

func acquireRuntimeOwnershipLease(ctx context.Context, distributedLock lock.DistributedLock, scope execution.IntentScope, ttl time.Duration, onLost func(error)) (*runtimeOwnershipLease, error) {
	if distributedLock == nil {
		return nil, fmt.Errorf("runtime ownership lock is unavailable")
	}
	// NopLock explicitly means distributed coordination is disabled. Do not
	// pretend its always-successful TryLock provides cross-process ownership.
	if _, disabled := distributedLock.(*lock.NopLock); disabled {
		return &runtimeOwnershipLease{}, nil
	}
	scopeKey, err := scope.Key()
	if err != nil {
		return nil, fmt.Errorf("runtime ownership scope: %w", err)
	}
	key := "runtime-owner:" + scopeKey
	acquired, err := distributedLock.TryLock(ctx, key, ttl)
	if err != nil {
		return nil, fmt.Errorf("acquire runtime ownership lease: %w", err)
	}
	if !acquired {
		return nil, fmt.Errorf("another process already owns this Bot runtime")
	}
	lease := &runtimeOwnershipLease{distributedLock: distributedLock, key: key}
	lease.stopRenew = lock.StartAutoRenew(distributedLock, key, ttl, func(renewErr error) {
		lease.lost.Store(true)
		if onLost != nil {
			onLost(renewErr)
		}
	})
	return lease, nil
}

func (l *runtimeOwnershipLease) Lost() bool {
	return l == nil || l.lost.Load()
}

func (l *runtimeOwnershipLease) Release() error {
	if l == nil {
		return nil
	}
	l.releaseOnce.Do(func() {
		if l.distributedLock == nil {
			return
		}
		if l.stopRenew != nil {
			l.stopRenew()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		l.releaseErr = l.distributedLock.Unlock(ctx, l.key)
	})
	return l.releaseErr
}
