package strategy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"quantmesh/position"
)

func TestSpotShortRepaymentPhaseRestoreRejectsInvalidPreparedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		version    int
		uncertain  bool
		transferID int64
	}{
		{"legacy_prepared", 6, true, 0},
		{"already_submitted", 7, true, 81},
		{"missing_intent", 7, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store := spotShortRestoreFixture(t, &signalTestExchange{}, &mockMarginExchange{})
			var state spotShortRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
				t.Fatal(err)
			}
			pending := state.PendingRepay[71]
			pending.RepayPrepared = true
			pending.RepayUncertain = tc.uncertain
			pending.RepayTransferID = tc.transferID
			state.PendingRepay[71] = pending
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store.payload, store.version = string(payload), tc.version
			s.mu.Lock()
			err = s.restoreRuntimeStateLocked()
			s.mu.Unlock()
			if err == nil {
				t.Fatal("invalid prepared evidence accepted")
			}
		})
	}
}

func TestSpotShortLegacyUnknownRepaymentDoesNotBecomePrepared(t *testing.T) {
	margin := &mockMarginExchange{}
	s, store := spotShortRestoreFixture(t, &signalTestExchange{}, margin)
	var state spotShortRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
		t.Fatal(err)
	}
	pending := state.PendingRepay[71]
	pending.RepayUncertain = true
	pending.RepayAmount = 0.499
	pending.RepayStartedAtUnixMilli = time.Now().UnixMilli()
	pending.RepayExpectedExecutedQty = 0.5
	pending.RepayExpectedBaseFeeQty = 0.001
	state.PendingRepay[71] = pending
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store.payload, store.version = string(payload), 6
	s.mu.Lock()
	err = s.restoreRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.version != spotShortRuntimeStateSchemaVersion || s.pendingRepay[71] != pending {
		t.Fatal("migration changed unknown repayment evidence")
	}
	err = s.onOrderUpdateWithWalletLock(context.Background(), &position.OrderUpdate{OrderID: 71, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5})
	if err == nil || len(margin.repaid) != 0 || s.pendingRepay[71] != pending {
		t.Fatal("legacy unknown repayment was resubmitted")
	}
}
