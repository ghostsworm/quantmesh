package order

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"quantmesh/execution"
	"quantmesh/lock"
)

type sharedPositionLock struct {
	mu         sync.Mutex
	held       map[string]bool
	failExtend bool
}

func newSharedPositionLock() *sharedPositionLock {
	return &sharedPositionLock{held: make(map[string]bool)}
}

func (l *sharedPositionLock) try(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[key] {
		return false
	}
	l.held[key] = true
	return true
}

func (l *sharedPositionLock) Lock(ctx context.Context, key string, _ time.Duration) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if l.try(key) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l *sharedPositionLock) TryLock(_ context.Context, key string, _ time.Duration) (bool, error) {
	return l.try(key), nil
}

func (l *sharedPositionLock) Unlock(_ context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.held[key] {
		return fmt.Errorf("lock %q is not held", key)
	}
	delete(l.held, key)
	return nil
}

func (l *sharedPositionLock) Extend(_ context.Context, key string, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failExtend {
		return fmt.Errorf("simulated renewal failure")
	}
	if !l.held[key] {
		return fmt.Errorf("lock %q is not held", key)
	}
	return nil
}

func (l *sharedPositionLock) Close() error { return nil }

func TestPositionReconciliationBarrierDrainsAndBlocksSubmissions(t *testing.T) {
	oe := &ExchangeOrderExecutor{}
	inFlightRelease, err := oe.submissionGate.Begin()
	if err != nil {
		t.Fatalf("begin in-flight submission: %v", err)
	}

	type result struct {
		release func()
		err     error
	}
	acquired := make(chan result, 1)
	go func() {
		release, err := oe.BeginPositionReconciliation(context.Background())
		acquired <- result{release: release, err: err}
	}()

	deadline := time.Now().Add(time.Second)
	for !oe.submissionGate.Blocked() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !oe.submissionGate.Blocked() {
		inFlightRelease()
		t.Fatal("reconciliation did not block new submissions")
	}
	select {
	case <-acquired:
		inFlightRelease()
		t.Fatal("reconciliation barrier returned before in-flight submission drained")
	default:
	}

	inFlightRelease()
	var barrierRelease func()
	select {
	case got := <-acquired:
		if got.err != nil {
			t.Fatalf("BeginPositionReconciliation() error = %v", got.err)
		}
		barrierRelease = got.release
	case <-time.After(time.Second):
		t.Fatal("reconciliation barrier did not acquire after drain")
	}
	if _, err := oe.admitSubmission(context.Background(), &OrderRequest{Side: "BUY"}); !errors.Is(err, ErrRuntimeStopping) {
		t.Fatalf("submission during reconciliation error = %v, want ErrRuntimeStopping", err)
	}
	barrierRelease()
	release, err := oe.admitSubmission(context.Background(), &OrderRequest{Side: "BUY"})
	if err != nil {
		t.Fatalf("submission after barrier release: %v", err)
	}
	release()
}

