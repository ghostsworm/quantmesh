package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"
)

var errConstructorStreamCleanup = errors.New("fixture stream cleanup failed")

type cleanupFaultConstructorVenue struct {
	*cleanConstructorVenue
	fail atomic.Bool
}

func (v *cleanupFaultConstructorVenue) StopOrderStream() error {
	err := v.constructorRecoveryVenue.StopOrderStream()
	if v.fail.Load() {
		return errors.Join(err, errConstructorStreamCleanup)
	}
	return err
}

func TestFundingCarryFullConstructorCleanupFailureRetainsCapital(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "StopBot", true: "StopAll"}[all], func(t *testing.T) { testFundingCarryConstructorCleanupWithStorage(t, newEnableStateStorage, all) })
	}
}

func testFundingCarryConstructorCleanupWithStorage(t *testing.T, newStorage func(*testing.T) *BotManager, all bool) {
	testFundingCarryConstructorCleanupScenario(t, newStorage, all, false)
}

func TestFundingCarryFullConstructorUnknownFinancialStopDoesNotRetryCleanup(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "StopBot", true: "StopAll"}[all], func(t *testing.T) { testFundingCarryConstructorCleanupScenario(t, newEnableStateStorage, all, true) })
	}
}

func testFundingCarryConstructorCleanupScenario(t *testing.T, newStorage func(*testing.T) *BotManager, all, financialUnknown bool) {
	bm := newStorage(t)
	bm.eventBus = event.NewEventBus(8)
	t.Cleanup(bm.eventBus.Close)
	cfg := bm.cfg
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}
	cfg.Timing.PriceSendInterval = 100
	provider := &runtimeLeaseTestLock{}
	venues := map[string]*cleanupFaultConstructorVenue{}
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
		},
		newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
			v := &cleanupFaultConstructorVenue{cleanConstructorVenue: &cleanConstructorVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}}
			venues[market] = v
			return v, nil
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	sym := config.SymbolConfig{ID: "cleanup-owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
		Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
	rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, bm.eventBus, bm.storageService, provider, nil, nil, deps)
	if err != nil || rt == nil {
		t.Fatalf("full constructor failed: %v", err)
	}
	venues["spot"].fail.Store(true)
	if financialUnknown {
		rt.StrategyManager.GetStrategy("funding_carry").(*strategy.FundingCarryStrategy).MarkExecutionUnknown(errors.New("fixture financial outcome unresolved"))
	}
	br := &BotRuntime{BotID: "cleanup-owner", Config: config.SymbolConfigToBotConfig(sym, false), Inner: rt}
	bm.AddRuntime(br)
	stop := func() error { return bm.StopBot("cleanup-owner") }
	if all {
		stop = bm.StopAll
	}
	if financialUnknown {
		assertConstructorUnknownFinancialCleanupWithheld(t, bm, br, provider, venues, stop)
		return
	}
	first := stop()
	if !errors.Is(first, errConstructorStreamCleanup) || isRetryableRuntimeStopVerification(first) || !isRetryableRuntimeStopCleanup(first) || !br.stopCleanupPending.Load() {
		t.Fatalf("cleanup failure was ignored or became a verification retry: %v", first)
	}
	original := rt.shutdownCloseUnverified.Load()
	if retry := stop(); !errors.Is(retry, errConstructorStreamCleanup) || !isRetryableRuntimeStopCleanup(retry) || rt.shutdownCloseUnverified.Load() != original {
		t.Fatal("still-failing cleanup lost error", retry)
	}
	if err := bm.EnableBot(br.BotID); err == nil {
		t.Fatal("pending stream cleanup accepted enable")
	}
	if _, err := bm.StartBot(t.Context(), br.Config); err == nil {
		t.Fatal("pending stream cleanup admitted duplicate runtime")
	}
	report := bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{br.Config}})
	if len(report.Applied) != 0 || report.Failed[br.BotID] != "bot_stop_cleanup_pending" {
		t.Fatal("pending cleanup accepted hot update", report)
	}
	adapter := &botManagerProviderAdapter{manager: &SymbolManager{botManager: bm}}
	if detail, ok := adapter.GetBot(br.BotID); !ok || !detail.StopPending || detail.Running {
		t.Fatal("actual status hides pending cleanup")
	}
	claims, err := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
	if err != nil || len(claims) != 3 {
		t.Fatalf("cleanup failure released capital claims: count=%d err=%v", len(claims), err)
	}
	provider.mu.Lock()
	owned := len(provider.held)
	provider.mu.Unlock()
	if owned < 3 {
		t.Fatal("cleanup failure surrendered runtime ownership")
	}
	for market, v := range venues {
		want := int32(1)
		if market == "spot" {
			want = 2
		}
		if v.mutations.Load() != 0 || v.stops.Load() != want {
			t.Fatal("cleanup retry did not retry only the failed stream without financial replay")
		}
	}
	venues["spot"].fail.Store(false)
	if err := stop(); err != nil {
		t.Fatal("recovered stream cleanup still cached forever", err)
	}
	claims, err = bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
	if err != nil || len(claims) != 0 {
		t.Fatal("recovered cleanup did not release independently verified SQL capital", err)
	}
	provider.mu.Lock()
	owned = len(provider.held)
	provider.mu.Unlock()
	if owned != 0 || rt.shutdownCloseUnverified.Load() != nil {
		t.Fatal("recovered cleanup retained ownership or own failure")
	}
	if _, ok := bm.Get(br.BotID); ok {
		t.Fatal("completed cleanup retained controller")
	}
	if err := rt.StopWithError(); err != nil {
		t.Fatal("completed stop was not idempotent", err)
	}
	for market, v := range venues {
		want := int32(1)
		if market == "spot" {
			want = 3
		}
		if v.stops.Load() != want || v.mutations.Load() != 0 {
			t.Fatal("completed stop replayed stream or finance")
		}
	}
}

