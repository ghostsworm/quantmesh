package strategy

import (
	"context"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
)

func TestFundingCarryFuturesOpeningAckAfterOwnerLossPreservesRecoveryState(t *testing.T) {
	s, _, _ := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	gate := &execution.OpeningGate{}
	s.SetOpeningGate(gate)
	if err := s.beginRuntimeIntent(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkpoint := store.payload

	gate.Block(strategyWalletRuntimeOwnershipBlock)
	err := s.recordFuturesOpening(&exchange.Order{ExecutedQty: 0.25}, exchange.SideSell, 0.25)
	if err == nil {
		t.Fatal("opening acknowledgement committed after runtime ownership was lost")
	}
	if store.payload != checkpoint {
		t.Fatal("stale owner overwrote the durable in-flight recovery checkpoint")
	}
	if !s.unownedExposure || !s.intentInFlight {
		t.Fatalf("stale acknowledgement did not remain blocked: exposure_unknown=%v intent_in_flight=%v", s.unownedExposure, s.intentInFlight)
	}
}

func TestFundingCarrySpotFillAfterOwnerLossPreservesRecoveryState(t *testing.T) {
	s, _, _ := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.strategySpotKnown = true
	s.strategySpotQty, s.spotQty = 0.5, 0.5
	gate := &execution.OpeningGate{}
	s.SetOpeningGate(gate)
	if err := s.beginRuntimeIntent(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkpoint := store.payload

	gate.Block(strategyWalletRuntimeOwnershipBlock)
	if err := s.recordStrategySpot(0.25); err == nil {
		t.Fatal("spot fill committed after runtime ownership was lost")
	}
	if store.payload != checkpoint || s.strategySpotQty != 0.5 || !s.unownedExposure || !s.intentInFlight {
		t.Fatal("stale owner changed spot inventory or overwrote its durable recovery checkpoint")
	}
}

func TestFundingCarrySpotCloseFillAfterOwnerLossPreservesRecoveryState(t *testing.T) {
	s, _, _ := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.strategySpotKnown = true
	s.strategySpotQty, s.spotQty = 0.5, 0.5
	gate := &execution.OpeningGate{}
	s.SetOpeningGate(gate)
	if err := s.beginRuntimeIntent(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkpoint := store.payload

	gate.Block(strategyWalletRuntimeOwnershipBlock)
	if err := s.releaseStrategySpot(0.25); err == nil {
		t.Fatal("spot close fill committed after runtime ownership was lost")
	}
	if store.payload != checkpoint || s.strategySpotQty != 0.5 || !s.unownedExposure || !s.intentInFlight {
		t.Fatal("stale owner changed spot inventory or overwrote its durable recovery checkpoint")
	}
}

func TestFundingCarryRuntimeStateWriteAfterOwnerLossPreservesRecoveryState(t *testing.T) {
	s, _, _ := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	gate := &execution.OpeningGate{}
	s.SetOpeningGate(gate)
	if err := s.beginRuntimeIntent(context.Background()); err != nil {
		t.Fatal(err)
	}
	checkpoint := store.payload

	gate.Block(strategyWalletRuntimeOwnershipBlock)
	s.direction, s.futQty = DirectionForward, 0.25
	if err := s.persistRuntimeStateLocked(); err == nil {
		t.Fatal("runtime-state store accepted a direct write after ownership loss")
	}
	if store.payload != checkpoint {
		t.Fatal("direct stale-owner write overwrote durable recovery checkpoint")
	}
}
