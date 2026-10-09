package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
)

type stoppedCloseCASStore struct {
	*borrowReceiptContextStore
	afterCAS func()
	ackError bool
}

func (s *stoppedCloseCASStore) CompareAndSwapRuntimeState(ctx context.Context, name string, version int, payload string, nextVersion int, nextPayload string) (bool, error) {
	saved, err := s.memoryRuntimeStateStore.CompareAndSwapRuntimeState(ctx, name, version, payload, nextVersion, nextPayload)
	if saved {
		if s.afterCAS != nil {
			s.afterCAS()
		}
		if s.ackError {
			return false, errors.New("saved clean checkpoint but acknowledgement lost")
		}
	}
	return saved, err
}

type stoppedCloseUnlockFault struct {
	*walletCoordinationTestLock
	fail bool
}

func (l *stoppedCloseUnlockFault) Unlock(ctx context.Context, key string) error {
	err := l.walletCoordinationTestLock.Unlock(ctx, key)
	if l.fail {
		return errors.Join(err, errors.New("wallet cleanup acknowledgement failed"))
	}
	return err
}

func stoppedCloseCheckpointFixture(t *testing.T) (*FundingCarryStrategy, *fundingCarryCloseLiabilityVenue, *memoryRuntimeStateStore) {
	t.Helper()
	s, margin, memory := newFundingCarryRepayIntentFixture()
	s.strategySpotKnown = true
	s.marginBorrowedAt = time.UnixMilli(1000).UTC()
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, OccurredAt: s.marginBorrowedAt, AccountScope: "scope-a"}}
	margin.queryErr = nil
	v := &fundingCarryCloseLiabilityVenue{fundingCarryRepayIntentExchange: margin, principal: 0.4}
	v.afterRead = func() {
		if margin.repayCalls > 0 {
			v.queryErr = errors.New("final liability query temporarily unavailable")
		}
	}
	s.marginEx = v
	s.SetRuntimeStateStore(&borrowReceiptContextStore{memory})
	s.ctx, s.cancel = context.WithCancel(t.Context())
	s.started = true
	s.runDone = make(chan struct{})
	close(s.runDone)
	s.accountWalletLock, s.accountWalletLockKey = &walletCoordinationTestLock{}, "stopped-close-"+t.Name()
	if err := s.StopContext(t.Context()); err == nil || margin.repayCalls != 1 || !s.marginCloseVerificationPending || !s.stopAttempted {
		t.Fatalf("production stop did not retain its final verification: %v", err)
	}
	return s, v, memory
}

func TestFundingCarryStoppedFinalVerificationCompletesWithoutFinancialReplay(t *testing.T) {
	s, venue, memory := stoppedCloseCheckpointFixture(t)
	before := memory.payload
	venue.queryErr, venue.afterRead = nil, nil
	if err := s.ReconcileStoppedMarginClose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !s.stopCompleted || s.stopErr != nil || s.marginCloseVerificationPending || s.unownedExposure || s.intentInFlight || s.direction != DirectionNone || s.IsRunning() || memory.payload == before {
		t.Fatal("readonly verification did not complete stopped state")
	}
	if err := s.StopContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileStoppedMarginClose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyFlat(t.Context()); err != nil {
		t.Fatal(err)
	}
	if venue.repayCalls != 1 || len(venue.placedOrders) != 1 || venue.borrowAmount != 0 {
		t.Fatal("readonly stop completion replayed finances")
	}
}

