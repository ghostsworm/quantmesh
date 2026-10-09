package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/storage"
)

type fundingPerpSpreadOfflineExchange struct{ *adapterFakeExchange }

type fundingPerpSpreadBaseAssetExchange struct {
	*fundingPerpSpreadOfflineExchange
	baseAsset    string
	accountReads *int
}

func (e *fundingPerpSpreadBaseAssetExchange) GetBaseAsset() string { return e.baseAsset }

func (e *fundingPerpSpreadBaseAssetExchange) GetAccount(ctx context.Context) (*exchange.Account, error) {
	if e.accountReads != nil {
		*e.accountReads++
	}
	return e.fundingPerpSpreadOfflineExchange.GetAccount(ctx)
}

func (*fundingPerpSpreadOfflineExchange) GetAccountOpenOrders(context.Context) ([]*exchange.Order, error) {
	return []*exchange.Order{}, nil
}

func TestFundingPerpSpreadRuntimeStateAdapterFencesBothLegGenerations(t *testing.T) {
	service, _ := newFundingCarryGenerationTestStorage(t)
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "binance-account", SecretKey: "binance-secret"},
		"bybit":   {APIKey: "bybit-account", SecretKey: "bybit-secret"},
	}}
	fp := &config.FundingPerpSpreadConfig{
		LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB: config.FundingPerpLeg{Exchange: "bybit", Symbol: "BTCUSDT"},
	}
	distributedLock := &runtimeLeaseTestLock{}
	firstLeases, err := acquireFundingPerpSpreadRuntimeOwnershipLeases(t.Context(), distributedLock, cfg, fp, nil)
	if err != nil {
		t.Fatal("acquire first two-leg runtime leases:", err)
	}
	t.Cleanup(func() { _ = releaseFundingPerpSpreadRuntimeOwnershipLeases(firstLeases) })
	oldAdapter, err := buildFundingPerpSpreadRuntimeStateAdapter(t.Context(), service, "spread-generation-test", cfg, fp, firstLeases)
	if err != nil {
		t.Fatal("build first fenced runtime adapter:", err)
	}
	initial := `{"state":"initial"}`
	if err := oldAdapter.SaveRuntimeState("funding_perp_spread", 7, initial); err != nil {
		t.Fatal("initial fenced write:", err)
	}

	if err := releaseFundingPerpSpreadRuntimeOwnershipLeases(firstLeases); err != nil {
		t.Fatal("release first two-leg runtime leases:", err)
	}
	currentLeases, err := acquireFundingPerpSpreadRuntimeOwnershipLeases(t.Context(), distributedLock, cfg, fp, nil)
	if err != nil {
		t.Fatal("acquire replacement two-leg runtime leases:", err)
	}
	t.Cleanup(func() { _ = releaseFundingPerpSpreadRuntimeOwnershipLeases(currentLeases) })
	currentAdapter, err := buildFundingPerpSpreadRuntimeStateAdapter(t.Context(), service, "spread-generation-test", cfg, fp, currentLeases)
	if err != nil {
		t.Fatal("build replacement fenced runtime adapter:", err)
	}
	if err := oldAdapter.SaveRuntimeState("funding_perp_spread", 7, `{"state":"stale"}`); !errors.Is(err, storage.ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("stale Save error = %v, want generation-lost", err)
	}
	if saved, err := oldAdapter.CompareAndSwapRuntimeState(t.Context(), "funding_perp_spread", 7, initial, 7, `{"state":"stale-cas"}`); !errors.Is(err, storage.ErrFundingCarryRuntimeGenerationLost) || saved {
		t.Fatalf("stale CAS = saved %v, err %v", saved, err)
	}
	state, err := service.GetStorage().(storage.StrategyRuntimeStateStore).GetStrategyRuntimeState("spread-generation-test", "funding_perp_spread")
	if err != nil || state == nil || state.Payload != initial {
		t.Fatalf("stale owner changed durable state: state=%+v err=%v", state, err)
	}

	if err := currentAdapter.SaveRuntimeState("funding_perp_spread", 7, `{"state":"current"}`); err != nil {
		t.Fatal("current fenced write:", err)
	}
	if saved, err := currentAdapter.CompareAndSwapRuntimeState(context.Background(), "funding_perp_spread", 7, `{"state":"current"}`, 7, `{"state":"current-cas"}`); err != nil || !saved {
		t.Fatalf("current CAS = saved %v, err %v", saved, err)
	}
}

