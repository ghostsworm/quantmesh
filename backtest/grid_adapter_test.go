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

func TestRunGridBacktest_SHORT_SpikeRange_ZeroClose(t *testing.T) {
	// 復現 bug：價格正常在 67000 附近波動，但曾有一次尖峰到 69500
	// 自動推導 priceHigh=69500，buildGridLevelsBySpacingFromHigh 配合 maxCount=20
	// 會把 20 檔全部放在 69500 附近（68170~69500），遠離正常交易區間
	// → 只在尖峰時開倉，之後永遠無法平倉
	// 修復後使用 buildGridLevelsByCenteredSpacing 以起始價為中心建檔位
	candles := make([]*exchange.Candle, 200)
	for i := range candles {
		base := 67000.0 + float64(i%30)*5 - 75 // 在 66925~67075 間波動
		candles[i] = &exchange.Candle{
			Open:      base,
			High:      base + 50,
			Low:       base - 50,
			Close:     base + 10,
			Volume:    100,
			Timestamp: int64(i) * 60000,
		}
	}
	// 第 20 根 K 線：一次尖峰到 69500（模擬 auto-derived priceHigh）
	candles[20] = &exchange.Candle{
		Open: 67000, High: 69500, Low: 66900, Close: 67100, Volume: 5000, Timestamp: 20 * 60000,
	}

	params := GridBacktestParams{
		GridSpacing:   70,
		GridCount:     20,
		OrderQuantity: 500,
		TotalCapital:  10000,
		FeeRate:       0.0004,
		Direction:     "SHORT",
	}
	result, err := RunGridBacktest("BTCUSDT", candles, params, 10000, nil)
	if err != nil {
		t.Fatalf("RunGridBacktest SHORT SpikeRange: %v", err)
	}
	t.Logf("SHORT SpikeRange: 賣出(開空)=%d, 買入(平空)=%d, 成對=%d",
		result.Metrics.SellCount, result.Metrics.BuyCount, result.Metrics.TotalTrades)

	if result.Metrics.SellCount == 0 {
		t.Error("預期有賣出(開空)交易")
	}
	if result.Metrics.BuyCount == 0 {
		t.Error("修復後預期有買入(平空)交易，但 BuyCount=0。" +
			"此前 bug：檔位集中在尖峰區域，正常價格波動無法觸發平倉")
	}
	if result.Metrics.TotalTrades == 0 && result.Metrics.SellCount > 0 {
		t.Error("有開倉但零平倉 — 這是修復前的 bug 症狀")
	}
}

