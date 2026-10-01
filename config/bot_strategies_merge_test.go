package config

import (
	"testing"
)

func TestApplyBotStrategiesToLocalConfig_PureGridLegacy(t *testing.T) {
	local := &Config{}
	local.Strategies.Enabled = true
	local.Strategies.Configs = map[string]StrategyConfig{
		"trend": {Enabled: true, Weight: 1, Config: map[string]interface{}{"x": 1}},
	}
	sym := &SymbolConfig{
		Strategies: []StrategyInstance{
			{Type: "grid", Weight: 1, Config: nil},
		},
	}
	ApplyBotStrategiesToLocalConfig(local, sym)
	if local.Strategies.Enabled {
		t.Fatal("expected legacy grid: Strategies.Enabled false")
	}
	if local.Strategies.Configs != nil {
		t.Fatalf("expected configs cleared, got %#v", local.Strategies.Configs)
	}
}

func TestApplyBotStrategiesToLocalConfigRejectsUnsupportedType(t *testing.T) {
	local := &Config{}
	symbol := &SymbolConfig{Strategies: []StrategyInstance{{Type: "momentum_grid", Weight: 1}}}
	if err := ApplyBotStrategiesToLocalConfig(local, symbol); err == nil {
		t.Fatal("unsupported strategy type must not fall back to the default grid runtime")
	}
	if local.Strategies.Enabled || len(local.Strategies.Configs) != 0 {
		t.Fatalf("failed strategy mapping mutated runtime config: %+v", local.Strategies)
	}
}

func TestApplyBotStrategiesToLocalConfigRejectsDuplicateRuntimeType(t *testing.T) {
	local := &Config{}
	symbol := &SymbolConfig{Strategies: []StrategyInstance{
		{Type: "momentum", Weight: 0.5},
		{Type: "momentum", Weight: 0.5},
	}}
	if err := ApplyBotStrategiesToLocalConfig(local, symbol); err == nil {
		t.Fatal("duplicate strategy mapping must not silently overwrite configuration")
	}
	if local.Strategies.Enabled || len(local.Strategies.Configs) != 0 {
		t.Fatalf("failed strategy mapping mutated runtime config: %+v", local.Strategies)
	}
}

func TestApplyBotStrategiesToLocalConfig_TrendFollowingMapsToTrend(t *testing.T) {
	local := &Config{}
	sym := &SymbolConfig{
		TotalAllocatedCapital: 10000,
		Strategies: []StrategyInstance{
			{
				Type:   "trend_following",
				Weight: 1,
				Config: map[string]interface{}{
					"fast_period": float64(12),
					"slow_period": float64(26),
				},
			},
		},
	}
	ApplyBotStrategiesToLocalConfig(local, sym)
	if !local.Strategies.Enabled {
		t.Fatal("expected Strategies.Enabled true")
	}
	tc, ok := local.Strategies.Configs["trend"]
	if !ok {
		t.Fatal("expected configs[\"trend\"]")
	}
	if !tc.Enabled || tc.Weight != 1 {
		t.Fatalf("trend config: %#v", tc)
	}
	if local.Strategies.CapitalAllocation.TotalCapital != 10000 {
		t.Fatalf("capital: %v", local.Strategies.CapitalAllocation.TotalCapital)
	}
	sp, _ := tc.Config["short_period"].(int)
	lp, _ := tc.Config["long_period"].(int)
	if sp != 12 || lp != 26 {
		t.Fatalf("expected short_period=12 long_period=26, got short=%v long=%v", sp, lp)
	}
}

func TestApplyBotStrategiesToLocalConfig_GridPlusTrend(t *testing.T) {
	local := &Config{}
	sym := &SymbolConfig{
		Strategies: []StrategyInstance{
			{
				Type:   "grid+trend",
				Weight: 1,
				Config: map[string]interface{}{
					"grid_weight":  float64(2),
					"trend_weight": float64(3),
					"fast_period":  float64(5),
					"slow_period":  float64(10),
				},
			},
		},
	}
	ApplyBotStrategiesToLocalConfig(local, sym)
	g, ok := local.Strategies.Configs["grid"]
	if !ok || g.Weight != 0.4 {
		t.Fatalf("grid: %#v", g)
	}
	tr, ok := local.Strategies.Configs["trend"]
	if !ok || tr.Weight != 0.6 {
		t.Fatalf("trend: %#v", tr)
	}
}

func TestShouldSkipInitialGridAdjustOrders(t *testing.T) {
	local := &Config{}
	local.Strategies.Enabled = true
	local.Strategies.Configs = map[string]StrategyConfig{
		"trend": {Enabled: true, Weight: 1},
	}
	if !ShouldSkipInitialGridAdjustOrders(local) {
		t.Fatal("expected skip when no grid")
	}
	local.Strategies.Configs["grid"] = StrategyConfig{Enabled: true, Weight: 0.5}
	if ShouldSkipInitialGridAdjustOrders(local) {
		t.Fatal("expected no skip when grid enabled")
	}
}
