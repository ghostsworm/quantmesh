package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"quantmesh/execution"
)

type borrowSubmissionFaultStore struct {
	*memoryRuntimeStateStore
	mode string
	gate *execution.OpeningGate
}

func (s *borrowSubmissionFaultStore) SaveRuntimeState(name string, version int, payload string) error {
	var state spotShortRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return err
	}
	prepared := false
	for _, intent := range state.PendingBorrow {
		prepared = prepared || intent.Phase == "prepared"
	}
	if prepared && s.mode == "before_commit" {
		return errors.New("submission stage not saved")
	}
	if err := s.memoryRuntimeStateStore.SaveRuntimeState(name, version, payload); err != nil {
		return err
	}
	if prepared {
		switch s.mode {
		case "after_commit":
			return errors.New("submission stage acknowledgment lost")
		case "ownership_loss":
			s.gate.Block("runtime_ownership_unverified")
		}
	}
	return nil
}

func TestSpotShortBorrowSubmissionFaultsNeverResubmitUnknownDebt(t *testing.T) {
	for _, mode := range []string{"before_commit", "after_commit", "ownership_loss"} {
		t.Run(mode, func(t *testing.T) {
			gate := &execution.OpeningGate{}
			margin := &mockMarginExchange{}
			executor := &spotShortOwnershipOrderExecutor{}
			store := &memoryRuntimeStateStore{}
			s := newSpotShortForTest(executor, &signalTestExchange{}, margin)
			s.SetOpeningGate(gate)
			s.SetRuntimeStateStore(&borrowSubmissionFaultStore{memoryRuntimeStateStore: store, mode: mode, gate: gate})
			coordinator := &walletCoordinationTestLock{}
			if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
				t.Fatal(err)
			}
			if err := s.increaseShort(context.Background(), 0.5); err == nil {
				t.Fatal("submission stage fault was ignored")
			}
			if err := s.increaseShort(context.Background(), 0.5); err == nil {
				t.Fatal("same instance silently retried unknown borrow")
			}
			if len(margin.borrowed) != 0 || len(executor.orders) != 0 || coordinator.active != 0 {
				t.Fatal("fault permitted venue mutation or leaked wallet lease")
			}
			var saved spotShortRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
				t.Fatal(err)
			}
			if len(saved.PendingBorrow) != 1 {
				t.Fatal("fault lost durable borrow intent")
			}
			wantPhase := "prepared"
			if mode == "before_commit" {
				wantPhase = "unsubmitted"
			}
			for _, pending := range saved.PendingBorrow {
				if pending.Phase != wantPhase || pending.BorrowTransferID != 0 {
					t.Fatalf("incorrect durable phase: %+v", pending)
				}
			}
			restarted := newSpotShortForTest(executor, &signalTestExchange{}, margin)
			restarted.SetRuntimeStateStore(store)
			restarted.SetOpeningGate(&execution.OpeningGate{})
			if err := restarted.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
				t.Fatal(err)
			}
			restarted.mu.Lock()
			err := restarted.restoreRuntimeStateLocked()
			restarted.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			err = restarted.reconcileRuntimeState(context.Background())
			if mode == "before_commit" {
				if err != nil || len(restarted.pendingBorrow) != 0 {
					t.Fatalf("definitely unsubmitted recovery blocked: %v", err)
				}
			} else {
				if err == nil || len(restarted.pendingBorrow) != 1 {
					t.Fatal("potentially submitted debt was cleared")
				}
				if err := restarted.increaseShort(context.Background(), 0.5); err == nil {
					t.Fatal("restarted instance reborrowed unknown intent")
				}
			}
			if len(margin.borrowed) != 0 || len(margin.repaid) != 0 || len(executor.orders) != 0 || coordinator.active != 0 {
				t.Fatal("restart mutated wallet or leaked lease")
			}
		})
	}
}
