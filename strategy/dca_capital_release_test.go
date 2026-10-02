package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

func TestDCACapitalReleaseRequiresSettledRecoveryState(t *testing.T) {
	changes := map[string]func(*dcaRuntimeState){
		"layer":               func(s *dcaRuntimeState) { s.Layers = []*DCALayer{{Status: position.OrderStatusUnknown}} },
		"nil layer":           func(s *dcaRuntimeState) { s.Layers = []*DCALayer{nil} },
		"quantity":            func(s *dcaRuntimeState) { s.TotalQty = 1 },
		"cost":                func(s *dcaRuntimeState) { s.TotalCost = 100 },
		"average":             func(s *dcaRuntimeState) { s.AvgEntryPrice = 100 },
		"layer index":         func(s *dcaRuntimeState) { s.CurrentLayer = 1 },
		"closing":             func(s *dcaRuntimeState) { s.IsClosing = true },
		"order id":            func(s *dcaRuntimeState) { s.CloseOrderID = 17 },
		"client id":           func(s *dcaRuntimeState) { s.CloseClientOrderID = "prepared" },
		"close layer":         func(s *dcaRuntimeState) { s.CloseLayerIndex = 0 },
		"invalid close layer": func(s *dcaRuntimeState) { s.CloseLayerIndex = -2 },
		"requested":           func(s *dcaRuntimeState) { s.CloseRequestedQty = 1 },
		"limit price":         func(s *dcaRuntimeState) { s.CloseLimitPrice = 100 },
		"fill":                func(s *dcaRuntimeState) { s.CloseProgress.Quantity = 1 },
		"notional":            func(s *dcaRuntimeState) { s.CloseProgress.Notional = 100 },
		"verified fee":        func(s *dcaRuntimeState) { s.CloseFeeVerifiedQty = 1 },
		"base fee":            func(s *dcaRuntimeState) { s.CloseBaseFeeQty = 0.01 },
		"wrong bot":           func(s *dcaRuntimeState) { s.BotID = "another-bot" },
		"wrong strategy":      func(s *dcaRuntimeState) { s.StrategyName = "another-strategy" },
		"wrong symbol":        func(s *dcaRuntimeState) { s.Symbol = "ETHUSDT" },
	}
	for scenario, change := range changes {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
			s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, nil, nil, nil)
			state := s.runtimeStateSnapshotLocked()
			change(&state)
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: string(payload), found: true}
			s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: store})
			if err := s.VerifyCapitalReleaseState(t.Context()); err == nil {
				t.Fatal("unverified durable recovery state allowed release")
			}
			if store.payload != string(payload) {
				t.Fatal("proof changed durable recovery state")
			}
		})
	}
}

func TestDCACapitalReleaseMemoryAndStorageBoundaries(t *testing.T) {
	for _, scenario := range []string{"empty", "no history", "historical profit", "memory nil layer", "memory close layer", "memory cost", "invalid memory", "persist failure", "old schema", "bad JSON", "storage failure", "legacy store", "missing store", "cancelled", "nil context"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
			s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, nil, nil, nil)
			state := s.runtimeStateSnapshotLocked()
			if scenario == "historical profit" {
				state.Stats.TotalPnL = 100
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: string(payload), found: true}
			switch scenario {
			case "memory nil layer":
				s.layers = []*DCALayer{nil}
			case "memory close layer":
				s.closeLayer = &DCALayer{Index: 0}
			case "memory cost":
				s.totalCost = 100
			case "invalid memory":
				s.totalQty = math.NaN()
			case "persist failure":
				s.runtimeStateErr = errors.New("accounting unavailable")
			case "no history":
				store.found = false
			case "old schema":
				store.version--
			case "bad JSON":
				store.payload = "invalid JSON"
			case "storage failure":
				store.err = errors.New("storage unavailable")
			}
			switch scenario {
			case "missing store":
			case "legacy store":
				s.SetRuntimeStateStore(store)
			default:
				s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: store})
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			before := store.payload
			if scenario == "nil context" {
				err = s.VerifyCapitalReleaseState(nil)
			} else {
				err = s.VerifyCapitalReleaseState(ctx)
			}
			wantSuccess := scenario == "empty" || scenario == "no history" || scenario == "historical profit"
			if (err == nil) != wantSuccess || store.payload != before {
				t.Fatalf("proof err=%v wantSuccess=%v payloadChanged=%v", err, wantSuccess, store.payload != before)
			}
		})
	}
}

func TestDCACapitalReleaseCancelledReadAllowsLaterRecovery(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, nil, nil, nil)
	s.SetRuntimeStateStore(&capitalReleaseWaitingStateStore{&memoryRuntimeStateStore{}})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.VerifyCapitalReleaseState(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read did not cancel: %v", err)
	}
	s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}})
	if err := s.VerifyCapitalReleaseState(t.Context()); err != nil {
		t.Fatalf("cancelled proof blocked later read: %v", err)
	}
}