func TestFundingCarryPersistedStoppedFinalVerificationReconstructsReadOnly(t *testing.T) {
	donor, venue, memory := stoppedCloseCheckpointFixture(t)
	venue.queryErr, venue.afterRead = nil, nil
	var checkpoint fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(memory.payload), &checkpoint); err != nil {
		t.Fatal(err)
	}
	if !checkpoint.MarginCloseVerificationPending || !checkpoint.IntentInFlight || checkpoint.MarginAccountScope != "scope-a" {
		t.Fatalf("fixture does not encode the expected final-verification-only checkpoint: %+v", checkpoint)
	}
	recovered := NewFundingCarryStrategy("funding_carry", nil, donor.symCfg, donor.fut, donor.spot, venue, nil)
	recovered.SetRuntimeStateStore(&borrowReceiptContextStore{memory})
	if err := recovered.SetMarginAccountScope("scope-a"); err != nil {
		t.Fatal(err)
	}
	if err := recovered.SetAccountWalletCoordinationLock(donor.accountWalletLock, donor.accountWalletLockKey); err != nil {
		t.Fatal(err)
	}
	if recovered.IsRunning() {
		t.Fatal("recovery verifier unexpectedly started producers")
	}
	if err := recovered.ReconcilePersistedStoppedMarginClose(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if recovered.IsRunning() || !recovered.stopCompleted || recovered.stopErr != nil || recovered.marginCloseVerificationPending || recovered.intentInFlight || recovered.unownedExposure {
		t.Fatal("persisted final-verification recovery did not finish in stopped, reconciled state")
	}
	if venue.repayCalls != 1 || venue.borrowAmount != 0 || len(venue.placedOrders) != 1 {
		t.Fatal("persisted final-verification recovery replayed a financial operation")
	}
}

func TestFundingCarryPersistedStoppedFinalVerificationRejectsUnmarkedState(t *testing.T) {
	donor, venue, memory := stoppedCloseCheckpointFixture(t)
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(memory.payload), &state); err != nil {
		t.Fatal(err)
	}
	state.MarginCloseVerificationPending = false
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(encoded)); err != nil {
		t.Fatal(err)
	}
	recovered := NewFundingCarryStrategy("funding_carry", nil, donor.symCfg, donor.fut, donor.spot, venue, nil)
	recovered.SetRuntimeStateStore(&borrowReceiptContextStore{memory})
	if err := recovered.SetMarginAccountScope("scope-a"); err != nil {
		t.Fatal(err)
	}
	before := memory.payload
	if err := recovered.ReconcilePersistedStoppedMarginClose(t.Context(), func() error { return nil }); err == nil {
		t.Fatal("recovery accepted a checkpoint without the explicit final-verification marker")
	}
	if memory.payload != before || recovered.IsRunning() || venue.repayCalls != 1 {
		t.Fatal("rejected checkpoint was mutated or financial work was replayed")
	}
}

func TestFundingCarryPersistedStoppedFinalVerificationRequiresCurrentOwnership(t *testing.T) {
	for _, loseDuringProof := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_recovery", true: "during_exchange_read"}[loseDuringProof], func(t *testing.T) {
			donor, venue, memory := stoppedCloseCheckpointFixture(t)
			venue.queryErr, venue.afterRead = nil, nil
			lost := !loseDuringProof
			if loseDuringProof {
				venue.afterRead = func() { lost = true }
			}
			recovered := NewFundingCarryStrategy("funding_carry", nil, donor.symCfg, donor.fut, donor.spot, venue, nil)
			recovered.SetRuntimeStateStore(&borrowReceiptContextStore{memory})
			if err := recovered.SetMarginAccountScope("scope-a"); err != nil {
				t.Fatal(err)
			}
			if err := recovered.SetAccountWalletCoordinationLock(donor.accountWalletLock, donor.accountWalletLockKey); err != nil {
				t.Fatal(err)
			}
			ownershipErr := errors.New("runtime ownership lease lost")
			guard := func() error {
				if lost {
					return ownershipErr
				}
				return nil
			}
			before := memory.payload
			err := recovered.ReconcilePersistedStoppedMarginClose(t.Context(), guard)
			if !errors.Is(err, ownershipErr) {
				t.Fatalf("lost ownership was not propagated: %v", err)
			}
			if memory.payload != before || recovered.IsRunning() || venue.repayCalls != 1 {
				t.Fatal("ownership loss committed the checkpoint or replayed financial work")
			}
		})
	}
}

