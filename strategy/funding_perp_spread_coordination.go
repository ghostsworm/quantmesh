package strategy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"quantmesh/execution"
	"quantmesh/lock"
)

const fundingSpreadCoordinationTTL = 30 * time.Second

// withLegCoordination serializes cross-venue ownership snapshots and leg orders
// with the standard order executor and position reconciler for both symbols.
func (s *FundingPerpSpreadStrategy) withLegCoordination(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil || fn == nil || s.legA == nil || s.legB == nil || s.symA == "" || s.symB == "" {
		return errors.New("funding_perp_spread coordination requires context, both legs, symbols, and operation")
	}
	s.mu.RLock()
	distributedLock := s.coordinationLock
	s.mu.RUnlock()
	if distributedLock == nil {
		return errors.New("funding_perp_spread requires a configured distributed coordination lock")
	}
	ttl := s.coordinationTTL
	if ttl <= 0 {
		ttl = fundingSpreadCoordinationTTL
	}

	keys := []string{
		execution.PositionReconciliationLockKey(s.legA.GetName(), s.symA),
		execution.PositionReconciliationLockKey(s.legB.GetName(), s.symB),
	}
	sort.Strings(keys)
	if keys[0] == keys[1] {
		keys = keys[:1]
	}
	acquired := make([]string, 0, len(keys))
	localReleases := make([]func(), 0, len(keys))
	operationCtx, cancelOperation := context.WithCancel(ctx)
	defer cancelOperation()
	var renewMu sync.Mutex
	var renewErr error
	stops := make([]func(), 0, len(keys))
	stopRenewers := func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
	}
	startRenewal := func(key string) {
		stops = append(stops, lock.StartAutoRenew(distributedLock, key, ttl, func(err error) {
			renewMu.Lock()
			renewErr = errors.Join(renewErr, fmt.Errorf("renew funding spread coordination lock %s: %w", key, err))
			renewMu.Unlock()
			cancelOperation()
			s.markExposureUnknown()
		}))
	}
	release := func() error {
		var releaseErr error
		for i := len(acquired) - 1; i >= 0; i-- {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := distributedLock.Unlock(unlockCtx, acquired[i])
			cancel()
			releaseErr = errors.Join(releaseErr, err)
		}
		for i := len(localReleases) - 1; i >= 0; i-- {
			localReleases[i]()
		}
		if releaseErr != nil {
			s.markExposureUnknown()
		}
		return releaseErr
	}
	for _, key := range keys {
		acquireCtx, cancel := context.WithTimeout(operationCtx, 15*time.Second)
		unlockLocal, localErr := execution.AcquireLocalPositionCoordination(acquireCtx, key)
		if localErr != nil {
			cancel()
			stopRenewers()
			return errors.Join(fmt.Errorf("acquire local funding spread coordination lock %s: %w", key, localErr), release())
		}
		localReleases = append(localReleases, unlockLocal)
		err := distributedLock.Lock(acquireCtx, key, ttl)
		cancel()
		if err != nil {
			stopRenewers()
			renewMu.Lock()
			err = errors.Join(fmt.Errorf("acquire funding spread coordination lock %s: %w", key, err), renewErr)
			renewMu.Unlock()
			return errors.Join(err, release())
		}
		acquired = append(acquired, key)
		startRenewal(key)
	}

	if err := operationCtx.Err(); err != nil {
		stopRenewers()
		return errors.Join(err, release())
	}
	opErr := fn(operationCtx)
	stopRenewers()
	renewMu.Lock()
	opErr = errors.Join(opErr, renewErr)
	renewMu.Unlock()
	return errors.Join(opErr, release())
}
