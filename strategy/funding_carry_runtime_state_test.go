package strategy

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
)

func TestDecodeFundingCarryRuntimeStateRequiresExactResolvedOwnership(t *testing.T) {
	base := fundingCarryRuntimeState{
		Strategy: "funding_carry", FuturesExchange: "binance", SpotExchange: "binance",
		Symbol: "BTCUSDT", OwnershipReady: true, Direction: DirectionForward,
		OwnedSpot: 0.25, OwnedFutures: 0.25,
		MarginDebt: 0.25, MarginBorrowTransferID: 12345, MarginBorrowedAt: time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC),
		MarginDebtEvents: []fundingCarryMarginDebtEvent{
			{Action: "borrow", TransferID: 12345, Asset: "BTC", Amount: 0.25, OccurredAt: time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC)},
			{Action: "repay", TransferID: 12346, Asset: "BTC", Amount: 0.1, OccurredAt: time.Date(2026, 10, 2, 4, 4, 5, 0, time.UTC)},
		},
	}
	cases := []struct {
		name    string
		version int
		state   fundingCarryRuntimeState
		wantErr string
	}{
		{name: "valid ownership", version: fundingCarryRuntimeStateVersion, state: base},
		{name: "unsupported schema", version: 99, state: base, wantErr: "unsupported"},
		{name: "wrong symbol", version: fundingCarryRuntimeStateVersion, state: func() fundingCarryRuntimeState { v := base; v.Symbol = "ETHUSDT"; return v }(), wantErr: "identity"},
		{name: "in flight intent", version: fundingCarryRuntimeStateVersion, state: func() fundingCarryRuntimeState { v := base; v.IntentInFlight = true; return v }(), wantErr: "unresolved"},
		{name: "unknown exposure", version: fundingCarryRuntimeStateVersion, state: func() fundingCarryRuntimeState { v := base; v.ExposureUnknown = true; return v }(), wantErr: "unresolved"},
		{name: "flat state with residual inventory", version: fundingCarryRuntimeStateVersion, state: func() fundingCarryRuntimeState { v := base; v.Direction = DirectionNone; return v }(), wantErr: "flat state"},
		{name: "flat state with borrow identity", version: fundingCarryRuntimeStateVersion, state: func() fundingCarryRuntimeState {
			v := base
			v.Direction = DirectionNone
			v.OwnedSpot = 0
			v.OwnedFutures = 0
			v.MarginDebt = 0
			return v
		}(), wantErr: "borrow identity"},
		{name: "invalid margin debt event", version: fundingCarryRuntimeStateVersion, state: func() fundingCarryRuntimeState {
			v := base
			v.MarginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), base.MarginDebtEvents...)
			v.MarginDebtEvents[0].Action = "transfer"
			return v
		}(), wantErr: "invalid margin debt event"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.state)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeFundingCarryRuntimeState(tc.version, string(payload), "binance", "binance", "BTCUSDT")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("decode state: %v", err)
				}
				if tc.state.MarginBorrowTransferID > 0 && (got.MarginBorrowTransferID != tc.state.MarginBorrowTransferID || !got.MarginBorrowedAt.Equal(tc.state.MarginBorrowedAt)) {
					t.Fatalf("borrow identity not preserved: got id=%d at=%s", got.MarginBorrowTransferID, got.MarginBorrowedAt)
				}
				if len(got.MarginDebtEvents) != len(tc.state.MarginDebtEvents) {
					t.Fatalf("margin debt event count=%d, want %d", len(got.MarginDebtEvents), len(tc.state.MarginDebtEvents))
				}
				if len(got.MarginDebtEvents) > 0 && got.MarginDebtEvents[1].TransferID != tc.state.MarginDebtEvents[1].TransferID {
					t.Fatalf("repay transaction identity not preserved: %+v", got.MarginDebtEvents)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("decode error=%v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestRestoreFundingCarryRuntimeStateRejectsChangedMarginAccountScope(t *testing.T) {
	state := fundingCarryRuntimeState{
		Strategy: "funding_carry", Symbol: "BTCUSDT", MarginAccountScope: "scope-original",
		OwnershipReady: true, Direction: DirectionNone,
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: fundingCarryRuntimeStateVersion, payload: string(payload), found: true}
	spot := &mockFCExchange{}
	futures := &mockFCExchange{}
	strategy := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, &mockFCExchange{}, nil)
	strategy.SetRuntimeStateStore(store)
	if err := strategy.SetMarginAccountScope("scope-new"); err != nil {
		t.Fatal("set configured account scope:", err)
	}
	if err := strategy.restoreRuntimeState(); err == nil || !strings.Contains(err.Error(), "scope mismatch") {
		t.Fatalf("restore should reject state from a different margin account: %v", err)
	}
}
