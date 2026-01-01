package main

import (
	"fmt"
	"time"

	"quantmesh/backtest"
	"quantmesh/config"
	"quantmesh/logger"
)

func main() {
	// 加载配置
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		logger.Error("❌ 加载配置失败: %v", err)
		return
	}

	logger.Info("🚀 开始 1 分钟周期回测 - 最近数据")
	logger.Info("=" + string(make([]rune, 70)))

	// 计算时间范围（最近 7 天）
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -7)

	symbol := "BTCUSDT"
	interval := "1m"
	initialCapital := 10000.0

	// 从配置文件获取 Binance 配置
	binanceExchange := cfg.Exchanges["binance"]
	binanceConfig := map[string]string{
		"api_key":    binanceExchange.APIKey,
		"secret_key": binanceExchange.SecretKey,
		"testnet":    fmt.Sprintf("%v", binanceExchange.Testnet),
	}

	logger.Info("📊 回测参数:")
	logger.Info("  交易对: %s", symbol)
	logger.Info("  周期: %s ⚡ (更精细的数据)", interval)
	logger.Info("  时间范围: %s 至 %s", startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))
	logger.Info("  初始资金: $%.2f", initialCapital)
	logger.Info("")

	// 1. 获取历史数据（优先缓存）
	logger.Info("📥 步骤 1: 获取历史数据...")
	startFetch := time.Now()
	candles, err := backtest.GetHistoricalData(symbol, interval, startTime, endTime, binanceConfig)
	if err != nil {
		logger.Error("❌ 获取历史数据失败: %v", err)
		return
	}
	fetchDuration := time.Since(startFetch)
	logger.Info("✅ 数据获取完成: %d 根 K 线 (耗时: %.2f 秒)", len(candles), fetchDuration.Seconds())
	logger.Info("")

	// 2. 运行三个策略的回测
	strategies := []struct {
		name    string
		adapter backtest.StrategyAdapter
	}{
		{"动量策略 (Momentum)", backtest.NewMomentumAdapter()},
		{"均值回归策略 (Mean Reversion)", backtest.NewMeanReversionAdapter()},
		{"趋势跟踪策略 (Trend Following)", backtest.NewTrendFollowingAdapter()},
	}

	results := make([]*backtest.BacktestResult, 0)
	totalBacktestTime := 0.0

	for i, strategy := range strategies {
		logger.Info("📊 步骤 %d: 回测 %s", i+2, strategy.name)
		logger.Info("-" + string(make([]rune, 70)))

		startBacktest := time.Now()

		// 创建回测器
		backtester := backtest.NewBacktester(symbol, candles, strategy.adapter, initialCapital)

		// 运行回测
		result, err := backtester.Run()
		if err != nil {
			logger.Error("❌ 回测失败: %v", err)
			continue
		}

		backtestDuration := time.Since(startBacktest)
		totalBacktestTime += backtestDuration.Seconds()

		// 生成报告
		reportPath, err := backtest.GenerateReport(result)
		if err != nil {
			logger.Warn("⚠️ 生成报告失败: %v", err)
		} else {
			logger.Info("📄 报告已生成: %s", reportPath)
		}

		// 保存权益曲线
		equityPath, err := backtest.SaveEquityCurveCSV(result)
		if err != nil {
			logger.Warn("⚠️ 保存权益曲线失败: %v", err)
		} else {
			logger.Info("📈 权益曲线已保存: %s", equityPath)
		}

		results = append(results, result)

		logger.Info("")
		logger.Info("✅ %s 回测完成 (耗时: %.3f 秒)", strategy.name, backtestDuration.Seconds())
		logger.Info("   总交易次数: %d", result.Metrics.TotalTrades)
		logger.Info("   总收益率: %.2f%%", result.Metrics.TotalReturn)
		logger.Info("   最大回撤: %.2f%%", result.Metrics.MaxDrawdown)
		logger.Info("   夏普比率: %.2f", result.Metrics.SharpeRatio)
		logger.Info("   胜率: %.2f%%", result.Metrics.WinRate)
		logger.Info("")
	}

	// 3. 性能统计
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("⚡ 性能统计")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")
	logger.Info("📊 数据量: %d 根 K 线", len(candles))
	logger.Info("⏱️  数据获取: %.2f 秒 (%.0f 根/秒)", fetchDuration.Seconds(), float64(len(candles))/fetchDuration.Seconds())
	logger.Info("⚡ 回测速度: %.3f 秒 (%.0f 根/秒)", totalBacktestTime, float64(len(candles)*3)/totalBacktestTime)
	logger.Info("💾 缓存状态: %s", map[bool]string{true: "命中 ✅", false: "未命中"}[fetchDuration.Seconds() < 1])
	logger.Info("")

	// 4. 生成对比总结
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("📊 回测结果对比")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")

	fmt.Println("┌────────────────────────┬──────────┬──────────┬──────────┬──────────┬──────────┐")
	fmt.Println("│ 策略                   │ 总收益率 │ 最大回撤 │ 夏普比率 │ 胜率     │ 交易次数 │")
	fmt.Println("├────────────────────────┼──────────┼──────────┼──────────┼──────────┼──────────┤")

	for i, result := range results {
		strategyName := strategies[i].name
		if len(strategyName) > 22 {
			strategyName = strategyName[:22]
		}

		fmt.Printf("│ %-22s │ %7.2f%% │ %7.2f%% │ %8.2f │ %7.2f%% │ %8d │\n",
			strategyName,
			result.Metrics.TotalReturn,
			result.Metrics.MaxDrawdown,
			result.Metrics.SharpeRatio,
			result.Metrics.WinRate,
			result.Metrics.TotalTrades,
		)
	}

	fmt.Println("└────────────────────────┴──────────┴──────────┴──────────┴──────────┴──────────┘")
	logger.Info("")

	// 5. 推荐最佳策略
	var bestStrategy *backtest.BacktestResult
	var bestStrategyName string
	bestScore := -999999.0

	for i, result := range results {
		// 综合评分：收益率 + 夏普比率*10 - 最大回撤
		score := result.Metrics.TotalReturn + result.Metrics.SharpeRatio*10 - result.Metrics.MaxDrawdown
		if score > bestScore {
			bestScore = score
			bestStrategy = result
			bestStrategyName = strategies[i].name
		}
	}

	if bestStrategy != nil {
		logger.Info("🏆 推荐策略: %s", bestStrategyName)
		logger.Info("   综合评分: %.2f", bestScore)
		logger.Info("   总收益率: %.2f%%", bestStrategy.Metrics.TotalReturn)
		logger.Info("   夏普比率: %.2f", bestStrategy.Metrics.SharpeRatio)
		logger.Info("   最大回撤: %.2f%%", bestStrategy.Metrics.MaxDrawdown)
		logger.Info("   胜率: %.2f%%", bestStrategy.Metrics.WinRate)
	}

	logger.Info("")
	
	// 6. 与 3m 周期对比
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("📊 1m vs 3m 周期对比")
	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("")
	logger.Info("1m 周期特点:")
	logger.Info("  ✅ 数据更精细，信号更及时")
	logger.Info("  ✅ 能捕捉更小的价格波动")
	logger.Info("  ⚠️ 交易次数更多，手续费成本更高")
	logger.Info("  ⚠️ 噪音更多，假信号可能增加")
	logger.Info("")
	logger.Info("3m 周期特点:")
	logger.Info("  ✅ 信号更稳定，噪音较少")
	logger.Info("  ✅ 交易次数适中，成本可控")
	logger.Info("  ⚠️ 信号响应稍慢")
	logger.Info("")

	logger.Info("=" + string(make([]rune, 70)))
	logger.Info("🎉 所有回测完成！")
	logger.Info("")
	logger.Info("查看详细报告:")
	logger.Info("  cd backtest/reports")
	logger.Info("  ls -lt *.md | head -3")
	logger.Info("")
	logger.Info("对比 3m 周期的报告:")
	logger.Info("  diff <(grep '总收益率' backtest/reports/*1m*.md) <(grep '总收益率' backtest/reports/*3m*.md)")
}

