package backtest

import (
	"testing"
	"time"

	"quantmesh/exchange"
)

// TestMomentumStrategy 测試动量策略
func TestMomentumStrategy(t *testing.T) {
	t.Log("测試动量策略回测...")

	// 生成模拟數據（震荡行情）
	candles := generateMockCandles("BTCUSDT", 1000, 30000, 0.02)

	// 創建策略
	strategy := NewMomentumAdapter()

	// 創建回测器
	backtester := NewBacktester("BTCUSDT", candles, strategy, 10000)

	// 運行回测
	result, err := backtester.Run()
	if err != nil {
		t.Fatalf("回测失敗: %v", err)
	}

	// 驗证結果
	if result == nil {
		t.Fatal("回测結果為空")
	}

	t.Logf("✅ 动量策略回测完成")
	t.Logf("   總交易次數: %d", result.Metrics.TotalTrades)
	t.Logf("   總收益率: %.2f%%", result.Metrics.TotalReturn)
	t.Logf("   最大回撤: %.2f%%", result.Metrics.MaxDrawdown)
	t.Logf("   夏普比率: %.2f", result.Metrics.SharpeRatio)
	t.Logf("   胜率: %.2f%%", result.Metrics.WinRate)

	// 基本驗证
	if result.Metrics.TotalTrades < 0 {
		t.Error("交易次數不能為负")
	}
	if result.FinalCapital < 0 {
		t.Error("最终資金不能為负")
	}
}

// TestMeanReversionStrategy 测試均值回归策略
func TestMeanReversionStrategy(t *testing.T) {
	t.Log("测試均值回归策略回测...")

	// 生成模拟數據（震荡行情）
	candles := generateMockCandles("BTCUSDT", 1000, 30000, 0.03)

	// 創建策略
	strategy := NewMeanReversionAdapter()

	// 創建回测器
	backtester := NewBacktester("BTCUSDT", candles, strategy, 10000)

	// 運行回测
	result, err := backtester.Run()
	if err != nil {
		t.Fatalf("回测失敗: %v", err)
	}

	// 驗证結果
	if result == nil {
		t.Fatal("回测結果為空")
	}

	t.Logf("✅ 均值回归策略回测完成")
	t.Logf("   總交易次數: %d", result.Metrics.TotalTrades)
	t.Logf("   總收益率: %.2f%%", result.Metrics.TotalReturn)
	t.Logf("   最大回撤: %.2f%%", result.Metrics.MaxDrawdown)
	t.Logf("   夏普比率: %.2f", result.Metrics.SharpeRatio)
	t.Logf("   胜率: %.2f%%", result.Metrics.WinRate)

	// 基本驗证
	if result.Metrics.TotalTrades < 0 {
		t.Error("交易次數不能為负")
	}
	if result.FinalCapital < 0 {
		t.Error("最终資金不能為负")
	}
}

// TestTrendFollowingStrategy 测試趋势跟踪策略
func TestTrendFollowingStrategy(t *testing.T) {
	t.Log("测試趋势跟踪策略回测...")

	// 生成模拟數據（趋势行情）
	candles := generateTrendingCandles("BTCUSDT", 1000, 30000, 0.001)

	// 創建策略
	strategy := NewTrendFollowingAdapter()

	// 創建回测器
	backtester := NewBacktester("BTCUSDT", candles, strategy, 10000)

	// 運行回测
	result, err := backtester.Run()
	if err != nil {
		t.Fatalf("回测失敗: %v", err)
	}

	// 驗证結果
	if result == nil {
		t.Fatal("回测結果為空")
	}

	t.Logf("✅ 趋势跟踪策略回测完成")
	t.Logf("   總交易次數: %d", result.Metrics.TotalTrades)
	t.Logf("   總收益率: %.2f%%", result.Metrics.TotalReturn)
	t.Logf("   最大回撤: %.2f%%", result.Metrics.MaxDrawdown)
	t.Logf("   夏普比率: %.2f", result.Metrics.SharpeRatio)
	t.Logf("   胜率: %.2f%%", result.Metrics.WinRate)

	// 基本驗证
	if result.Metrics.TotalTrades < 0 {
		t.Error("交易次數不能為负")
	}
	if result.FinalCapital < 0 {
		t.Error("最终資金不能為负")
	}
}

// TestReportGeneration 测試报告生成
func TestReportGeneration(t *testing.T) {
	t.Log("测試报告生成...")
	t.Chdir(t.TempDir())

	// 生成模拟數據
	candles := generateMockCandles("BTCUSDT", 500, 30000, 0.02)

	// 創建策略
	strategy := NewMomentumAdapter()

	// 創建回测器
	backtester := NewBacktester("BTCUSDT", candles, strategy, 10000)

	// 運行回测
	result, err := backtester.Run()
	if err != nil {
		t.Fatalf("回测失敗: %v", err)
	}

	// 生成报告
	reportPath, err := GenerateReport(result)
	if err != nil {
		t.Fatalf("生成报告失敗: %v", err)
	}

	t.Logf("✅ 报告已生成: %s", reportPath)

	// 保存權益曲線
	equityPath, err := SaveEquityCurveCSV(result)
	if err != nil {
		t.Fatalf("保存權益曲線失敗: %v", err)
	}

	t.Logf("✅ 權益曲線已保存: %s", equityPath)
}

// generateMockCandles 生成模拟K線數據（震荡行情）
func generateMockCandles(symbol string, count int, basePrice float64, volatility float64) []*exchange.Candle {
	candles := make([]*exchange.Candle, count)
	currentPrice := basePrice
	timestamp := time.Now().Add(-time.Duration(count)*time.Hour).Unix() * 1000

	for i := 0; i < count; i++ {
		// 随机波动
		change := (float64(i%10) - 5) * volatility * basePrice
		currentPrice += change

		// 确保價格在合理範圍内
		if currentPrice < basePrice*0.8 {
			currentPrice = basePrice * 0.8
		}
		if currentPrice > basePrice*1.2 {
			currentPrice = basePrice * 1.2
		}

		open := currentPrice
		high := currentPrice * (1 + volatility)
		low := currentPrice * (1 - volatility)
		close := currentPrice + (float64(i%3)-1)*volatility*basePrice

		candles[i] = &exchange.Candle{
			Symbol:    symbol,
			Timestamp: timestamp + int64(i)*3600000, // 每小時一根K線
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    1000 + float64(i%100)*10,
			IsClosed:  true,
		}
	}

	return candles
}

// generateTrendingCandles 生成趋势行情數據
func generateTrendingCandles(symbol string, count int, basePrice float64, trendRate float64) []*exchange.Candle {
	candles := make([]*exchange.Candle, count)
	currentPrice := basePrice
	timestamp := time.Now().Add(-time.Duration(count)*time.Hour).Unix() * 1000

	for i := 0; i < count; i++ {
		// 趋势上涨
		currentPrice *= (1 + trendRate)

		open := currentPrice
		high := currentPrice * 1.01
		low := currentPrice * 0.99
		close := currentPrice * (1 + trendRate*0.5)

		candles[i] = &exchange.Candle{
			Symbol:    symbol,
			Timestamp: timestamp + int64(i)*3600000,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    1000 + float64(i%100)*10,
			IsClosed:  true,
		}
	}

	return candles
}