func TestPositionReconciliationBarrierCancellationUnblocksExecutor(t *testing.T) {
	oe := &ExchangeOrderExecutor{}
	inFlightRelease, err := oe.submissionGate.Begin()
	if err != nil {
		t.Fatalf("begin in-flight submission: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := oe.BeginPositionReconciliation(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("BeginPositionReconciliation() error = %v, want context.Canceled", err)
	}
	if oe.submissionGate.Blocked() {
		t.Fatal("canceled reconciliation left the executor blocked")
	}
	inFlightRelease()
	release, err := oe.admitSubmission(context.Background(), &OrderRequest{Side: "BUY"})
	if err != nil {
		t.Fatalf("submission after canceled barrier: %v", err)
	}
	release()
}

func TestConcurrentPositionReconciliationBarriersReleaseIndependently(t *testing.T) {
	oe := &ExchangeOrderExecutor{}
	releaseOne, err := oe.BeginPositionReconciliation(context.Background())
	if err != nil {
		t.Fatalf("first barrier: %v", err)
	}
	releaseTwo, err := oe.BeginPositionReconciliation(context.Background())
	if err != nil {
		releaseOne()
		t.Fatalf("second barrier: %v", err)
	}
	releaseOne()
	if !oe.submissionGate.Blocked() {
		releaseTwo()
		t.Fatal("releasing one barrier cleared another active reconciliation block")
	}
	releaseTwo()
	if oe.submissionGate.Blocked() {
		t.Fatal("all reconciliation blocks released but executor remains blocked")
	}
}

func TestOrderSubmissionWaitsForSharedPositionReconciliationLock(t *testing.T) {
	sharedLock := newSharedPositionLock()
	const exchangeName, symbol = "binance", "BTCUSDT"
	key := execution.PositionReconciliationLockKey(exchangeName, symbol)
	if err := sharedLock.Lock(context.Background(), key, execution.PositionReconciliationLockTTL); err != nil {
		t.Fatalf("acquire reconciliation lock: %v", err)
	}
	defer func() { _ = sharedLock.Unlock(context.Background(), key) }()

	oe := &ExchangeOrderExecutor{lock: sharedLock}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := oe.acquirePositionSubmissionLock(ctx, exchangeName, symbol); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("submission lock while reconciliation holds it: %v, want deadline exceeded", err)
	}
	if err := sharedLock.Unlock(context.Background(), key); err != nil {
		t.Fatalf("release reconciliation lock: %v", err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, release, err := oe.acquirePositionSubmissionLock(ctx, exchangeName, symbol)
	if err != nil {
		t.Fatalf("submission after reconciliation: %v", err)
	}
	release()
}

func TestPositionSnapshotHoldsSharedLeaseAndSubmissionBarrier(t *testing.T) {
	sharedLock := newSharedPositionLock()
	ex := &fakeOrderExchange{}
	snapshotter := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, sharedLock, "snapshot")
	ctx, release, err := snapshotter.BeginPositionSnapshot(context.Background())
	if err != nil {
		t.Fatalf("BeginPositionSnapshot() error = %v", err)
	}
	defer release()
	if err := ctx.Err(); err != nil {
		t.Fatalf("snapshot context unexpectedly canceled: %v", err)
	}
	if sharedLock.try(execution.PositionReconciliationLockKey("fake", "BTCUSDT")) {
		t.Fatal("distributed position lease was not held during snapshot")
	}
	if _, err := snapshotter.admitSubmission(context.Background(), &OrderRequest{Side: "BUY"}); !errors.Is(err, ErrRuntimeStopping) {
		t.Fatalf("local submission during snapshot = %v, want blocked", err)
	}

	submitter := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, sharedLock, "submitter")
	deadlineCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := submitter.acquirePositionSubmissionLock(deadlineCtx, "fake", "BTCUSDT"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cross-executor submission lease during snapshot = %v, want deadline exceeded", err)
	}
	release()
	_, submitRelease, err := submitter.acquirePositionSubmissionLock(context.Background(), "fake", "BTCUSDT")
	if err != nil {
		t.Fatalf("submission lease after snapshot release: %v", err)
	}
	submitRelease()
}

func TestPositionSubmissionUsesLocalBarrierWithNoopDistributedLock(t *testing.T) {
	const exchangeName, symbol = "binance", "BTCUSDT"
	key := execution.PositionReconciliationLockKey(exchangeName, symbol)
	unlockSnapshot, err := execution.AcquireLocalPositionCoordination(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	oe := &ExchangeOrderExecutor{lock: lock.NewNopLock()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := oe.acquirePositionSubmissionLock(ctx, exchangeName, symbol); !errors.Is(err, context.DeadlineExceeded) {
		unlockSnapshot()
		t.Fatalf("submission did not wait for local reconciliation barrier: %v", err)
	}
	unlockSnapshot()
	_, release, err := oe.acquirePositionSubmissionLock(context.Background(), exchangeName, symbol)
	if err != nil {
		t.Fatalf("submission after local reconciliation: %v", err)
	}
	release()
}

func TestPositionSubmissionLockRenewalFailureCancelsAndBlocksExecutor(t *testing.T) {
	sharedLock := newSharedPositionLock()
	sharedLock.failExtend = true
	oe := &ExchangeOrderExecutor{lock: sharedLock}
	ctx, release, err := oe.acquirePositionSubmissionLockWithTTL(context.Background(), "binance", "BTCUSDT", 15*time.Millisecond)
	if err != nil {
		t.Fatalf("acquire submission lock: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		release()
		t.Fatal("lock renewal failure did not cancel the submission context")
	}
	if !oe.submissionGate.HasBlock(execution.PositionCoordinationLockLostBlock) {
		release()
		t.Fatal("lock renewal failure did not fail closed for this executor")
	}
	release()
	if _, err := oe.admitSubmission(context.Background(), &OrderRequest{Side: "BUY"}); !errors.Is(err, ErrRuntimeStopping) {
		t.Fatalf("submission after coordination lock loss = %v, want ErrRuntimeStopping", err)
	}
}

func TestSuccessfulPositionReconciliationClearsOnlyItsOwnGate(t *testing.T) {
	oe := &ExchangeOrderExecutor{}
	oe.FailPositionReconciliation(errors.New("position evidence unavailable"))
	oe.submissionGate.Block(execution.PositionCoordinationLockLostBlock)

	if !oe.submissionGate.HasBlock(execution.PositionReconciliationUnverifiedBlock) {
		t.Fatal("failed reconciliation did not block physical submissions")
	}
	oe.CompletePositionReconciliation()
	if oe.submissionGate.HasBlock(execution.PositionReconciliationUnverifiedBlock) {
		t.Fatal("successful reconciliation did not clear its own block")
	}
	if !oe.submissionGate.HasBlock(execution.PositionCoordinationLockLostBlock) {
		t.Fatal("successful reconciliation incorrectly cleared independent lock-loss block")
	}
}
