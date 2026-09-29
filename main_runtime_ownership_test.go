package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"quantmesh/execution"
	"quantmesh/lock"
)

type runtimeLeaseTestLock struct {
	mu        sync.Mutex
	held      map[string]bool
	extendErr error
}

func TestRuntimeOwnershipLeaseDoesNotTreatNopLockAsDistributedOwnership(t *testing.T) {
	lease, err := acquireRuntimeOwnershipLease(t.Context(), lock.NewNopLock(),
		execution.IntentScope{Account: "account", Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Bot: "bot-a"},
		time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Lost() {
		t.Fatal("disabled coordination must not be reported as a lost lease")
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release without distributed coordination: %v", err)
	}
}

func (l *runtimeLeaseTestLock) Lock(ctx context.Context, key string, ttl time.Duration) error {
	acquired, err := l.TryLock(ctx, key, ttl)
	if err != nil {
		return err
	}
	if !acquired {
		return errors.New("already held")
	}
	return nil
}

func (l *runtimeLeaseTestLock) TryLock(_ context.Context, key string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held == nil {
		l.held = make(map[string]bool)
	}
	if l.held[key] {
		return false, nil
	}
	l.held[key] = true
	return true, nil
}

func (l *runtimeLeaseTestLock) Unlock(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.held[key] {
		return errors.New("not held")
	}
	delete(l.held, key)
	return nil
}

func (l *runtimeLeaseTestLock) Extend(_ context.Context, key string, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.extendErr != nil {
		return l.extendErr
	}
	if !l.held[key] {
		return errors.New("not held")
	}
	return nil
}

func (*runtimeLeaseTestLock) Close() error { return nil }

func TestRuntimeOwnershipLeaseAllowsOneOwnerAndReleases(t *testing.T) {
	distributedLock := &runtimeLeaseTestLock{}
	scope := execution.IntentScope{Account: "account", Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Bot: "bot-a"}
	first, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil); err == nil {
		t.Fatal("duplicate runtime acquired a second ownership lease")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil)
	if err != nil {
		t.Fatalf("runtime could not acquire released ownership lease: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeOwnershipLeaseRenewFailureSignalsLoss(t *testing.T) {
	renewErr := errors.New("lease expired")
	distributedLock := &runtimeLeaseTestLock{extendErr: renewErr}
	callback := make(chan error, 1)
	lease, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock,
		execution.IntentScope{Account: "account", Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Bot: "bot-a"},
		30*time.Millisecond, func(err error) { callback <- err })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-callback:
		if !errors.Is(got, renewErr) || !lease.Lost() {
			t.Fatalf("renew loss callback = %v, lease lost=%v", got, lease.Lost())
		}
	case <-time.After(time.Second):
		t.Fatal("runtime ownership lease renewal failure was not reported")
	}
	if err := lease.Release(); err != nil {
		t.Fatal(err)
	}
}