func assertConstructorUnknownFinancialCleanupWithheld(t *testing.T, bm *BotManager, br *BotRuntime, provider *runtimeLeaseTestLock, venues map[string]*cleanupFaultConstructorVenue, stop func() error) {
	t.Helper()
	store := bm.storageService.GetStorage().(storage.StrategyRuntimeStateStore)
	before, err := store.GetStrategyRuntimeState(br.BotID, "funding_carry")
	if err != nil || before == nil {
		t.Fatal("unknown SQL checkpoint missing", err)
	}
	var state struct {
		Unknown bool `json:"exposure_unknown"`
	}
	if err := json.Unmarshal([]byte(before.Payload), &state); err != nil || !state.Unknown {
		t.Fatal("fixture did not persist actual unknown state", err)
	}
	if err := stop(); err == nil || isRetryableRuntimeStopCleanup(err) || isRetryableRuntimeStopVerification(err) {
		t.Fatal("generic financial UNKNOWN became retryable cleanup", err)
	}
	original := br.Inner.shutdownCloseUnverified.Load()
	if original == nil {
		t.Fatal("generic financial failure disappeared")
	}
	if err := bm.EnableBot(br.BotID); err == nil {
		t.Error("generic financial UNKNOWN accepted enable")
	}
	if _, err := bm.StartBot(t.Context(), br.Config); err == nil {
		t.Error("generic financial UNKNOWN accepted duplicate start")
	}
	report := bm.UpdateRuntimeTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{br.Config}})
	if len(report.Applied) != 0 || report.Failed[br.BotID] != "bot_stop_reconciliation_pending" {
		t.Errorf("generic financial UNKNOWN accepted hot update: %+v", report)
	}
	adapter := &botManagerProviderAdapter{manager: &SymbolManager{botManager: bm}}
	if detail, ok := adapter.GetBot(br.BotID); !ok || detail.Running || !detail.StopPending {
		t.Error("actual status hides generic financial pending stop")
	}
	assertConstructorUnknownPendingLists(t, bm, br, adapter)
	venues["spot"].fail.Store(false)
	if err := stop(); err == nil || isRetryableRuntimeStopCleanup(err) || br.Inner.shutdownCloseUnverified.Load() != original {
		t.Fatal("stream recovery erased financial fault", err)
	}
	after, err := store.GetStrategyRuntimeState(br.BotID, "funding_carry")
	if err != nil || after == nil || after.Payload != before.Payload || after.SchemaVersion != before.SchemaVersion {
		t.Fatal("generic stop rewrote unknown provenance", err)
	}
	claims, err := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
	if err != nil || len(claims) != 3 {
		t.Fatal("generic unknown released SQL capital", err)
	}
	provider.mu.Lock()
	held := len(provider.held)
	provider.mu.Unlock()
	if held < 3 {
		t.Fatal("generic unknown surrendered runtime ownership")
	}
	for _, v := range venues {
		if v.stops.Load() != 1 || v.mutations.Load() != 0 {
			t.Fatal("generic unknown retried cleanup or financial operations")
		}
	}
}

func assertConstructorUnknownPendingLists(t *testing.T, bm *BotManager, br *BotRuntime, adapter *botManagerProviderAdapter) {
	t.Helper()
	oldConfig, oldStore := web.GetConfig(), web.GetPrimaryStorageForAppConfig()
	defer func() {
		web.SetPrimaryStorageForAppConfig(oldStore)
		if oldConfig == nil {
			web.SetFileConfigManager(nil)
			return
		}
		restored := web.NewFileConfigManager("")
		if err := restored.SetRuntimeConfig(oldConfig); err != nil {
			t.Error(err)
		}
		web.SetFileConfigManager(restored)
	}()
	web.SetPrimaryStorageForAppConfig(nil)
	for _, legacy := range []bool{false, true} {
		cfg := &config.Config{Bots: []config.BotConfig{br.Config}, Exchanges: bm.cfg.Exchanges}
		if legacy {
			cfg.Bots = nil
			cfg.Trading.Symbols = []config.SymbolConfig{br.Inner.Config}
		}
		fcm := web.NewFileConfigManager("")
		if err := fcm.SetRuntimeConfig(cfg); err != nil {
			t.Fatal(err)
		}
		web.SetFileConfigManager(fcm)
		list := adapter.ListBots()
		if len(list) != 1 || list[0].BotID != br.BotID || list[0].Running || !list[0].StopPending {
			t.Errorf("actual list hides generic financial pending stop (legacy=%v): %+v", legacy, list)
		}
	}
}
