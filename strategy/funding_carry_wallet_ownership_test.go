package strategy

import (
	"context"
	"testing"
	"time"

	"quantmesh/execution"
)

type ownershipLossWalletLock struct {
	walletCoordinationTestLock
	gate *execution.OpeningGate
}

func (l *ownershipLossWalletLock) Lock(ctx context.Context, key string, ttl time.Duration) error {
	if err := l.walletCoordinationTestLock.Lock(ctx, key, ttl); err != nil {
		return err
	}
	l.gate.Block("runtime_ownership_unverified")
	return nil
}

func TestFundingCarryWalletOperationRejectsLostRuntimeOwnership(t *testing.T) {
	gate := &execution.OpeningGate{}
	coordinator := &walletCoordinationTestLock{}
	s := &FundingCarryStrategy{}
	s.SetOpeningGate(gate)
	if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	venue := &mockFCExchange{}
	operation := func(ctx context.Context) error { _, err := venue.Repay(ctx, "BTC", 0.1); return err }
	gate.Block("runtime_ownership_unverified")
	if err := s.withAccountWalletCoordination(context.Background(), operation); err == nil {
		t.Fatal("lost runtime owner still repaid shared wallet debt")
	}
	if venue.repayCalls != 0 || len(coordinator.acquired) != 0 {
		t.Fatal("lost runtime entered wallet lease or mutated wallet")
	}
	gate.Unblock("runtime_ownership_unverified")
	gate.Block("manual")
	if err := s.withAccountWalletCoordination(context.Background(), operation); err != nil || venue.repayCalls != 1 || !gate.HasBlock("manual") {
		t.Fatalf("manual opening pause blocked protective wallet operation: calls=%d err=%v", venue.repayCalls, err)
	}
}

func TestFundingCarryWalletOperationRechecksOwnerAfterLeaseAcquisition(t *testing.T) {
	gate := &execution.OpeningGate{}
	coordinator := &ownershipLossWalletLock{gate: gate}
	s := &FundingCarryStrategy{}
	s.SetOpeningGate(gate)
	if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	venue := &mockFCExchange{}
	err := s.withAccountWalletCoordination(context.Background(), func(ctx context.Context) error { _, err := venue.Repay(ctx, "BTC", 0.1); return err })
	if err == nil || venue.repayCalls != 0 || coordinator.active != 0 {
		t.Fatalf("ownership loss after acquiring wallet lease permitted mutation or leaked lease: calls=%d active=%d err=%v", venue.repayCalls, coordinator.active, err)
	}
}
