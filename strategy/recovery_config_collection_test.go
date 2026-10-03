package strategy

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/storage"
)

func TestHedgeRecoveryConfigActualProducer(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "owner", "BTCUSDT"
	store := &memoryRuntimeStateStore{}
	binding := HedgeRecoveryBinding{BotID: "owner", StrategyName: "spot_long", GroupID: "group-a", Symbol: "BTCUSDT", BaseAsset: "BTC"}
	long := NewSpotLongStrategy(binding.StrategyName, cfg, nil, nil, map[string]interface{}{"group_id": binding.GroupID})
	long.SetRuntimeStateStore(store)
	if err := long.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	if err := VerifySpotLongRecoveryConfigState(store.version, store.payload, binding); err != nil {
		t.Fatal(err)
	}
	long.pendingIntents["pending"] = spotLongPendingIntent{Side: "BUY", Quantity: 0.4, CreatedAtUnixMilli: 1}
	if err := long.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	before := store.payload
	if err := VerifySpotLongRecoveryConfigState(store.version, before, binding); !errors.Is(err, ErrRecoveryConfigRequired) {
		t.Fatalf("prepared order accepted: %v", err)
	}
	if store.payload != before {
		t.Fatal("proof wrote journal")
	}
	binding.StrategyName = "spot_short"
	short := NewSpotShortStrategy(binding.StrategyName, cfg, nil, nil, nil, map[string]interface{}{"group_id": binding.GroupID})
	short.SetRuntimeStateStore(store)
	short.consumedRepayTransfers = map[int64]int64{42: 17}
	if err := short.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	if err := VerifySpotShortRecoveryConfigState(store.version, store.payload, binding); err != nil {
		t.Fatalf("confirmed repayment history rejected: %v", err)
	}
	short.pendingBorrow["borrow"] = spotShortPendingBorrow{Amount: 0.4, Phase: "borrowed", BorrowTransferID: 42, CreatedAtUnixMilli: 1}
	if err := short.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	if err := VerifySpotShortRecoveryConfigState(store.version, store.payload, binding); !errors.Is(err, ErrRecoveryConfigRequired) {
		t.Fatalf("borrowed debt accepted: %v", err)
	}
	for _, name := range []string{"futures_long", "futures_short"} {
		binding.StrategyName = name
		tracker := newFuturesHedgeOrderTracker(cfg, name, binding.GroupID, binding.Symbol, "fixture")
		tracker.SetStore(store)
		if err := tracker.persistLocked(); err != nil {
			t.Fatal(err)
		}
		if err := VerifyFuturesHedgeRecoveryConfigState(store.version, store.payload, binding); err != nil {
			t.Fatal(err)
		}
		if _, err := tracker.Begin("BUY", 0.4); err != nil {
			t.Fatal(err)
		}
		if err := VerifyFuturesHedgeRecoveryConfigState(store.version, store.payload, binding); !errors.Is(err, ErrRecoveryConfigRequired) {
			t.Fatalf("prepared futures intent accepted: %v", err)
		}
	}
}