func TestBuildGridLevelsByCenteredSpacing(t *testing.T) {
	// 驗證 centered spacing 以 centerPrice 為中心生成檔位
	levels := buildGridLevelsByCenteredSpacing(60000, 75000, 70, 20, 67000)
	if len(levels) != 20 {
		t.Fatalf("預期 20 個檔位，得到 %d", len(levels))
	}
	// 檔位應圍繞 67000 分佈
	mid := levels[len(levels)/2]
	if mid < 66500 || mid > 67500 {
		t.Errorf("中間檔位 %.2f 應接近 67000，但偏離太遠", mid)
	}
	// 最低不應低於 60000
	if levels[0] < 60000 {
		t.Errorf("最低檔位 %.2f 不應低於 60000", levels[0])
	}
	// 最高不應高於 75000
	if levels[len(levels)-1] > 75000 {
		t.Errorf("最高檔位 %.2f 不應高於 75000", levels[len(levels)-1])
	}
	t.Logf("Centered 檔位範圍: %.2f ~ %.2f (center=67000)", levels[0], levels[len(levels)-1])

	// 對比舊的 buildGridLevelsBySpacingFromHigh：檔位集中在高位
	oldLevels := buildGridLevelsBySpacingFromHigh(60000, 75000, 70, 20)
	if oldLevels[0] > 73000 {
		t.Logf("舊方法最低檔位 %.2f 遠離 67000（集中在高位），已棄用", oldLevels[0])
	}

	// maxCount=0 時應生成全部檔位
	allLevels := buildGridLevelsByCenteredSpacing(60000, 60700, 70, 0, 60350)
	expectedCount := 11 // (60700-60000)/70 + 1 = 11
	if len(allLevels) != expectedCount {
		t.Errorf("maxCount=0 時預期 %d 檔位，得到 %d", expectedCount, len(allLevels))
	}
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

func TestRunGridBacktest_FlashCrashRecovery(t *testing.T) {
	// 闪崩场景：价格从 60000 暴跌到 59000（一根 K 线内跌穿多个档位），然后恢复
	// 修复前：所有买入在闪崩 K 线内完成，后续恢复时只有最高档能匹配卖出
	// 修复后：恢复时每个卖出触发都能匹配到实际有持仓的档位
	candles := []*exchange.Candle{
		// K1: 平稳
		{Open: 60000, High: 60100, Low: 59900, Close: 60000, Timestamp: 1000, IsClosed: true},
		// K2: 闪崩！从 60000 暴跌到 59000，收在 59100
		{Open: 60000, High: 60050, Low: 59000, Close: 59100, Timestamp: 2000, IsClosed: true},
		// K3-K6: 缓慢恢复
		{Open: 59100, High: 59400, Low: 59050, Close: 59350, Timestamp: 3000, IsClosed: true},
		{Open: 59350, High: 59700, Low: 59300, Close: 59650, Timestamp: 4000, IsClosed: true},
		{Open: 59650, High: 60000, Low: 59600, Close: 59950, Timestamp: 5000, IsClosed: true},
		{Open: 59950, High: 60300, Low: 59900, Close: 60250, Timestamp: 6000, IsClosed: true},
	}
	params := GridBacktestParams{
		PriceLow:      58900,
		PriceHigh:     60400,
		GridSpacing:   100,
		OrderQuantity: 500,
		TotalCapital:  10000,
		FeeRate:       0.0004,
		SlippageRatio: 0.0003,
		Direction:     "LONG",
	}
	result, err := RunGridBacktest("BTCUSDT", candles, params, 10000, nil)
	if err != nil {
		t.Fatalf("RunGridBacktest FlashCrash: %v", err)
	}

	t.Logf("闪崩恢复: 买入=%d, 卖出=%d, 成对=%d, 总收益=%.2f%%",
		result.Metrics.BuyCount, result.Metrics.SellCount,
		result.Metrics.TotalTrades, result.Metrics.TotalReturn)

	if result.Metrics.BuyCount == 0 {
		t.Error("预期有买入交易")
	}
	if result.Metrics.SellCount == 0 {
		t.Error("闪崩后价格恢复，应有卖出（平仓）交易。修复前此值为 0（卖出匹配仅找紧邻档位）")
	}
	if result.Metrics.TotalTrades == 0 && result.Metrics.BuyCount > 0 && result.Metrics.SellCount > 0 {
		t.Error("有买卖交易但成对数为 0")
	}

	for _, trade := range result.Trades {
		t.Logf("  %s @ %.2f qty=%.6f pnl=%.4f", trade.Type, trade.Price, trade.Quantity, trade.PnL)
	}
}

func TestRunGridBacktest_SHORT_FlashPumpRecovery(t *testing.T) {
	// SHORT 方向的闪崩（暴涨）场景：价格从 60000 暴涨到 61000，然后回落
	candles := []*exchange.Candle{
		{Open: 60000, High: 60100, Low: 59900, Close: 60000, Timestamp: 1000, IsClosed: true},
		// 暴涨 K 线：开空多档
		{Open: 60000, High: 61000, Low: 59950, Close: 60900, Timestamp: 2000, IsClosed: true},
		// 回落：应平空
		{Open: 60900, High: 60950, Low: 60500, Close: 60600, Timestamp: 3000, IsClosed: true},
		{Open: 60600, High: 60650, Low: 60100, Close: 60200, Timestamp: 4000, IsClosed: true},
		{Open: 60200, High: 60250, Low: 59700, Close: 59800, Timestamp: 5000, IsClosed: true},
	}
	params := GridBacktestParams{
		PriceLow:      59600,
		PriceHigh:     61100,
		GridSpacing:   100,
		OrderQuantity: 500,
		TotalCapital:  10000,
		FeeRate:       0.0004,
		SlippageRatio: 0.0003,
		Direction:     "SHORT",
	}
	result, err := RunGridBacktest("BTCUSDT", candles, params, 10000, nil)
	if err != nil {
		t.Fatalf("RunGridBacktest SHORT FlashPump: %v", err)
	}

	t.Logf("SHORT 暴涨恢复: 卖出(开空)=%d, 买入(平空)=%d, 成对=%d",
		result.Metrics.SellCount, result.Metrics.BuyCount, result.Metrics.TotalTrades)

	if result.Metrics.SellCount == 0 {
		t.Error("预期有卖出(开空)交易")
	}
	if result.Metrics.BuyCount == 0 {
		t.Error("暴涨后价格回落，应有买入(平空)交易")
	}
}

func TestFindHighestPositionBelow(t *testing.T) {
	positions := map[float64]float64{
		56700: 0.01,
		56770: 0.01,
		56840: 0.01,
		57050: 0.01,
	}
	tests := []struct {
		target float64
		want   float64
	}{
		{57120, 57050},  // 应找到 57050（有持仓的最高档）
		{57050, 56840},  // 严格小于 target
		{56840, 56770},
		{56770, 56700},
		{56700, -1},     // 没有更低的持仓
		{56000, -1},
	}
	for _, tt := range tests {
		got := findHighestPositionBelow(positions, tt.target)
		if got != tt.want {
			t.Errorf("findHighestPositionBelow(positions, %.0f) = %.0f, want %.0f", tt.target, got, tt.want)
		}
	}
}

func TestFindLowestPositionAbove(t *testing.T) {
	positions := map[float64]float64{
		60200: 0.01,
		60300: 0.01,
		60500: 0.01,
		60700: 0.01,
	}
	tests := []struct {
		target float64
		want   float64
	}{
		{60100, 60200},  // 应找到 60200（有持仓的最低档）
		{60200, 60300},
		{60300, 60500},
		{60500, 60700},
		{60700, -1},     // 没有更高的持仓
		{61000, -1},
	}
	for _, tt := range tests {
		got := findLowestPositionAbove(positions, tt.target)
		if got != tt.want {
			t.Errorf("findLowestPositionAbove(positions, %.0f) = %.0f, want %.0f", tt.target, got, tt.want)
		}
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
