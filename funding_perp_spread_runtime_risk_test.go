package main

import (
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/strategy"
)

func TestFundingPerpSpreadRuntimeAppliesBotRiskWithinVerifiedBudget(t *testing.T) {
	local := &config.Config{}
	local.Trading.OpenPositionControl.MaxPositionValue = 1200
	bot := config.SymbolConfig{OpenPositionControl: config.OpenPositionControl{
		BotRiskControl: &config.BotRiskControl{
			Enabled:             true,
			MaxPositionQuantity: 2.5,
			MaxPositionValue:    900,
			MaxPositionLayers:   2,
		},
	}}

	if err := applyFundingPerpSpreadRiskControls(local, bot, 500); err != nil {
		t.Fatalf("applyFundingPerpSpreadRiskControls() error = %v", err)
	}
	quantity, value, layers := local.Trading.OpenPositionControl.PositionLimits()
	if quantity != 2.5 || value != 500 || layers != 2 {
		t.Fatalf("applied Bot limits = (%v,%v,%v), want (2.5,500,2)", quantity, value, layers)
	}
	if local.Trading.OpenPositionControl.MaxPositionValue != 500 {
		t.Fatalf("global notional backstop = %v, want verified budget 500", local.Trading.OpenPositionControl.MaxPositionValue)
	}
}

func TestFundingPerpSpreadRuntimeRiskCallbacksClampAndApplyHotUpdates(t *testing.T) {
	cfg := &config.Config{}
	st := strategy.NewFundingPerpSpreadStrategy("funding_perp_spread", cfg, config.SymbolConfig{}, nil, nil,
		&config.FundingPerpSpreadConfig{LegA: config.FundingPerpLeg{Symbol: "BTCUSDT"}, LegB: config.FundingPerpLeg{Symbol: "BTCUSDT"}}, nil)
	rt := &SymbolRuntime{}
	configureFundingPerpSpreadRiskCallbacks(rt, st, 400)

	clamped, err := rt.ClampOpenControl(config.OpenPositionControl{MaxPositionValue: 900, MaxPositionQuantity: 3})
	if err != nil {
		t.Fatalf("ClampOpenControl() error = %v", err)
	}
	if clamped.MaxPositionValue != 400 || clamped.MaxPositionQuantity != 3 {
		t.Fatalf("clamped controls = %+v, want notional 400 and quantity 3", clamped)
	}
	if err := rt.UpdateOpenControl(clamped); err != nil {
		t.Fatalf("UpdateOpenControl() error = %v", err)
	}
	if got := rt.GetOpenControl(); got.MaxPositionValue != 400 || got.MaxPositionQuantity != 3 {
		t.Fatalf("runtime controls = %+v, want applied clamped controls", got)
	}
	if err := rt.UpdateOpenControl(config.OpenPositionControl{MaxPositionQuantity: math.NaN()}); err == nil {
		t.Fatal("UpdateOpenControl() accepted a non-finite active quantity limit")
	}
}
