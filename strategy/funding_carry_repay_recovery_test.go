package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

type repaymentRecoveryConflictStore struct {
	*memoryRuntimeStateStore
	writes, conflictAt int
	onConflict         func(string)
}

func (s *repaymentRecoveryConflictStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	return (&borrowReceiptContextStore{s.memoryRuntimeStateStore}).LoadRuntimeStateContext(ctx, name)
}

func (s *repaymentRecoveryConflictStore) CompareAndSwapRuntimeState(ctx context.Context, name string, version int, payload string, nextVersion int, nextPayload string) (bool, error) {
	s.writes++
	if s.writes == s.conflictAt {
		changed := strings.Replace(s.payload, `"transfer_id":7`, `"transfer_id":8`, 1)
		if changed == s.payload {
			return false, errors.New("fixture repayment identity unchanged")
		}
		s.payload = changed
		s.onConflict(changed)
	}
	return s.memoryRuntimeStateStore.CompareAndSwapRuntimeState(ctx, name, version, payload, nextVersion, nextPayload)
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
	for _, mode := range []string{"confirmed", "already_recorded", "query_failure", "wrong_scope", "invalid_ledger", "no_ack", "wrong_asset", "owner_lost", "cancelled", "save_failure", "changed_checkpoint", "principal_write_conflict", "intent_cleanup_conflict"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
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
			var conflicting *repaymentRecoveryConflictStore
			if mode == "principal_write_conflict" || mode == "intent_cleanup_conflict" {
				conflictAt := 1
				if mode == "intent_cleanup_conflict" {
					conflictAt = 2
				}
				conflicting = &repaymentRecoveryConflictStore{memoryRuntimeStateStore: store, conflictAt: conflictAt, onConflict: func(payload string) { before = payload }}
				s.SetRuntimeStateStore(conflicting)
			}
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
			if mode == "changed_checkpoint" {
				venue.afterQuery = func() {
					before = strings.Replace(store.payload, `"transfer_id":7`, `"transfer_id":8`, 1)
					if before == store.payload {
						t.Fatal("fixture did not change repayment identity")
					}
					store.payload = before
				}
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
			if conflicting != nil && conflicting.writes != conflicting.conflictAt {
				t.Fatal("recovery did not stop at conditional write conflict")
			}
			if (mode == "wrong_scope" || mode == "invalid_ledger" || mode == "no_ack" || mode == "wrong_asset") && venue.queries != 0 {
				t.Fatal("invalid attribution queried venue")
			}
		})
	}
}
