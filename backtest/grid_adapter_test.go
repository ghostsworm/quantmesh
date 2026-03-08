package backtest

import (
	"testing"

	"quantmesh/exchange"
)

func TestRunGridBacktest_IntrabarCrossing(t *testing.T) {
	// 模擬 K 線：每根內有 High/Low 波動，但收盤價相鄰變化小於 130
	// 原邏輯僅用收盤價會導致零交易；修復後應能檢測 K 線內穿越
	candles := []*exchange.Candle{
		{Open: 67900, High: 68000, Low: 67700, Close: 67850, Timestamp: 1000},
		{Open: 67850, High: 67950, Low: 67650, Close: 67780, Timestamp: 2000},
		{Open: 67780, High: 67880, Low: 67580, Close: 67650, Timestamp: 3000},
		{Open: 67650, High: 67750, Low: 67450, Close: 67520, Timestamp: 4000},
		{Open: 67520, High: 67620, Low: 67320, Close: 67400, Timestamp: 5000},
	}
	params := GridBacktestParams{
		PriceLow:      67300,
		PriceHigh:     68100,
		GridSpacing:   130,
		GridCount:     40,
		OrderQuantity: 500,
		TotalCapital:  10000,
		FeeRate:       0.0004,
	}
	result, err := RunGridBacktest("BTCUSDT", candles, params, 10000, nil)
	if err != nil {
		t.Fatalf("RunGridBacktest: %v", err)
	}
	if len(result.Trades) == 0 {
		t.Error("預期有交易：K 線內有穿越 130 間距的檔位，但交易數為 0")
	}
	t.Logf("交易次數: %d, 買入/賣出: %d/%d", result.Metrics.TotalTrades,
		result.Metrics.BuyCount, result.Metrics.SellCount)
}

func TestRunGridBacktest_SHORT(t *testing.T) {
	// 做空網格：價格上漲賣出開空，價格下跌買入平空
	candles := []*exchange.Candle{
		{Open: 67400, High: 67700, Low: 67300, Close: 67650, Timestamp: 1000}, // 上漲，開空
		{Open: 67650, High: 67750, Low: 67450, Close: 67520, Timestamp: 2000}, // 下跌，平空
		{Open: 67520, High: 67820, Low: 67420, Close: 67700, Timestamp: 3000}, // 上漲，開空
		{Open: 67700, High: 67800, Low: 67500, Close: 67580, Timestamp: 4000}, // 下跌，平空
	}
	params := GridBacktestParams{
		PriceLow:      67300,
		PriceHigh:    67900,
		GridSpacing:  150,
		GridCount:    10,
		OrderQuantity: 500,
		TotalCapital: 10000,
		FeeRate:       0.0004,
		Direction:     "SHORT",
	}
	result, err := RunGridBacktest("BTCUSDT", candles, params, 10000, nil)
	if err != nil {
		t.Fatalf("RunGridBacktest SHORT: %v", err)
	}
	if result.Metrics.SellCount == 0 {
		t.Error("做空網格預期有賣出(開空)，但 SellCount=0")
	}
	if result.Metrics.BuyCount == 0 {
		t.Error("做空網格預期有買入(平空)，但 BuyCount=0")
	}
	t.Logf("SHORT 交易次數: %d, 買入(平空)/賣出(開空): %d/%d", result.Metrics.TotalTrades,
		result.Metrics.BuyCount, result.Metrics.SellCount)
}

func TestBuildGridLevelsBySpacingFromHigh(t *testing.T) {
	// 大區間 + maxCount 限制：從高位往下構建，確保檔位覆蓋價格高位
	levels := buildGridLevelsBySpacingFromHigh(60000, 75000, 70, 20)
	if len(levels) != 20 {
		t.Fatalf("預期 20 個檔位，得到 %d", len(levels))
	}
	// 最高檔位應接近 priceHigh
	if levels[len(levels)-1] < 74000 {
		t.Errorf("最高檔位 %.2f 應接近 75000，但偏離太遠", levels[len(levels)-1])
	}
	// 最低檔位不應低於 priceHigh - 70*19 = 73670
	if levels[0] < 73000 {
		t.Errorf("最低檔位 %.2f 應在 73000 以上", levels[0])
	}
	t.Logf("SHORT 檔位範圍: %.2f ~ %.2f", levels[0], levels[len(levels)-1])

	// 對比 LONG（從下限構建）
	longLevels := buildGridLevelsBySpacing(60000, 75000, 70, 20)
	if longLevels[len(longLevels)-1] > 62000 {
		t.Errorf("LONG 最高檔位 %.2f 應在 62000 以下", longLevels[len(longLevels)-1])
	}
	t.Logf("LONG 檔位範圍: %.2f ~ %.2f", longLevels[0], longLevels[len(longLevels)-1])
}