func TestHedgeRecoveryConfigInvalidEvidence(t *testing.T) {
	binding := HedgeRecoveryBinding{BotID: "owner", StrategyName: "hedge", GroupID: "group-a", Symbol: "BTCUSDT", BaseAsset: "BTC"}
	fixtures := []struct {
		version int
		payload string
		verify  func(int, string, HedgeRecoveryBinding) error
	}{
		{2, recoveryProofPayload(t, spotLongRuntimeState{BotID: binding.BotID, Strategy: binding.StrategyName, GroupID: binding.GroupID, Symbol: binding.Symbol, BaseAsset: binding.BaseAsset, PendingOrders: map[int64]spotLongPendingOrder{}}), VerifySpotLongRecoveryConfigState},
		{9, recoveryProofPayload(t, spotShortRuntimeState{BotID: binding.BotID, Strategy: binding.StrategyName, GroupID: binding.GroupID, Symbol: binding.Symbol, BaseAsset: binding.BaseAsset, PendingRepay: map[int64]spotShortPendingRepay{}, ConsumedRepayTransfers: map[int64]int64{42: 17}}), VerifySpotShortRecoveryConfigState},
		{1, recoveryProofPayload(t, futuresHedgeRuntimeState{BotID: binding.BotID, Strategy: binding.StrategyName, GroupID: binding.GroupID, Symbol: binding.Symbol}), VerifyFuturesHedgeRecoveryConfigState},
	}
	for _, fixture := range fixtures {
		for _, payload := range []string{"{}", "null", fixture.payload + "{}", strings.Replace(fixture.payload, `"symbol":"BTCUSDT"`, `"symbol":"BTCUSDT","SYMBOL":"BTCUSDT"`, 1), strings.Replace(fixture.payload, `"group_id":"group-a",`, "", 1)} {
			if err := fixture.verify(fixture.version, payload, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
				t.Fatalf("ambiguous hedge accepted: %v", err)
			}
		}
		wrong := binding
		wrong.GroupID = "other"
		if err := fixture.verify(fixture.version, fixture.payload, wrong); !errors.Is(err, ErrRecoveryConfigUnverified) {
			t.Fatal("wrong group accepted")
		}
		if err := fixture.verify(99, fixture.payload, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
			t.Fatal("unknown schema accepted")
		}
	}
	state := spotShortRuntimeState{BotID: binding.BotID, Strategy: binding.StrategyName, GroupID: binding.GroupID, Symbol: binding.Symbol, BaseAsset: binding.BaseAsset, PendingRepay: map[int64]spotShortPendingRepay{}, ConsumedRepayTransfers: map[int64]int64{0: 17}}
	if err := VerifySpotShortRecoveryConfigState(9, recoveryProofPayload(t, state), binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("invalid repayment identity accepted")
	}
}

func TestRecoveryConfigCollectionReadsEveryOldRecord(t *testing.T) {
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "collection.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	bindings := make([]RecoveryConfigStateBinding, 0, 105)
	for index := 0; index < 105; index++ {
		name := fmt.Sprintf("old-signal-%03d", index)
		state := signalRuntimeState{BotID: "owner", StrategyName: name, Symbol: "BTCUSDT"}
		if index == 104 {
			state.PendingAction = signalActionOpenLong
		}
		if err := store.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: "owner", StrategyName: name, SchemaVersion: 1, Payload: recoveryProofPayload(t, state)}); err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, RecoveryConfigStateBinding{StateKey: name, StrategyType: "trend", SingleLeg: SingleLegRecoveryBinding{BotID: "owner", StrategyName: name, Symbol: "BTCUSDT"}})
	}
	states, err := store.ListBotStrategyRuntimeStatesContext(t.Context(), "owner")
	if err != nil || len(states) != 105 {
		t.Fatalf("incomplete collection read: %d %v", len(states), err)
	}
	before := states[104].Payload
	if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", states, bindings); !errors.Is(err, ErrRecoveryConfigRequired) {
		t.Fatalf("late historical pending state missed: %v", err)
	}
	if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", states, bindings[:104]); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatalf("unknown old state inferred flat: %v", err)
	}
	states[104].Payload = recoveryProofPayload(t, signalRuntimeState{BotID: "owner", StrategyName: states[104].StrategyName, Symbol: "BTCUSDT"})
	if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", states, bindings); err != nil {
		t.Fatalf("all reconciled records rejected: %v", err)
	}
	readback, err := store.ListBotStrategyRuntimeStatesContext(t.Context(), "owner")
	if err != nil || readback[104].Payload != before {
		t.Fatal("collection proof wrote durable evidence")
	}
}

