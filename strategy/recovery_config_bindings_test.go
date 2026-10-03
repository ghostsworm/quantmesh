package strategy

import (
	"errors"
	"reflect"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"
)

func TestRecoveryBindingResolverMatchesActualComboConstructor(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "owner", "BTCUSDT"
	raw := map[string]interface{}{"symbol": "ETHUSDT", "strategies": []interface{}{map[string]interface{}{"name": "old-custom", "type": "martingale", "direction": "SHORT", "parameters": map[string]interface{}{"symbol": "ETHUSDT"}}}}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"combo": {Enabled: false, Type: "combo", Config: raw}}
	bot := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	combo := NewComboStrategy("combo", bot.Symbol, cfg, nil, nil, raw)
	child := combo.strategies[0].(*MartingaleStrategy)
	key, err := comboChildRuntimeStateKey("combo", child.Name())
	if err != nil {
		t.Fatal(err)
	}
	states := []*storage.StrategyRuntimeState{{BotID: "owner", StrategyName: "combo", SchemaVersion: 1, Payload: recoveryProofPayload(t, comboRuntimeState{BotID: "owner", StrategyName: "combo", Symbol: "BTCUSDT", PeakEquity: 1000})}, {BotID: "owner", StrategyName: key, SchemaVersion: 1, Payload: recoveryProofPayload(t, child.runtimeStateSnapshotLocked())}}
	before := raw["symbol"]
	bindings, err := ResolveRecoveryConfigBindings(cfg, bot, states)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", states, bindings); err != nil {
		t.Fatalf("actual producer/binding mismatch: %v", err)
	}
	if raw["symbol"] != before || bindings[1].SingleLeg.Direction != "SHORT" || bindings[1].SingleLeg.Symbol != "BTCUSDT" {
		t.Fatal("resolver changed configuration or ignored constructor overrides")
	}
	cfg.Strategies.Configs = nil
	if _, err := ResolveRecoveryConfigBindings(cfg, bot, states); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("removed custom definition guessed from payload")
	}
}

func TestRecoveryBindingResolverCanonicalHistoryAndScope(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-identity"}}}
	bot := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	for _, name := range []string{"dca", "dca_enhanced", "trend", "mean_reversion", "momentum", "martingale"} {
		state, binding := recoveryDispatchFixture(t, name)
		if name != "martingale" {
			binding.SingleLeg.Direction = ""
		}
		resolved, err := ResolveRecoveryConfigBindings(cfg, bot, []*storage.StrategyRuntimeState{state})
		if err != nil || len(resolved) != 1 {
			t.Fatalf("removed canonical %s unavailable: %v", name, err)
		}
		if !reflect.DeepEqual(resolved[0].SingleLeg, binding.SingleLeg) {
			t.Fatalf("canonical producer mismatch %s", name)
		}
		if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", []*storage.StrategyRuntimeState{state}, resolved); err != nil {
			t.Fatal(err)
		}
	}
	state, _ := recoveryDispatchFixture(t, "funding_carry")
	bindings, err := ResolveRecoveryConfigBindings(cfg, bot, []*storage.StrategyRuntimeState{state})
	if err != nil {
		t.Fatal(err)
	}
	if bindings[0].Carry.MarginAccountScope != config.AccountScopeID("binance", cfg.Exchanges["binance"]) || bindings[0].Carry.BaseAsset != "BTC" {
		t.Fatal("independent account/asset not bound")
	}
	delete(cfg.Exchanges, "binance")
	if _, err := ResolveRecoveryConfigBindings(cfg, bot, []*storage.StrategyRuntimeState{state}); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("absent account identity guessed")
	}
}

func TestRecoveryBindingResolverUsesProductionNormalization(t *testing.T) {
	cfg := &config.Config{}
	state, _ := recoveryDispatchFixture(t, "trend")
	bot := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", Strategies: []config.StrategyInstance{{Type: " TREND_FOLLOWING "}}}
	bindings, err := ResolveRecoveryConfigBindings(cfg, bot, []*storage.StrategyRuntimeState{state})
	if err != nil || len(bindings) != 1 || bindings[0].StrategyType != "trend" {
		t.Fatalf("production alias normalization mismatch: %v", err)
	}
	bot.Strategies = append(bot.Strategies, config.StrategyInstance{Type: "trend"})
	if _, err := ResolveRecoveryConfigBindings(cfg, bot, []*storage.StrategyRuntimeState{state}); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("duplicate runtime definitions accepted")
	}
	bot.Strategies = []config.StrategyInstance{{Type: "removed-type"}}
	if _, err := ResolveRecoveryConfigBindings(cfg, bot, []*storage.StrategyRuntimeState{state}); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("invalid independent definition accepted")
	}
}

func TestRecoveryBindingResolverMatchesHedgeConstructor(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "owner", "BTCUSDT"
	raw := map[string]interface{}{"group_id": "hedge-group", "symbol": "ETHUSDT"}
	bot := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", Strategies: []config.StrategyInstance{{Type: "spot_long", Config: raw}}}
	producer := NewSpotLongStrategy("spot_long", cfg, nil, nil, raw)
	store := &memoryRuntimeStateStore{}
	producer.SetRuntimeStateStore(store)
	if err := producer.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	state := &storage.StrategyRuntimeState{BotID: bot.ID, StrategyName: "spot_long", SchemaVersion: store.version, Payload: store.payload}
	bindings, err := ResolveRecoveryConfigBindings(cfg, bot, []*storage.StrategyRuntimeState{state})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyBotRecoveryConfigStates(t.Context(), bot.ID, []*storage.StrategyRuntimeState{state}, bindings); err != nil {
		t.Fatalf("hedge constructor binding mismatch: %v", err)
	}
}
