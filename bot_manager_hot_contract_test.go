package main

import (
	"errors"
	"reflect"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

func TestRuntimeHotUpdateMustNotRewriteFinancialContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*config.BotConfig)
	}{
		{"exchange", func(c *config.BotConfig) { c.Exchange = "okx" }},
		{"symbol", func(c *config.BotConfig) { c.Symbol = "ETHUSDT" }},
		{"market", func(c *config.BotConfig) { c.MarketType = "spot" }},
		{"testnet", func(c *config.BotConfig) { c.Testnet = true }},
		{"direction", func(c *config.BotConfig) { c.Direction = "SHORT" }},
		{"strategy", func(c *config.BotConfig) { c.Strategies = []config.StrategyInstance{{Type: "trend_following"}} }},
		{"inventory", func(c *config.BotConfig) { c.SpotInventoryPolicy = "adopt_all" }},
		{"capital", func(c *config.BotConfig) { c.TotalAllocatedCapital = 5000 }},
		{"grid_bounds", func(c *config.BotConfig) { c.PriceLow = 50 }},
		{"specialized_grid_risk", func(c *config.BotConfig) { c.GridRiskControl.Enabled = true }},
		{"specialized_grid_interval", func(c *config.BotConfig) { c.PriceInterval = 200 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", Direction: "LONG", MarketType: "futures", PriceInterval: 100}
			calls := 0
			br := &BotRuntime{BotID: old.ID, Config: old, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(old), UpdateOpenControl: func(config.OpenPositionControl) error { calls++; return nil }}}
			next := old
			tc.change(&next)
			if err := br.applyRuntimeTradingParams(next); !errors.Is(err, errRuntimeConfigurationRequiresRestart) || calls != 0 {
				t.Fatalf("financial contract change reached hot callback: err=%v calls=%d", err, calls)
			}
			if br.Config.Exchange != old.Exchange || br.Config.Symbol != old.Symbol || br.Config.Direction != old.Direction || br.Inner.Config.Symbol != old.Symbol {
				t.Fatal("rejected contract changed runtime identity")
			}
			if !reflect.DeepEqual(br.Config, old) || !reflect.DeepEqual(br.Inner.Config, config.BotConfigToSymbolConfig(old)) {
				t.Fatal("cold change partially published configuration")
			}
		})
	}
}

func TestRuntimeHotContractReportThroughWebAdapter(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	old := config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT"}
	calls := 0
	bm.AddRuntime(&BotRuntime{BotID: old.ID, Config: old, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(old), UpdateOpenControl: func(config.OpenPositionControl) error { calls++; return nil }}})
	next := old
	next.Symbol = "ETHUSDT"
	adapter := &symbolManagerWebAdapter{manager: &SymbolManager{botManager: bm}}
	report := adapter.UpdateTradingParamsWithReport(&config.Config{Bots: []config.BotConfig{next}})
	if !report.Verified || len(report.Applied) != 0 || report.Failed[old.ID] != "runtime_restart_required" || calls != 0 {
		t.Fatalf("contract failure receipt lost: %+v calls=%d", report, calls)
	}
}

func TestRuntimeHotContractKeepsSupportedGridUpdates(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	old := config.BotConfig{ID: "grid", Exchange: "binance", Symbol: "BTCUSDT", PriceInterval: 100}
	br := &BotRuntime{BotID: old.ID, Config: old, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(old), SuperPositionManager: spm}}
	next := old
	next.Name = "updated display name"
	next.PriceInterval, next.ProfitSpread, next.OrderQuantity = 200, 250, 50
	next.BuyWindowSize, next.SellWindowSize = 4, 5
	next.OpenPositionControl.BotRiskControl = &config.BotRiskControl{Enabled: true, MaxPositionValue: 300}
	if err := br.applyRuntimeTradingParams(next); err != nil {
		t.Fatalf("supported grid update rejected: %v", err)
	}
	if br.Config.Name != next.Name || br.Inner.Config.PriceInterval != 200 || spm.GetTradingParamsSummary()["order_quantity"] != float64(50) || br.GetBotRiskControl().MaxPositionValue != 300 {
		t.Fatal("supported hot update not published")
	}
}

func TestRuntimeHotContractEquivalentDefaultsRemainHot(t *testing.T) {
	old := config.BotConfig{Exchange: "binance", Symbol: "BTCUSDT"}
	next := old
	next.ID = config.BotIDOrGenerate(old)
	next.Direction = "LONG"
	next.MarketType = "futures"
	next.SpotInventoryPolicy = "conservative"
	next.PriceInterval = 200
	if !runtimeHotContractMatches(old, next) {
		t.Fatal("explicit equivalent defaults incorrectly require restart")
	}
	margin := config.BotConfig{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", UseSpotMargin: true}
	spot := margin
	spot.UseSpotMargin = false
	if runtimeHotContractMatches(margin, spot) {
		t.Fatal("spot margin and spot treated as equivalent runtime identities")
	}
}
