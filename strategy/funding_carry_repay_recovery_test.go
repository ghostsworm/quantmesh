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

type fundingCarryRecoveryExchange struct {
	*fundingCarryRepayIntentExchange
	queries    int
	queriedID  int64
	afterQuery func()
}

func (e *fundingCarryRecoveryExchange) GetMarginTransactionByID(ctx context.Context, asset, kind string, id int64) (exchange.MarginBorrowRecord, error) {
	e.queries++
	e.queriedID = id
	record, err := e.fundingCarryRepayIntentExchange.GetMarginTransactionByID(ctx, asset, kind, id)
	record.Timestamp = 2000 // venue transaction timestamps are immutable
	if e.afterQuery != nil {
		e.afterQuery()
	}
	return record, err
}

func TestFundingCarryStartupReconcilesSavedRepaymentWithoutRPC(t *testing.T) {
	for _, mode := range []string{"confirmed", "already_recorded", "query_failure", "wrong_scope", "invalid_ledger", "no_ack", "wrong_asset", "owner_lost", "cancelled", "save_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			venue := &fundingCarryRecoveryExchange{fundingCarryRepayIntentExchange: margin}
			s.marginEx = venue
			s.strategySpotKnown, s.intentInFlight, s.unownedExposure = true, true, true
			s.marginBorrowedAt = time.UnixMilli(1000).UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
			s.marginRepayIntent = &fundingCarryRepayIntent{Asset: "BTC", AccountScope: "scope-a", Amount: 0.4, ExpectedRemaining: 0, BorrowTransferID: 42, TransferID: 7}
			margin.repayAmount = 0.4
			margin.queryErr = nil
			if mode == "already_recorded" {
				s.marginDebt = 0
				s.marginDebtEvents = append(s.marginDebtEvents, fundingCarryMarginDebtEvent{Action: "repay", TransferID: 7, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: time.UnixMilli(2000).UTC()})
			}
			if mode == "wrong_asset" {
				s.marginRepayIntent.Asset = "ETH"
			}
			if mode == "query_failure" {
				margin.queryErr = context.DeadlineExceeded
			}
			if mode == "invalid_ledger" {
				s.marginDebtEvents = nil
			}
			if mode == "no_ack" {
				s.marginRepayIntent.TransferID = 0
			}
			if err := s.persistRuntimeStateLocked(); err != nil {
				t.Fatal(err)
			}
			before := store.payload
			// Simulate a fresh process: import must come from durable state.
			s.marginDebt, s.marginBorrowTransferID = 0, 0
			s.marginDebtEvents, s.marginRepayIntent = nil, nil
			s.intentInFlight, s.unownedExposure, s.strategySpotKnown = false, false, false
			if mode == "wrong_scope" {
				s.marginAccountScope = "scope-b"
			}
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "owner_lost" {
				venue.afterQuery = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
			}
			if mode == "cancelled" {
				venue.afterQuery = cancel
			}
			if mode == "save_failure" {
				venue.afterQuery = func() { store.err = errors.New("injected recovery save failure") }
			}
			if err := s.Start(ctx); err == nil {
				t.Fatal("interrupted operation falsely started")
			}
			if margin.repayCalls != 0 || s.started {
				t.Fatal("startup repeated repayment or started trading")
			}
			var saved fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
				t.Fatal(err)
			}
			if mode == "confirmed" || mode == "already_recorded" {
				if venue.queries != 1 || venue.queriedID != 7 || saved.MarginDebt != 0 || saved.MarginRepayIntent != nil || len(saved.MarginDebtEvents) != 2 || !saved.ExposureUnknown || !saved.IntentInFlight {
					t.Fatal("accepted repayment not reconciled conservatively")
				}
				if err := s.Start(ctx); err == nil || venue.queries != 1 {
					t.Fatal("second startup falsely resumed or repeated completed query")
				}
			} else if store.payload != before {
				t.Fatal("failed recovery changed durable state")
			}
			if (mode == "wrong_scope" || mode == "invalid_ledger" || mode == "no_ack" || mode == "wrong_asset") && venue.queries != 0 {
				t.Fatal("invalid attribution queried venue")
			}
		})
	}
}
