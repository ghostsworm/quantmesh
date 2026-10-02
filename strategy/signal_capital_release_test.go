package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/config"
)

func TestSignalCapitalReleaseRequiresSettledState(t *testing.T) {
	for _, kind := range []string{"trend", "mean_reversion", "momentum"} {
		for _, scenario := range []string{"empty", "no history", "historical profit", "memory position", "memory order", "memory action", "memory cost", "invalid memory", "persist failure", "durable position", "durable order", "durable action", "durable alias", "durable cost", "wrong bot", "wrong strategy", "wrong symbol", "old schema", "bad JSON", "storage failure", "legacy store", "missing store", "cancelled", "nil context"} {
			t.Run(kind+"/"+scenario, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
				memory := signalRuntimeState{}
				state := signalRuntimeState{BotID: "bot-a", StrategyName: kind, Symbol: "BTCUSDT"}
				var stateErr error
				switch scenario {
				case "historical profit":
					state.Statistics.TotalPnL = 100
				case "memory position":
					memory.Position = &Position{Size: 1}
				case "memory order":
					memory.ActiveOrder = &Order{ClientOrderID: "pending"}
				case "memory action":
					memory.PendingAction = signalActionOpenLong
				case "memory cost":
					memory.EntryPrice = 100
				case "invalid memory":
					memory.EntryPrice = math.NaN()
				case "persist failure":
					stateErr = errors.New("accounting unavailable")
				case "durable position":
					state.Position = &Position{Size: 1}
				case "durable order":
					state.ActiveOrder = &Order{ClientOrderID: "pending"}
				case "durable action":
					state.PendingAction = signalActionOpenLong
				case "durable alias":
					state.OrderAlias = "pending"
				case "durable cost":
					state.EntryPrice = 100
				case "wrong bot":
					state.BotID = "another-bot"
				case "wrong strategy":
					state.StrategyName = "another-strategy"
				case "wrong symbol":
					state.Symbol = "ETHUSDT"
				}
				payload, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				backing := &memoryRuntimeStateStore{version: signalRuntimeStateSchemaVersion, payload: string(payload), found: true}
				switch scenario {
				case "no history":
					backing.found = false
				case "old schema":
					backing.version++
				case "bad JSON":
					backing.payload = "invalid JSON"
				case "storage failure":
					backing.err = errors.New("storage unavailable")
				}
				var store RuntimeStateStore = &capitalReleaseContextStateStore{memoryRuntimeStateStore: backing}
				if scenario == "legacy store" {
					store = backing
				} else if scenario == "missing store" {
					store = nil
				}
				var verify func(context.Context) error
				switch kind {
				case "trend":
					s := NewTrendFollowingStrategy(kind, cfg, nil, nil, nil)
					s.position, s.activeOrder, s.entryPrice, s.pendingAction, s.runtimeStateErr = memory.Position, memory.ActiveOrder, memory.EntryPrice, memory.PendingAction, stateErr
					s.SetRuntimeStateStore(store)
					verify = s.VerifyCapitalReleaseState
				case "mean_reversion":
					s := NewMeanReversionStrategy(kind, cfg, nil, nil, nil)
					s.position, s.activeOrder, s.entryPrice, s.pendingAction, s.runtimeStateErr = memory.Position, memory.ActiveOrder, memory.EntryPrice, memory.PendingAction, stateErr
					s.SetRuntimeStateStore(store)
					verify = s.VerifyCapitalReleaseState
				case "momentum":
					s := NewMomentumStrategy(kind, cfg, nil, nil, nil)
					s.position, s.activeOrder, s.entryPrice, s.pendingAction, s.runtimeStateErr = memory.Position, memory.ActiveOrder, memory.EntryPrice, memory.PendingAction, stateErr
					s.SetRuntimeStateStore(store)
					verify = s.VerifyCapitalReleaseState
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if scenario == "cancelled" {
					cancel()
				}
				before := backing.payload
				if scenario == "nil context" {
					err = verify(nil)
				} else {
					err = verify(ctx)
				}
				wantSuccess := scenario == "empty" || scenario == "no history" || scenario == "historical profit"
				if (err == nil) != wantSuccess || backing.payload != before {
					t.Fatalf("proof err=%v wantSuccess=%v payloadChanged=%v", err, wantSuccess, backing.payload != before)
				}
			})
		}
	}
}

func TestSignalCapitalReleaseCancelledReadAllowsLaterRecovery(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewTrendFollowingStrategy("trend", cfg, nil, nil, nil)
	s.SetRuntimeStateStore(&capitalReleaseWaitingStateStore{&memoryRuntimeStateStore{}})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.VerifyCapitalReleaseState(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read did not cancel: %v", err)
	}
	s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}})
	if err := s.VerifyCapitalReleaseState(t.Context()); err != nil {
		t.Fatalf("cancelled proof blocked legal later read: %v", err)
	}
}
