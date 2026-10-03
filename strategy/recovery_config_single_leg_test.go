package strategy

import (
	"errors"
	"math"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

func TestSingleLegRecoveryConfigCanonicalFlat(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "owner-a", "BTCUSDT"
	binding := SingleLegRecoveryBinding{BotID: "owner-a", StrategyName: "custom-dca", Symbol: "BTCUSDT"}
	dca := NewDCAEnhancedStrategy(binding.StrategyName, binding.Symbol, cfg, nil, nil, nil)
	store := &memoryRuntimeStateStore{}
	dca.SetRuntimeStateStore(store)
	if err := dca.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	before := store.payload
	if !strings.Contains(before, `"layers":null`) {
		t.Fatal("fixture is not actual canonical nil collection")
	}
	if err := VerifyDCARecoveryConfigState(store.version, before, binding); err != nil {
		t.Fatalf("canonical DCA rejected: %v", err)
	}
	if before != store.payload {
		t.Fatal("proof wrote durable state")
	}
	binding.StrategyName, binding.Direction = "custom-martin", "LONG"
	martin := NewMartingaleStrategy(binding.StrategyName, binding.Symbol, cfg, nil, nil, nil)
	martin.SetRuntimeStateStore(store)
	if err := martin.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	before = store.payload
	if !strings.Contains(before, `"entries":null`) {
		t.Fatal("fixture is not actual canonical nil collection")
	}
	if err := VerifyMartingaleRecoveryConfigState(store.version, before, binding); err != nil {
		t.Fatalf("canonical martingale rejected: %v", err)
	}
	if before != store.payload {
		t.Fatal("proof wrote durable state")
	}
	for _, name := range []string{"trend", "mean_reversion", "momentum", "custom-combo-child"} {
		binding.StrategyName = name
		stats := &StrategyStatistics{TotalTrades: 3, WinRate: 0.5, TotalPnL: -20, TotalVolume: 100}
		if err := saveSignalRuntimeState(store, cfg, nil, name, binding.Symbol, nil, 0, nil, "", stats, true); err != nil {
			t.Fatal(err)
		}
		before = store.payload
		if err := VerifySignalRecoveryConfigState(store.version, before, binding); err != nil {
			t.Fatalf("canonical signal %s rejected: %v", name, err)
		}
		if before != store.payload {
			t.Fatal("proof wrote durable state")
		}
	}
}

func TestSingleLegRecoveryConfigCannotDiscardPendingOrTinyExposure(t *testing.T) {
	binding := SingleLegRecoveryBinding{BotID: "owner", StrategyName: "old-name", Symbol: "BTCUSDT", Direction: "LONG"}
	dca := dcaRuntimeState{BotID: binding.BotID, StrategyName: binding.StrategyName, Symbol: binding.Symbol, CloseLayerIndex: -1}
	dcaChanges := map[string]func(*dcaRuntimeState){
		"unknown layer": func(s *dcaRuntimeState) {
			s.Layers = []*DCALayer{{Status: position.OrderStatusUnknown, ClientOrderID: "prepared"}}
		},
		"tiny qty": func(s *dcaRuntimeState) { s.TotalQty = math.SmallestNonzeroFloat64 },
		"cost":     func(s *dcaRuntimeState) { s.TotalCost = 1 },
		"close id": func(s *dcaRuntimeState) { s.CloseClientOrderID = "prepared" },
		"fee":      func(s *dcaRuntimeState) { s.CloseBaseFeeQty = math.SmallestNonzeroFloat64 },
		"progress": func(s *dcaRuntimeState) { s.CloseProgress.Notional = 1 },
	}
	for name, change := range dcaChanges {
		t.Run("dca/"+name, func(t *testing.T) {
			state := dca
			change(&state)
			if err := VerifyDCARecoveryConfigState(2, recoveryProofPayload(t, state), binding); !errors.Is(err, ErrRecoveryConfigRequired) {
				t.Fatalf("unresolved DCA accepted: %v", err)
			}
		})
	}
	martin := martingaleRuntimeState{BotID: binding.BotID, StrategyName: binding.StrategyName, Symbol: binding.Symbol, Direction: binding.Direction}
	for _, name := range []string{"unknown", "tiny qty", "reason", "pnl", "fill"} {
		t.Run("martingale/"+name, func(t *testing.T) {
			state := martin
			switch name {
			case "unknown":
				state.Entries = []*MartingaleEntry{{Status: position.OrderStatusUnknown, ClientOrderID: "prepared"}}
			case "tiny qty":
				state.TotalQty = math.SmallestNonzeroFloat64
			case "reason":
				state.PendingCloseReason = "stop_loss"
			case "pnl":
				state.CloseRealizedPnL = -1
			case "fill":
				state.CloseProgress.Quantity = 0.1
			}
			if err := VerifyMartingaleRecoveryConfigState(1, recoveryProofPayload(t, state), binding); !errors.Is(err, ErrRecoveryConfigRequired) {
				t.Fatalf("unresolved martingale accepted: %v", err)
			}
		})
	}
	for _, name := range []string{"position", "order", "action", "alias", "tiny price"} {
		t.Run("signal/"+name, func(t *testing.T) {
			state := signalRuntimeState{BotID: binding.BotID, StrategyName: binding.StrategyName, Symbol: binding.Symbol}
			switch name {
			case "position":
				state.Position = &Position{Size: 0}
			case "order":
				state.ActiveOrder = &Order{ClientOrderID: "prepared"}
			case "action":
				state.PendingAction = signalActionOpenLong
			case "alias":
				state.OrderAlias = "prepared"
			case "tiny price":
				state.EntryPrice = math.SmallestNonzeroFloat64
			}
			if err := VerifySignalRecoveryConfigState(1, recoveryProofPayload(t, state), binding); !errors.Is(err, ErrRecoveryConfigRequired) {
				t.Fatalf("unresolved signal accepted: %v", err)
			}
		})
	}
}

func TestSingleLegRecoveryConfigInvalidEvidence(t *testing.T) {
	binding := SingleLegRecoveryBinding{BotID: "owner", StrategyName: "old-name", Symbol: "BTCUSDT", Direction: "LONG"}
	fixtures := []struct {
		name    string
		version int
		payload string
		verify  func(int, string, SingleLegRecoveryBinding) error
	}{
		{"dca", 2, recoveryProofPayload(t, dcaRuntimeState{BotID: binding.BotID, StrategyName: binding.StrategyName, Symbol: binding.Symbol, CloseLayerIndex: -1}), VerifyDCARecoveryConfigState},
		{"martingale", 1, recoveryProofPayload(t, martingaleRuntimeState{BotID: binding.BotID, StrategyName: binding.StrategyName, Symbol: binding.Symbol, Direction: "LONG"}), VerifyMartingaleRecoveryConfigState},
		{"signal", 1, recoveryProofPayload(t, signalRuntimeState{BotID: binding.BotID, StrategyName: binding.StrategyName, Symbol: binding.Symbol}), VerifySignalRecoveryConfigState},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			for _, payload := range []string{"null", "{}", fixture.payload + "{}", strings.Replace(fixture.payload, `"WinRate":0`, `"WinRate":2`, 1), strings.Replace(fixture.payload, `"TotalVolume":0`, `"TotalVolume":null`, 1), strings.Replace(fixture.payload, `"TotalTrades":0,`, "", 1), strings.Replace(fixture.payload, `"TotalPnL":0`, `"TotalPnL":1,"TOTALPNL":0`, 1)} {
				if err := fixture.verify(fixture.version, payload, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
					t.Fatalf("ambiguous evidence accepted: %v", err)
				}
			}
			for _, bad := range []SingleLegRecoveryBinding{{}, {BotID: "other", StrategyName: binding.StrategyName, Symbol: binding.Symbol, Direction: "LONG"}, {BotID: binding.BotID, StrategyName: "other", Symbol: binding.Symbol, Direction: "LONG"}, {BotID: binding.BotID, StrategyName: binding.StrategyName, Symbol: "ETHUSDT", Direction: "LONG"}} {
				if err := fixture.verify(fixture.version, fixture.payload, bad); !errors.Is(err, ErrRecoveryConfigUnverified) {
					t.Fatal("owner mismatch accepted")
				}
			}
			if err := fixture.verify(99, fixture.payload, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
				t.Fatal("unknown schema accepted")
			}
			if fixture.name != "signal" {
				for _, payload := range []string{strings.Replace(fixture.payload, `"Notional":0`, `"Notional":null`, 1), strings.Replace(fixture.payload, `"Quantity":0,`, "", 1), strings.Replace(fixture.payload, ":null", ":[null]", 1)} {
					if err := fixture.verify(fixture.version, payload, binding); !errors.Is(err, ErrRecoveryConfigUnverified) {
						t.Fatalf("incomplete progress or nil entry accepted: %v", err)
					}
				}
			}
		})
	}
}
