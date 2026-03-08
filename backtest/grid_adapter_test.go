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