func TestRecoveryConfigComboCollection(t *testing.T) {
	for _, kind := range []string{"dca", "dca_enhanced", "martingale", "trend", "mean_reversion", "momentum"} {
		t.Run(kind, func(t *testing.T) {
			combo, store, scoped := comboCapitalFixture(t, kind)
			name := combo.strategies[0].Name()
			version := 1
			var clean, dirty string
			switch child := combo.strategies[0].(type) {
			case *DCAEnhancedStrategy:
				version = 2
				state := child.runtimeStateSnapshotLocked()
				clean = recoveryProofPayload(t, state)
				state.CloseClientOrderID = "pending"
				dirty = recoveryProofPayload(t, state)
			case *MartingaleStrategy:
				state := child.runtimeStateSnapshotLocked()
				clean = recoveryProofPayload(t, state)
				state.PendingCloseReason = "pending"
				dirty = recoveryProofPayload(t, state)
			default:
				state := signalRuntimeState{BotID: "bot-a", StrategyName: name, Symbol: "BTCUSDT"}
				clean = recoveryProofPayload(t, state)
				state.PendingAction = signalActionOpenLong
				dirty = recoveryProofPayload(t, state)
			}
			if err := scoped.SaveRuntimeState(name, version, clean); err != nil {
				t.Fatal(err)
			}
			key, err := scoped.key(name)
			if err != nil {
				t.Fatal(err)
			}
			parent := &storage.StrategyRuntimeState{BotID: "bot-a", StrategyName: "combo", SchemaVersion: 1, Payload: store.states["combo"].payload}
			child := &storage.StrategyRuntimeState{BotID: "bot-a", StrategyName: key, SchemaVersion: version, Payload: store.states[key].payload}
			bindings := []RecoveryConfigStateBinding{{StateKey: "combo", StrategyType: "combo", SingleLeg: SingleLegRecoveryBinding{BotID: "bot-a", StrategyName: "combo", Symbol: "BTCUSDT"}}, {StateKey: key, StrategyType: kind, ComboParent: "combo", SingleLeg: SingleLegRecoveryBinding{BotID: "bot-a", StrategyName: name, Symbol: "BTCUSDT", Direction: "LONG"}}}
			for _, records := range [][]*storage.StrategyRuntimeState{{parent, child}, {child, parent}} {
				if err := VerifyBotRecoveryConfigStates(t.Context(), "bot-a", records, bindings); err != nil {
					t.Fatalf("canonical Combo rejected: %v", err)
				}
			}
			child.Payload = dirty
			if err := VerifyBotRecoveryConfigStates(t.Context(), "bot-a", []*storage.StrategyRuntimeState{parent, child}, bindings); !errors.Is(err, ErrRecoveryConfigRequired) {
				t.Fatalf("flat parent hid pending child: %v", err)
			}
			child.Payload = clean
			if err := VerifyBotRecoveryConfigStates(t.Context(), "bot-a", []*storage.StrategyRuntimeState{child}, bindings); !errors.Is(err, ErrRecoveryConfigUnverified) {
				t.Fatal("missing parent accepted")
			}
			bindings[1].ComboParent = "other-combo"
			if err := VerifyBotRecoveryConfigStates(t.Context(), "bot-a", []*storage.StrategyRuntimeState{parent, child}, bindings); !errors.Is(err, ErrRecoveryConfigUnverified) {
				t.Fatal("wrong parent binding accepted")
			}
		})
	}
}

func TestRecoveryConfigCollectionRejectsIncompleteIdentityAndCancellation(t *testing.T) {
	state := &storage.StrategyRuntimeState{BotID: "owner", StrategyName: "trend", SchemaVersion: 1, Payload: recoveryProofPayload(t, signalRuntimeState{BotID: "owner", StrategyName: "trend", Symbol: "BTCUSDT"})}
	binding := RecoveryConfigStateBinding{StateKey: "trend", StrategyType: "trend", SingleLeg: SingleLegRecoveryBinding{BotID: "owner", StrategyName: "trend", Symbol: "BTCUSDT"}}
	for _, states := range [][]*storage.StrategyRuntimeState{nil, {nil}, {state, state}, {{BotID: "other", StrategyName: "trend"}}} {
		if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", states, []RecoveryConfigStateBinding{binding}); !errors.Is(err, ErrRecoveryConfigUnverified) {
			t.Fatal("incomplete state evidence accepted")
		}
	}
	if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", []*storage.StrategyRuntimeState{}, nil); err != nil {
		t.Fatal("known absence rejected")
	}
	if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", []*storage.StrategyRuntimeState{state}, []RecoveryConfigStateBinding{binding, binding}); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("duplicate binding accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := VerifyBotRecoveryConfigStates(ctx, "owner", []*storage.StrategyRuntimeState{state}, []RecoveryConfigStateBinding{binding}); !errors.Is(err, context.Canceled) || !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if err := VerifyBotRecoveryConfigStates(nil, "owner", []*storage.StrategyRuntimeState{}, nil); !errors.Is(err, ErrRecoveryConfigUnverified) {
		t.Fatal("nil context accepted")
	}
	for _, kind := range []string{"unknown", "funding_carry"} {
		binding.StrategyType = kind
		if err := VerifyBotRecoveryConfigStates(t.Context(), "owner", []*storage.StrategyRuntimeState{state}, []RecoveryConfigStateBinding{binding}); !errors.Is(err, ErrRecoveryConfigUnverified) {
			t.Fatal("wrong type inferred from JSON")
		}
	}
}
