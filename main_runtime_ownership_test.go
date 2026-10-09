package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
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

func TestRuntimeOwnershipLeaseSerializesSharedPositionScope(t *testing.T) {
	distributedLock := &runtimeLeaseTestLock{}
	scope := runtimeOwnershipScope("account", " Binance ", " FUTURES ", "btcusdt")
	if scope != runtimeOwnershipScope("account", "binance", "futures", "BTCUSDT") {
		t.Fatal("runtime ownership scope is not normalized")
	}
	if scope.Bot != runtimeOwnershipScopeOwner {
		t.Fatalf("scope owner = %q, want shared physical-position owner", scope.Bot)
	}
	first, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil); err == nil {
		t.Fatal("different Bot instances for one exchange position scope acquired separate ownership leases")
	}
	for _, independent := range []execution.IntentScope{
		runtimeOwnershipScope("other-account", "binance", "futures", "BTCUSDT"),
		runtimeOwnershipScope("account", "binance", "spot", "BTCUSDT"),
		runtimeOwnershipScope("account", "binance", "futures", "ETHUSDT"),
	} {
		lease, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, independent, time.Second, nil)
		if err != nil {
			t.Fatalf("independent position scope %+v could not acquire its own lease: %v", independent, err)
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
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

func TestFundingCarryRuntimeOwnershipCoversEveryTradingMarket(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "test-key", SecretKey: "test-secret"},
	}}
	withoutMargin, err := fundingCarryRuntimeOwnershipScopes(cfg, "binance", "BTCUSDT", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutMargin) != 2 || withoutMargin[0].Market == withoutMargin[1].Market {
		t.Fatalf("spot/futures ownership scopes = %+v, want two distinct markets", withoutMargin)
	}
	withMargin, err := fundingCarryRuntimeOwnershipScopes(cfg, "binance", "BTCUSDT", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(withMargin) != 3 {
		t.Fatalf("reverse carry ownership scopes = %+v, want spot, futures, and spot_margin", withMargin)
	}
	for _, scope := range withMargin {
		if scope.Account != equityAccountScopeID("binance", cfg.Exchanges["binance"]) || scope.Symbol != "BTCUSDT" || scope.Bot != runtimeOwnershipScopeOwner {
			t.Fatalf("unexpected funding_carry physical-position scope: %+v", scope)
		}
	}
}

func TestFundingCarryRuntimeOwnershipAcquisitionRollsBackPartialLeases(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "test-key", SecretKey: "test-secret"},
	}}
	scopes, err := fundingCarryRuntimeOwnershipScopes(cfg, "binance", "BTCUSDT", false)
	if err != nil {
		t.Fatal(err)
	}
	distributedLock := &runtimeLeaseTestLock{}
	blockedKey, err := scopes[1].Key()
	if err != nil {
		t.Fatal(err)
	}
	if acquired, err := distributedLock.TryLock(t.Context(), "runtime-owner:"+blockedKey, time.Second); err != nil || !acquired {
		t.Fatalf("prepare blocked second scope: acquired=%v err=%v", acquired, err)
	}
	if _, err := acquireFundingCarryRuntimeOwnershipLeases(t.Context(), distributedLock, cfg, "binance", "BTCUSDT", false, nil); err == nil {
		t.Fatal("expected acquisition failure for occupied second scope")
	}
	firstKey, _ := scopes[0].Key()
	if acquired, err := distributedLock.TryLock(t.Context(), "runtime-owner:"+firstKey, time.Second); err != nil || !acquired {
		t.Fatalf("partial acquisition was not rolled back: acquired=%v err=%v", acquired, err)
	}
}

