package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/execution"
	"quantmesh/lock"
)

const runtimeOwnershipLeaseTTL = 30 * time.Second
const runtimeOwnershipScopeOwner = "shared-symbol-position-scope"

func runtimeOwnershipScope(account, exchangeName, market, symbol string) execution.IntentScope {
	return execution.IntentScope{
		Account:  account,
		Exchange: strings.ToLower(strings.TrimSpace(exchangeName)),
		Market:   strings.ToLower(strings.TrimSpace(market)),
		Symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
		Bot:      runtimeOwnershipScopeOwner,
	}
}

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

func releaseRuntimeOwnershipLeaseAfterVerifiedStop(lease *runtimeOwnershipLease, stopErrors []error, unverifiedReason string) (bool, error) {
	if lease == nil {
		return false, fmt.Errorf("runtime ownership lease is unavailable")
	}
	if len(stopErrors) > 0 {
		return false, nil
	}
	if strings.TrimSpace(unverifiedReason) != "" {
		return false, fmt.Errorf("runtime stop still requires reconciliation: %s", strings.TrimSpace(unverifiedReason))
	}
	return true, lease.Release()
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
