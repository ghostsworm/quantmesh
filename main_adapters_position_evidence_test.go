package main

import (
	"context"
	"testing"

	"quantmesh/exchange"
	"quantmesh/position"
)

func TestPositionAdapterPreservesSnapshotEvidence(t *testing.T) {
	cases := []struct {
		name      string
		positions []*exchange.Position
		wantError bool
	}{
		{name: "unavailable nil snapshot", wantError: true},
		{name: "nil position entry", positions: []*exchange.Position{nil}, wantError: true},
		{name: "explicit empty snapshot", positions: []*exchange.Position{}},
		{name: "confirmed short", positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.5}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("invalid snapshot caused a panic: %v", recovered)
				}
			}()
			adapter := &positionExchangeAdapter{exchange: &adapterFakeExchange{positions: tc.positions}}
			raw, err := adapter.GetPositions(context.Background(), "BTCUSDT")
			if tc.wantError {
				if err == nil || raw != nil {
					t.Fatalf("unverified snapshot became usable data: raw=%v err=%v", raw, err)
				}
				return
			}
			infos, ok := raw.([]*position.PositionInfo)
			if err != nil || !ok || infos == nil || len(infos) != len(tc.positions) {
				t.Fatalf("verified snapshot lost: raw=%v err=%v", raw, err)
			}
			for i, info := range infos {
				if info.Symbol != tc.positions[i].Symbol || info.Size != tc.positions[i].Size {
					t.Fatalf("position identity/size changed: %+v", info)
				}
			}
		})
	}
}
