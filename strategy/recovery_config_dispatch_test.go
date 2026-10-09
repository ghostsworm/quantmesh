package strategy

import (
	"errors"
	"testing"

	"quantmesh/storage"
)

func TestRecoveryConfigCollectionDispatchesEverySupportedType(t *testing.T) {
	for _, kind := range []string{"dca", "dca_enhanced", "martingale", "trend", "mean_reversion", "momentum", "spot_long", "spot_short", "futures_long", "futures_short", "funding_carry", "funding_perp_spread", "combo"} {
		t.Run(kind, func(t *testing.T) {
			state, binding := recoveryDispatchFixture(t, kind)
			if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", []*storage.StrategyRuntimeState{state}, []RecoveryConfigStateBinding{binding}); err != nil {
				t.Fatalf("dispatch %s rejected canonical state: %v", kind, err)
			}
			state.SchemaVersion = 99
			if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", []*storage.StrategyRuntimeState{state}, []RecoveryConfigStateBinding{binding}); !errors.Is(err, ErrRecoveryConfigUnverified) {
				t.Fatal("schema validation skipped by dispatcher")
			}
		})
	}
}

func recoveryDispatchFixture(t *testing.T, kind string) (*storage.StrategyRuntimeState, RecoveryConfigStateBinding) {
	t.Helper()
	binding := RecoveryConfigStateBinding{StateKey: kind, StrategyType: kind,
		SingleLeg: SingleLegRecoveryBinding{BotID: "owner", StrategyName: kind, Symbol: "BTCUSDT", Direction: "LONG"},
		Hedge:     HedgeRecoveryBinding{BotID: "owner", StrategyName: kind, Symbol: "BTCUSDT", BaseAsset: "BTC"}}
	version := 1
	var evidence interface{}
	switch kind {
	case "dca", "dca_enhanced":
		version = 2
		evidence = dcaRuntimeState{BotID: "owner", StrategyName: kind, Symbol: "BTCUSDT", CloseLayerIndex: -1}
	case "martingale":
		evidence = martingaleRuntimeState{BotID: "owner", StrategyName: kind, Symbol: "BTCUSDT", Direction: "LONG"}
	case "trend", "mean_reversion", "momentum":
		evidence = signalRuntimeState{BotID: "owner", StrategyName: kind, Symbol: "BTCUSDT"}
	case "spot_long":
		version = 2
		evidence = spotLongRuntimeState{BotID: "owner", Strategy: kind, Symbol: "BTCUSDT", BaseAsset: "BTC", PendingOrders: map[int64]spotLongPendingOrder{}}
	case "spot_short":
		version = spotShortRuntimeStateSchemaVersion
		evidence = spotShortRuntimeState{BotID: "owner", Strategy: kind, Symbol: "BTCUSDT", BaseAsset: "BTC", PendingRepay: map[int64]spotShortPendingRepay{}, ConsumedRepayTransfers: map[int64]int64{}}
	case "futures_long", "futures_short":
		evidence = futuresHedgeRuntimeState{BotID: "owner", Strategy: kind, Symbol: "BTCUSDT"}
	case "funding_carry":
		version = 6
		s, _ := remainingCoverFixture(0.4)
		s.fut.(*mockFCExchange).name = "futures-venue"
		s.spot.(*mockFCExchange).name = "spot-venue"
		evidence = s.runtimeStateSnapshotLocked()
		binding.Carry = FundingCarryRecoveryBinding{FuturesExchange: "futures-venue", SpotExchange: "spot-venue", Symbol: "BTCUSDT", BaseAsset: "BTC", MarginAccountScope: "scope-a"}
	case "funding_perp_spread":
		version = 6
		evidence = fundingPerpSpreadRuntimeState{Strategy: kind, LegAExchange: "venue-a", LegASymbol: "BTCUSDT", LegBExchange: "venue-b", LegBSymbol: "BTCUSDT", OwnershipReady: true}
		binding.Spread = FundingPerpSpreadRecoveryBinding{"venue-a", "BTCUSDT", "venue-b", "BTCUSDT"}
	case "combo":
		evidence = comboRuntimeState{BotID: "owner", StrategyName: kind, Symbol: "BTCUSDT", PeakEquity: 1000}
	default:
		t.Fatal("unsupported fixture type")
	}
	return &storage.StrategyRuntimeState{BotID: "owner", StrategyName: kind, SchemaVersion: version, Payload: recoveryProofPayload(t, evidence)}, binding
}
