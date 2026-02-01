//go:build ignore

package main

import (
	"fmt"
	"log"
	"time"

	"quantmesh/backtest"
	"quantmesh/config"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	fmt.Println("🚀 ETH/USDT 回测 - 多周期多时长对比")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")

	// 加载配置
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("❌ 加载配置失败: %v", err)
	}

	binanceConfig, ok := cfg.Exchanges["binance"]
	if !ok {
		log.Fatalf("❌ Binance 配置未找到")
	}

	symbol := "ETHUSDT"
	initialCapital := 10000.0

	// 测试配置
	testConfigs := []struct {
		interval string
		days     int
		name     string
	}{
		{"3m", 7, "3分钟-7天"},
		{"3m", 15, "3分钟-15天"},
		{"5m", 7, "5分钟-7天"},
		{"5m", 15, "5分钟-15天"},
	}

	// 存储所有结果
	type TestResult struct {
		Config       string
		Strategy     string
		Candles      int
		TotalReturn  float64
		MaxDrawdown  float64
		SharpeRatio  float64
		WinRate      float64
		TotalTrades  int
		ReportPath   string
		FetchTime    float64
		BacktestTime float64
	}

	allResults := make([]TestResult, 0)

	// 对每个配置运行测试
	for _, tc := range testConfigs {
		fmt.Println("")
		fmt.Println("=" + string(make([]rune, 80)))
		fmt.Printf("📊 测试配置: %s (%s 周期, %d 天数据)\n", tc.name, tc.interval, tc.days)
		fmt.Println("=" + string(make([]rune, 80)))
		fmt.Println("")

		endTime := time.Now()
		startTime := endTime.AddDate(0, 0, -tc.days)

		fmt.Printf("⏰ 时间范围: %s 至 %s\n", startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))
		fmt.Printf("💰 初始资金: $%.2f\n", initialCapital)
		fmt.Println("")

		// 获取历史数据
		fmt.Println("📥 获取历史数据...")
		fetchStart := time.Now()
		candles, err := backtest.GetHistoricalData(symbol, tc.interval, startTime, endTime, map[string]string{
			"api_key":    binanceConfig.APIKey,
			"secret_key": binanceConfig.SecretKey,
			"testnet":    fmt.Sprintf("%t", binanceConfig.Testnet),
		})
		if err != nil {
			log.Printf("❌ 获取历史数据失败: %v", err)
			continue
		}
		fetchDuration := time.Since(fetchStart).Seconds()
		fmt.Printf("✅ 获取到 %d 根 K 线 (耗时: %.2f 秒)\n", len(candles), fetchDuration)
		fmt.Println("")

		// 定义策略
		pluginPath := "/Users/user/Sites/quantmesh-premium/plugins/multi_strategy/multi_strategy.so"
		strategies := []struct {
			Name         string
			StrategyName string
		}{
			{"momentum", "momentum"},
			{"mean_reversion", "mean_reversion"},
			{"trend_following", "trend_following"},
		}

		// 运行每个策略
		for _, s := range strategies {
			fmt.Printf("▶️  运行 %s 策略...\n", s.Name)
			backtestStart := time.Now()

			// 创建策略适配器
			adapter, err := backtest.NewPluginStrategyAdapter(pluginPath, s.StrategyName, map[string]interface{}{})
			if err != nil {
				fmt.Printf("❌ 加载策略失败: %v\n", err)
				continue
			}

			bt := backtest.NewBacktester(symbol, candles, adapter, initialCapital)
			result, err := bt.Run()
			if err != nil {
				fmt.Printf("❌ %s 策略回测失败: %v\n", s.Name, err)
				continue
			}

			backtestDuration := time.Since(backtestStart).Seconds()

			// 生成报告
			reportPath, err := backtest.GenerateReport(result)
			if err != nil {
				fmt.Printf("⚠️  生成报告失败: %v\n", err)
			}

			// 保存结果
			allResults = append(allResults, TestResult{
				Config:       tc.name,
				Strategy:     s.Name,
				Candles:      len(candles),
				TotalReturn:  result.Metrics.TotalReturn,
				MaxDrawdown:  result.Metrics.MaxDrawdown,
				SharpeRatio:  result.Metrics.SharpeRatio,
				WinRate:      result.Metrics.WinRate,
				TotalTrades:  result.Metrics.TotalTrades,
				ReportPath:   reportPath,
				FetchTime:    fetchDuration,
				BacktestTime: backtestDuration,
			})

			fmt.Printf("✅ %s 策略完成 (%.3f 秒)\n", s.Name, backtestDuration)
			fmt.Printf("   总收益率: %.2f%%, 最大回撤: %.2f%%, 夏普: %.2f, 胜率: %.2f%%, 交易: %d 笔\n",
				result.Metrics.TotalReturn, result.Metrics.MaxDrawdown, result.Metrics.SharpeRatio, result.Metrics.WinRate, result.Metrics.TotalTrades)
			fmt.Println("")
		}
	}

	// 生成对比报告
	fmt.Println("")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("📊 ETH/USDT 回测结果总览")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")

	// 按配置分组显示
	for _, tc := range testConfigs {
		fmt.Printf("\n### %s\n\n", tc.name)
		fmt.Println("┌────────────────────┬──────────┬──────────┬──────────┬──────────┬──────────┐")
		fmt.Println("│ 策略               │ 总收益率 │ 最大回撤 │ 夏普比率 │ 胜率     │ 交易次数 │")
		fmt.Println("├────────────────────┼──────────┼──────────┼──────────┼──────────┼──────────┤")

		for _, result := range allResults {
			if result.Config == tc.name {
				strategyName := result.Strategy
				if len(strategyName) > 18 {
					strategyName = strategyName[:18]
				}
				fmt.Printf("│ %-18s │ %7.2f%% │ %7.2f%% │ %8.2f │ %7.2f%% │ %8d │\n",
					strategyName,
					result.TotalReturn,
					result.MaxDrawdown,
					result.SharpeRatio,
					result.WinRate,
					result.TotalTrades,
				)
			}
		}
		fmt.Println("└────────────────────┴──────────┴──────────┴──────────┴──────────┴──────────┘")
	}

	// 找出最佳配置
	fmt.Println("")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("🏆 最佳表现")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")

	var bestResult *TestResult
	bestScore := -999999.0

	for i := range allResults {
		result := &allResults[i]
		// 综合评分：收益率 + 夏普*10 - 回撤
		score := result.TotalReturn + result.SharpeRatio*10 - result.MaxDrawdown
		if score > bestScore {
			bestScore = score
			bestResult = result
		}
	}

	if bestResult != nil {
		fmt.Printf("🥇 最佳配置: %s - %s 策略\n", bestResult.Config, bestResult.Strategy)
		fmt.Printf("   总收益率: %.2f%%\n", bestResult.TotalReturn)
		fmt.Printf("   最大回撤: %.2f%%\n", bestResult.MaxDrawdown)
		fmt.Printf("   夏普比率: %.2f\n", bestResult.SharpeRatio)
		fmt.Printf("   胜率: %.2f%%\n", bestResult.WinRate)
		fmt.Printf("   交易次数: %d 笔\n", bestResult.TotalTrades)
		fmt.Printf("   综合评分: %.2f\n", bestScore)
		fmt.Println("")
		fmt.Printf("📄 详细报告: %s\n", bestResult.ReportPath)
	}

	// 周期对比
	fmt.Println("")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("📈 周期对比分析")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")

	// 计算每个周期的平均表现
	periodStats := make(map[string]struct {
		avgReturn   float64
		avgDrawdown float64
		avgSharpe   float64
		avgWinRate  float64
		count       int
	})

	for _, result := range allResults {
		stats := periodStats[result.Config]
		stats.avgReturn += result.TotalReturn
		stats.avgDrawdown += result.MaxDrawdown
		stats.avgSharpe += result.SharpeRatio
		stats.avgWinRate += result.WinRate
		stats.count++
		periodStats[result.Config] = stats
	}

	fmt.Println("┌────────────────┬──────────┬──────────┬──────────┬──────────┐")
	fmt.Println("│ 配置           │ 平均收益 │ 平均回撤 │ 平均夏普 │ 平均胜率 │")
	fmt.Println("├────────────────┼──────────┼──────────┼──────────┼──────────┤")

	for _, tc := range testConfigs {
		stats := periodStats[tc.name]
		if stats.count > 0 {
			fmt.Printf("│ %-14s │ %7.2f%% │ %7.2f%% │ %8.2f │ %7.2f%% │\n",
				tc.name,
				stats.avgReturn/float64(stats.count),
				stats.avgDrawdown/float64(stats.count),
				stats.avgSharpe/float64(stats.count),
				stats.avgWinRate/float64(stats.count),
			)
		}
	}
	fmt.Println("└────────────────┴──────────┴──────────┴──────────┴──────────┘")

	fmt.Println("")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("🎉 所有测试完成！")
	fmt.Println("")
	fmt.Println("查看所有报告:")
	fmt.Println("  cd backtest/reports")
	fmt.Println("  ls -lt *ETHUSDT*.md | head -12")
	fmt.Println("")
}
