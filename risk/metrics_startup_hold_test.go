package risk

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUnavailableMetricsPersistHoldForLaterBotStart(t *testing.T) {
	store := &openingPauseStateTestStore{rows: make(map[string]string)}
	coordinator, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	cfg := newCircuitBreakerTestConfig()
	cfg.Triggers.MaxDrawdown.Enabled = true
	gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{})
	gcb.SetPauseCoordinator(coordinator)
	cfg.Enabled = true
	gcb.UpdateMetricsHealth(MetricsHealth{Available: false, CheckedAt: time.Now(), ValidUntil: time.Now().Add(time.Minute)})
	holders, finish := coordinator.BeginBotStart()
	finish()
	if len(holders) != 1 {
		t.Fatal("unavailable equity did not reach future Bot startup holders")
	}
	bot := &sourceOpeningPauseTestBot{safeMockBot: &safeMockBot{}, gates: make(map[string]bool)}
	for _, holder := range holders {
		bot.PauseOpeningForSource(holder.Source, holder.Reason)
	}
	if err := coordinator.Pause("manual_fixture", "operator hold", []BotController{bot}); err != nil {
		t.Fatal(err)
	}
	manualSource := ""
	for source := range coordinator.Holders() {
		if source != holders[0].Source {
			manualSource = source
		}
	}
	if manualSource == "" {
		t.Fatal("operator hold did not install its owner-scoped source")
	}
	gcb.botProvider = &circuitBreakerMockProvider{bots: []BotController{bot}}
	now := time.Now()
	gcb.UpdateMetricsObservation(MetricsSnapshot{}, MetricsHealth{Available: true, DrawdownAvailable: true, Persisted: true, CashFlowAdjusted: true, CheckedAt: now, ValidUntil: now.Add(time.Minute)})
	if bot.hasGate(holders[0].Source) || !bot.hasGate(manualSource) {
		t.Fatal("healthy recovery failed to release only metrics startup hold")
	}
	restored, err := NewOpeningPauseCoordinatorWithStore(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	remaining, done := restored.BeginBotStart()
	done()
	if len(remaining) != 1 || !strings.HasSuffix(remaining[0].Source, ":manual_fixture") {
		t.Fatal("durable hold recovery erased operator hold or retained recovered metrics hold")
	}
}

func TestMetricsCoordinatorBindingBlocksBeforeFirstObservation(t *testing.T) {
	for _, mode := range []string{"drawdown", "daily_loss", "loss_streak", "connectivity_only"} {
		t.Run(mode, func(t *testing.T) {
			cfg := newCircuitBreakerTestConfig()
			switch mode {
			case "drawdown":
				cfg.Triggers.MaxDrawdown.Enabled = true
			case "daily_loss":
				cfg.Triggers.TotalDailyLoss.Enabled = true
			case "loss_streak":
				cfg.Triggers.ConsecutiveLosses.Enabled = true
			}
			gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{})
			cfg.Enabled = true // avoid background ticker; production binds while enabled
			coordinator := NewOpeningPauseCoordinator()
			gcb.SetPauseCoordinator(coordinator)
			holders, finish := coordinator.BeginBotStart()
			finish()
			wantHeld := mode != "connectivity_only"
			if (len(holders) > 0) != wantHeld {
				t.Fatal("coordinator binding did not gate required unobserved metrics")
			}
			if wantHeld && (len(holders) != 1 || holders[0].Source != metricsUnavailablePauseSource) {
				t.Fatal("initial metric hold used wrong risk source")
			}
		})
	}
}

func TestMetricsStartupHoldPersistenceFailureRemainsBlocked(t *testing.T) {
	for _, mode := range []string{"upsert", "delete"} {
		t.Run(mode, func(t *testing.T) {
			store := &openingPauseStateTestStore{rows: make(map[string]string)}
			coordinator, err := NewOpeningPauseCoordinatorWithStore(t.Context(), store)
			if err != nil {
				t.Fatal(err)
			}
			cfg := newCircuitBreakerTestConfig()
			cfg.Triggers.MaxDrawdown.Enabled = true
			gcb := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{})
			gcb.SetPauseCoordinator(coordinator)
			cfg.Enabled = true
			if mode == "upsert" {
				store.upsertErr = errors.New("fixture pause store unavailable")
			}
			gcb.UpdateMetricsHealth(MetricsHealth{Available: false, CheckedAt: time.Now(), ValidUntil: time.Now().Add(time.Minute)})
			if mode == "delete" {
				store.deleteErr = errors.New("fixture pause retirement unavailable")
				now := time.Now()
				gcb.UpdateMetricsObservation(MetricsSnapshot{}, MetricsHealth{Available: true, DrawdownAvailable: true, Persisted: true, CashFlowAdjusted: true, CheckedAt: now, ValidUntil: now.Add(time.Minute)})
			}
			holders, finish := coordinator.BeginBotStart()
			finish()
			if len(holders) == 0 {
				t.Fatal("pause persistence failure let future startup escape")
			}
		})
	}
}
