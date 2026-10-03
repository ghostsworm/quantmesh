package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/execution"
)

func TestFundingCarryFlatVerificationDoesNotHoldWalletWhileWaitingForOperation(t *testing.T) {
	s, margin, _ := newFundingCarryRepayIntentFixture()
	margin.positions = nil // fixture's authoritative empty margin snapshot
	s.direction, s.marginDebt, s.marginBorrowTransferID, s.strategySpotKnown = DirectionNone, 0, 0, true
	if err := s.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	coordinator := &walletCoordinationTestLock{}
	if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	if err := s.acquireOperation(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.VerifyFlat(ctx) }()
	// Give the verifier a chance to reach its first lock. Detecting acquisition
	// here is positive evidence of inversion, not an inference from timeout.
	deadline := time.NewTimer(80 * time.Millisecond)
	ticker := time.NewTicker(time.Millisecond)
	walletTaken := false
wait:
	for {
		select {
		case <-ticker.C:
			coordinator.mu.Lock()
			walletTaken = coordinator.active != 0
			coordinator.mu.Unlock()
			if walletTaken {
				break wait
			}
		case <-deadline.C:
			break wait
		}
	}
	deadline.Stop()
	ticker.Stop()
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	probeErr := s.withAccountWalletCoordination(probeCtx, func(context.Context) error { return nil })
	probeCancel()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("cancelled verifier did not release its wait")
	}
	s.releaseOperation()
	if walletTaken || probeErr != nil {
		t.Fatalf("flat verifier held wallet while waiting for operation: acquired=%v probe=%v", walletTaken, probeErr)
	}
	// Cancellation must not leave either token or lease unavailable.
	retryCtx, retryCancel := context.WithTimeout(context.Background(), time.Second)
	defer retryCancel()
	if err := s.VerifyFlat(retryCtx); err != nil {
		t.Fatal("released verifier cannot retry:", err)
	}
}

func TestFundingCarryFlatLostOwnerDoesNotWaitForOperation(t *testing.T) {
	s, _, _ := newFundingCarryRepayIntentFixture()
	gate := &execution.OpeningGate{}
	gate.Block(strategyWalletRuntimeOwnershipBlock)
	s.SetOpeningGate(gate)
	if err := s.acquireOperation(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.releaseOperation()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.VerifyFlat(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lost owner queued behind operation instead of being refused: %v", err)
	}
}
