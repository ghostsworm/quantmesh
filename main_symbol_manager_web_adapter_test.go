package main

import (
	"context"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
)

func newWebAdapterTestManager() (*SymbolManager, *config.Config) {
	cfg := &config.Config{}
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {}}
	cfg.Trading.Symbols = []config.SymbolConfig{
		{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"},
		{Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"},
	}
	manager := NewSymbolManager(cfg, event.NewEventBus(4), nil, nil, "")
	manager.Add(&SymbolRuntime{Config: cfg.Trading.Symbols[0], Exchange: &adapterFakeExchange{name: "spot-ex"}})
	manager.Add(&SymbolRuntime{Config: cfg.Trading.Symbols[1], Exchange: &adapterFakeExchange{name: "futures-ex"}})
	return manager, cfg
}

func TestSymbolManagerWebAdapterLookupPaths(t *testing.T) {
	manager, cfg := newWebAdapterTestManager()
	adapter := &symbolManagerWebAdapter{
		manager:  manager,
		ctx:      context.Background(),
		cfg:      cfg,
		eventBus: event.NewEventBus(4),
	}

	if got, ok := adapter.Get("binance", "BTCUSDT"); !ok || got.(*SymbolRuntime).Config.GetMarketType() != "spot" {
		t.Fatalf("Get spot = %+v/%v", got, ok)
	}
	if got, ok := adapter.GetEx("binance", "ETHUSDT", "futures"); !ok || got.(*SymbolRuntime).Config.Symbol != "ETHUSDT" {
		t.Fatalf("GetEx futures = %+v/%v", got, ok)
	}
	if got, ok := adapter.GetEx("binance", "MISSING", "spot"); ok || got != nil {
		t.Fatalf("GetEx missing = %+v/%v", got, ok)
	}

	botID := config.GenerateBotID("binance", "BTCUSDT", "spot")
	if got, ok := adapter.GetByBotID(botID); !ok || got.(*SymbolRuntime).Config.Symbol != "BTCUSDT" {
		t.Fatalf("GetByBotID() = %+v/%v", got, ok)
	}
	if got, ok := adapter.GetByBotID(" "); ok || got != nil {
		t.Fatalf("GetByBotID(blank) = %+v/%v", got, ok)
	}
	if got := adapter.List(); len(got) != 2 {
		t.Fatalf("List() length = %d, want 2", len(got))
	}
	if mt := adapter.resolveMarketType("binance", "BTCUSDT"); mt != "spot" {
		t.Fatalf("resolveMarketType(config) = %q, want spot", mt)
	}
	if mt := adapter.resolveMarketType("binance", "UNKNOWN"); mt != "futures" {
		t.Fatalf("resolveMarketType(default) = %q, want futures", mt)
	}
}

func TestSymbolManagerWebAdapterAmbiguousAndErrorPaths(t *testing.T) {
	cfg := &config.Config{}
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {}}
	cfg.Trading.Symbols = []config.SymbolConfig{
		{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"},
		{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"},
	}
	manager := NewSymbolManager(cfg, event.NewEventBus(4), nil, nil, "")
	manager.Add(&SymbolRuntime{Config: cfg.Trading.Symbols[0], Exchange: &adapterFakeExchange{name: "spot-ex"}})
	manager.Add(&SymbolRuntime{Config: cfg.Trading.Symbols[1], Exchange: &adapterFakeExchange{name: "futures-ex"}})
	adapter := &symbolManagerWebAdapter{
		manager:  manager,
		ctx:      context.Background(),
		cfg:      cfg,
		eventBus: event.NewEventBus(4),
	}

	if got, ok := adapter.Get("binance", "BTCUSDT"); ok || got != nil {
		t.Fatalf("ambiguous Get should fail, got %+v/%v", got, ok)
	}
	if err := adapter.StartSymbol("binance", "MISSING", ""); err == nil {
		t.Fatal("StartSymbol should reject missing symbol config")
	}
	if err := adapter.StartSymbol("binance", "BTCUSDT", "margin"); err == nil {
		t.Fatal("StartSymbol should reject when all candidates are already running")
	}
	if err := adapter.StartSymbol("binance", "BTCUSDT", "spot"); err == nil {
		t.Fatal("StartSymbol should reject duplicate running spot symbol")
	}
	if err := adapter.StopSymbol("binance", "MISSING"); err == nil {
		t.Fatal("StopSymbol should reject missing runtime")
	}
	if _, err := adapter.ClosePositions("binance", "MISSING"); err == nil {
		t.Fatal("ClosePositions should reject missing runtime")
	}

	emptyCfg := &config.Config{}
	emptyCfg.Exchanges = map[string]config.ExchangeConfig{"binance": {}}
	emptyManager := NewSymbolManager(emptyCfg, event.NewEventBus(1), nil, nil, "")
	emptyAdapter := &symbolManagerWebAdapter{manager: emptyManager, ctx: context.Background(), cfg: emptyCfg}
	if mt := emptyAdapter.resolveMarketType("binance", "BTCUSDT"); mt != "futures" {
		t.Fatalf("empty resolveMarketType = %q", mt)
	}
}

func TestSymbolManagerWebAdapterUpdateTradingParamsDelegates(t *testing.T) {
	manager, cfg := newWebAdapterTestManager()
	adapter := &symbolManagerWebAdapter{manager: manager, ctx: context.Background(), cfg: cfg}
	latest := &config.Config{}
	latest.Trading.PriceInterval = 42
	_ = adapter.UpdateTradingParams(latest)
}