func TestRunGridBacktest_SHORT_AutoDerivedRange(t *testing.T) {
	// 模擬真實場景：大價格區間 + spacing + maxCount → 此前 SHORT 方向會零交易
	// 價格從 63000 波動到 67000，K 線最低谷 60200、最高峰 68000
	candles := make([]*exchange.Candle, 100)
	for i := range candles {
		base := 63000.0 + float64(i)*40
		candles[i] = &exchange.Candle{
			Open:      base,
			High:      base + 300,
			Low:       base - 200,
			Close:     base + 100,
			Volume:    1000,
			Timestamp: int64(i) * 60000,
		}
	}
	// 加一根極端低價 K 線（模擬最低谷 60200）
	candles[50] = &exchange.Candle{
		Open: 63000, High: 63200, Low: 60200, Close: 63100, Volume: 5000, Timestamp: 50 * 60000,
	}

	params := GridBacktestParams{
		// PriceLow/PriceHigh = 0 → 自動推導
		GridSpacing:   70,
		GridCount:     20,
		OrderQuantity: 500,
		TotalCapital:  10000,
		FeeRate:       0.0004,
		Direction:     "SHORT",
	}
	result, err := RunGridBacktest("BTCUSDT", candles, params, 10000, nil)
	if err != nil {
		t.Fatalf("RunGridBacktest SHORT AutoDerived: %v", err)
	}
	if len(result.Trades) == 0 {
		t.Error("SHORT 方向（自動推導區間 + spacing + maxCount）預期有交易，但交易數為 0。" +
			"修復前此場景因網格檔位集中在價格低位而產生零交易")
	}
	t.Logf("SHORT AutoDerived 交易次數: %d", len(result.Trades))
}

func TestRiskSimulator_SHORT_Direction(t *testing.T) {
	// SHORT 方向的風控應在價格高於均價 + 放量時觸發（看漲對空頭不利）
	cfg := &RiskSimulatorConfig{
		VolumeMultiplier: 3.0,
		AverageWindow:    5,
		Direction:        "SHORT",
	}
	rs := NewRiskSimulator(cfg)

	candles := make([]*exchange.Candle, 10)
	base := 60000.0
	for i := range candles {
		candles[i] = &exchange.Candle{
			Close:     base + float64(i)*10,
			Volume:    100,
			Timestamp: int64(i) * 60000,
		}
	}
	// 第 7 根 K 線：價格高於均價 + 放量 → SHORT 風控應觸發
	candles[7] = &exchange.Candle{
		Close:     base + 500,
		Volume:    500,
		Timestamp: 7 * 60000,
	}

	skip, reason := rs.Check(candles, 7)
	if !skip {
		t.Errorf("SHORT 風控：價格高於均價 + 放量應觸發，但未觸發 (reason=%s)", reason)
	}
	t.Logf("SHORT 風控觸發: skip=%v, reason=%s", skip, reason)

	// 對比：LONG 方向同樣條件不應觸發（價格在均價之上 = 安全）
	cfgLong := &RiskSimulatorConfig{
		VolumeMultiplier: 3.0,
		AverageWindow:    5,
		Direction:        "LONG",
	}
	rsLong := NewRiskSimulator(cfgLong)
	skipLong, _ := rsLong.Check(candles, 7)
	if skipLong {
		t.Error("LONG 風控：價格高於均價不應觸發，但觸發了")
	}
}

func TestGetCrossedLevelsIntrabar(t *testing.T) {
	levels := []float64{67500, 67630, 67760, 67890}
	// prevClose=67950, Low=67600, High=68000: 穿越 67890,67760,67630 向下
	got := getCrossedLevelsIntrabar(67950, 67600, 68000, 67700, levels)
	if len(got) != 3 {
		t.Errorf("預期 3 個向下穿越，得到 %d", len(got))
	}
	for _, cl := range got {
		if !cl.isBuy {
			t.Errorf("預期向下穿越(isBuy=true)，得到 isBuy=%v", cl.isBuy)
		}
	}
	// prevClose=67400, Low=67300, High=67700: 穿越 67500,67630 向上
	got2 := getCrossedLevelsIntrabar(67400, 67300, 67700, 67650, levels)
	if len(got2) != 2 {
		t.Errorf("預期 2 個向上穿越，得到 %d", len(got2))
	}
	for _, cl := range got2 {
		if cl.isBuy {
			t.Errorf("預期向上穿越(isBuy=false)，得到 isBuy=%v", cl.isBuy)
		}
	}
}
