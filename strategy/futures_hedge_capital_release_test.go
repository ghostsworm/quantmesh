package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
)

func TestFuturesHedgeCapitalReleaseRequiresSettledState(t *testing.T) {
	for _, name := range []string{"futures_long", "futures_short"} {
		for _, scenario := range []string{"empty", "no history", "memory pending", "durable pending", "wrong bot", "wrong strategy", "wrong group", "wrong symbol", "tracker mismatch", "missing tracker", "old schema", "bad JSON", "storage failure", "legacy store", "missing store", "cancelled", "nil context"} {
			t.Run(name+"/"+scenario, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
				var tracker *futuresHedgeOrderTracker
				var verify func(context.Context) error
				if name == "futures_long" {
					s := NewFuturesLongStrategy(name, cfg, nil, nil, nil)
					tracker = s.orderTracker
					if scenario == "missing tracker" {
						s.orderTracker = nil
					}
					verify = s.VerifyCapitalReleaseState
				} else {
					s := NewFuturesShortStrategy(name, cfg, nil, nil, nil)
					tracker = s.orderTracker
					if scenario == "missing tracker" {
						s.orderTracker = nil
					}
					verify = s.VerifyCapitalReleaseState
				}
				state := futuresHedgeRuntimeState{BotID: "bot-a", Strategy: name, Symbol: "BTCUSDT"}
				switch scenario {
				case "memory pending":
					tracker.pending = &futuresHedgePendingOrder{ClientOrderID: "pending", Side: "BUY", Quantity: 1}
				case "durable pending":
					state.Pending = &futuresHedgePendingOrder{ClientOrderID: "pending", Side: "BUY", Quantity: 1}
				case "wrong bot":
					state.BotID = "other-bot"
				case "wrong strategy":
					state.Strategy = "other-strategy"
				case "wrong group":
					state.GroupID = "other-group"
				case "wrong symbol":
					state.Symbol = "ETHUSDT"
				case "tracker mismatch":
					tracker.botID = "other-bot"
				}
				payload, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				store := &memoryRuntimeStateStore{version: futuresHedgeRuntimeStateVersion, payload: string(payload), found: true}
				switch scenario {
				case "no history":
					store.found = false
				case "old schema":
					store.version++
				case "bad JSON":
					store.payload = "invalid JSON"
				case "storage failure":
					store.err = errors.New("storage unavailable")
				}
				switch scenario {
				case "missing store":
				case "legacy store":
					tracker.SetStore(store)
				default:
					tracker.SetStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: store})
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				if scenario == "cancelled" {
					cancel()
				}
				before := store.payload
				if scenario == "nil context" {
					err = verify(nil)
				} else {
					err = verify(ctx)
				}
				wantSuccess := scenario == "empty" || scenario == "no history"
				if (err == nil) != wantSuccess || store.payload != before {
					t.Fatalf("proof err=%v wantSuccess=%v payloadChanged=%v", err, wantSuccess, store.payload != before)
				}
			})
		}
	}
}

func TestFuturesHedgeCapitalReleaseCancelledReadUnlocksTracker(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewFuturesLongStrategy("futures_long", cfg, nil, nil, nil)
	s.SetRuntimeStateStore(&capitalReleaseWaitingStateStore{&memoryRuntimeStateStore{}})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.VerifyCapitalReleaseState(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read did not cancel: %v", err)
	}
	s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}})
	if err := s.VerifyCapitalReleaseState(t.Context()); err != nil {
		t.Fatalf("cancelled read blocked later verification: %v", err)
	}
}
