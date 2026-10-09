package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
)

func TestFundingCarryStartupRecordsSavedBorrowReceiptWithoutReplaying(t *testing.T) {
	s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
	if err := s.SetMarginAccountScope("fixture-account-scope"); err != nil {
		t.Fatal(err)
	}
	venue := &fundingCarryInterruptedBorrowExchange{fundingCarryReturnedPrincipalExchange: parent, queryErr: errors.New("initial borrow query unavailable")}
	s.marginEx = venue
	if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err == nil {
		t.Fatal("initial interruption ignored")
	}
	var pending fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.MarginBorrowTransferID <= 0 || pending.Direction != DirectionNone || !pending.IntentInFlight || pending.MarginDebt != 0 {
		t.Fatal("not an ACK-only fixture")
	}
	restarted, _, _ := newFundingCarryReturnedPrincipalFixture(0)
	restarted.marginEx = venue
	restarted.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	if err := restarted.SetMarginAccountScope("fixture-account-scope"); err != nil {
		t.Fatal(err)
	}
	venue.queryErr = nil
	beforeQueries := venue.borrowQueries
	if err := restarted.Start(context.Background()); err == nil {
		t.Fatal("debt checkpoint released trading")
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Direction != DirectionReverse || saved.MarginDebt != venue.borrowAmount || len(saved.MarginDebtEvents) != 1 || saved.MarginBorrowTransferID != pending.MarginBorrowTransferID || !saved.IntentInFlight || !saved.ExposureUnknown || saved.OwnedSpot != 0 || saved.OwnedFutures != 0 {
		t.Fatal("saved ACK did not become a durable, still-pending actual debt receipt")
	}
	if venue.borrowCalls != 1 || venue.repayCalls != 0 || len(venue.placedOrders) != 0 || venue.borrowQueries != beforeQueries+1 {
		t.Fatal("receipt recovery replayed financial operations or skipped exact query")
	}
	checkpoint := store.payload
	if err := restarted.Start(context.Background()); err == nil {
		t.Fatal("second start released trading")
	}
	if store.payload != checkpoint || venue.borrowQueries != beforeQueries+1 {
		t.Fatal("repeated startup duplicated receipt recovery")
	}
	if err := restarted.StopContext(context.Background()); err == nil {
		t.Fatal("durable unknown debt was absent from local stop state")
	}
	if restarted.marginDebt != saved.MarginDebt || restarted.direction != DirectionReverse || !restarted.unownedExposure || !restarted.intentInFlight || store.payload != checkpoint || venue.repayCalls != 0 {
		t.Fatal("stopped object's debt view disagrees with checkpoint or changed finances")
	}
}

func TestFundingCarryBorrowReceiptQueryFailureKeepsUnstartedStopBlocked(t *testing.T) {
	s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
	if err := s.SetMarginAccountScope("fixture-account-scope"); err != nil {
		t.Fatal(err)
	}
	venue := &fundingCarryInterruptedBorrowExchange{fundingCarryReturnedPrincipalExchange: parent, queryErr: errors.New("initial borrow query unavailable")}
	s.marginEx = venue
	if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err == nil {
		t.Fatal("initial borrow interruption ignored")
	}

	restarted, _, _ := newFundingCarryReturnedPrincipalFixture(0)
	restarted.marginEx = venue
	restarted.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	if err := restarted.SetMarginAccountScope("fixture-account-scope"); err != nil {
		t.Fatal(err)
	}
	venue.queryErr = errors.New("recovery borrow query unavailable")
	if err := restarted.Start(context.Background()); err == nil {
		t.Fatal("startup ignored unavailable exact borrow receipt")
	}
	if required, ok := restarted.GetFundingStatus()["reconciliation_required"].(bool); !ok || !required {
		t.Fatal("failed startup did not expose the unresolved recovery gate")
	}
	if err := restarted.verifyFlat(context.Background(), false, nil); err == nil {
		t.Fatal("failed startup was accepted by the durable flat-position proof")
	}
	if err := restarted.StopContext(context.Background()); err == nil {
		t.Fatal("unstarted StopContext reported success despite unverified durable borrow acknowledgement")
	}
	if parent.borrowCalls != 1 || parent.repayCalls != 0 || len(parent.placedOrders) != 0 {
		t.Fatal("unknown recovery outcome replayed a financial request")
	}
}

type borrowReceiptFaultVenue struct {
	*fundingCarryReturnedPrincipalExchange
	mode       string
	queries    int
	gate       *execution.OpeningGate
	afterQuery func()
}

func (v *borrowReceiptFaultVenue) GetMarginTransactionByID(_ context.Context, asset, kind string, id int64) (exchange.MarginBorrowRecord, error) {
	v.queries++
	if v.afterQuery != nil {
		v.afterQuery()
	}
	row := exchange.MarginBorrowRecord{TransferID: id, Asset: asset, Status: "CONFIRMED", Amount: 0.01, Principal: 0.01, Timestamp: 1000}
	switch v.mode {
	case "wrong_id":
		row.TransferID++
	case "wrong_asset":
		row.Asset = "ETH"
	case "interest":
		row.Interest = 0.001
	case "missing_principal":
		row.Principal = 0
	case "nonfinite":
		row.Amount, row.Principal = math.NaN(), math.NaN()
	case "pending":
		row.Status = "PENDING"
	case "owner_lost":
		v.gate.Block(strategyWalletRuntimeOwnershipBlock)
	}
	return row, nil
}

func TestFundingCarryBorrowReceiptDoesNotOverwriteChangedCheckpoint(t *testing.T) {
	s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
	s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	if err := s.SetMarginAccountScope("fixture-scope"); err != nil {
		t.Fatal(err)
	}
	state := fundingCarryRuntimeState{Strategy: "funding_carry", FuturesExchange: s.fut.GetName(), SpotExchange: s.spot.GetName(), Symbol: s.symbol,
		OwnershipReady: true, IntentInFlight: true, ExposureUnknown: true, MarginAccountScope: "fixture-scope", MarginBorrowTransferID: 42}
	seed, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(seed)); err != nil {
		t.Fatal(err)
	}
	var current string
	venue := &borrowReceiptFaultVenue{fundingCarryReturnedPrincipalExchange: parent}
	venue.afterQuery = func() {
		state.MarginBorrowTransferID = 43
		changed, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		current = string(changed)
		if err := store.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, current); err != nil {
			t.Fatal(err)
		}
	}
	s.marginEx = venue
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("changed checkpoint released trading")
	}
	if current == "" || store.payload != current || s.marginDebt != 0 || len(s.marginDebtEvents) != 0 {
		t.Fatal("old borrow receipt overwrote or imported a newer checkpoint")
	}
	if venue.queries != 1 || parent.borrowCalls != 0 || parent.repayCalls != 0 || len(parent.placedOrders) != 0 {
		t.Fatal("changed checkpoint replayed financial requests")
	}
}

