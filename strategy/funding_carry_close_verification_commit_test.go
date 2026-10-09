package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type closeVerificationWriteFault struct {
	*memoryRuntimeStateStore
	mode  string
	fired bool
}

func (s *closeVerificationWriteFault) SaveRuntimeState(name string, version int, payload string) error {
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return err
	}
	markerWrite := state.MarginCloseVerificationPending && !state.ExposureUnknown
	cleanWrite := state.Direction == DirectionNone && !state.IntentInFlight && !state.ExposureUnknown && !state.MarginCloseVerificationPending
	if !s.fired && ((s.mode == "marker_save" && markerWrite) || (s.mode != "marker_save" && cleanWrite)) {
		s.fired = true
		if s.mode == "final_ack_error" {
			if err := s.memoryRuntimeStateStore.SaveRuntimeState(name, version, payload); err != nil {
				return err
			}
		}
		return errors.New("injected close verification checkpoint fault")
	}
	return s.memoryRuntimeStateStore.SaveRuntimeState(name, version, payload)
}

func TestFundingCarryCloseVerificationCommitFaultRetainsRecoveryIdentity(t *testing.T) {
	for _, mode := range []string{"marker_save", "final_save", "final_ack_error"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, memory := newFundingCarryRepayIntentFixture()
			s.strategySpotKnown = true
			s.marginBorrowedAt = time.UnixMilli(1000).UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, OccurredAt: s.marginBorrowedAt, AccountScope: "scope-a"}}
			margin.queryErr = nil
			fault := &closeVerificationWriteFault{memoryRuntimeStateStore: memory, mode: mode}
			s.SetRuntimeStateStore(fault)
			if err := s.closeReverse(context.Background(), mode); err == nil || !fault.fired || margin.repayCalls != 1 {
				t.Fatal("checkpoint fault was ignored or finances were replayed")
			}
			var saved fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(memory.payload), &saved); err != nil {
				t.Fatal(err)
			}
			if !saved.MarginCloseVerificationPending || !saved.IntentInFlight || !saved.ExposureUnknown || saved.Direction != DirectionReverse || saved.MarginBorrowTransferID != 42 || saved.MarginDebt != 0 || len(saved.MarginDebtEvents) != 2 {
				t.Fatal("failed final write discarded completed financial evidence or recovery identity")
			}
			if !s.marginCloseVerificationPending || s.direction != DirectionReverse || s.marginBorrowTransferID != 42 || !s.unownedExposure {
				t.Fatal("failed final write falsely declared local close complete")
			}
		})
	}
}
