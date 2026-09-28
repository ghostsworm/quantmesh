package strategy

import (
	"context"
	"sync"
	"testing"
	"time"

	"quantmesh/lock"
)

type walletCoordinationTestLock struct {
	mu       sync.Mutex
	active   int
	max      int
	acquired []string
}

func (l *walletCoordinationTestLock) Lock(_ context.Context, key string, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active++
	if l.active > l.max {
		l.max = l.active
	}
	l.acquired = append(l.acquired, key)
	return nil
}

func (l *walletCoordinationTestLock) TryLock(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func (l *walletCoordinationTestLock) Unlock(_ context.Context, _ string) error {
	l.mu.Lock()
	l.active--
	l.mu.Unlock()
	return nil
}

func (*walletCoordinationTestLock) Extend(context.Context, string, time.Duration) error { return nil }
func (*walletCoordinationTestLock) Close() error                                        { return nil }

var _ lock.DistributedLock = (*walletCoordinationTestLock)(nil)

func TestFundingCarryWalletCoordinationSerializesSameAccountInProcess(t *testing.T) {
	coordinator := &walletCoordinationTestLock{}
	const key = "funding_carry_wallet:account-a"
	first := &FundingCarryStrategy{}
	second := &FundingCarryStrategy{}
	for _, strategy := range []*FundingCarryStrategy{first, second} {
		if err := strategy.SetAccountWalletCoordinationLock(coordinator, key); err != nil {
			t.Fatal(err)
		}
	}

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.withAccountWalletCoordination(context.Background(), func(context.Context) error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()
	<-firstEntered

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- second.withAccountWalletCoordination(context.Background(), func(context.Context) error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("same-account operation entered before the first released its wallet gate")
	case <-time.After(30 * time.Millisecond):
	}
	close(releaseFirst)
	for _, done := range []<-chan error{firstDone, secondDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("wallet-coordinated operation failed: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("wallet-coordinated operation did not finish")
		}
	}
	if coordinator.max != 1 {
		t.Fatalf("distributed lease overlapped %d operations; want 1", coordinator.max)
	}
	if len(coordinator.acquired) != 2 || coordinator.acquired[0] != key || coordinator.acquired[1] != key {
		t.Fatalf("unexpected distributed lock keys: %v", coordinator.acquired)
	}
}