type borrowReceiptSaveFault struct {
	*memoryRuntimeStateStore
}

func (s *memoryRuntimeStateStore) CompareAndSwapRuntimeState(ctx context.Context, name string, version int, payload string, nextVersion int, nextPayload string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !s.found || s.version != version || s.payload != payload {
		return false, nil
	}
	err := s.SaveRuntimeState(name, nextVersion, nextPayload)
	return err == nil, err
}

type borrowReceiptContextStore struct{ *memoryRuntimeStateStore }

func (s *borrowReceiptContextStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, "", false, err
	}
	return s.LoadRuntimeState(name)
}

type borrowReceiptLegacyReadCounter struct {
	store *memoryRuntimeStateStore
	reads int
}

func (s *borrowReceiptLegacyReadCounter) LoadRuntimeState(name string) (int, string, bool, error) {
	s.reads++
	return s.store.LoadRuntimeState(name)
}

func (s *borrowReceiptLegacyReadCounter) SaveRuntimeState(name string, version int, payload string) error {
	return s.store.SaveRuntimeState(name, version, payload)
}

func TestFundingCarryBorrowReceiptRequiresContextReaderBeforeAnyRead(t *testing.T) {
	strategy, _, memoryStore := newFundingCarryReturnedPrincipalFixture(0)
	state := fundingCarryRuntimeState{Strategy: "funding_carry", FuturesExchange: strategy.fut.GetName(), SpotExchange: strategy.spot.GetName(), Symbol: strategy.symbol,
		OwnershipReady: true, IntentInFlight: true, ExposureUnknown: true, MarginAccountScope: "fixture-scope", MarginBorrowTransferID: 42}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload)); err != nil {
		t.Fatal(err)
	}
	legacyStore := &borrowReceiptLegacyReadCounter{store: memoryStore}
	strategy.SetRuntimeStateStore(legacyStore)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := strategy.reconcileSavedMarginBorrowReceipt(ctx); err == nil || !strings.Contains(err.Error(), "requires cancellable checkpoint reads") {
		t.Fatalf("error=%v want context-reader requirement", err)
	}
	if legacyStore.reads != 0 {
		t.Fatalf("legacy non-cancellable reads=%d want=0", legacyStore.reads)
	}
}

func (s *borrowReceiptSaveFault) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	return (&borrowReceiptContextStore{s.memoryRuntimeStateStore}).LoadRuntimeStateContext(ctx, name)
}

func (s *borrowReceiptSaveFault) CompareAndSwapRuntimeState(context.Context, string, int, string, int, string) (bool, error) {
	return false, errors.New("fixture conditional write failure")
}

