package risk

import (
	"errors"
	"testing"
	"time"

	"quantmesh/execution"
)

func TestMetricsExpiryAdmissionWithoutBackgroundCheck(t *testing.T) {
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
			breaker := NewGlobalCircuitBreaker(cfg, nil, &circuitBreakerMockProvider{})
			cfg.Enabled = true // deliberately no background checker
			coordinator := NewOpeningPauseCoordinator()
			breaker.SetPauseCoordinator(coordinator)
			var gate execution.OpeningGate
			gate.ApplyAdmissionContext(execution.WithOpeningAdmissionCheck(t.Context(), coordinator.OpeningAdmissionAllowed))
			now := time.Now()
			health := MetricsHealth{Available: true, DrawdownAvailable: true, Persisted: true, CashFlowAdjusted: true, CheckedAt: now, ValidUntil: now.Add(time.Minute)}
			breaker.UpdateMetricsHealth(health)
			release, err := gate.Begin()
			if err != nil {
				t.Fatalf("healthy admission rejected: %v", err)
			}
			release()
			// Move only the observation's expiry, without publishing a hold or
			// calling checkTriggers. This isolates the admission-time read.
			breaker.statusMu.Lock()
			breaker.metricsHealth.ValidUntil = now.Add(-time.Second)
			breaker.statusMu.Unlock()
			if coordinator.IsHeldBy(metricsUnavailablePauseSource) || len(gate.Sources()) != 0 {
				t.Fatal("fixture installed a cached pause instead of testing expiry")
			}
			release, err = gate.Begin()
			if mode == "connectivity_only" {
				if err != nil {
					t.Fatal(err)
				}
				release()
			} else if !errors.Is(err, execution.ErrOpeningPaused) || !gate.Blocked() {
				t.Fatal("expired metrics escaped admission before background check")
			}
			breaker.UpdateMetricsHealth(health)
			release, err = gate.Begin()
			if err != nil {
				t.Fatal("renewed observation did not recover admission")
			}
			release()
			gate.Block("manual_fixture")
			breaker.UpdateMetricsHealth(health)
			if _, err := gate.Begin(); !errors.Is(err, execution.ErrOpeningPaused) {
				t.Fatal("healthy recovery erased independent hold")
			}
		})
	}
}
