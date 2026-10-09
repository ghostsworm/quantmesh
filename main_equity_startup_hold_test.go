package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/risk"
	"quantmesh/storage"
)

func TestEquityUnavailableHoldReachesCompleteFundingCarryConstructor(t *testing.T) {
	testEquityStartupHoldConstructor(t, false)
}

func TestInitialEquityHoldReachesCompleteFundingCarryConstructor(t *testing.T) {
	testEquityStartupHoldConstructor(t, true)
}

func testEquityStartupHoldConstructor(t *testing.T, initial bool) {
	t.Helper()
	bm := newEnableStateStorage(t)
	cfg := bm.cfg
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}
	cfg.Timing.PriceSendInterval = 100
	store := bm.storageService.GetStorage().(storage.OpeningPauseStateStore)
	migrator := bm.storageService.GetStorage().(interface{ MigrateOpeningPauseHolders(context.Context) error })
	if err := migrator.MigrateOpeningPauseHolders(t.Context()); err != nil {
		t.Fatal(err)
	}
	coordinator, err := risk.NewOpeningPauseCoordinatorWithStore(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}
	bm.SetOpeningPauseCoordinator(coordinator)
	provider := &botManagerProviderAdapter{manager: &SymbolManager{botManager: bm}}
	riskCfg := &config.CircuitBreakerConfig{}
	riskCfg.Triggers.MaxDrawdown.Enabled = true
	breaker := risk.NewGlobalCircuitBreaker(riskCfg, nil, provider)
	if initial {
		riskCfg.Enabled = true
	}
	breaker.SetPauseCoordinator(coordinator)
	riskCfg.Enabled = true // no background ticker in this isolated test
	if !initial {
		breaker.UpdateMetricsHealth(risk.MetricsHealth{Available: false, CheckedAt: time.Now(), ValidUntil: time.Now().Add(time.Minute)})
	}
	rows, err := store.LoadOpeningPauseHolders(t.Context())
	hasMetricsSource, hasLegacySource := false, false
	for _, row := range rows {
		hasMetricsSource = hasMetricsSource || row.Source == "risk_metrics_unavailable"
		hasLegacySource = hasLegacySource || row.Source == "opening_pause_legacy_state_unverified"
	}
	if err != nil || len(rows) != 2 || !hasMetricsSource || !hasLegacySource {
		sources := make([]string, 0, len(rows))
		for _, row := range rows {
			sources = append(sources, row.Source)
		}
		t.Fatalf("missing actual SQL startup hold: rows=%d sources=%v coordinatorHeld=%v error=%v", len(rows), sources, coordinator.IsHeldBy("risk_metrics_unavailable"), err)
	}
	holders, finish := coordinator.BeginBotStart()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var admissionCalls atomic.Int64
	var forceAdmissionFailure atomic.Bool
	ctx = execution.WithOpeningAdmissionCheck(ctx, func() bool {
		admissionCalls.Add(1)
		return !forceAdmissionFailure.Load() && coordinator.OpeningAdmissionAllowed()
	})
	sym := config.SymbolConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
		Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
		},
		newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
			return &cleanConstructorVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}, nil
		},
	}
	rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, nil, bm.storageService, lock.NewNopLock(), nil, holders, deps)
	if err == nil && rt != nil {
		bm.AddRuntime(&BotRuntime{BotID: "owner", Config: config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry}, Inner: rt})
	}
	finish()
	if err != nil || rt == nil {
		t.Fatalf("complete constructor failed: %v", err)
	}
	t.Cleanup(func() { _ = rt.StopWithError() })
	metricsSource := ""
	for _, holder := range holders {
		if !rt.OpeningGate.HasBlock(holder.Source) {
			t.Fatal("complete constructor omitted inherited risk source")
		}
		if strings.HasSuffix(holder.Source, ":risk_metrics_unavailable") {
			metricsSource = holder.Source
		}
	}
	if len(holders) != 2 || metricsSource == "" {
		t.Fatal("startup did not inherit exact metrics and legacy holds")
	}
	now := time.Now()
	breaker.UpdateMetricsObservation(risk.MetricsSnapshot{}, risk.MetricsHealth{Available: true, DrawdownAvailable: true, Persisted: true, CashFlowAdjusted: true, CheckedAt: now, ValidUntil: now.Add(time.Minute)})
	if rt.OpeningGate.HasBlock(metricsSource) {
		t.Fatal("healthy update did not release inherited exact source")
	}
	rows, err = store.LoadOpeningPauseHolders(t.Context())
	if err != nil || len(rows) != 1 || rows[0].Source != "opening_pause_legacy_state_unverified" || !rt.OpeningGate.HasBlock(rows[0].Source) {
		t.Fatalf("healthy update lost SQL source retirement: %v", err)
	}
	// Keep the real legacy SQL holder intact. Test the callback after removing
	// the fixture's legacy gate locally only, never deleting its SQL row.
	rt.OpeningGate.Unblock(rows[0].Source)
	release, admissionErr := rt.OpeningGate.Begin()
	if admissionErr != nil || admissionCalls.Load() == 0 {
		t.Fatalf("complete constructor omitted live admission: %v", admissionErr)
	}
	release()
	forceAdmissionFailure.Store(true)
	if _, err := rt.OpeningGate.Begin(); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatal("complete constructor ignored live admission failure")
	}
	forceAdmissionFailure.Store(false)
	rt.OpeningGate.Block(rows[0].Source)
}