type borrowReceiptCASConflict struct{ *memoryRuntimeStateStore }

func (s *borrowReceiptCASConflict) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	return (&borrowReceiptContextStore{s.memoryRuntimeStateStore}).LoadRuntimeStateContext(ctx, name)
}

func (s *borrowReceiptCASConflict) CompareAndSwapRuntimeState(ctx context.Context, name string, version int, payload string, nextVersion int, nextPayload string) (bool, error) {
	// A new checkpoint appears after the final read, at the write boundary.
	s.payload = strings.Replace(payload, `"margin_borrow_transfer_id":42`, `"margin_borrow_transfer_id":43`, 1)
	return s.memoryRuntimeStateStore.CompareAndSwapRuntimeState(ctx, name, version, payload, nextVersion, nextPayload)
}

func TestFundingCarryBorrowReceiptConditionalConflictPreservesEvidence(t *testing.T) {
	s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
	if err := s.SetMarginAccountScope("fixture-scope"); err != nil {
		t.Fatal(err)
	}
	state := fundingCarryRuntimeState{Strategy: "funding_carry", FuturesExchange: s.fut.GetName(), SpotExchange: s.spot.GetName(), Symbol: s.symbol,
		OwnershipReady: true, IntentInFlight: true, ExposureUnknown: true, MarginAccountScope: "fixture-scope", MarginBorrowTransferID: 42}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload)); err != nil {
		t.Fatal(err)
	}
	s.SetRuntimeStateStore(&borrowReceiptCASConflict{store})
	s.marginEx = &borrowReceiptFaultVenue{fundingCarryReturnedPrincipalExchange: parent}
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("conditional conflict released trading")
	}
	if store.payload == string(payload) || !strings.Contains(store.payload, `"margin_borrow_transfer_id":43`) || s.marginDebt != 0 || len(s.marginDebtEvents) != 0 {
		t.Fatal("conditional conflict lost new evidence or imported stale receipt")
	}
	if parent.borrowCalls != 0 || parent.repayCalls != 0 || len(parent.placedOrders) != 0 {
		t.Fatal("conflict replayed financial RPC")
	}
}

func (s *borrowReceiptSaveFault) SaveRuntimeState(string, int, string) error {
	return errors.New("receipt checkpoint write failed")
}

func TestFundingCarryBorrowReceiptRecoveryRejectsInvalidEvidenceWithoutMutation(t *testing.T) {
	for _, mode := range []string{"wrong_scope", "wrong_id", "wrong_asset", "interest", "missing_principal", "nonfinite", "pending", "owner_lost", "save_failure", "old_schema"} {
		t.Run(mode, func(t *testing.T) {
			s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
			s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
			if err := s.SetMarginAccountScope("fixture-scope"); err != nil {
				t.Fatal(err)
			}
			var gate execution.OpeningGate
			s.SetOpeningGate(&gate)
			venue := &borrowReceiptFaultVenue{fundingCarryReturnedPrincipalExchange: parent, mode: mode, gate: &gate}
			s.marginEx = venue
			state := fundingCarryRuntimeState{Strategy: "funding_carry", FuturesExchange: s.fut.GetName(), SpotExchange: s.spot.GetName(), Symbol: s.symbol,
				OwnershipReady: true, IntentInFlight: true, ExposureUnknown: true, MarginAccountScope: "fixture-scope", MarginBorrowTransferID: 42}
			if mode == "wrong_scope" {
				state.MarginAccountScope = "other-account"
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			version := fundingCarryRuntimeStateVersion
			if mode == "old_schema" {
				// Schema 7 introduced durable borrow receipts. Schema 8 adds
				// prepared/dispatching intent phases, so schema 7 is still valid
				// for this recovery path; use schema 6 for a genuinely old receipt.
				version = 6
			}
			if err := store.SaveRuntimeState("funding_carry", version, string(payload)); err != nil {
				t.Fatal(err)
			}
			if mode == "save_failure" {
				s.SetRuntimeStateStore(&borrowReceiptSaveFault{memoryRuntimeStateStore: store})
			}
			if err := s.Start(context.Background()); err == nil {
				t.Fatal("invalid receipt released startup")
			}
			if store.payload != string(payload) || parent.borrowCalls != 0 || parent.repayCalls != 0 || len(parent.placedOrders) != 0 {
				t.Fatal("invalid receipt changed durable state or finances")
			}
			wantQueries := 1
			if mode == "wrong_scope" || mode == "old_schema" {
				wantQueries = 0
			}
			if venue.queries != wantQueries {
				t.Fatalf("queries=%d want=%d", venue.queries, wantQueries)
			}
		})
	}
}
