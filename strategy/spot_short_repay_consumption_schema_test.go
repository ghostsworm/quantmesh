package strategy

import (
	"encoding/json"
	"testing"
)

func TestSpotShortRepaymentConsumptionSchemaEvidence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		version     int
		claims      map[int64]int64
		unsubmitted bool
		wantErr     bool
	}{
		{name: "valid_claim", version: 9, claims: map[int64]int64{81: 71}},
		{name: "invalid_transfer", version: 9, claims: map[int64]int64{0: 71}, wantErr: true},
		{name: "invalid_owner", version: 9, claims: map[int64]int64{81: 0}, wantErr: true},
		{name: "legacy_forged_claim", version: 8, claims: map[int64]int64{81: 71}, wantErr: true},
		{name: "legacy_unsubmitted_borrow", version: 8, unsubmitted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{})
			state := spotShortRuntimeState{Strategy: s.name, Symbol: s.symbol, BaseAsset: s.baseAsset, ConsumedRepayTransfers: tc.claims}
			if tc.unsubmitted {
				state.PendingBorrow = map[string]spotShortPendingBorrow{"cid": {Amount: 0.5, Phase: "unsubmitted", CreatedAtUnixMilli: 1}}
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: tc.version, payload: string(payload), found: true}
			s.SetRuntimeStateStore(store)
			s.mu.Lock()
			err = s.restoreRuntimeStateLocked()
			s.mu.Unlock()
			if (err != nil) != tc.wantErr {
				t.Fatalf("restore err=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				if len(s.consumedRepayTransfers) != 0 || store.payload != string(payload) {
					t.Fatal("invalid evidence mutated runtime or durable state")
				}
				return
			}
			if store.version != spotShortRuntimeStateSchemaVersion || len(s.consumedRepayTransfers) != len(tc.claims) {
				t.Fatal("migration lost consumption evidence")
			}
			if tc.unsubmitted && s.pendingBorrow["cid"].Phase != "unsubmitted" {
				t.Fatal("schema8 known-unsubmitted borrow was not preserved")
			}
		})
	}
}
