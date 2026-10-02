package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"quantmesh/config"
)

type comboCapitalReadStore struct {
	states    map[string]*memoryRuntimeStateStore
	lastKey   string
	received  context.Context
	err       error
	waitChild bool
}

func (s *comboCapitalReadStore) LoadRuntimeState(name string) (int, string, bool, error) {
	if state := s.states[name]; state != nil {
		return state.LoadRuntimeState(name)
	}
	return 0, "", false, nil
}
func (s *comboCapitalReadStore) SaveRuntimeState(name string, version int, payload string) error {
	s.states[name] = &memoryRuntimeStateStore{version: version, payload: payload, found: true}
	return nil
}
func (s *comboCapitalReadStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	s.lastKey, s.received = name, ctx
	if err := ctx.Err(); err != nil {
		return 0, "", false, err
	}
	if s.err != nil {
		return 0, "", false, s.err
	}
	if s.waitChild && name != "combo" {
		<-ctx.Done()
		return 0, "", false, ctx.Err()
	}
	return s.LoadRuntimeState(name)
}

func comboCapitalFixture(t *testing.T, kind string) (*ComboStrategy, *comboCapitalReadStore, comboChildRuntimeStateStore) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	combo := NewComboStrategy("combo", "BTCUSDT", cfg, nil, nil, map[string]interface{}{
		"strategies": []interface{}{map[string]interface{}{"name": "child", "type": kind, "weight": 1.0}},
	})
	store := &comboCapitalReadStore{states: map[string]*memoryRuntimeStateStore{}}
	if err := combo.SetRuntimeStateStore(store); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeState("combo", 1, `{"bot_id":"bot-a","strategy_name":"combo","symbol":"BTCUSDT","peak_equity":1000}`); err != nil {
		t.Fatal(err)
	}
	return combo, store, comboChildRuntimeStateStore{store: store, comboName: "combo", childName: "child"}
}

