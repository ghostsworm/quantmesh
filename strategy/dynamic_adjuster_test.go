package strategy

import (
	"quantmesh/config"
	"testing"
)

func TestDynamicAdjuster_AdjustPriceInterval(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.PriceInterval = 2.0
	cfg.Trading.DynamicAdjustment.PriceInterval.Enabled = true
	cfg.Trading.DynamicAdjustment.PriceInterval.Min = 1.0
	cfg.Trading.DynamicAdjustment.PriceInterval.Max = 5.0
	cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityThreshold = 0.01 // 1%
	cfg.Trading.DynamicAdjustment.PriceInterval.AdjustmentStep = 0.5
	cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityWindow = 5

	da := NewDynamicAdjuster(cfg, nil, nil)

	// 场景 1: 高波动率 (增加价格间隔)
	// 制造 2% 的波动
	pricesHighVol := []float64{100, 102, 100, 102, 100, 102}
	da.priceHistory = pricesHighVol
	da.AdjustPriceInterval()
	if cfg.Trading.PriceInterval <= 2.0 {
		t.Errorf("高波动下价格间隔应增加, 得到 %.2f", cfg.Trading.PriceInterval)
	}

	// 场景 2: 低波动率 (减少价格间隔)
	cfg.Trading.PriceInterval = 2.0
	pricesLowVol := []float64{100, 100.1, 100, 100.1, 100, 100.1}
	da.priceHistory = pricesLowVol
	da.AdjustPriceInterval()
	if cfg.Trading.PriceInterval >= 2.0 {
		t.Errorf("低波动下价格间隔应减少, 得到 %.2f", cfg.Trading.PriceInterval)
	}
}

func TestDynamicAdjuster_AdjustWindowSize(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.SellWindowSize = 10
	cfg.Trading.DynamicAdjustment.WindowSize.Enabled = true
	cfg.Trading.DynamicAdjustment.WindowSize.BuyWindow.Min = 5
	cfg.Trading.DynamicAdjustment.WindowSize.BuyWindow.Max = 20
	cfg.Trading.DynamicAdjustment.WindowSize.SellWindow.Min = 5
	cfg.Trading.DynamicAdjustment.WindowSize.SellWindow.Max = 20
	cfg.Trading.DynamicAdjustment.WindowSize.UtilizationThreshold = 0.8
	cfg.Trading.DynamicAdjustment.WindowSize.AdjustmentStep = 2

	da := NewDynamicAdjuster(cfg, nil, nil)

	// 场景 1: 资金利用率低 (增加窗口) - 目前 CalculateUtilization 返回 0.5 < 0.8
	da.AdjustWindowSize()
	if cfg.Trading.BuyWindowSize <= 10 {
		t.Errorf("低利用率下窗口应增加, 得到 %d", cfg.Trading.BuyWindowSize)
	}
}

