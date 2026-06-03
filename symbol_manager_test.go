package main

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
)

func TestSymbolManagerRuntimeMapAndProfileSelection(t *testing.T) {
	cfg := config.CreateMinimalConfig()
	cfg.Exchanges["binance"] = config.ExchangeConfig{Testnet: true}
	manager := NewSymbolManager(cfg, event.NewEventBus(16), nil, nil, "")

	rt := &SymbolRuntime{Config: config.SymbolConfig{
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "spot",
	}}
	manager.Add(rt)
	if got, ok := manager.Get("binance", "BTCUSDT", "spot"); !ok || got != rt {
		t.Fatalf("get runtime=%#v ok=%v", got, ok)
	}
	if len(manager.List()) != 1 {
		t.Fatalf("list length mismatch")
	}
	if manager.GetBotManager() == nil {
		t.Fatalf("bot manager should be available")
	}
	manager.Remove("binance", "BTCUSDT", "spot")
	if _, ok := manager.Get("binance", "BTCUSDT", "spot"); ok {
		t.Fatalf("runtime should be removed")
	}
	manager.StopAll()

	if runtimeKey("BINANCE", "BTCUSDT") != config.GenerateBotID("BINANCE", "BTCUSDT", "futures") {
		t.Fatalf("runtime key default mismatch")
	}
	if runtimeKey("binance", "ETHUSDT", "spot") != config.GenerateBotID("binance", "ETHUSDT", "spot") {
		t.Fatalf("runtime key spot mismatch")
	}

	base := config.SymbolConfig{
		Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures",
		PriceInterval: 10, ProfitSpread: 20, OrderQuantity: 30, BuyWindowSize: 2, SellWindowSize: 3, MinOrderValue: 40,
		Profiles: map[string]config.ProfileConfig{
			"positive": {PriceInterval: 11, ProfitSpread: 22, OrderQuantity: 33, BuyWindowSize: 4, SellWindowSize: 5, MinOrderValue: 44},
			"negative": {PriceInterval: 7},
		},
	}
	base.SwitchRules.FundingRate.Threshold = 0.01
	profile, name := selectProfile(context.Background(), base, &symbolProfileExchange{fundingRate: 0.02}, 0, nil)
	if name != "positive" || profile.PriceInterval != 11 {
		t.Fatalf("positive profile=%#v name=%s", profile, name)
	}
	profile, name = selectProfile(context.Background(), base, &symbolProfileExchange{fundingRate: -0.02}, 0, nil)
	if name != "negative" || profile.PriceInterval != 7 {
		t.Fatalf("negative profile=%#v name=%s", profile, name)
	}
	noProfile, name := selectProfile(context.Background(), config.SymbolConfig{}, nil, 0, nil)
	if name != "" || noProfile.PriceInterval != 0 {
		t.Fatalf("no profile=%#v name=%s", noProfile, name)
	}
	base.SwitchRules.FundingRate.Threshold = 0
	base.SwitchRules.FeeRate.Threshold = 0.001
	if _, name := selectProfile(context.Background(), base, nil, 0.002, nil); name != "positive" {
		t.Fatalf("fee positive profile not selected")
	}
	if _, name := selectProfile(context.Background(), base, nil, 0.0002, nil); name != "negative" {
		t.Fatalf("fee negative profile not selected")
	}

	applied := applyProfile(base, base.Profiles["positive"])
	if applied.PriceInterval != 11 || applied.ProfitSpread != 22 || applied.OrderQuantity != 33 ||
		applied.BuyWindowSize != 4 || applied.SellWindowSize != 5 || applied.MinOrderValue != 44 {
		t.Fatalf("applied profile=%#v", applied)
	}
}

type symbolProfileExchange struct {
	exchange.IExchange
	fundingRate float64
}

func (e *symbolProfileExchange) GetFundingRate(context.Context, string) (float64, error) {
	return e.fundingRate, nil
}
