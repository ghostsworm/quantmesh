package main

import (
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/risk"
)

func TestEquityScopeChangeInvalidatesOpeningAdmissionBeforeNewSnapshot(t *testing.T) {
	account := config.ExchangeConfig{APIKey: "account-a-key", SecretKey: "account-a-secret", Testnet: true}
	initial := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": account},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	botManager := NewBotManager(initial, nil, nil, nil, "")
	symbolManager := &SymbolManager{botManager: botManager}

	circuitConfig := &config.CircuitBreakerConfig{Enabled: true}
	circuitConfig.Triggers.MaxDrawdown.Enabled = true
	breaker := risk.NewGlobalCircuitBreaker(circuitConfig, nil, nil)
	coordinator := risk.NewOpeningPauseCoordinator()
	breaker.SetPauseCoordinator(coordinator)
	feeder := risk.NewMetricsFeeder(breaker, nil, nil, nil, risk.MetricsFeederOptions{})
	wireEquityScopeChangeInvalidation(symbolManager, feeder)

	healthy := risk.MetricsHealth{Available: true, DrawdownAvailable: true, CashFlowAdjusted: true, Persisted: true,
		CheckedAt: time.Now(), ValidUntil: time.Now().Add(time.Minute)}
	breaker.UpdateMetricsHealth(healthy)
	if !coordinator.OpeningAdmissionAllowed() {
		t.Fatal("verified current-scope health did not permit admission")
	}

	unrelated := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": account},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}}}
	botManager.registerEquityScopeConfig(unrelated)
	if !coordinator.OpeningAdmissionAllowed() {
		t.Fatal("symbol-only change invalidated unchanged account-scope health")
	}

	rotated := account
	rotated.SecretKey = "rotated-account-secret"
	changedScope := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": rotated},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}}}
	botManager.registerEquityScopeConfig(changedScope)
	if coordinator.OpeningAdmissionAllowed() || !coordinator.IsHeldBy("risk_metrics_unavailable") {
		t.Fatal("credential-scope change left stale healthy admission open")
	}

	breaker.UpdateMetricsHealth(healthy)
	if !coordinator.OpeningAdmissionAllowed() {
		t.Fatal("freshly verified scope health did not release its independent hold")
	}
}
