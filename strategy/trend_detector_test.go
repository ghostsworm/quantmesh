package strategy

import (
	"quantmesh/config"
	"testing"
)

func TestTrendDetector_DetectTrend(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.SmartPosition.TrendDetection.Enabled = true
	cfg.Trading.SmartPosition.TrendDetection.ShortPeriod = 3
	cfg.Trading.SmartPosition.TrendDetection.LongPeriod = 5
	cfg.Trading.SmartPosition.TrendDetection.Method = "ma"

	td := NewTrendDetector(cfg, nil)

	// 场景 1: 上涨趋势 (MA3 > MA5 且 价格 > MA3)
	// MA5: (100+101+102+103+104)/5 = 102
	// MA3: (102+103+104)/3 = 103
	// Current: 105
	pricesUp := []float64{100, 101, 102, 103, 104, 105}
	td.priceHistory = pricesUp
	trend := td.DetectTrend()
	if trend != TrendUp {
		t.Errorf("应检测为上涨趋势, 得到 %s", trend)
	}

	// 场景 2: 下跌趋势 (MA3 < MA5 且 价格 < MA3)
	// MA5: (100+99+98+97+96)/5 = 98
	// MA3: (98+97+96)/3 = 97
	// Current: 95
	pricesDown := []float64{100, 99, 98, 97, 96, 95}
	td.priceHistory = pricesDown
	trend = td.DetectTrend()
	if trend != TrendDown {
		t.Errorf("应检测为下跌趋势, 得到 %s", trend)
	}

	// 场景 3: 震荡 (不满足上涨或下跌条件)
	// MA5: (100+100+100+100+100)/5 = 100
	// MA3: (100+100+100)/3 = 100
	// Current: 100
	pricesSide := []float64{100, 100, 100, 100, 100, 100}
	td.priceHistory = pricesSide
	trend = td.DetectTrend()
	if trend != TrendSide {
		t.Errorf("应检测为震荡趋势, 得到 %s", trend)
	}
}

func TestTrendDetector_AdjustWindows(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BuyWindowSize = 10
	cfg.Trading.SellWindowSize = 10
	cfg.Trading.SmartPosition.WindowAdjustment.MaxAdjustment = 0.5
	cfg.Trading.SmartPosition.WindowAdjustment.MinBuyWindow = 5
	cfg.Trading.SmartPosition.WindowAdjustment.MinSellWindow = 5

	td := NewTrendDetector(cfg, nil)

	// 上涨趋势：减少买单，增加卖单
	td.currentTrend = TrendUp
	buy, sell := td.AdjustWindows()
	if buy >= 10 || sell <= 10 {
		t.Errorf("上涨趋势调整错误: buy=%d, sell=%d", buy, sell)
	}

	// 下跌趋势：增加买单，减少卖单
	td.currentTrend = TrendDown
	buy, sell = td.AdjustWindows()
	if buy <= 10 || sell >= 10 {
		t.Errorf("下跌趋势调整错误: buy=%d, sell=%d", buy, sell)
	}
}