func TestFundingPerpSpreadConstructorBindsFencedStateBeforeStrategyStart(t *testing.T) {
	service, _ := newFundingCarryGenerationTestStorage(t)
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "test-binance-key", SecretKey: "test-binance-secret"},
		"bybit":   {APIKey: "test-bybit-key", SecretKey: "test-bybit-secret"},
	}}
	cfg.Timing.PriceSendInterval = 1
	cfg.Timing.PricePollInterval = 1
	fp := &config.FundingPerpSpreadConfig{
		LegA:             config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB:             config.FundingPerpLeg{Exchange: "bybit", Symbol: "BTCUSDT"},
		MinFundingSpread: 0.001,
		MaxBasisPct:      1,
	}
	symCfg := config.SymbolConfig{
		ID:                    "funding-perp-spread-generation-integration",
		Exchange:              "binance",
		Symbol:                "BTCUSDT",
		MarketType:            config.MarketTypeFundingPerpSpread,
		TotalAllocatedCapital: 100,
		FundingPerpSpread:     fp,
		Strategies:            []config.StrategyInstance{{Type: "funding_perp_spread", Weight: 1}},
	}
	newClient := func(name string) *fundingPerpSpreadOfflineExchange {
		return &fundingPerpSpreadOfflineExchange{adapterFakeExchange: &adapterFakeExchange{
			name:      name,
			price:     50000,
			positions: []*exchange.Position{},
			orders:    []*exchange.Order{},
			account:   &exchange.Account{TotalWalletBalance: 1000, TotalMarginBalance: 1000, BalanceAsset: "USDT"},
		}}
	}
	clients := map[string]*fundingPerpSpreadOfflineExchange{"binance": newClient("binance"), "bybit": newClient("bybit")}
	var calls int
	factory := func(_ *config.Config, name, symbol, market string) (exchange.IExchange, error) {
		calls++
		client := clients[name]
		if client == nil || symbol != "BTCUSDT" || market != "futures" {
			return nil, errors.New("unexpected offline exchange factory request")
		}
		return client, nil
	}
	serviceLock := &runtimeLeaseTestLock{}
	runtime, err := startFundingPerpSpreadSymbolRuntimeWithExchangeFactory(t.Context(), cfg, symCfg, nil,
		service, serviceLock, nil, nil, factory)
	if err != nil {
		t.Fatal("start FundingPerpSpread through the production constructor with offline exchanges:", err)
	}
	if calls != 2 {
		t.Fatalf("exchange factory calls = %d, want both configured legs", calls)
	}
	t.Cleanup(func() {
		if err := runtime.StopWithError(); err != nil {
			t.Errorf("stop offline FundingPerpSpread runtime: %v", err)
		}
	})
	stateStore, ok := service.GetStorage().(storage.StrategyRuntimeStateStore)
	if !ok {
		t.Fatal("SQLite storage does not expose the runtime-state readback contract")
	}
	state, err := stateStore.GetStrategyRuntimeState(fundingPerpSpreadStateScope(symCfg.ID, cfg, fp), "funding_perp_spread")
	if err != nil || state == nil || state.SchemaVersion <= 0 || state.Payload == "" {
		t.Fatalf("production constructor did not persist strategy startup state through its fenced adapter: state=%+v err=%v", state, err)
	}
}

func TestFundingPerpSpreadConstructorRejectsMismatchedBaseBeforeReadingAccounts(t *testing.T) {
	service, _ := newFundingCarryGenerationTestStorage(t)
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "test-binance-key", SecretKey: "test-binance-secret"},
		"bybit":   {APIKey: "test-bybit-key", SecretKey: "test-bybit-secret"},
	}}
	fp := &config.FundingPerpSpreadConfig{
		LegA:             config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB:             config.FundingPerpLeg{Exchange: "bybit", Symbol: "BTCUSDT"},
		MinFundingSpread: 0.001,
		MaxBasisPct:      1,
	}
	symCfg := config.SymbolConfig{
		ID:                    "funding-perp-spread-mismatched-base",
		Exchange:              "binance",
		Symbol:                "BTCUSDT",
		MarketType:            config.MarketTypeFundingPerpSpread,
		TotalAllocatedCapital: 100,
		FundingPerpSpread:     fp,
		Strategies:            []config.StrategyInstance{{Type: "funding_perp_spread", Weight: 1}},
	}
	var legAAccountReads, legBAccountReads int
	legA := &fundingPerpSpreadBaseAssetExchange{
		fundingPerpSpreadOfflineExchange: &fundingPerpSpreadOfflineExchange{adapterFakeExchange: &adapterFakeExchange{
			name: "binance", price: 50000, account: &exchange.Account{TotalMarginBalance: 1000, BalanceAsset: "USDT"},
		}},
		baseAsset: "BTC", accountReads: &legAAccountReads,
	}
	legB := &fundingPerpSpreadBaseAssetExchange{
		fundingPerpSpreadOfflineExchange: &fundingPerpSpreadOfflineExchange{adapterFakeExchange: &adapterFakeExchange{
			name: "bybit", price: 50000, account: &exchange.Account{TotalMarginBalance: 1000, BalanceAsset: "USDT"},
		}},
		baseAsset: "ETH", accountReads: &legBAccountReads,
	}
	factory := func(_ *config.Config, name, symbol, market string) (exchange.IExchange, error) {
		if symbol != "BTCUSDT" || market != "futures" {
			return nil, errors.New("unexpected offline exchange factory request")
		}
		switch name {
		case "binance":
			return legA, nil
		case "bybit":
			return legB, nil
		default:
			return nil, errors.New("unexpected exchange")
		}
	}
	_, err := startFundingPerpSpreadSymbolRuntimeWithExchangeFactory(t.Context(), cfg, symCfg, nil,
		service, &runtimeLeaseTestLock{}, nil, nil, factory)
	if err == nil || !strings.Contains(err.Error(), `same verified base asset; got "BTC" and "ETH"`) {
		t.Fatalf("mismatched base constructor error = %v, want explicit BTC/ETH rejection", err)
	}
	if legAAccountReads != 0 || legBAccountReads != 0 {
		t.Fatalf("account reads before base-asset validation: leg A=%d leg B=%d, want both zero", legAAccountReads, legBAccountReads)
	}
	stateStore, ok := service.GetStorage().(storage.StrategyRuntimeStateStore)
	if !ok {
		t.Fatal("SQLite storage does not expose runtime-state readback")
	}
	state, stateErr := stateStore.GetStrategyRuntimeState(fundingPerpSpreadStateScope(symCfg.ID, cfg, fp), "funding_perp_spread")
	if stateErr != nil || state != nil {
		t.Fatalf("mismatched-base startup persisted strategy state: state=%+v err=%v", state, stateErr)
	}
}
