package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
)

func TestSpotLongCapitalReleaseRequiresDurableOrderSettlement(t *testing.T) {
	for _, scenario := range []string{"empty", "no history", "memory order", "memory intent", "durable order", "durable intent", "wrong bot", "wrong strategy", "wrong group", "wrong symbol", "wrong asset", "old schema", "bad payload", "missing store", "legacy store", "storage failure", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
			s := NewSpotLongStrategy("spot_long", cfg, nil, nil, nil)
			state := spotLongRuntimeState{BotID: "bot-a", Strategy: "spot_long", Symbol: "BTCUSDT", BaseAsset: "BTC"}
			switch scenario {
			case "memory order":
				s.pendingOrders[17] = spotLongPendingOrder{Side: "BUY", Quantity: 1}
			case "memory intent":
				s.pendingIntents["pending"] = spotLongPendingIntent{Side: "BUY", Quantity: 1}
			case "durable order":
				state.PendingOrders = map[int64]spotLongPendingOrder{17: {Side: "SELL", Quantity: 1}}
			case "durable intent":
				state.PendingIntents = map[string]spotLongPendingIntent{"pending": {Side: "BUY", Quantity: 1}}
			case "wrong bot":
				state.BotID = "another-bot"
			case "wrong strategy":
				state.Strategy = "another-strategy"
			case "wrong group":
				state.GroupID = "another-group"
			case "wrong symbol":
				state.Symbol = "ETHUSDT"
			case "wrong asset":
				state.BaseAsset = "ETH"
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: spotLongRuntimeStateSchemaVersion, payload: string(payload), found: true}
			switch scenario {
			case "no history":
				store.found = false
			case "old schema":
				store.version--
			case "bad payload":
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
			err = s.VerifyCapitalReleaseState(ctx)
			wantSuccess := scenario == "empty" || scenario == "no history"
			if (err == nil) != wantSuccess {
				t.Fatalf("order proof err=%v wantSuccess=%v", err, wantSuccess)
			}
			if before != store.payload {
				t.Fatal("release proof changed durable recovery state")
			}
		})
	}
}

func TestSpotLongCapitalReleaseCancelledReadAllowsLaterRecovery(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewSpotLongStrategy("spot_long", cfg, nil, nil, nil)
	s.SetRuntimeStateStore(&capitalReleaseWaitingStateStore{&memoryRuntimeStateStore{}})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.VerifyCapitalReleaseState(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read did not respect deadline: %v", err)
	}
	s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}})
	if err := s.VerifyCapitalReleaseState(t.Context()); err != nil {
		t.Fatalf("cancelled read prevented normal recovery: %v", err)
	}
}
