package main

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
)

func TestRuntimeKeyDefaultsMarketType(t *testing.T) {
	if got := runtimeKey("binance", "BTCUSDT"); got != "binance:btcusdt:futures" {
		t.Fatalf("runtimeKey default = %q", got)
	}
	if got := runtimeKey("binance", "BTCUSDT", "spot"); got != "binance:btcusdt:spot" {
		t.Fatalf("runtimeKey spot = %q", got)
	}
}

func TestApplyProfileOverridesOnlyPositiveValues(t *testing.T) {
	base := config.SymbolConfig{
		PriceInterval:  10,
		ProfitSpread:   2,
		OrderQuantity:  100,
		BuyWindowSize:  5,
		SellWindowSize: 6,
		MinOrderValue:  20,
	}
	profile := config.ProfileConfig{
		PriceInterval:  15,
		ProfitSpread:   0,
		OrderQuantity:  200,
		BuyWindowSize:  7,
		SellWindowSize: 8,
		MinOrderValue:  30,
	}

	got := applyProfile(base, profile)
	if got.PriceInterval != 15 || got.OrderQuantity != 200 || got.BuyWindowSize != 7 || got.SellWindowSize != 8 || got.MinOrderValue != 30 {
		t.Fatalf("profile overrides not applied: %#v", got)
	}
	if got.ProfitSpread != 2 {
		t.Fatalf("zero profile ProfitSpread should keep base value, got %v", got.ProfitSpread)
	}
}

func TestSelectProfileByFundingAndFeeRules(t *testing.T) {
	cfg := config.SymbolConfig{
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "futures",
		Profiles: map[string]config.ProfileConfig{
			"positive": {PriceInterval: 20},
			"negative": {PriceInterval: 5},
		},
	}
	cfg.SwitchRules.FundingRate.Threshold = 0.01

	positive, name := selectProfile(context.Background(), cfg, &fakeFundingExchange{rate: 0.02}, 0, nil)
	if name != "positive" || positive.PriceInterval != 20 {
		t.Fatalf("expected positive funding profile, got name=%q profile=%#v", name, positive)
	}

	negative, name := selectProfile(context.Background(), cfg, &fakeFundingExchange{rate: -0.02}, 0, nil)
	if name != "negative" || negative.PriceInterval != 5 {
		t.Fatalf("expected negative funding profile, got name=%q profile=%#v", name, negative)
	}

	cfg.SwitchRules.FundingRate.Threshold = 0
	cfg.SwitchRules.FeeRate.Threshold = 0.001
	feeProfile, name := selectProfile(context.Background(), cfg, nil, 0.002, nil)
	if name != "positive" || feeProfile.PriceInterval != 20 {
		t.Fatalf("expected positive fee profile, got name=%q profile=%#v", name, feeProfile)
	}

	feeProfile, name = selectProfile(context.Background(), cfg, nil, 0.0005, nil)
	if name != "negative" || feeProfile.PriceInterval != 5 {
		t.Fatalf("expected negative fee profile, got name=%q profile=%#v", name, feeProfile)
	}
}

func TestSelectProfileReturnsDefaultWithoutProfiles(t *testing.T) {
	profile, name := selectProfile(context.Background(), config.SymbolConfig{}, nil, 0, nil)
	if name != "" || profile.PriceInterval != 0 {
		t.Fatalf("expected default profile, got name=%q profile=%#v", name, profile)
	}
}

func TestSymbolManagerAddGetListAndRemove(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {Testnet: true},
		},
	}
	manager := NewSymbolManager(cfg, event.NewEventBus(8), nil, nil, "")
	rt := &SymbolRuntime{Config: config.SymbolConfig{
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "spot",
	}}

	manager.Add(rt)
	got, ok := manager.Get("binance", "BTCUSDT", "spot")
	if !ok || got != rt {
		t.Fatalf("Get returned %#v ok=%v", got, ok)
	}
	if _, ok := manager.Get("binance", "BTCUSDT", "futures"); ok {
		t.Fatal("futures runtime should not exist")
	}
	if list := manager.List(); len(list) != 1 || list[0] != rt {
		t.Fatalf("List = %#v, want runtime", list)
	}
	manager.Remove("binance", "BTCUSDT", "spot")
	if _, ok := manager.Get("binance", "BTCUSDT", "spot"); ok {
		t.Fatal("runtime should be removed")
	}
	manager.StopAll()
}

func TestSymbolManagerWebAdapterLookupHelpers(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {Testnet: false},
		},
	}
	manager := NewSymbolManager(cfg, event.NewEventBus(8), nil, nil, "")
	spot := &SymbolRuntime{Config: config.SymbolConfig{
		Exchange:   "binance",
		Symbol:     "ETHUSDT",
		MarketType: "spot",
	}}
	futures := &SymbolRuntime{Config: config.SymbolConfig{
		Exchange:   "binance",
		Symbol:     "ETHUSDT",
		MarketType: "futures",
	}}
	custom := &SymbolRuntime{Config: config.SymbolConfig{
		ID:         "custom-bot",
		Exchange:   "binance",
		Symbol:     "XRPUSDT",
		MarketType: "spot",
	}}
	manager.Add(spot)
	manager.Add(futures)
	manager.Add(custom)

	adapter := &symbolManagerWebAdapter{manager: manager, cfg: cfg, ctx: context.Background()}
	if got, ok := adapter.GetEx("binance", "ETHUSDT", "spot"); !ok || got != spot {
		t.Fatalf("GetEx spot returned %#v ok=%v", got, ok)
	}
	if got, ok := adapter.GetEx("binance", "ETHUSDT", "futures"); !ok || got != futures {
		t.Fatalf("GetEx futures returned %#v ok=%v", got, ok)
	}
	if got, ok := adapter.Get("binance", "ETHUSDT"); ok || got != nil {
		t.Fatalf("ambiguous Get returned %#v ok=%v, want nil false", got, ok)
	}
	if got, ok := adapter.GetByBotID("custom-bot"); !ok || got != custom {
		t.Fatalf("GetByBotID returned %#v ok=%v", got, ok)
	}
	if got, ok := adapter.GetByBotID(" "); ok || got != nil {
		t.Fatalf("blank GetByBotID returned %#v ok=%v", got, ok)
	}
	if list := adapter.List(); len(list) != 3 {
		t.Fatalf("adapter List length = %d, want 3", len(list))
	}
	if got := adapter.resolveMarketType("binance", "ETHUSDT"); got != "spot" && got != "futures" {
		t.Fatalf("resolveMarketType = %s, want spot or futures", got)
	}
	if got := adapter.resolveMarketType("binance", "MISSING"); got != "futures" {
		t.Fatalf("missing resolveMarketType = %s, want futures", got)
	}
	if _, err := adapter.GetAllStrategyStatus("binance", "MISSING"); err == nil {
		t.Fatal("missing strategy status should return error")
	}
	if got, err := adapter.GetAllStrategyStatus("binance", "ETHUSDT"); err != nil || len(got) != 0 {
		t.Fatalf("empty strategy status = %#v err=%v", got, err)
	}
	if got, err := adapter.GetStrategyStatus("binance", "ETHUSDT", "grid"); err != nil || got != nil {
		t.Fatalf("missing single strategy status = %#v err=%v", got, err)
	}
}

type fakeFundingExchange struct {
	exchange.IExchange
	rate float64
}

func (e *fakeFundingExchange) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return e.rate, nil
}
