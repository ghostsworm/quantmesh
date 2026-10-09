package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type retryRuntimeLeaseLock struct {
	*runtimeLeaseTestLock
	unlockCalls atomic.Int32
	renewed     chan struct{}
}

func (l *retryRuntimeLeaseLock) Unlock(ctx context.Context, key string) error {
	if l.unlockCalls.Add(1) == 1 {
		return errors.New("fixture transient unlock failure before mutation")
	}
	return l.runtimeLeaseTestLock.Unlock(ctx, key)
}

func (l *retryRuntimeLeaseLock) Extend(ctx context.Context, key string, ttl time.Duration) error {
	err := l.runtimeLeaseTestLock.Extend(ctx, key, ttl)
	select {
	case l.renewed <- struct{}{}:
	default:
	}
	return err
}

func TestRuntimeOwnershipReleaseFailureRetainsRenewalAndCanRetry(t *testing.T) {
	provider := &retryRuntimeLeaseLock{runtimeLeaseTestLock: &runtimeLeaseTestLock{}, renewed: make(chan struct{}, 8)}
	scope := runtimeOwnershipScope("fixture-account", "binance", "futures", "BTCUSDT")
	lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, scope, 150*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if lease.stopRenew != nil {
			lease.stopRenew()
		}
	})
	released, err := releaseRuntimeOwnershipLeaseAfterVerifiedStop(lease, nil, "")
	if err == nil || released {
		t.Error("failed unlock reported lease released")
	}
	if _, err := acquireRuntimeOwnershipLease(t.Context(), provider, scope, time.Second, nil); err == nil {
		t.Error("peer acquired unresolved lease")
	}
	select {
	case <-provider.renewed:
	case <-time.After(time.Second):
		t.Error("transient unlock failure permanently stopped renewal")
	}
	released, err = releaseRuntimeOwnershipLeaseAfterVerifiedStop(lease, nil, "")
	if err != nil || !released || provider.unlockCalls.Load() != 2 {
		t.Errorf("recovered provider never retried unlock: released=%v err=%v calls=%d", released, err, provider.unlockCalls.Load())
	}
	if err := lease.Release(); err != nil || provider.unlockCalls.Load() != 2 {
		t.Error("confirmed release is not idempotent")
	}
	peer, err := acquireRuntimeOwnershipLease(t.Context(), provider, scope, time.Second, nil)
	if err != nil {
		t.Errorf("peer cannot acquire after verified release: %v", err)
		return
	}
	if err := peer.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestStartupOwnershipReleaseFailureDoesNotOrphanRenewal(t *testing.T) {
	provider := &retryRuntimeLeaseLock{runtimeLeaseTestLock: &runtimeLeaseTestLock{}, renewed: make(chan struct{}, 8)}
	lease, err := acquireRuntimeOwnershipLease(t.Context(), provider, runtimeOwnershipScope("fixture", "binance", "futures", "BTCUSDT"), 150*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.stopRenew)
	if err := lease.Release(); err == nil {
		t.Fatal("startup unlock failure missing")
	}
	select {
	case <-provider.renewed:
		t.Fatal("abandoned startup keeps renewing without a controller")
	case <-time.After(200 * time.Millisecond):
	}
	if err := lease.Release(); err != nil || provider.unlockCalls.Load() != 2 {
		t.Fatal("explicit cleanup retry still caches unlock failure")
	}
}
