package strategy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

func TestFundingCarryRestoreRejectsInvalidDebtEventEconomicsAndScope(t *testing.T) {
	for _, name := range []string{"negative_principal", "negative_interest", "unbalanced_components", "missing_principal", "foreign_event_scope", "missing_event_scope", "borrow_with_interest"} {
		t.Run(name, func(t *testing.T) {
			e := fundingCarryMarginDebtEvent{Action: "repay", TransferID: 81, Asset: "BTC", Amount: 0.5, Principal: 0.49, InterestPaid: 0.01, OccurredAt: time.Now().Add(-time.Second), AccountScope: "scope-a"}
			switch name {
			case "negative_principal":
				e.Principal, e.InterestPaid = -0.1, 0.6
			case "negative_interest":
				e.Principal, e.InterestPaid = 0.6, -0.1
			case "unbalanced_components":
				e.Principal = 0.48
			case "missing_principal":
				e.Principal = 0
			case "foreign_event_scope":
				e.AccountScope = "scope-b"
			case "missing_event_scope":
				e.AccountScope = ""
			case "borrow_with_interest":
				e.Action = "borrow"
			}
			state := fundingCarryRuntimeState{Strategy: "funding_carry", Symbol: "BTCUSDT", OwnershipReady: true, MarginAccountScope: "scope-a", MarginDebtEvents: []fundingCarryMarginDebtEvent{e}}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: fundingCarryRuntimeStateVersion, payload: string(payload), found: true}
			venue := &mockFCExchange{}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, venue, venue, nil)
			s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
			if err := s.SetMarginAccountScope("scope-a"); err != nil {
				t.Fatal(err)
			}
			s.unownedExposure, s.intentInFlight = true, true
			if err := s.restoreRuntimeState(); err == nil {
				t.Fatal("invalid debt economics or account scope became verified after restart")
			}
			if s.strategySpotKnown || !s.unownedExposure || !s.intentInFlight || len(s.marginDebtEvents) != 0 || store.payload != string(payload) {
				t.Fatal("invalid ledger changed runtime or durable ownership state")
			}
		})
	}
}

func TestFundingCarryRestorePreservesValidScopedDebtEvents(t *testing.T) {
	for _, name := range []string{"borrow", "repay_interest", "interest_only", "repay_zero_interest", "legacy_flat_without_debt"} {
		t.Run(name, func(t *testing.T) {
			e := fundingCarryMarginDebtEvent{Action: "repay", TransferID: 81, Asset: "BTC", Amount: 0.5, Principal: 0.49, InterestPaid: 0.01, OccurredAt: time.Now().Add(-time.Second), AccountScope: "scope-a"}
			switch name {
			case "borrow":
				e.Action, e.Principal, e.InterestPaid = "borrow", 0.5, 0
			case "interest_only":
				e.Principal, e.InterestPaid = 0, 0.5
			case "repay_zero_interest":
				e.Principal, e.InterestPaid = 0.5, 0
			}
			state := fundingCarryRuntimeState{Strategy: "funding_carry", Symbol: "BTCUSDT", OwnershipReady: true, MarginAccountScope: "scope-a", MarginDebtEvents: []fundingCarryMarginDebtEvent{e}}
			if name == "legacy_flat_without_debt" {
				state.MarginAccountScope, state.MarginDebtEvents = "", nil
			}
			if name == "borrow" {
				state.Direction, state.MarginDebt = DirectionReverse, e.Principal
				state.MarginBorrowTransferID, state.MarginBorrowedAt = e.TransferID, e.OccurredAt
			} else if e.Principal > 0 && name != "legacy_flat_without_debt" {
				borrow := e
				borrow.Action, borrow.TransferID, borrow.Amount, borrow.InterestPaid = "borrow", 80, e.Principal, 0
				borrow.OccurredAt = e.OccurredAt.Add(-time.Second)
				state.MarginDebtEvents = []fundingCarryMarginDebtEvent{borrow, e}
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: fundingCarryRuntimeStateVersion, payload: string(payload), found: true}
			venue := &mockFCExchange{baseAsset: "BTC"}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, venue, venue, nil)
			s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
			if err := s.SetMarginAccountScope("scope-a"); err != nil {
				t.Fatal(err)
			}
			s.unownedExposure, s.intentInFlight = true, true
			if err := s.restoreRuntimeState(); err != nil {
				t.Fatal(err)
			}
			if !s.strategySpotKnown || s.unownedExposure || s.intentInFlight || len(s.marginDebtEvents) != len(state.MarginDebtEvents) || store.payload != string(payload) {
				t.Fatal("valid evidence was blocked, changed or rewritten")
			}
			if len(state.MarginDebtEvents) != 0 {
				got := s.marginDebtEvents[len(s.marginDebtEvents)-1]
				// JSON drops monotonic clock metadata, not the event instant.
				instant := got.OccurredAt
				got.OccurredAt = e.OccurredAt
				if got != e || !instant.Equal(e.OccurredAt) {
					t.Fatal("financial evidence not preserved")
				}
			}
		})
	}
}

func TestFundingCarryConfirmationRejectsBorrowWithPaidInterest(t *testing.T) {
	venue := &fundingCarryStableDebtExchange{row: exchange.MarginBorrowRecord{TransferID: 81, Asset: "BTC", Amount: 0.5, Principal: 0.49, Interest: 0.01, Status: "CONFIRMED", Timestamp: time.Now().Add(-time.Second).UnixMilli()}}
	s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, &venue.mockFCExchange, &venue.mockFCExchange, venue, nil)
	if _, err := s.confirmMarginDebtTransaction(context.Background(), "borrow", 81, "BTC", 0.5); err == nil {
		t.Fatal("borrow acknowledgement produced inconsistent principal/paid-interest event")
	}
	venue.row.Principal, venue.row.Interest = 0.5, 0
	if _, err := s.confirmMarginDebtTransaction(context.Background(), "borrow", 81, "BTC", 0.5); err != nil {
		t.Fatal(err)
	}
}

func TestFundingCarryRestoreCannotAssignScopeToUnscopedDebtHistory(t *testing.T) {
	e := fundingCarryMarginDebtEvent{Action: "repay", TransferID: 81, Asset: "BTC", Amount: 0.5, Principal: 0.49, InterestPaid: 0.01, OccurredAt: time.Now().Add(-time.Second)}
	state := fundingCarryRuntimeState{Strategy: "funding_carry", Symbol: "BTCUSDT", OwnershipReady: true, MarginDebtEvents: []fundingCarryMarginDebtEvent{e}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	venue := &mockFCExchange{}
	s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, venue, venue, nil)
	s.SetRuntimeStateStore(&borrowReceiptContextStore{&memoryRuntimeStateStore{version: fundingCarryRuntimeStateVersion, payload: string(payload), found: true}})
	if err := s.SetMarginAccountScope("scope-configured"); err != nil {
		t.Fatal(err)
	}
	if err := s.restoreRuntimeState(); err == nil {
		t.Fatal("configured scope was guessed for unattributed historical debt")
	}
}
