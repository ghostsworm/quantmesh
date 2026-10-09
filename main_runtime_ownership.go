package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
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

func fundingCarryRuntimeOwnershipScopes(cfg *config.Config, exchangeName, symbol string, includeMargin bool) ([]execution.IntentScope, error) {
	if cfg == nil || strings.TrimSpace(exchangeName) == "" || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("funding_carry ownership requires config, exchange, and symbol")
	}
	exchangeCfg, ok := cfg.Exchanges[exchangeName]
	if !ok || strings.TrimSpace(exchangeCfg.APIKey) == "" {
		return nil, fmt.Errorf("funding_carry ownership account identity unavailable for %s", exchangeName)
	}
	markets := []string{"futures", "spot"}
	if includeMargin {
		markets = append(markets, "spot_margin")
	}
	accountScope := equityAccountScopeID(exchangeName, exchangeCfg)
	scopes := make([]execution.IntentScope, 0, len(markets))
	for _, market := range markets {
		scopes = append(scopes, runtimeOwnershipScope(accountScope, exchangeName, market, symbol))
	}
	sort.Slice(scopes, func(i, j int) bool {
		left, _ := scopes[i].Key()
		right, _ := scopes[j].Key()
		return left < right
	})
	return scopes, nil
}

func acquireFundingCarryRuntimeOwnershipLeases(ctx context.Context, distributedLock lock.DistributedLock, cfg *config.Config, exchangeName, symbol string, includeMargin bool, onLost func(error)) ([]*runtimeOwnershipLease, error) {
	scopes, err := fundingCarryRuntimeOwnershipScopes(cfg, exchangeName, symbol, includeMargin)
	if err != nil {
		return nil, fmt.Errorf("build funding_carry runtime ownership scopes: %w", err)
	}
	leases := make([]*runtimeOwnershipLease, 0, len(scopes))
	for _, scope := range scopes {
		lease, acquireErr := acquireRuntimeOwnershipLease(ctx, distributedLock, scope, runtimeOwnershipLeaseTTL, onLost)
		if acquireErr != nil {
			var releaseErr error
			for index := len(leases) - 1; index >= 0; index-- {
				releaseErr = errors.Join(releaseErr, leases[index].Release())
			}
			return nil, errors.Join(fmt.Errorf("acquire funding_carry runtime ownership: %w", acquireErr), releaseErr)
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

func releaseFundingCarryRuntimeOwnershipLeases(leases []*runtimeOwnershipLease) error {
	var releaseErr error
	for index := len(leases) - 1; index >= 0; index-- {
		releaseErr = errors.Join(releaseErr, leases[index].Release())
	}
	return releaseErr
}

func fundingCarryRuntimeOwnershipLeaseLost(leases []*runtimeOwnershipLease) bool {
	for _, lease := range leases {
		if lease.Lost() {
			return true
		}
	}
	return false
}

type runtimeOwnershipLease struct {
	distributedLock lock.DistributedLock
	key             string
	stopRenew       func()
	onLost          func(error)
	lossReported    sync.Once
	lost            atomic.Bool
	releaseMu       sync.Mutex
	released        atomic.Bool
}

func acquireRuntimeOwnershipLease(ctx context.Context, distributedLock lock.DistributedLock, scope execution.IntentScope, ttl time.Duration, onLost func(error)) (*runtimeOwnershipLease, error) {
	if distributedLock == nil {
		return nil, fmt.Errorf("runtime ownership lock is unavailable")
	}
	// NopLock explicitly means distributed coordination is disabled. Do not
	// pretend its always-successful TryLock provides cross-process ownership.
	if _, disabled := distributedLock.(*lock.NopLock); disabled {
		return &runtimeOwnershipLease{distributedLock: distributedLock}, nil
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
	lease := &runtimeOwnershipLease{distributedLock: distributedLock, key: key, onLost: onLost}
	lease.stopRenew = lock.StartAutoRenew(&runtimeOwnershipRenewLock{DistributedLock: distributedLock, lease: lease}, key, ttl, func(renewErr error) {
		lease.releaseMu.Lock()
		if lease.released.Load() {
			lease.releaseMu.Unlock()
			return
		}
		lease.lost.Store(true)
		lease.releaseMu.Unlock()
		lease.reportLost(renewErr)
	})
	return lease, nil
}

func (l *runtimeOwnershipLease) Lost() bool {
	return l == nil || l.lost.Load()
}

// Validate synchronously renews a distributed lease at a sensitive boundary.
// The background renewal worker bounds ordinary detection latency, but cannot
// be the sole admission check immediately before a new financial request.
func (l *runtimeOwnershipLease) Validate(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("runtime ownership validation requires context")
	}
	if l == nil || l.Lost() {
		return fmt.Errorf("runtime ownership lease is unavailable or lost")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.distributedLock == nil {
		return fmt.Errorf("runtime ownership lock is unavailable")
	}
	if _, disabled := l.distributedLock.(*lock.NopLock); disabled {
		return nil
	}
	l.releaseMu.Lock()
	if l.released.Load() || l.Lost() {
		l.releaseMu.Unlock()
		return fmt.Errorf("runtime ownership lease is unavailable or lost")
	}
	if err := ctx.Err(); err != nil {
		l.releaseMu.Unlock()
		return err
	}
	if err := l.distributedLock.Extend(ctx, l.key, runtimeOwnershipLeaseTTL); err != nil {
		l.lost.Store(true)
		l.releaseMu.Unlock()
		l.reportLost(err)
		return fmt.Errorf("validate runtime ownership lease: %w", err)
	}
	l.releaseMu.Unlock()
	if l.Lost() {
		return fmt.Errorf("runtime ownership lease was lost during validation")
	}
	return nil
}

func (l *runtimeOwnershipLease) reportLost(err error) {
	if l == nil || err == nil {
		return
	}
	l.lossReported.Do(func() {
		if l.onLost != nil {
			l.onLost(err)
		}
	})
}

func validateRuntimeOwnershipLeases(ctx context.Context, leases []*runtimeOwnershipLease) error {
	if len(leases) == 0 {
		return fmt.Errorf("runtime ownership leases are unavailable")
	}
	for index, lease := range leases {
		if err := lease.Validate(ctx); err != nil {
			return fmt.Errorf("validate runtime ownership lease %d: %w", index+1, err)
		}
	}
	return nil
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
	if lease.Lost() {
		return false, fmt.Errorf("runtime ownership was lost; stop requires reconciliation")
	}
	err := lease.release(true)
	if err == nil && lease.Lost() {
		return false, fmt.Errorf("runtime ownership was lost during release; stop requires reconciliation")
	}
	return err == nil, err
}

func (l *runtimeOwnershipLease) Release() error {
	// Startup rollback has no managed controller to retry indefinitely. Keep
	// its prior bounded cleanup semantics instead of orphaning a renewer.
	return l.release(false)
}

func (l *runtimeOwnershipLease) release(retainOnFailure bool) error {
	if l == nil {
		return nil
	}
	l.releaseMu.Lock()
	if l.released.Load() {
		l.releaseMu.Unlock()
		return nil
	}
	var err error
	if l.distributedLock != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = l.distributedLock.Unlock(ctx, l.key)
		cancel()
	}
	if err == nil {
		l.released.Store(true)
	}
	l.releaseMu.Unlock()
	// Do not wait for the renewal worker while holding the mutex it uses.
	// An uncertain unlock keeps renewal alive and permits an ownership-checked
	// retry; no new token is acquired and no peer-owned lease may be deleted.
	if (err == nil || !retainOnFailure) && l.stopRenew != nil {
		l.stopRenew()
	}
	return err
}

type runtimeOwnershipRenewLock struct {
	lock.DistributedLock
	lease *runtimeOwnershipLease
}

func (l *runtimeOwnershipRenewLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	l.lease.releaseMu.Lock()
	defer l.lease.releaseMu.Unlock()
	if l.lease.released.Load() {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return l.DistributedLock.Extend(ctx, key, ttl)
}
