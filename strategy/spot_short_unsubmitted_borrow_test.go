package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"quantmesh/execution"
)

func TestSpotShortRestartClearsDefinitelyUnsubmittedBorrow(t *testing.T) {
	gate := &execution.OpeningGate{}
	margin := &mockMarginExchange{}
	store := &memoryRuntimeStateStore{}
	s := newSpotShortForTest(&spotShortOwnershipOrderExecutor{}, &signalTestExchange{}, margin)
	s.SetOpeningGate(gate)
	s.SetRuntimeStateStore(&spotShortBorrowOwnershipStore{memoryRuntimeStateStore: store, gate: gate})
	if err := s.increaseShort(context.Background(), 0.5); err == nil || len(margin.borrowed) != 0 {
		t.Fatal("fixture must stop before borrowing after intent persistence")
	}
	restarted := newSpotShortForTest(&spotShortOwnershipOrderExecutor{}, &signalTestExchange{}, margin)
	restarted.SetOpeningGate(&execution.OpeningGate{})
	restarted.SetRuntimeStateStore(store)
	restarted.mu.Lock()
	err := restarted.restoreRuntimeStateLocked()
	restarted.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatalf("definitely unsubmitted borrow blocked recovery: %v", err)
	}
	var state spotShortRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.PendingBorrow) != 0 || len(restarted.pendingBorrow) != 0 || len(margin.borrowed) != 0 || len(margin.repaid) != 0 {
		t.Fatal("unsubmitted recovery mutated wallet or retained intent")
	}
}

func TestSpotShortUnsubmittedBorrowRecoveryPreservesIntentOnWriteFailure(t *testing.T) {
	margin := &mockMarginExchange{}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	intent := spotShortPendingBorrow{Amount: 0.5, Phase: "unsubmitted", CreatedAtUnixMilli: 1}
	s.pendingBorrow["cid"] = intent
	s.mu.Lock()
	err := s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	original := store.payload
	store.err = errors.New("injected recovery write failure")
	if err := s.reconcileRuntimeState(context.Background()); err == nil {
		t.Fatal("recovery swallowed durable failure")
	}
	if s.pendingBorrow["cid"] != intent || store.payload != original || len(margin.borrowed) != 0 {
		t.Fatal("failed recovery lost intent")
	}
	store.err = nil
	if err := s.reconcileRuntimeState(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSpotShortRestoreRejectsUnsubmittedBorrowWithoutCurrentSchemaEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    int
		transferID int64
	}{
		{"legacy_schema", 7, 0}, {"confirmed_transfer", 8, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{})
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			s.pendingBorrow["cid"] = spotShortPendingBorrow{Amount: 0.5, Phase: "unsubmitted", CreatedAtUnixMilli: 1, BorrowTransferID: tc.transferID}
			s.mu.Lock()
			err := s.persistRuntimeStateLocked()
			s.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			store.version = tc.version
			restarted := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{})
			restarted.SetRuntimeStateStore(store)
			restarted.mu.Lock()
			err = restarted.restoreRuntimeStateLocked()
			restarted.mu.Unlock()
			if err == nil {
				t.Fatal("invalid unsubmitted evidence accepted")
			}
		})
	}
}