func TestFundingCarryRuntimeOwnershipConflictsWithStandardRuntimePerMarket(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "test-key", SecretKey: "test-secret"},
	}}
	distributedLock := &runtimeLeaseTestLock{}
	carryLeases, err := acquireFundingCarryRuntimeOwnershipLeases(t.Context(), distributedLock,
		cfg, "binance", "BTCUSDT", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	carryScopes, err := fundingCarryRuntimeOwnershipScopes(cfg, "binance", "BTCUSDT", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range carryScopes {
		if _, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil); err == nil {
			t.Fatalf("standard runtime acquired Funding Carry-owned physical scope: %+v", scope)
		}
	}
	if err := releaseFundingCarryRuntimeOwnershipLeases(carryLeases); err != nil {
		t.Fatal(err)
	}
	for _, scope := range carryScopes {
		lease, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil)
		if err != nil {
			t.Fatalf("standard runtime could not acquire released Funding Carry scope %+v: %v", scope, err)
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeOwnershipLeaseRetainedUntilStopIsVerified(t *testing.T) {
	distributedLock := &runtimeLeaseTestLock{}
	scope := runtimeOwnershipScope("account", "binance", "futures", "BTCUSDT")

	t.Run("stop error retains lease", func(t *testing.T) {
		lease, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}
		released, err := releaseRuntimeOwnershipLeaseAfterVerifiedStop(lease, []error{errors.New("open order unresolved")}, "")
		if err != nil || released {
			t.Fatalf("release result = %v, %v; want retained without helper error", released, err)
		}
		if _, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil); err == nil {
			t.Fatal("competing runtime acquired ownership while prior stop was unverified")
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unverified marker retains lease", func(t *testing.T) {
		lease, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}
		released, err := releaseRuntimeOwnershipLeaseAfterVerifiedStop(lease, nil, "position snapshot unavailable")
		if err == nil || released {
			t.Fatalf("release result = %v, %v; want fail-closed retention", released, err)
		}
		if _, acquireErr := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil); acquireErr == nil {
			t.Fatal("competing runtime acquired ownership while reconciliation marker remained")
		}
		if err := lease.Release(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("verified stop releases lease", func(t *testing.T) {
		lease, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}
		released, err := releaseRuntimeOwnershipLeaseAfterVerifiedStop(lease, nil, "")
		if err != nil || !released {
			t.Fatalf("release result = %v, %v; want released", released, err)
		}
		competitor, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil)
		if err != nil {
			t.Fatalf("verified stop kept lease held: %v", err)
		}
		if err := competitor.Release(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestFundingPerpSpreadRuntimeOwnsBothLegsUntilRelease(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "binance-account", SecretKey: "binance-secret"},
		"bybit":   {APIKey: "bybit-account", SecretKey: "bybit-secret"},
	}}
	fp := &config.FundingPerpSpreadConfig{
		LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB: config.FundingPerpLeg{Exchange: "bybit", Symbol: "BTCUSDT"},
	}
	distributedLock := &runtimeLeaseTestLock{}
	scopes, err := fundingPerpSpreadRuntimeOwnershipScopes(cfg, fp)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 {
		t.Fatalf("ownership scopes should cover both legs: %+v", scopes)
	}
	firstKey, firstErr := scopes[0].Key()
	secondKey, secondErr := scopes[1].Key()
	if firstErr != nil || secondErr != nil || firstKey > secondKey {
		t.Fatalf("ownership scopes should cover both legs in stable order: %+v", scopes)
	}
	leases, err := acquireFundingPerpSpreadRuntimeOwnershipLeases(t.Context(), distributedLock, cfg, fp, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range scopes {
		if _, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock, scope, time.Second, nil); err == nil {
			t.Fatalf("a competing runtime acquired spread leg %s/%s while the paired runtime was active", scope.Exchange, scope.Symbol)
		}
	}
	if err := releaseFundingPerpSpreadRuntimeOwnershipLeases(leases); err != nil {
		t.Fatal(err)
	}
	competitor, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock,
		runtimeOwnershipScope(equityAccountScopeID("binance", cfg.Exchanges["binance"]), "binance", "futures", "BTCUSDT"),
		time.Second, nil)
	if err != nil {
		t.Fatalf("leg ownership remained held after a flat runtime released its leases: %v", err)
	}
	if err := competitor.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestFundingPerpSpreadRuntimeOwnershipRollsBackPartialAcquisition(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "binance-account", SecretKey: "binance-secret"},
		"bybit":   {APIKey: "bybit-account", SecretKey: "bybit-secret"},
	}}
	fp := &config.FundingPerpSpreadConfig{
		LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB: config.FundingPerpLeg{Exchange: "bybit", Symbol: "BTCUSDT"},
	}
	scopes, err := fundingPerpSpreadRuntimeOwnershipScopes(cfg, fp)
	if err != nil {
		t.Fatal(err)
	}
	blockedKey, err := scopes[1].Key()
	if err != nil {
		t.Fatal(err)
	}
	distributedLock := &runtimeLeaseTestLock{}
	if _, err := distributedLock.TryLock(t.Context(), "runtime-owner:"+blockedKey, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireFundingPerpSpreadRuntimeOwnershipLeases(t.Context(), distributedLock, cfg, fp, nil); err == nil {
		t.Fatal("partial ownership acquisition unexpectedly succeeded")
	}
	firstKey, err := scopes[0].Key()
	if err != nil {
		t.Fatal(err)
	}
	distributedLock.mu.Lock()
	firstHeld := distributedLock.held["runtime-owner:"+firstKey]
	distributedLock.mu.Unlock()
	if firstHeld {
		t.Fatal("first-leg lease leaked after second-leg acquisition failed")
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

func TestRuntimeOwnershipLeaseValidateDetectsExpiredLeaseSynchronously(t *testing.T) {
	lockErr := errors.New("redis lease token no longer owns key")
	distributedLock := &runtimeLeaseTestLock{}
	lost := make(chan error, 1)
	lease, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock,
		execution.IntentScope{Account: "account", Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Bot: "bot-a"},
		time.Hour, func(err error) { lost <- err })
	if err != nil {
		t.Fatal(err)
	}
	distributedLock.mu.Lock()
	distributedLock.extendErr = lockErr
	distributedLock.mu.Unlock()
	if err := lease.Validate(t.Context()); !errors.Is(err, lockErr) {
		t.Fatalf("synchronous lease validation error = %v, want %v", err, lockErr)
	}
	if !lease.Lost() {
		t.Fatal("lease validation failure did not irreversibly mark ownership lost")
	}
	select {
	case got := <-lost:
		if !errors.Is(got, lockErr) {
			t.Fatalf("synchronous ownership loss notification = %v, want %v", got, lockErr)
		}
	case <-time.After(time.Second):
		t.Fatal("synchronous validation failure did not notify the runtime loss handler")
	}
	if err := lease.Validate(t.Context()); err == nil {
		t.Fatal("lost lease passed a later synchronous validation")
	}
	select {
	case duplicate := <-lost:
		t.Fatalf("ownership loss handler ran more than once: %v", duplicate)
	default:
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release expired test lease: %v", err)
	}
}

func TestRuntimeOwnershipLeaseValidateRequiresLiveContext(t *testing.T) {
	distributedLock := &runtimeLeaseTestLock{}
	lease, err := acquireRuntimeOwnershipLease(t.Context(), distributedLock,
		execution.IntentScope{Account: "account", Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Bot: "bot-a"},
		time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := lease.Validate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("validation with canceled context = %v, want context.Canceled", err)
	}
}