func TestComboCapitalReleaseChecksEverySupportedChild(t *testing.T) {
	for _, kind := range []string{"dca", "dca_enhanced", "martingale", "trend", "mean_reversion", "momentum"} {
		t.Run(kind, func(t *testing.T) {
			combo, store, scoped := comboCapitalFixture(t, kind)
			var clean, dirty []byte
			var err error
			version := 1
			switch child := combo.strategies[0].(type) {
			case *DCAEnhancedStrategy:
				version = dcaRuntimeStateSchemaVersion
				state := child.runtimeStateSnapshotLocked()
				clean, err = json.Marshal(state)
				state.Layers = []*DCALayer{{ClientOrderID: "pending", Status: "UNKNOWN", RequestedQuantity: 1}}
				dirty, err = json.Marshal(state)
			case *MartingaleStrategy:
				state := child.runtimeStateSnapshotLocked()
				clean, err = json.Marshal(state)
				state.Entries = []*MartingaleEntry{{ClientOrderID: "pending", Status: "UNKNOWN", RequestedQuantity: 1}}
				dirty, err = json.Marshal(state)
			default:
				state := signalRuntimeState{BotID: "bot-a", StrategyName: "child", Symbol: "BTCUSDT"}
				clean, err = json.Marshal(state)
				state.PendingAction = signalActionOpenLong
				state.ActiveOrder = &Order{ClientOrderID: "pending"}
				dirty, err = json.Marshal(state)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := scoped.SaveRuntimeState("child", version, string(clean)); err != nil {
				t.Fatal(err)
			}
			if err := combo.VerifyCapitalReleaseState(t.Context()); err != nil {
				t.Fatalf("clean child blocked release: %v", err)
			}
			if err := scoped.SaveRuntimeState("child", version, string(dirty)); err != nil {
				t.Fatal(err)
			}
			if err := combo.VerifyCapitalReleaseState(t.Context()); err == nil {
				t.Fatal("unsettled child allowed release")
			}
			key, _ := scoped.key("child")
			if store.states[key].payload != string(dirty) {
				t.Fatal("proof changed child recovery state")
			}
			if err := scoped.SaveRuntimeState("child", version, string(clean)); err != nil {
				t.Fatal(err)
			}
			if err := combo.VerifyCapitalReleaseState(t.Context()); err != nil {
				t.Fatalf("reconciled child still blocked: %v", err)
			}
		})
	}
}

func TestComboChildContextReaderPreservesNamespaceAndCancellation(t *testing.T) {
	_, store, scoped := comboCapitalFixture(t, "trend")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if _, _, _, err := scoped.LoadRuntimeStateContext(ctx, "child"); err != nil {
		t.Fatal(err)
	}
	expected, _ := scoped.key("child")
	if store.lastKey != expected || store.received != ctx || expected == "child" {
		t.Fatal("namespace/context not forwarded")
	}
	other := scoped
	other.comboName = "other-combo"
	otherKey, _ := other.key("child")
	if otherKey == expected {
		t.Fatal("Combo names share child storage")
	}
	if _, _, _, err := scoped.LoadRuntimeStateContext(ctx, "another-child"); err == nil {
		t.Fatal("wrong child accepted")
	}
	store.err = context.DeadlineExceeded
	if _, _, _, err := scoped.LoadRuntimeStateContext(ctx, "child"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read deadline ignored: %v", err)
	}
	store.err = nil
	cancel()
	if _, _, _, err := scoped.LoadRuntimeStateContext(ctx, "child"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
	if _, _, _, err := scoped.LoadRuntimeStateContext(nil, "child"); err == nil {
		t.Fatal("nil context accepted")
	}
	legacy := comboChildRuntimeStateStore{store: &memoryRuntimeStateStore{}, comboName: "combo", childName: "child"}
	if _, _, _, err := legacy.LoadRuntimeStateContext(t.Context(), "child"); err == nil {
		t.Fatal("legacy unbounded reader used")
	}
}

func TestComboCapitalReleaseRejectsMissingOrInvalidParentProof(t *testing.T) {
	for _, scenario := range []string{"dirty", "init failure", "nil child", "typed nil child", "unsupported child", "duplicate child", "no children", "bad memory peak", "invalid drawdown", "wrong bot", "wrong name", "wrong symbol", "bad peak", "missing baseline", "schema", "bad JSON", "storage failure", "missing store", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			combo, store, _ := comboCapitalFixture(t, "trend")
			switch scenario {
			case "dirty":
				combo.runtimeStateDirty = true
			case "init failure":
				combo.initErr = errors.New("invalid configuration")
			case "nil child":
				combo.strategies = []Strategy{nil}
			case "typed nil child":
				var child *TrendFollowingStrategy
				combo.strategies = []Strategy{child}
			case "unsupported child":
				combo.strategies = []Strategy{&fakeComboSubStrategy{name: "child"}}
			case "duplicate child":
				combo.strategies = append(combo.strategies, combo.strategies[0])
			case "no children":
				combo.strategies = nil
			case "bad memory peak":
				combo.peakEquity = math.NaN()
			case "invalid drawdown":
				combo.strategyCfg.MaxDrawdown = math.NaN()
			case "missing baseline":
				delete(store.states, "combo")
			case "schema":
				store.states["combo"].version++
			case "bad JSON":
				store.states["combo"].payload = "invalid JSON"
			case "storage failure":
				store.err = errors.New("storage unavailable")
			case "missing store":
				combo.runtimeStateStore = nil
			default:
				bot, name, symbol, peak := "bot-a", "combo", "BTCUSDT", 1000.0
				switch scenario {
				case "wrong bot":
					bot = "other"
				case "wrong name":
					name = "other"
				case "wrong symbol":
					symbol = "ETHUSDT"
				case "bad peak":
					peak = -1
				}
				store.states["combo"].payload = fmt.Sprintf(`{"bot_id":%q,"strategy_name":%q,"symbol":%q,"peak_equity":%v}`, bot, name, symbol, peak)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			if err := combo.VerifyCapitalReleaseState(ctx); err == nil {
				t.Fatal("invalid parent/child proof allowed release")
			}
		})
	}
}

func TestComboCapitalReleaseCancelsChildReadAndAllowsLaterRecovery(t *testing.T) {
	combo, store, _ := comboCapitalFixture(t, "trend")
	store.waitChild = true
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := combo.VerifyCapitalReleaseState(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("child read did not cancel: %v", err)
	}
	store.waitChild = false
	// Setter takes both child and parent write locks; no proof lock may survive.
	if err := combo.SetRuntimeStateStore(store); err != nil {
		t.Fatal(err)
	}
	if err := combo.VerifyCapitalReleaseState(t.Context()); err != nil {
		t.Fatalf("cancelled child blocked later release: %v", err)
	}
	if err := combo.VerifyCapitalReleaseState(nil); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestComboCapitalReleaseDoesNotRequireDisabledDrawdownBaseline(t *testing.T) {
	combo, store, _ := comboCapitalFixture(t, "trend")
	combo.strategyCfg.MaxDrawdown = 0
	delete(store.states, "combo")
	if err := combo.VerifyCapitalReleaseState(t.Context()); err != nil {
		t.Fatalf("explicitly disabled drawdown blocked clean child release: %v", err)
	}
}
