package strategy

import (
	"testing"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/position"
)

// newBoundsTestAdjuster 所有動態調整開關打開，但不配置任何 Min/Max
func newBoundsTestAdjuster(t *testing.T) (*config.Config, *position.SuperPositionManager, *DynamicAdjuster) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Symbols = []config.SymbolConfig{{Symbol: "BTCUSDT"}}
	cfg.Trading.PriceInterval = 3
	cfg.Trading.BuyWindowSize = 20
	cfg.Trading.SellWindowSize = 20
	cfg.Trading.OrderQuantity = 200
	cfg.Trading.DynamicAdjustment.PriceInterval.Enabled = true
	cfg.Trading.DynamicAdjustment.WindowSize.Enabled = true
	cfg.Trading.DynamicAdjustment.OrderQuantity.Enabled = true

	manager := position.NewSuperPositionManager(cfg, &signalTestExecutor{}, &signalTestExchange{}, 2, 3)
	return cfg, manager, NewDynamicAdjuster(cfg, nil, manager)
}

func TestDynamicAdjusterExtremeVolatilityUsesDefaultBoundsWhenUnconfigured(t *testing.T) {
	cfg, _, da := newBoundsTestAdjuster(t)

	da.adjustForExtremeVolatility()

	if cfg.Trading.PriceInterval != defaultMaxPriceInterval {
		t.Fatalf("price interval=%v, want default max %v (not 0)", cfg.Trading.PriceInterval, defaultMaxPriceInterval)
	}
	if cfg.Trading.BuyWindowSize != defaultMinWindowSize || cfg.Trading.SellWindowSize != defaultMinWindowSize {
		t.Fatalf("windows=%d/%d, want default min %d", cfg.Trading.BuyWindowSize, cfg.Trading.SellWindowSize, defaultMinWindowSize)
	}
	if cfg.Trading.OrderQuantity != defaultMinOrderQuantity {
		t.Fatalf("order quantity=%v, want default min %v (not 0)", cfg.Trading.OrderQuantity, defaultMinOrderQuantity)
	}
}

func TestDynamicAdjusterLowVolatilityNeverShrinksIntervalBelowMin(t *testing.T) {
	cases := []struct {
		name       string
		configMin  float64
		current    float64
		wantResult float64
	}{
		{name: "unconfigured min floors at default", configMin: 0, current: 0.55, wantResult: defaultMinPriceInterval},
		{name: "zero interval raised to default min", configMin: 0, current: 0, wantResult: defaultMinPriceInterval},
		{name: "configured min respected", configMin: 2, current: 2.1, wantResult: 2},
		{name: "normal shrink", configMin: 1, current: 5, wantResult: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _, da := newBoundsTestAdjuster(t)
			cfg.Trading.DynamicAdjustment.PriceInterval.Min = tc.configMin
			cfg.Trading.PriceInterval = tc.current

			da.adjustForLowVolatility()

			if cfg.Trading.PriceInterval != tc.wantResult {
				t.Fatalf("interval=%v, want %v", cfg.Trading.PriceInterval, tc.wantResult)
			}
		})
	}
}

func TestDynamicAdjusterWindowSizeRefusesWithoutUtilizationData(t *testing.T) {
	cfg, _, da := newBoundsTestAdjuster(t)

	if _, ok := da.CalculateUtilization(); ok {
		t.Fatalf("utilization should be unavailable without position allocation")
	}
	da.AdjustWindowSize()
	da.AdjustWindowSize()
	if cfg.Trading.BuyWindowSize != 20 || cfg.Trading.SellWindowSize != 20 {
		t.Fatalf("window size changed without real utilization: %d/%d", cfg.Trading.BuyWindowSize, cfg.Trading.SellWindowSize)
	}
	if !da.utilizationWarned.Load() {
		t.Fatalf("expected one-time warning flag to be set")
	}
}

func newVolatilityPauseAdjuster(t *testing.T) (*config.Config, *position.SuperPositionManager, *DynamicAdjuster) {
	t.Helper()
	cfg, manager, da := newBoundsTestAdjuster(t)
	botRisk := &config.BotRiskControl{Enabled: true, VolatilityPauseEnabled: true}
	botRisk.VolatilityPauseConfig.PauseOnExtremeVolatility = true
	botRisk.VolatilityPauseConfig.AutoResumeOnNormal = true
	cfg.Trading.Symbols[0].OpenPositionControl.BotRiskControl = botRisk
	manager.SetOpenPositionControl(config.OpenPositionControl{BotRiskControl: botRisk})
	t.Cleanup(da.Stop)
	return cfg, manager, da
}

func TestDynamicAdjusterVolatilityPauseResumesAtNormalRegime(t *testing.T) {
	_, manager, da := newVolatilityPauseAdjuster(t)

	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeExtreme})
	if !manager.IsOpeningPaused() || !isVolatilityPauseReason(manager.GetOpeningPauseReason()) {
		t.Fatalf("extreme regime should pause opening, reason=%q", manager.GetOpeningPauseReason())
	}

	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeNormal})
	if manager.IsOpeningPaused() {
		t.Fatalf("normal regime should resume volatility pause (previously only RegimeLow resumed)")
	}
}

func TestDynamicAdjusterVolatilityResumeDoesNotLiftRiskPause(t *testing.T) {
	_, manager, da := newVolatilityPauseAdjuster(t)
	const riskReason = "circuit_breaker: drawdown"
	manager.PauseOpening(riskReason)

	// 熔斷暫停期間出現極端波動：不能把原因改寫成波動率暫停
	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeExtreme})
	if manager.GetOpeningPauseReason() != riskReason {
		t.Fatalf("volatility pause overwrote risk reason: %q", manager.GetOpeningPauseReason())
	}

	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeLow})
	if !manager.IsOpeningPaused() || manager.GetOpeningPauseReason() != riskReason {
		t.Fatalf("volatility resume must not lift risk pause, paused=%v reason=%q",
			manager.IsOpeningPaused(), manager.GetOpeningPauseReason())
	}
}

func TestDynamicAdjusterAutoResumeDisabledKeepsPause(t *testing.T) {
	cfg, manager, da := newVolatilityPauseAdjuster(t)
	cfg.Trading.Symbols[0].OpenPositionControl.BotRiskControl.VolatilityPauseConfig.AutoResumeOnNormal = false
	manager.SetOpenPositionControl(cfg.Trading.Symbols[0].OpenPositionControl)

	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeExtreme})
	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeLow})
	if !manager.IsOpeningPaused() {
		t.Fatalf("auto resume disabled: pause should remain")
	}
}
