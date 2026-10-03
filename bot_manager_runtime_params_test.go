package main

import (
	"errors"
	"math"
	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/position"
	"testing"
)

func TestRuntimeSpecializedHotRiskFailureMustBeObservable(t *testing.T) {
	cfg := &config.Config{}
	old := config.BotConfig{ID: "audit-specialized", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, OpenPositionControl: config.OpenPositionControl{BotRiskControl: &config.BotRiskControl{Enabled: true, MaxPositionValue: 100}}}
	calls := 0
	cause := errors.New("fixture apply rejected")
	gate := &execution.OpeningGate{}
	br := &BotRuntime{BotID: old.ID, Config: old, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(old), verifiedCapitalBudget: 500, OpeningGate: gate, UpdateOpenControl: func(config.OpenPositionControl) error { calls++; return cause }}}
	bm := NewBotManager(cfg, nil, nil, nil, "")
	bm.AddRuntime(br)
	next := old
	next.OpenPositionControl = config.CloneOpenPositionControl(old.OpenPositionControl)
	next.OpenPositionControl.BotRiskControl.MaxPositionValue = 400
	latest := &config.Config{Bots: []config.BotConfig{next}}
	updated := bm.UpdateRuntimeTradingParams(latest)
	t.Logf("specialized callbacks=%d updated_ids=%d retained_old_limit=%t rejection_gate=%t", calls, len(updated), br.GetBotRiskControl().MaxPositionValue == 100, gate.HasBlock("risk_control_update_unverified"))
	if calls != 1 || !gate.HasBlock("risk_control_update_unverified") {
		t.Fatal("specialized hot update was silently skipped rather than attempted/rejected")
	}
	br.Inner.UpdateOpenControl = func(control config.OpenPositionControl) error {
		calls++
		if control.BotRiskControl.MaxPositionValue != 400 {
			t.Fatal("retry did not receive requested risk control")
		}
		return nil
	}
	updated = bm.UpdateRuntimeTradingParams(latest)
	if calls != 2 || len(updated) != 1 || br.GetBotRiskControl().MaxPositionValue != 400 || gate.HasBlock("risk_control_update_unverified") {
		t.Fatal("verified specialized retry did not publish and clear its own block")
	}
}

func TestRuntimeRejectedGridRiskMustNotPartiallyPublishParams(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 50
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	old := config.BotConfig{ID: "audit-grid", Exchange: "binance", Symbol: "BTCUSDT", PriceInterval: 100, OrderQuantity: 50, OpenPositionControl: config.OpenPositionControl{BotRiskControl: &config.BotRiskControl{Enabled: true, MaxPositionValue: 100}}}
	gate := &execution.OpeningGate{}
	br := &BotRuntime{BotID: old.ID, Config: old, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(old), SuperPositionManager: spm, verifiedCapitalBudget: math.Inf(1), OpeningGate: gate}}
	bm := NewBotManager(cfg, nil, nil, nil, "")
	bm.AddRuntime(br)
	next := old
	next.PriceInterval = 200
	next.OrderQuantity = 250
	updated := bm.UpdateRuntimeTradingParams(&config.Config{Bots: []config.BotConfig{next}})
	got := spm.GetTradingParamsSummary()
	t.Logf("grid updated_ids=%d manager_interval=%v inner_interval=%v spm_interval=%v spm_quantity=%v invalid_budget_gate=%t", len(updated), br.Config.PriceInterval, br.Inner.Config.PriceInterval, got["price_interval"], got["order_quantity"], gate.HasBlock("verified_capital_budget_invalid"))
	if len(updated) != 0 || br.Inner.Config.PriceInterval != 100 || got["price_interval"] != float64(100) || got["order_quantity"] != float64(50) {
		t.Fatal("rejected risk update partially published parameters and/or reported updated")
	}
}

func TestRuntimeNormalGridHotUpdateControl(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	spm := position.NewSuperPositionManager(cfg, &pauseTestExecutor{}, pauseTestExchange{}, 2, 3)
	old := config.BotConfig{ID: "audit-normal", Exchange: "binance", Symbol: "BTCUSDT", PriceInterval: 100, OrderQuantity: 50}
	br := &BotRuntime{BotID: old.ID, Config: old, Inner: &SymbolRuntime{Config: config.BotConfigToSymbolConfig(old), SuperPositionManager: spm, verifiedCapitalBudget: 500}}
	bm := NewBotManager(cfg, nil, nil, nil, "")
	bm.AddRuntime(br)
	next := old
	next.PriceInterval = 200
	updated := bm.UpdateRuntimeTradingParams(&config.Config{Bots: []config.BotConfig{next}})
	if len(updated) != 1 || spm.GetTradingParamsSummary()["price_interval"] != float64(200) || br.Inner.Config.PriceInterval != 200 {
		t.Fatal("normal control does not update")
	}
}
