package strategy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeFundingCarryRuntimeStateRequiresExactResolvedOwnership(t *testing.T) {
	base := fundingCarryRuntimeState{
		Strategy: "funding_carry", FuturesExchange: "binance", SpotExchange: "binance",
		Symbol: "BTCUSDT", OwnershipReady: true, Direction: DirectionForward,
		OwnedSpot: 0.25, OwnedFutures: 0.25,
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(tc.state)
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeFundingCarryRuntimeState(tc.version, string(payload), "binance", "binance", "BTCUSDT")
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("decode state: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("decode error=%v, want substring %q", err, tc.wantErr)
			}
		})
	}
}
