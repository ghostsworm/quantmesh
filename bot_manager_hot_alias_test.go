package main

import (
	"testing"

	"quantmesh/config"
)

func TestRuntimeHotUpdateDoesNotAttachIncomingColdReferences(t *testing.T) {
	makeConfig := func() config.BotConfig {
		enabled := true
		return config.BotConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", Enabled: &enabled,
			Strategies:        []config.StrategyInstance{{Type: "grid", Config: map[string]interface{}{"nested": []interface{}{float64(2)}}}},
			FundingPerpSpread: &config.FundingPerpSpreadConfig{LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"}},
			Profiles:          map[string]config.ProfileConfig{"normal": {PriceInterval: 100}},
			SlotFilter:        config.SlotFilterConfig{Rules: []config.SlotFilterRule{{Prices: []float64{100}}}},
		}
	}
	old, next := makeConfig(), makeConfig()
	calls := 0
	br := &BotRuntime{BotID: old.ID, Config: old, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(old), UpdateOpenControl: func(config.OpenPositionControl) error { calls++; return nil }}}
	next.OpenPositionControl.PauseOpening = true
	if err := br.applyRuntimeTradingParams(next); err != nil {
		t.Fatal(err)
	}
	*next.Enabled = false
	next.Strategies[0].Config["nested"].([]interface{})[0] = float64(999)
	next.FundingPerpSpread.LegA.Symbol = "ETHUSDT"
	next.Profiles["normal"] = config.ProfileConfig{PriceInterval: 999}
	next.SlotFilter.Rules[0].Prices[0] = 999
	if !br.Config.IsEnabled() || br.Config.Strategies[0].Config["nested"].([]interface{})[0] != float64(2) ||
		br.Config.FundingPerpSpread.LegA.Symbol != "BTCUSDT" || br.Config.Profiles["normal"].PriceInterval != 100 || br.Config.SlotFilter.Rules[0].Prices[0] != 100 {
		t.Fatal("post-update caller mutation bypassed runtime cold contract guard")
	}
	if br.Inner.Config.Strategies[0].Config["nested"].([]interface{})[0] != float64(2) || br.Inner.Config.FundingPerpSpread.LegA.Symbol != "BTCUSDT" {
		t.Fatal("incoming cold references leaked into Inner configuration")
	}
	if !br.Config.OpenPositionControl.PauseOpening {
		t.Fatal("reference isolation lost supported hot update")
	}
	if err := br.applyRuntimeTradingParams(next); err == nil || calls != 1 {
		t.Fatalf("later mutated cold contract bypassed application gate: %v calls=%d", err, calls)
	}
}