func TestFundingCarryPersistedCleanStoppedFlatVerificationAfterRestart(t *testing.T) {
	donor, venue, memory := stoppedCloseCheckpointFixture(t)
	venue.queryErr, venue.afterRead = nil, nil
	closeVerifier := NewFundingCarryStrategy("funding_carry", nil, donor.symCfg, donor.fut, donor.spot, venue, nil)
	closeVerifier.SetRuntimeStateStore(&borrowReceiptContextStore{memory})
	if err := closeVerifier.SetMarginAccountScope("scope-a"); err != nil {
		t.Fatal(err)
	}
	if err := closeVerifier.SetAccountWalletCoordinationLock(donor.accountWalletLock, donor.accountWalletLockKey); err != nil {
		t.Fatal(err)
	}
	if err := closeVerifier.ReconcilePersistedStoppedMarginClose(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	type financialCounts struct {
		repays   int
		orders   int
		borrowed float64
	}
	beforeFinancials := financialCounts{repays: venue.repayCalls, orders: len(venue.placedOrders), borrowed: venue.borrowAmount}

	// Model a process restart after the clean state CAS but before external
	// capital/lease release and journal retirement.
	restarted := NewFundingCarryStrategy("funding_carry", nil, donor.symCfg, donor.fut, donor.spot, venue, nil)
	restarted.SetRuntimeStateStore(&borrowReceiptContextStore{memory})
	if err := restarted.SetMarginAccountScope("scope-a"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SetAccountWalletCoordinationLock(donor.accountWalletLock, donor.accountWalletLockKey); err != nil {
		t.Fatal(err)
	}
	if err := restarted.VerifyPersistedStoppedFlat(t.Context(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if restarted.IsRunning() || restarted.marginCloseVerificationPending || restarted.intentInFlight || restarted.unownedExposure || restarted.direction != DirectionNone {
		t.Fatal("clean persisted verifier started producers or restored unresolved state")
	}
	if got := (financialCounts{repays: venue.repayCalls, orders: len(venue.placedOrders), borrowed: venue.borrowAmount}); got != beforeFinancials {
		t.Fatalf("restart flat verification changed financial operations: before=%+v after=%+v", beforeFinancials, got)
	}
}

func TestFundingCarryPersistedStoppedFlatVerificationRejectsPendingMarker(t *testing.T) {
	donor, venue, memory := stoppedCloseCheckpointFixture(t)
	restarted := NewFundingCarryStrategy("funding_carry", nil, donor.symCfg, donor.fut, donor.spot, venue, nil)
	restarted.SetRuntimeStateStore(&borrowReceiptContextStore{memory})
	if err := restarted.SetMarginAccountScope("scope-a"); err != nil {
		t.Fatal(err)
	}
	before := memory.payload
	err := restarted.VerifyPersistedStoppedFlat(t.Context(), func() error { return nil })
	if err == nil {
		t.Fatal("clean stopped flat verifier accepted an unresolved final-verification marker")
	}
	if memory.payload != before || restarted.IsRunning() || venue.repayCalls != 1 {
		t.Fatal("rejected pending-marker checkpoint was mutated or replayed financial work")
	}
}

func TestFundingCarryStoppedFinalVerificationFailuresPreserveRetryEvidence(t *testing.T) {
	donor, _, memory := stoppedCloseCheckpointFixture(t)
	var seed fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(memory.payload), &seed); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"verified", "persistence_recovered", "no_marker", "not_attempted", "active_producer", "unfinished_producer", "debt", "interest", "futures", "open_orders", "unknown_orders", "execution_pending", "cancelled", "owner_lost", "source_changed", "cas_ack_error", "cancel_after_cas", "unlock_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, parent, storeMemory := newFundingCarryRepayIntentFixture()
			state := seed
			if mode == "no_marker" {
				state.MarginCloseVerificationPending = false
			}
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := storeMemory.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(encoded)); err != nil {
				t.Fatal(err)
			}
			s.direction, s.marginDebt, s.marginBorrowTransferID, s.marginBorrowedAt = state.Direction, state.MarginDebt, state.MarginBorrowTransferID, state.MarginBorrowedAt
			s.strategySpotKnown, s.intentInFlight, s.unownedExposure, s.marginCloseVerificationPending = true, true, true, state.MarginCloseVerificationPending
			s.marginDebtEvents, s.marginCoverOrders = state.MarginDebtEvents, cloneFundingCarryCoverOrders(state.MarginCoverOrders)
			s.started = true
			s.ctx, s.cancel = context.WithCancel(t.Context())
			s.cancel()
			s.runDone = make(chan struct{})
			close(s.runDone)
			s.stopAttempted, s.stopErr = true, donor.stopErr
			venue := &fundingCarryCloseLiabilityVenue{fundingCarryRepayIntentExchange: parent}
			s.marginEx = venue
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			store := &stoppedCloseCASStore{borrowReceiptContextStore: &borrowReceiptContextStore{storeMemory}}
			s.SetRuntimeStateStore(store)
			coordinator := &stoppedCloseUnlockFault{walletCoordinationTestLock: &walletCoordinationTestLock{}}
			s.accountWalletLock, s.accountWalletLockKey = coordinator, "stopped-matrix-"+t.Name()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			expected := storeMemory.payload
			switch mode {
			case "persistence_recovered":
				s.runtimeStateErr = errors.New("earlier live write failed")
			case "not_attempted":
				s.stopAttempted = false
			case "active_producer":
				s.ctx, s.cancel = context.WithCancel(t.Context())
				defer s.cancel()
			case "unfinished_producer":
				s.runDone = make(chan struct{})
			case "debt":
				venue.principal = 0.00001
			case "interest":
				venue.interest = 0.00001
			case "futures":
				s.fut.(*mockFCExchange).positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.00001}}
			case "open_orders":
				parent.openOrders = []*exchange.Order{{OrderID: 9, Symbol: "ETHUSDT"}}
			case "unknown_orders":
				venue.unknownOrders = true
			case "execution_pending":
				s.RequireExecutionRecovery()
			case "cancelled":
				venue.afterRead = cancel
			case "owner_lost":
				venue.afterRead = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
			case "source_changed":
				venue.afterRead = func() {
					changed := state
					changed.OwnedFutures = 0.1
					updated, _ := json.Marshal(changed)
					storeMemory.payload, expected = string(updated), string(updated)
				}
			case "cas_ack_error":
				store.ackError = true
			case "cancel_after_cas":
				store.afterCAS = cancel
			case "unlock_failure":
				coordinator.fail = true
			}
			err = s.ReconcileStoppedMarginClose(ctx)
			positive := mode == "verified" || mode == "persistence_recovered"
			committedFailure := mode == "cas_ack_error" || mode == "cancel_after_cas" || mode == "unlock_failure"
			if positive {
				if err != nil || !s.stopCompleted || s.stopErr != nil || s.runtimeStateErr != nil || s.marginCloseVerificationPending {
					t.Fatalf("stopped verified completion failed: %v", err)
				}
			} else {
				if err == nil || s.stopCompleted || s.stopErr == nil {
					t.Fatal("failed proof completed stop")
				}
				if !committedFailure && storeMemory.payload != expected {
					t.Fatal("failed proof overwrote durable evidence")
				}
				if mode != "no_marker" && !s.marginCloseVerificationPending {
					t.Fatal("failed cleanup lost original readonly retry provenance")
				}
			}
			if committedFailure {
				store.ackError, store.afterCAS, coordinator.fail = false, nil, false
				if err := s.ReconcileStoppedMarginClose(t.Context()); err != nil || !s.stopCompleted {
					t.Fatalf("committed clean state could not retry readonly verification: %v", err)
				}
			}
			if parent.repayCalls != 0 || parent.borrowAmount != 0 || len(parent.placedOrders) != 0 || len(s.fut.(*mockFCExchange).placedOrders) != 0 || len(s.spot.(*mockFCExchange).placedOrders) != 0 {
				t.Fatal("readonly retry replayed finances")
			}
			coordinator.mu.Lock()
			active := coordinator.active
			coordinator.mu.Unlock()
			if active != 0 {
				t.Fatal("stopped verification leaked wallet lock")
			}
		})
	}
}
