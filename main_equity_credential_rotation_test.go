package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange/accounting"
	"quantmesh/risk"
	"quantmesh/storage"
)

func TestRuntimeEquityUsesCurrentCredentialsAfterRotation(t *testing.T) {
	now := time.Now().Add(-time.Second)
	oldCredentials := config.ExchangeConfig{APIKey: "stable-account-key", SecretKey: "old-secret", Testnet: true}
	initial := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": oldCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	manager := &SymbolManager{botManager: NewBotManager(initial, nil, nil, nil, "")}
	oldRuntimeSource := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	runtime := configuredWalletRuntimeFixture("binance", "futures", oldCredentials, oldRuntimeSource)
	manager.botManager.AddRuntime(&BotRuntime{BotID: "binance-btc", Inner: runtime})

	currentCredentials := oldCredentials
	currentCredentials.SecretKey = "new-secret"
	updated := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": currentCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	manager.botManager.registerEquityScopeConfig(updated)

	configuredSource := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "2500", 2500)}
	factoryCalls := 0
	var observedSecret string
	source := &runtimeEquitySource{manager: manager, accountEvidenceSourceFactory: func(_ context.Context, account equityAccountEvidenceConfig) (accounting.Source, error) {
		factoryCalls++
		observedSecret = account.SecretKey
		return configuredSource, nil
	}}
	observation, err := source.ObserveAccountEquity(t.Context(), nil)
	if err != nil {
		t.Fatalf("observe current configured account credentials: %v", err)
	}
	if factoryCalls != 1 || observedSecret != currentCredentials.SecretKey {
		t.Fatalf("current credential source calls=%d secret=%q, want one source with configured credential version", factoryCalls, observedSecret)
	}
	if oldRuntimeSource.evidenceCalls != 0 || configuredSource.evidenceCalls != 1 || observation.Equity != 2500 {
		t.Fatalf("equity reused stale runtime credentials: old reads=%d configured reads=%d equity=%v", oldRuntimeSource.evidenceCalls, configuredSource.evidenceCalls, observation.Equity)
	}

	blockedSource := &runtimeEquitySource{manager: manager, accountEvidenceSourceFactory: func(context.Context, equityAccountEvidenceConfig) (accounting.Source, error) {
		return nil, errors.New("current credentials are unavailable")
	}}
	if staleObservation, err := blockedSource.ObserveAccountEquity(t.Context(), nil); err == nil || staleObservation.CashFlowComplete || oldRuntimeSource.evidenceCalls != 0 {
		t.Fatalf("unavailable current credentials fell back to stale runtime evidence: observation=%+v err=%v stale_reads=%d", staleObservation, err, oldRuntimeSource.evidenceCalls)
	}
}

func TestMetricsFeederRevalidatesAfterCredentialRotationWithConfiguredSource(t *testing.T) {
	oldCredentials := config.ExchangeConfig{APIKey: "stable-account-key", SecretKey: "old-secret", Testnet: true}
	initial := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": oldCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	manager := &SymbolManager{botManager: NewBotManager(initial, nil, nil, nil, "")}
	now := time.Now().Add(-10 * time.Second).Truncate(time.Millisecond)
	oldRuntimeSource := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	runtime := configuredWalletRuntimeFixture("binance", "futures", oldCredentials, oldRuntimeSource)
	manager.botManager.AddRuntime(&BotRuntime{BotID: "binance-btc", Inner: runtime})

	db, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "equity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.MigrateRiskCheckpoints(t.Context()); err != nil {
		t.Fatal(err)
	}

	circuitConfig := &config.CircuitBreakerConfig{Enabled: true}
	circuitConfig.Triggers.MaxDrawdown.Enabled = true
	breaker := risk.NewGlobalCircuitBreaker(circuitConfig, nil, nil)
	coordinator := risk.NewOpeningPauseCoordinator()
	breaker.SetPauseCoordinator(coordinator)
	options := risk.MetricsFeederOptions{Now: func() time.Time { return now }, MaxEquityAge: time.Minute,
		EquityStore: &persistedEquityState{backend: db}, RequirePersistence: true, RequireCashFlowReconciliation: true}
	configuredSource := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "1000", 1000)}
	factoryCalls := 0
	currentSourceUnavailable := false
	source := &runtimeEquitySource{manager: manager, accountEvidenceSourceFactory: func(_ context.Context, account equityAccountEvidenceConfig) (accounting.Source, error) {
		factoryCalls++
		if account.SecretKey != "new-secret" {
			t.Fatal("configured source received credentials other than the rotated version")
		}
		if currentSourceUnavailable {
			return nil, errors.New("current credentials are unavailable")
		}
		return configuredSource, nil
	}}
	feeder := risk.NewMetricsFeeder(breaker, nil, source, nil, options)
	wireEquityScopeChangeInvalidation(manager, feeder)
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatalf("initial old-credential observation: %v", err)
	}
	if !coordinator.OpeningAdmissionAllowed() || oldRuntimeSource.evidenceCalls != 1 {
		t.Fatal("initial verified runtime evidence did not establish the expected baseline")
	}

	rotatedCredentials := oldCredentials
	rotatedCredentials.SecretKey = "new-secret"
	updated := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": rotatedCredentials},
		Bots: []config.BotConfig{{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}}}
	manager.botManager.registerEquityScopeConfig(updated)
	if coordinator.OpeningAdmissionAllowed() || !coordinator.IsHeldBy("risk_metrics_unavailable") {
		t.Fatal("credential rotation failed to invalidate old risk health")
	}

	now = now.Add(time.Second)
	currentSourceUnavailable = true
	if _, err := feeder.Tick(t.Context()); err == nil || coordinator.OpeningAdmissionAllowed() {
		t.Fatal("failed current-credential source unexpectedly restored verified admission")
	}
	if oldRuntimeSource.evidenceCalls != 1 || configuredSource.evidenceCalls != 0 {
		t.Fatalf("failed current-source attempt fell back to stale runtime: stale=%d current=%d", oldRuntimeSource.evidenceCalls, configuredSource.evidenceCalls)
	}
	currentSourceUnavailable = false
	now = now.Add(time.Second)
	configuredSource.snapshot = runtimeWalletFixture(now, "1000", 1000)
	configuredSource.snapshot.Wallet.From = oldRuntimeSource.snapshot.Wallet.From
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatalf("revalidate using current configured read-only credentials: %v", err)
	}
	if factoryCalls != 2 || configuredSource.evidenceCalls != 1 || oldRuntimeSource.evidenceCalls != 1 {
		t.Fatalf("unexpected evidence sources: factory=%d current=%d stale=%d", factoryCalls, configuredSource.evidenceCalls, oldRuntimeSource.evidenceCalls)
	}
	health := breaker.GetMetricsHealth()
	if !coordinator.OpeningAdmissionAllowed() || !health.Available || !health.DrawdownAvailable || !health.CashFlowAdjusted || !health.Persisted {
		t.Fatal("freshly reconciled configured credentials did not restore verified admission")
	}
}
