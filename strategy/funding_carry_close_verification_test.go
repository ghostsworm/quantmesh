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

type closeVerificationStore struct {
	*borrowReceiptContextStore
	beforeCAS func()
	casCalls  int
}

func (s *closeVerificationStore) CompareAndSwapRuntimeState(ctx context.Context, name string, version int, payload string, nextVersion int, nextPayload string) (bool, error) {
	s.casCalls++
	if s.beforeCAS != nil {
		s.beforeCAS()
	}
	return s.memoryRuntimeStateStore.CompareAndSwapRuntimeState(ctx, name, version, payload, nextVersion, nextPayload)
}

func TestFundingCarryFinalCloseVerificationRestartDoesNotReplayFinances(t *testing.T) {
	// Obtain the checkpoint from the actual close path, not a fabricated flag.
	donor, margin, originalStore := newFundingCarryRepayIntentFixture()
	donor.strategySpotKnown = true
	donor.marginBorrowedAt = time.UnixMilli(1000).UTC()
	donor.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, OccurredAt: donor.marginBorrowedAt, AccountScope: "scope-a"}}
	margin.queryErr = nil
	venue := &fundingCarryCloseLiabilityVenue{fundingCarryRepayIntentExchange: margin, principal: 0.4}
	venue.afterRead = func() {
		if margin.repayCalls > 0 {
			venue.queryErr = errors.New("final read unavailable after confirmed repayment")
		}
	}
	donor.marginEx = venue
	donor.cancel = func() {}
	donor.runDone = make(chan struct{})
	close(donor.runDone)
	donor.accountWalletLockKey = "close-verification-" + t.Name()
	coordinator := &walletCoordinationTestLock{}
	donor.accountWalletLock = coordinator
	stopErr := donor.StopContext(t.Context())
	if stopErr == nil || margin.repayCalls != 1 || !donor.marginCloseVerificationPending || !donor.intentInFlight {
		t.Fatalf("actual close did not preserve its completed financial phase: repay=%d marker=%v intent=%v err=%v", margin.repayCalls, donor.marginCloseVerificationPending, donor.intentInFlight, stopErr)
	}
	beforeRetry := originalStore.payload
	if err := donor.StopContext(t.Context()); err != stopErr || margin.repayCalls != 1 || len(margin.placedOrders) != 1 || originalStore.payload != beforeRetry {
		t.Fatal("failed production stop was financially replayed before restart verification")
	}
	var seed fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(originalStore.payload), &seed); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"verified", "verified_terminal_history", "no_marker", "external_debt", "tiny_interest", "futures_exposure", "open_order", "unknown_orders", "wrong_scope", "execution_pending", "cancelled", "owner_lost", "query_changed_source", "cas_conflict", "cas_error", "missing_ledger", "wrong_borrow_identity"} {
		t.Run(mode, func(t *testing.T) {
			s, parent, memory := newFundingCarryRepayIntentFixture()
			s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionNone, 0, 0
			s.marginDebtEvents = nil
			s.accountWalletLockKey = donor.accountWalletLockKey
			s.accountWalletLock = coordinator
			state := seed
			state.MarginCoverOrders = cloneFundingCarryCoverOrders(seed.MarginCoverOrders)
			if mode == "verified_terminal_history" {
				state.MarginCoverOrders[0].TerminalStatus = exchange.OrderStatusCanceled
				state.MarginCoverOrders[0].Requested = 0.401
			}
			if mode == "no_marker" {
				state.MarginCloseVerificationPending = false
			}
			if mode == "missing_ledger" {
				state.MarginDebtEvents = nil
			}
			if mode == "wrong_borrow_identity" {
				state.MarginBorrowTransferID++
			}
			encoded, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := memory.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(encoded)); err != nil {
				t.Fatal(err)
			}
			store := &closeVerificationStore{borrowReceiptContextStore: &borrowReceiptContextStore{memory}}
			s.SetRuntimeStateStore(store)
			current := &fundingCarryCloseLiabilityVenue{fundingCarryRepayIntentExchange: parent}
			s.marginEx = current
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			expected := memory.payload
			changeSource := func() {
				changed := state
				changed.OwnedFutures = 0.1
				newPayload, _ := json.Marshal(changed)
				memory.payload, expected = string(newPayload), string(newPayload)
			}
			switch mode {
			case "external_debt":
				current.principal = 0.00001
			case "tiny_interest":
				current.interest = 0.00001
			case "futures_exposure":
				s.fut.(*mockFCExchange).positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.00001}}
			case "open_order":
				parent.openOrders = []*exchange.Order{{OrderID: 99, Symbol: "ETHUSDT"}}
			case "unknown_orders":
				current.unknownOrders = true
			case "wrong_scope":
				s.marginAccountScope = "other-account"
			case "execution_pending":
				s.RequireExecutionRecovery()
			case "cancelled":
				current.afterRead = cancel
			case "owner_lost":
				current.afterRead = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
			case "query_changed_source":
				current.afterRead = changeSource
			case "cas_conflict":
				store.beforeCAS = changeSource
			case "cas_error":
				store.beforeCAS = func() { memory.err = errors.New("conditional checkpoint write failed") }
			}
			err = s.Start(ctx)
			if mode == "verified" || mode == "verified_terminal_history" {
				if err != nil {
					t.Fatal(err)
				}
				if err := s.QuiesceContext(t.Context()); err != nil {
					t.Fatal(err)
				}
				decoded, decodeErr := decodeFundingCarryRuntimeState(memory.version, memory.payload, s.fut.GetName(), s.spot.GetName(), s.symbol)
				if decodeErr != nil || decoded.MarginCloseVerificationPending || decoded.Direction != DirectionNone || len(decoded.MarginDebtEvents) != 2 || store.casCalls != 1 {
					t.Fatalf("verified readonly close lacks durable clean evidence: %v", decodeErr)
				}
			} else if err == nil || s.started || memory.payload != expected {
				if err == nil {
					_ = s.QuiesceContext(context.Background())
				}
				t.Fatalf("unverified close resumed trading or changed source: mode=%s err=%v", mode, err)
			}
			if parent.repayCalls != 0 || parent.borrowAmount != 0 || len(parent.placedOrders) != 0 || len(s.fut.(*mockFCExchange).placedOrders) != 0 || len(s.spot.(*mockFCExchange).placedOrders) != 0 {
				t.Fatal("final verification replayed a financial action")
			}
			if err := s.acquireOperation(t.Context()); err != nil {
				t.Fatal(err)
			}
			s.releaseOperation()
			coordinator.mu.Lock()
			active := coordinator.active
			coordinator.mu.Unlock()
			if active != 0 {
				t.Fatal("readonly restart leaked wallet coordination")
			}
		})
	}
}
