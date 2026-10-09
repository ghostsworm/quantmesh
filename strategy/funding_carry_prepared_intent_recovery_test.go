package strategy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/storage"
)

func TestRecoverPreparedRuntimeIntentClearsOnlyExplicitPreparedPhase(t *testing.T) {
	cases := []struct {
		name        string
		version     int
		phase       string
		unknown     bool
		ownedSpot   float64
		coverIntent bool
		wantCAS     bool
		wantErr     bool
		wantClean   bool
	}{
		{name: "new schema prepared", version: fundingCarryRuntimeStateVersion, phase: fundingCarryIntentPhasePrepared, wantCAS: true, wantClean: true},
		{name: "prepared standalone spot close preserves owned inventory", version: fundingCarryRuntimeStateVersion, phase: fundingCarryIntentPhasePrepared, ownedSpot: 0.01, wantCAS: true, wantClean: true},
		{name: "prepared cover CID is cleared before any order RPC", version: fundingCarryRuntimeStateVersion, phase: fundingCarryIntentPhasePrepared, coverIntent: true, wantCAS: true, wantClean: true},
		{name: "dispatching remains unresolved", version: fundingCarryRuntimeStateVersion, phase: fundingCarryIntentPhaseDispatching},
		{name: "prepared with unknown exposure remains unresolved", version: fundingCarryRuntimeStateVersion, phase: fundingCarryIntentPhasePrepared, unknown: true, wantErr: true},
		{name: "legacy in-flight remains unresolved", version: fundingCarryRuntimeStateVersion - 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			strategy, _, _ := newFundingCarryBudgetStrategy(false, 0, 500, 500)
			strategy.symbol = "BTCUSDT"
			strategy.strategySpotKnown = true
			strategy.spot.(*fundingCarryBudgetExchange).mockFCExchange.baseAsset = "BTC"
			if tc.coverIntent {
				strategy.marginAccountScope = "fixture-scope"
			}
			store := newCommitConfirmedCanceledRuntimeStore(t, "")
			strategy.SetRuntimeStateStore(store)

			state := strategy.runtimeStateSnapshotLocked()
			state.IntentInFlight = true
			state.IntentPhase = tc.phase
			state.ExposureUnknown = tc.unknown
			state.OwnedSpot = tc.ownedSpot
			state.StandaloneSpotOwned = tc.ownedSpot > 0
			if tc.coverIntent {
				state.Direction = DirectionReverse
				state.MarginAccountScope = "fixture-scope"
				state.MarginCoverIntent = &fundingCarryCoverIntent{
					ClientOrderID: "prepared-cover", Symbol: state.Symbol, Asset: "BTC", AccountScope: "fixture-scope",
					Quantity: 0.01, Price: 100, DebtToCover: 0.009, PreparedAt: time.Now().UTC(),
				}
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal("encode intent:", err)
			}
			if err := store.db.SetFundingCarryRuntimeState(t.Context(), store.generation, &storage.StrategyRuntimeState{
				BotID: store.botID, StrategyName: "funding_carry", SchemaVersion: tc.version, Payload: string(payload),
			}); err != nil {
				t.Fatal("persist intent fixture:", err)
			}

			err = strategy.recoverPreparedRuntimeIntentContext(t.Context())
			if (err != nil) != tc.wantErr {
				t.Fatalf("prepared intent recovery error=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(err.Error(), "unresolved financial evidence") {
				t.Fatalf("unexpected rejection reason: %v", err)
			}
			if (store.casCalls > 0) != tc.wantCAS {
				t.Fatalf("CAS attempted=%v, want %v", store.casCalls > 0, tc.wantCAS)
			}
			persisted, err := store.db.GetStrategyRuntimeStateContext(context.Background(), store.botID, "funding_carry")
			if err != nil || persisted == nil {
				t.Fatalf("read durable result: state=%+v err=%v", persisted, err)
			}
			if tc.wantClean {
				var clean fundingCarryRuntimeState
				if err := json.Unmarshal([]byte(persisted.Payload), &clean); err != nil {
					t.Fatal("decode cleared source state:", err)
				}
				if persisted.SchemaVersion != fundingCarryRuntimeStateVersion || clean.IntentInFlight || clean.IntentPhase != "" || clean.ExposureUnknown ||
					clean.OwnedSpot != tc.ownedSpot || clean.MarginCoverIntent != nil || clean.StandaloneSpotOwned != (tc.ownedSpot > 0) {
					t.Fatalf("prepared phase did not CAS back to clean source: version=%d state=%+v", persisted.SchemaVersion, clean)
				}
			} else if persisted.SchemaVersion != tc.version || persisted.Payload != string(payload) {
				t.Fatalf("unresolved intent was modified: version=%d payload=%q", persisted.SchemaVersion, persisted.Payload)
			}
		})
	}
}

func TestFundingCarryStartClearsPreparedSpotStopIntentButKeepsExposureBlocked(t *testing.T) {
	spot := &mockFCExchange{name: "binance", marketType: "spot", baseAsset: "BTC", balance: 0.01, quantityDecimals: 8}
	futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", quantityDecimals: 8}
	strategy := NewFundingCarryStrategy("prepared-spot-stop", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	store := newCommitConfirmedCanceledRuntimeStore(t, "")
	strategy.SetRuntimeStateStore(store)
	state := fundingCarryRuntimeState{
		Strategy: "funding_carry", FuturesExchange: futures.GetName(), SpotExchange: spot.GetName(), Symbol: "BTCUSDT",
		OwnershipReady: true, IntentInFlight: true, IntentPhase: fundingCarryIntentPhasePrepared,
		Direction: DirectionNone, OwnedSpot: 0.01, StandaloneSpotOwned: true,
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.SetFundingCarryRuntimeState(t.Context(), store.generation, &storage.StrategyRuntimeState{
		BotID: store.botID, StrategyName: "funding_carry", SchemaVersion: fundingCarryRuntimeStateVersion, Payload: string(payload),
	}); err != nil {
		t.Fatal("persist prepared stop snapshot:", err)
	}
	// Allow the later live-position mismatch hold to persist through the same
	// fenced adapter after startup has conditionally cleared only the intent.
	store.saveCalls = 1

	if err := strategy.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "exchange exposure exists without an active") {
		t.Fatalf("unhedged owned spot was not kept behind the exposure gate: %v", err)
	}
	version, savedPayload, found, err := store.LoadRuntimeStateContext(t.Context(), "funding_carry")
	if err != nil || !found {
		t.Fatalf("read recovered stop snapshot: found=%v err=%v", found, err)
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(savedPayload), &saved); err != nil {
		t.Fatal(err)
	}
	if version != fundingCarryRuntimeStateVersion || saved.IntentInFlight || saved.IntentPhase != "" || !saved.ExposureUnknown ||
		saved.OwnedSpot != 0.01 || !saved.StandaloneSpotOwned || len(spot.placedOrders) != 0 || strategy.IsRunning() {
		t.Fatalf("startup lost the owned spot or admitted trading: version=%d state=%+v orders=%d running=%v", version, saved, len(spot.placedOrders), strategy.IsRunning())
	}
}
