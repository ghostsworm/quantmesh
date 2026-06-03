package strategy

import (
	"testing"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/position"
)

func TestDynamicAdjusterCalculationsAndVolatilityRegimeAdjustments(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbols = []config.SymbolConfig{{Symbol: "BTCUSDT"}}
	cfg.Trading.PriceInterval = 10
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.SellWindowSize = 10
	cfg.Trading.OrderQuantity = 200
	cfg.Trading.DynamicAdjustment.PriceInterval.Enabled = true
	cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityWindow = 3
	cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityThreshold = 0.001
	cfg.Trading.DynamicAdjustment.PriceInterval.AdjustmentStep = 2
	cfg.Trading.DynamicAdjustment.PriceInterval.Min = 4
	cfg.Trading.DynamicAdjustment.PriceInterval.Max = 30
	cfg.Trading.DynamicAdjustment.WindowSize.Enabled = true
	cfg.Trading.DynamicAdjustment.WindowSize.AdjustmentStep = 3
	cfg.Trading.DynamicAdjustment.WindowSize.BuyWindow.Min = 4
	cfg.Trading.DynamicAdjustment.WindowSize.BuyWindow.Max = 20
	cfg.Trading.DynamicAdjustment.WindowSize.SellWindow.Min = 4
	cfg.Trading.DynamicAdjustment.WindowSize.SellWindow.Max = 20
	cfg.Trading.DynamicAdjustment.OrderQuantity.Enabled = true
	cfg.Trading.DynamicAdjustment.OrderQuantity.Min = 50
	cfg.Trading.DynamicAdjustment.OrderQuantity.Max = 300
	cfg.Trading.DynamicAdjustment.OrderQuantity.AdjustmentStep = 25

	manager := &position.SuperPositionManager{}
	da := NewDynamicAdjuster(cfg, nil, manager)
	if da.currentSymbol != "BTCUSDT" {
		t.Fatalf("current symbol=%s", da.currentSymbol)
	}
	da.addPrice(100)
	if da.CalculateVolatility() != 0 {
		t.Fatalf("single price volatility should be zero")
	}
	da.addPrice(103)
	da.addPrice(98)
	da.addPrice(106)
	if da.CalculateVolatility() == 0 {
		t.Fatalf("volatility should be positive")
	}
	oldInterval := cfg.Trading.PriceInterval
	da.AdjustPriceInterval()
	if cfg.Trading.PriceInterval == oldInterval {
		t.Fatalf("price interval should change")
	}
	da.updatePriceInterval(12)
	if cfg.Trading.PriceInterval != 12 {
		t.Fatalf("update price interval failed")
	}

	oldBuy := cfg.Trading.BuyWindowSize
	da.AdjustWindowSize()
	if cfg.Trading.BuyWindowSize == oldBuy {
		t.Fatalf("window size should change")
	}
	da.updateWindowSize(8, 9)
	if cfg.Trading.BuyWindowSize != 8 || cfg.Trading.SellWindowSize != 9 {
		t.Fatalf("update window size failed")
	}

	oldQty := cfg.Trading.OrderQuantity
	da.AdjustOrderQuantity()
	if cfg.Trading.OrderQuantity == oldQty {
		t.Fatalf("order quantity should change")
	}
	da.updateOrderQuantity(123)
	if cfg.Trading.OrderQuantity != 123 {
		t.Fatalf("update quantity failed")
	}
	if da.CalculateUtilization() != 0.5 {
		t.Fatalf("placeholder utilization mismatch")
	}

	da.adjustForVolatilityRegime(indicators.RegimeLow, "info")
	da.adjustForVolatilityRegime(indicators.RegimeNormal, "info")
	da.adjustForVolatilityRegime(indicators.RegimeHigh, "warning")
	da.adjustForVolatilityRegime(indicators.RegimeHigh, "critical")
	da.adjustForVolatilityRegime(indicators.RegimeExtreme, "critical")
	if cfg.Trading.PriceInterval != cfg.Trading.DynamicAdjustment.PriceInterval.Max {
		t.Fatalf("extreme regime should set max interval")
	}
	if cfg.Trading.BuyWindowSize != cfg.Trading.DynamicAdjustment.WindowSize.BuyWindow.Min ||
		cfg.Trading.SellWindowSize != cfg.Trading.DynamicAdjustment.WindowSize.SellWindow.Min {
		t.Fatalf("extreme regime should set min windows")
	}
	if cfg.Trading.OrderQuantity != cfg.Trading.DynamicAdjustment.OrderQuantity.Min {
		t.Fatalf("extreme regime should set min quantity")
	}

	da.handleVolatilityRegimeChange(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeHigh, Severity: "warning"})
	if da.currentRegime != indicators.RegimeHigh {
		t.Fatalf("current regime not updated")
	}
	if da.GetCurrentVolatilityRegime() != indicators.RegimeNormal || !da.IsGridFriendly() || da.GetVolatilityRiskLevel() != 3 {
		t.Fatalf("nil volatility alert fallback mismatch")
	}
	if stats := da.GetVolatilityStatistics(); stats["enabled"] != false {
		t.Fatalf("fallback stats=%#v", stats)
	}

	for _, price := range []float64{100, 101, 102, 104, 106} {
		da.updateTrend(price)
	}
	if da.currentTrend != "up" {
		t.Fatalf("trend=%s", da.currentTrend)
	}
	for _, price := range []float64{104, 100, 96, 92} {
		da.updateTrend(price)
	}
	if da.currentTrend != "down" {
		t.Fatalf("trend=%s", da.currentTrend)
	}
	da.Stop()
}
