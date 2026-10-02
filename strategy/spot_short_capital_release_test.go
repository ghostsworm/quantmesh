package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
)

type capitalReleaseContextStateStore struct {
	*memoryRuntimeStateStore
	contextErr error
}

type capitalReleaseWaitingStateStore struct{ *memoryRuntimeStateStore }

func (s *capitalReleaseWaitingStateStore) LoadRuntimeStateContext(ctx context.Context, _ string) (int, string, bool, error) {
	<-ctx.Done()
	return 0, "", false, ctx.Err()
}

func TestSpotShortCapitalReleaseCancelledReadUnlocksState(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewSpotShortStrategy("spot_short", cfg, nil, nil, nil, nil)
	s.SetRuntimeStateStore(&capitalReleaseWaitingStateStore{&memoryRuntimeStateStore{}})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := s.VerifyCapitalReleaseState(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked read did not cancel: %v", err)
	}
	// This setter takes the write lock held out by the proof's read lock.
	s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}})
	if err := s.VerifyCapitalReleaseState(t.Context()); err != nil {
		t.Fatalf("cancelled proof blocked later valid verification: %v", err)
	}
}

func TestSpotShortCapitalReleaseRejectsLegacyOnlyStateReader(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewSpotShortStrategy("spot_short", cfg, nil, nil, nil, nil)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	if err := s.VerifyCapitalReleaseState(t.Context()); err == nil {
		t.Fatal("uncancellable legacy read accepted as release proof")
	}
}

func (s *capitalReleaseContextStateStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, "", false, err
	}
	if s.contextErr != nil {
		return 0, "", false, s.contextErr
	}
	return s.LoadRuntimeState(name)
}

func TestSpotShortCapitalReleaseUsesCancellableStateReader(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	s := NewSpotShortStrategy("spot_short", cfg, nil, nil, nil, nil)
	s.SetRuntimeStateStore(&capitalReleaseContextStateStore{
		memoryRuntimeStateStore: &memoryRuntimeStateStore{},
		contextErr:              context.DeadlineExceeded,
	})
	if err := s.VerifyCapitalReleaseState(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellable debt read bypassed: %v", err)
	}
}

func TestSpotShortCapitalReleaseRequiresDurableDebtSettlement(t *testing.T) {
	for _, scenario := range []string{"empty", "no durable history", "memory borrow", "memory repay", "memory buy", "durable borrow", "durable repay", "durable buy", "wrong owner", "old schema", "bad payload", "storage failure", "missing store", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
			s := NewSpotShortStrategy("spot_short", cfg, nil, nil, nil, nil)
			state := spotShortRuntimeState{BotID: "bot-a", Strategy: "spot_short", Symbol: "BTCUSDT", BaseAsset: "BTC"}
			switch scenario {
			case "memory borrow":
				s.pendingBorrow["pending"] = spotShortPendingBorrow{Amount: 1}
			case "memory repay":
				s.pendingRepay[17] = spotShortPendingRepay{OrderQuantity: 1}
			case "memory buy":
				s.pendingBuy["pending"] = spotShortPendingBuy{Quantity: 1}
			case "durable borrow":
				state.PendingBorrow = map[string]spotShortPendingBorrow{"pending": {Amount: 1}}
			case "durable repay":
				state.PendingRepay = map[int64]spotShortPendingRepay{17: {OrderQuantity: 1}}
			case "durable buy":
				state.PendingBuy = map[string]spotShortPendingBuy{"pending": {Quantity: 1}}
			case "wrong owner":
				state.BotID = "another-bot"
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &memoryRuntimeStateStore{version: spotShortRuntimeStateSchemaVersion, payload: string(payload), found: true}
			switch scenario {
			case "no durable history":
				store.found = false
			case "old schema":
				store.version--
			case "bad payload":
				store.payload = "invalid JSON"
			case "storage failure":
				store.err = errors.New("state unavailable")
			}
			if scenario != "missing store" {
				s.SetRuntimeStateStore(&capitalReleaseContextStateStore{memoryRuntimeStateStore: store})
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			before := store.payload
			err = s.VerifyCapitalReleaseState(ctx)
			wantSuccess := scenario == "empty" || scenario == "no durable history"
			if (err == nil) != wantSuccess {
				t.Fatalf("debt proof err=%v wantSuccess=%v", err, wantSuccess)
			}
			if store.payload != before {
				t.Fatal("read-only release proof changed durable recovery state")
			}
		})
	}
}
