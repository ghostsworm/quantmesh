package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"quantmesh/execution"
)

func TestFundingCarryIntentFinishRejectsLostOwner(t *testing.T) {
	for _, success := range []bool{false, true} {
		s, _, store := newFundingCarryReturnedPrincipalFixture(0)
		gate := &execution.OpeningGate{}
		s.SetOpeningGate(gate)
		if err := s.beginRuntimeIntent(context.Background()); err != nil {
			t.Fatal(err)
		}
		original := store.payload
		gate.Block(strategyWalletRuntimeOwnershipBlock)
		if err := s.finishRuntimeIntent(context.Background(), success); err == nil {
			t.Fatal("lost owner finished runtime intent")
		}
		if !s.intentInFlight || store.payload != original {
			t.Fatal("lost owner cleared durable recovery intent")
		}
		s.blockOnUnownedExposure(errors.New("owner lost during intent"))
		if store.payload != original {
			t.Fatal("old owner overwrote durable state via risk block")
		}
	}
}

func TestFundingCarryTransferOwnerLossPreservesIntentAndReturnsError(t *testing.T) {
	s, futures, spot := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	s.strategySpotKnown = true
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	gate := &execution.OpeningGate{}
	s.SetOpeningGate(gate)
	var intentPayload string
	spot.transfer = func(amount float64) (string, error) {
		intentPayload = store.payload
		spot.balance -= amount
		futures.balance += amount
		gate.Block(strategyWalletRuntimeOwnershipBlock)
		return "accepted-before-owner-loss", nil
	}
	if err := s.ensureFuturesMargin(context.Background(), 200, 0); err == nil {
		t.Fatal("transfer reported success after owner loss")
	}
	if spot.transferCalls != 1 || store.payload != intentPayload || !s.intentInFlight {
		t.Fatal("old owner cleared transfer intent")
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
		t.Fatal(err)
	}
	if !state.IntentInFlight {
		t.Fatal("durable transfer no longer requires recovery")
	}
	if _, err := decodeFundingCarryRuntimeState(store.version, store.payload, s.fut.GetName(), s.spot.GetName(), s.symbol); err == nil {
		t.Fatal("restart accepted unresolved transfer")
	}
}

func TestFundingCarryIntentCancellationAndSaveFailurePreserveRecovery(t *testing.T) {
	s, _, store := newFundingCarryReturnedPrincipalFixture(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.beginRuntimeIntent(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled intent accepted: %v", err)
	}
	if s.intentInFlight || store.payload != "" {
		t.Fatal("canceled start mutated state")
	}
	if err := s.beginRuntimeIntent(context.Background()); err != nil {
		t.Fatal(err)
	}
	original := store.payload
	if err := s.finishRuntimeIntent(ctx, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled finish accepted: %v", err)
	}
	if !s.intentInFlight || store.payload != original {
		t.Fatal("canceled finish cleared recovery intent")
	}
	if err := s.beginRuntimeIntent(context.Background()); err == nil {
		t.Fatal("new intent overwrote unresolved canceled operation")
	}
	if store.payload != original || !s.unownedExposure {
		t.Fatal("canceled finish allowed further automatic work")
	}
	store.err = errors.New("injected intent finish save failure")
	if err := s.finishRuntimeIntent(context.Background(), true); err == nil {
		t.Fatal("save failure ignored")
	}
	if !s.intentInFlight || store.payload != original || !s.unownedExposure {
		t.Fatal("save failure advanced recovery state")
	}
	store.err = nil
	if err := s.finishRuntimeIntent(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if s.intentInFlight || !s.unownedExposure {
		t.Fatal("retry cleared unknown exposure or retained completed intent")
	}
}
