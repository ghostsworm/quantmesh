package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"quantmesh/execution"
)

func TestFundingCarryBorrowCallInterruptionRetainsAcknowledgement(t *testing.T) {
	for _, mode := range []string{"canceled", "owner_lost", "ack_with_error", "canceled_save_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			venue := &fundingCarryInterruptedBorrowExchange{fundingCarryReturnedPrincipalExchange: parent}
			var before string
			venue.afterBorrow = func() {
				before = store.payload
				switch mode {
				case "owner_lost":
					gate.Block(strategyWalletRuntimeOwnershipBlock)
				case "canceled", "canceled_save_failure":
					cancel()
					if mode == "canceled_save_failure" {
						store.err = errors.New("injected ACK persistence failure")
					}
				}
			}
			if mode == "ack_with_error" {
				venue.borrowErr = errors.New("accepted borrow with uncertain transport result")
			}
			s.marginEx = venue
			if err := s.openReverseHedge(ctx, 50000, 50000, -0.01); err == nil {
				t.Fatal("interrupted borrow succeeded")
			}
			if s.marginBorrowTransferID != 1 || !s.unownedExposure || !s.intentInFlight {
				t.Fatal("accepted identity or unresolved intent lost")
			}
			if venue.borrowCalls != 1 || venue.borrowQueries != 0 || venue.repayCalls != 0 || s.marginDebt != 0 || len(s.marginDebtEvents) != 0 || len(parent.placedOrders) != 0 || len(s.fut.(*fundingCarryBudgetExchange).placedOrders) != 0 {
				t.Fatal("uncertain ACK advanced financial state or execution")
			}
			if mode == "owner_lost" || mode == "canceled_save_failure" {
				if store.payload != before {
					t.Fatal("lost owner or failed write changed durable intent")
				}
				return
			}
			var saved fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
				t.Fatal(err)
			}
			if saved.MarginBorrowTransferID != 1 || !saved.ExposureUnknown || !saved.IntentInFlight || saved.MarginDebt != 0 || len(saved.MarginDebtEvents) != 0 {
				t.Fatal("durable recovery identity missing or treated as verified debt")
			}
		})
	}
}

func TestFundingCarryBorrowAcknowledgementRejectsMissingIntentOrIdentity(t *testing.T) {
	for _, active := range []bool{false, true} {
		s, _, store := newFundingCarryReturnedPrincipalFixture(0)
		s.intentInFlight = active
		id := int64(1)
		if active {
			id = 0
		}
		if err := s.recordMarginBorrowAcknowledgement(context.Background(), id); err == nil {
			t.Fatal("invalid acknowledgement accepted")
		}
		if s.marginBorrowTransferID != 0 || store.payload != "" {
			t.Fatal("invalid acknowledgement changed recovery evidence")
		}
	}
}
