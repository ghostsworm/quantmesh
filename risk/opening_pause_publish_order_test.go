package risk

import (
	"context"
	"testing"
	"time"

	"quantmesh/storage"
)

type blockedPauseWriteStore struct {
	*openingPauseStateTestStore
	entered chan struct{}
	release chan struct{}
}

func (s *blockedPauseWriteStore) UpsertOpeningPauseHolder(ctx context.Context, holder storage.OpeningPauseHolder) error {
	close(s.entered)
	select {
	case <-s.release:
		return s.openingPauseStateTestStore.UpsertOpeningPauseHolder(ctx, holder)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRiskHoldBlocksBeforeDurableWriteReturns(t *testing.T) {
	for _, mode := range []string{"direct_pause", "metrics_unavailable"} {
		t.Run(mode, func(t *testing.T) {
			store := &blockedPauseWriteStore{openingPauseStateTestStore: &openingPauseStateTestStore{rows: make(map[string]string)}, entered: make(chan struct{}), release: make(chan struct{})}
			coordinator, err := NewOpeningPauseCoordinatorWithStore(t.Context(), store)
			if err != nil {
				t.Fatal(err)
			}
			bot := &sourceOpeningPauseTestBot{safeMockBot: &safeMockBot{}, gates: make(map[string]bool)}
			source := "fixture_risk_hold"
			var publish func()
			if mode == "direct_pause" {
				publish = func() { _ = coordinator.Pause(source, "fixture known risk", []BotController{bot}) }
			} else {
				source = metricsUnavailablePauseSource
				cfg := newCircuitBreakerTestConfig()
				cfg.Triggers.MaxDrawdown.Enabled = true
				gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{bots: []BotController{bot}})
				gcb.SetPauseCoordinator(coordinator)
				cfg.Enabled = true
				publish = func() {
					gcb.UpdateMetricsHealth(MetricsHealth{Available: false, CheckedAt: time.Now(), ValidUntil: time.Now().Add(time.Minute)})
				}
			}
			done := make(chan struct{})
			go func() { defer close(done); publish() }()
			defer func() { close(store.release); <-done }()
			select {
			case <-store.entered:
			case <-time.After(time.Second):
				t.Fatal("fixture never entered durable pause write")
			}
			gateSource := openingPauseGateSource(coordinator.ownerID, source)
			if !bot.hasGate(gateSource) || !coordinator.IsHeldBy(source) {
				t.Fatal("known risk left current Bot open while durable write was blocked")
			}
		})
	}
}
