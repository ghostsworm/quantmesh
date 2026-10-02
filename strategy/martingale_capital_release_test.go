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

func TestMartingaleCapitalReleaseRequiresSettledRecoveryState(t *testing.T) {
	for _, scenario := range []string{"empty", "no history", "historical profit", "memory entry", "nil memory entry", "memory close", "memory cost", "persist failure", "durable entry", "durable quantity", "durable cost", "durable average", "durable level", "durable closing", "durable close id", "durable close cid", "durable close reason", "durable pending close", "durable requested", "durable fill", "durable notional", "durable close pnl", "wrong bot", "wrong strategy", "wrong symbol", "wrong direction", "old schema", "bad JSON", "storage failure", "legacy store", "missing store", "cancelled", "invalid memory"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
			s := NewMartingaleStrategy("martingale", "BTCUSDT", cfg, nil, nil, nil)
			state := s.runtimeStateSnapshotLocked()
			switch scenario {
			case "historical profit":
				state.Stats.TotalPnL = 100
			case "memory entry":
				s.entries = []*MartingaleEntry{{Status: position.OrderStatusUnknown}}
			case "nil memory entry":
				s.entries = []*MartingaleEntry{nil}
			case "memory close":
				s.closeClientOrderID = "prepared"
			case "memory cost":
				s.totalCost = 100
			case "invalid memory":
				s.totalQty = math.NaN()
			case "persist failure":
				s.runtimeStateErr = errors.New("unsettled accounting")
			case "durable entry":
				state.Entries = []*MartingaleEntry{{Status: position.OrderStatusUnknown}}
			case "durable quantity":
				state.TotalQty = 1
			case "durable cost":
				state.TotalCost = 100
			case "durable average":
				state.AvgEntryPrice = 100
			case "durable level":
				state.CurrentLevel = 1
			case "durable closing":
				state.IsClosing = true
			case "durable close id":
				state.CloseOrderID = 17
			case "durable close cid":
				state.CloseClientOrderID = "prepared"
			case "durable close reason":
				state.CloseReason = "stop_loss"
			case "durable pending close":
				state.PendingCloseReason = "stop_loss"
			case "durable requested":
				state.CloseRequestedQty = 1
			case "durable fill":
				state.CloseProgress.Quantity = 1
			case "durable notional":
				state.CloseProgress.Notional = 100
			case "durable close pnl":
				state.CloseRealizedPnL = 1
			case "wrong bot":
				state.BotID = "another-bot"
			case "wrong strategy":
				state.StrategyName = "another-strategy"
			case "wrong symbol":
				state.Symbol = "ETHUSDT"
			case "wrong direction":
				state.Direction = "SHORT"
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: martingaleRuntimeStateSchemaVersion, payload: string(payload), found: true}
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
			wantSuccess := scenario == "empty" || scenario == "no history" || scenario == "historical profit"
			if (err == nil) != wantSuccess || store.payload != before {
				t.Fatalf("release proof err=%v wantSuccess=%v payloadChanged=%v", err, wantSuccess, store.payload != before)
			}
		})
	}
}

func TestMartingaleCapitalReleaseCancelledReadAllowsLaterRecovery(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewMartingaleStrategy("martingale", "BTCUSDT", cfg, nil, nil, nil)
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
