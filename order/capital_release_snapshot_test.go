package order

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/execution"
	"quantmesh/lock"
)

var errCapitalSnapshotUnlock = errors.New("snapshot unlock failed")
var errCapitalSnapshotRenewal = errors.New("snapshot lease renewal lost")

type capitalSnapshotRenewalFailure struct{ lock.DistributedLock }

func (l capitalSnapshotRenewalFailure) Extend(context.Context, string, time.Duration) error {
	return errCapitalSnapshotRenewal
}

type capitalSnapshotCleanupLock struct {
	lock.DistributedLock
	unlockCalls atomic.Int32
	unlockErr   error
}

func (l *capitalSnapshotCleanupLock) Unlock(ctx context.Context, _ string) error {
	l.unlockCalls.Add(1)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return l.unlockErr
}

func TestCapitalReleaseSnapshotCleanupReportsFailureOnceAndReleasesLocalGate(t *testing.T) {
	coordinator := &capitalSnapshotCleanupLock{DistributedLock: lock.NewNopLock(), unlockErr: errCapitalSnapshotUnlock}
	oe := NewExchangeOrderExecutor(&fakeOrderExchange{}, "BTCUSDT", 0, 0, coordinator, "")
	ctx, release, err := oe.BeginCapitalReleaseSnapshot(t.Context())
	if err != nil || !oe.submissionGate.Blocked() {
		t.Fatalf("snapshot barrier not acquired: %v", err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := release(); !errors.Is(err, errCapitalSnapshotUnlock) {
				t.Errorf("cleanup result lost: %v", err)
			}
		}()
	}
	wg.Wait()
	if coordinator.unlockCalls.Load() != 1 || ctx.Err() == nil || oe.submissionGate.Blocked() {
		t.Fatal("cleanup repeated or left temporary barrier/context alive")
	}
	coordinator.unlockErr = nil
	_, retry, err := oe.BeginCapitalReleaseSnapshot(t.Context())
	if err != nil || retry() != nil {
		t.Fatalf("local coordination gate was not released: %v", err)
	}
}

func TestCapitalReleaseSnapshotDrainFailureRetainsBothErrors(t *testing.T) {
	coordinator := &capitalSnapshotCleanupLock{DistributedLock: lock.NewNopLock(), unlockErr: errCapitalSnapshotUnlock}
	oe := NewExchangeOrderExecutor(&fakeOrderExchange{}, "BTCUSDT", 0, 0, coordinator, "")
	finish, err := oe.submissionGate.Begin()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	snapshotCtx, release, err := oe.BeginCapitalReleaseSnapshot(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, errCapitalSnapshotUnlock) || release != nil || snapshotCtx != nil {
		t.Fatalf("drain or cleanup failure hidden: %v", err)
	}
	finish()
	if oe.submissionGate.Blocked() || coordinator.unlockCalls.Load() != 1 {
		t.Fatal("drain failure left its barrier or skipped cleanup")
	}
	coordinator.unlockErr = nil
	oe.submissionGate.Block(execution.PositionCoordinationLockLostBlock)
	_, retry, err := oe.BeginCapitalReleaseSnapshot(t.Context())
	if err != nil || retry() != nil || !oe.submissionGate.HasBlock(execution.PositionCoordinationLockLostBlock) {
		t.Fatalf("retry lost independent safety block: %v", err)
	}
}

func TestCapitalReleaseSnapshotCleanupRetainsLeaseLoss(t *testing.T) {
	coordinator := capitalSnapshotRenewalFailure{lock.NewNopLock()}
	oe := NewExchangeOrderExecutor(&fakeOrderExchange{}, "BTCUSDT", 0, 0, coordinator, "")
	ctx, release, err := oe.acquirePositionSubmissionLease(t.Context(), "fake", "BTCUSDT", 15*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		_ = release()
		t.Fatal("lease loss did not cancel snapshot")
	}
	if err := release(); !errors.Is(err, errCapitalSnapshotRenewal) {
		t.Fatalf("cleanup hid renewal failure: %v", err)
	}
	if !oe.submissionGate.HasBlock(execution.PositionCoordinationLockLostBlock) {
		t.Fatal("cleanup removed lease-loss safety block")
	}
}
