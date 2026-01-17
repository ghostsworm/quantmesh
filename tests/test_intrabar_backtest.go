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

	fmt.Println("🔬 K线内模拟回测验证实验")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")
	fmt.Println("目的: 验证实盘盈利 vs 回测亏损的原因")
	fmt.Println("假设: 实盘是毫秒级决策，回测是分钟级决策")
	fmt.Println("方法: 对比普通回测 vs K线内模拟回测")
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

	// 测试参数
	symbol := "BTCUSDT"
	interval := "3m"
	days := 7
	initialCapital := 10000.0

	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -days)

	fmt.Println("📊 测试参数:")
	fmt.Printf("  交易对: %s\n", symbol)
	fmt.Printf("  周期: %s\n", interval)
	fmt.Printf("  时间范围: %s 至 %s (%d天)\n",
		startTime.Format("2006-01-02"), endTime.Format("2006-01-02"), days)
	fmt.Printf("  初始资金: $%.2f\n", initialCapital)
	fmt.Println("")

	// 获取历史数据
	fmt.Println("📥 获取历史数据...")
	fetchStart := time.Now()
	candles, err := backtest.GetHistoricalData(symbol, interval, startTime, endTime, map[string]string{
		"api_key":    binanceConfig.APIKey,
		"secret_key": binanceConfig.SecretKey,
		"testnet":    fmt.Sprintf("%t", binanceConfig.Testnet),
	})
	if err != nil {
		log.Fatalf("❌ 获取历史数据失败: %v", err)
	}
	fetchDuration := time.Since(fetchStart).Seconds()
	fmt.Printf("✅ 获取到 %d 根 K 线 (耗时: %.2f 秒)\n", len(candles), fetchDuration)
	fmt.Println("")

	// 测试策略（使用内置适配器）
	strategies := []struct {
		Name    string
		Adapter func() backtest.StrategyAdapter
	}{
		{"动量策略", func() backtest.StrategyAdapter { return backtest.NewMomentumAdapter() }},
		{"均值回归策略", func() backtest.StrategyAdapter { return backtest.NewMeanReversionAdapter() }},
		{"趋势跟踪策略", func() backtest.StrategyAdapter { return backtest.NewTrendFollowingAdapter() }},
	}

	// 存储结果
	type ComparisonResult struct {
		Strategy         string
		NormalReturn     float64
		NormalDrawdown   float64
		NormalTrades     int
		IntrabarReturn   float64
		IntrabarDrawdown float64
		IntrabarTrades   int
		Improvement      float64
	}

	results := make([]ComparisonResult, 0)

	// 对每个策略进行对比测试
	for _, s := range strategies {
		fmt.Println("")
		fmt.Println("=" + string(make([]rune, 80)))
		fmt.Printf("🧪 测试策略: %s\n", s.Name)
		fmt.Println("=" + string(make([]rune, 80)))
		fmt.Println("")

		// 创建策略适配器
		adapter := s.Adapter()

		// ========== 测试1: 普通回测（每根K线决策1次）==========
		fmt.Println("📊 测试 1: 普通回测（传统方法）")
		fmt.Println("   - 每根 K线决策 1 次")
		fmt.Println("   - 只在 K线收盘时决策")
		fmt.Println("")

		normalStart := time.Now()
		normalBT := backtest.NewBacktester(symbol, candles, adapter, initialCapital)
		normalResult, err := normalBT.Run()
		if err != nil {
			fmt.Printf("❌ 普通回测失败: %v\n", err)
			continue
		}
		normalDuration := time.Since(normalStart).Seconds()

		fmt.Printf("✅ 普通回测完成 (耗时: %.3f 秒)\n", normalDuration)
		fmt.Printf("   总收益率: %.2f%%\n", normalResult.Metrics.TotalReturn)
		fmt.Printf("   最大回撤: %.2f%%\n", normalResult.Metrics.MaxDrawdown)
		fmt.Printf("   夏普比率: %.2f\n", normalResult.Metrics.SharpeRatio)
		fmt.Printf("   胜率: %.2f%%\n", normalResult.Metrics.WinRate)
		fmt.Printf("   交易次数: %d 笔\n", normalResult.Metrics.TotalTrades)
		fmt.Println("")

		// ========== 测试2: K线内模拟回测（每根K线决策60次）==========
		fmt.Println("📊 测试 2: K线内模拟回测（模拟实盘）")
		fmt.Println("   - 每根 K线决策 60 次")
		fmt.Println("   - 模拟 K线内部的价格波动")
		fmt.Println("   - 接近实盘的毫秒级决策")
		fmt.Println("")

		// 重新创建适配器（避免状态污染）
		adapter2 := s.Adapter()

		intrabarStart := time.Now()
		intrabarBT := backtest.NewIntrabarBacktester(symbol, candles, adapter2, initialCapital, 60) // 每根K线60次tick
		intrabarResult, err := intrabarBT.Run()
		if err != nil {
			fmt.Printf("❌ K线内模拟回测失败: %v\n", err)
			continue
		}
		intrabarDuration := time.Since(intrabarStart).Seconds()

		fmt.Printf("✅ K线内模拟回测完成 (耗时: %.3f 秒)\n", intrabarDuration)
		fmt.Printf("   总收益率: %.2f%%\n", intrabarResult.Metrics.TotalReturn)
		fmt.Printf("   最大回撤: %.2f%%\n", intrabarResult.Metrics.MaxDrawdown)
		fmt.Printf("   夏普比率: %.2f\n", intrabarResult.Metrics.SharpeRatio)
		fmt.Printf("   胜率: %.2f%%\n", intrabarResult.Metrics.WinRate)
		fmt.Printf("   交易次数: %d 笔\n", intrabarResult.Metrics.TotalTrades)
		fmt.Println("")

		// ========== 对比分析 ==========
		improvement := intrabarResult.Metrics.TotalReturn - normalResult.Metrics.TotalReturn

		fmt.Println("📈 对比结果:")
		fmt.Printf("   收益率改善: %.2f%% → %.2f%% (",
			normalResult.Metrics.TotalReturn, intrabarResult.Metrics.TotalReturn)
		if improvement > 0 {
			fmt.Printf("+%.2f%% ✅)\n", improvement)
		} else {
			fmt.Printf("%.2f%% ❌)\n", improvement)
		}

		fmt.Printf("   回撤变化: %.2f%% → %.2f%%\n",
			normalResult.Metrics.MaxDrawdown, intrabarResult.Metrics.MaxDrawdown)

		fmt.Printf("   交易次数: %d → %d 笔 (%.1fx)\n",
			normalResult.Metrics.TotalTrades,
			intrabarResult.Metrics.TotalTrades,
			float64(intrabarResult.Metrics.TotalTrades)/float64(normalResult.Metrics.TotalTrades))

		fmt.Printf("   胜率变化: %.2f%% → %.2f%%\n",
			normalResult.Metrics.WinRate, intrabarResult.Metrics.WinRate)

		fmt.Println("")

		// 保存结果
		results = append(results, ComparisonResult{
			Strategy:         s.Name,
			NormalReturn:     normalResult.Metrics.TotalReturn,
			NormalDrawdown:   normalResult.Metrics.MaxDrawdown,
			NormalTrades:     normalResult.Metrics.TotalTrades,
			IntrabarReturn:   intrabarResult.Metrics.TotalReturn,
			IntrabarDrawdown: intrabarResult.Metrics.MaxDrawdown,
			IntrabarTrades:   intrabarResult.Metrics.TotalTrades,
			Improvement:      improvement,
		})
	}

	// ========== 总结报告 ==========
	fmt.Println("")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("📊 实验结果总结")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")

	fmt.Println("┌────────────────────┬──────────┬──────────┬──────────┬──────────┬──────────┐")
	fmt.Println("│ 策略               │ 普通回测 │ K线内模拟│ 改善幅度 │ 交易倍数 │ 结论     │")
	fmt.Println("├────────────────────┼──────────┼──────────┼──────────┼──────────┼──────────┤")

	totalImprovement := 0.0
	improvedCount := 0

	for _, r := range results {
		conclusion := ""
		if r.Improvement > 10 {
			conclusion = "显著改善 ✅✅"
			improvedCount++
		} else if r.Improvement > 5 {
			conclusion = "明显改善 ✅"
			improvedCount++
		} else if r.Improvement > 0 {
			conclusion = "略有改善 ⚠️"
			improvedCount++
		} else {
			conclusion = "无改善 ❌"
		}

		tradeMultiple := float64(r.IntrabarTrades) / float64(r.NormalTrades)

		fmt.Printf("│ %-18s │ %7.2f%% │ %7.2f%% │ %+7.2f%% │ %7.1fx │ %-12s │\n",
			r.Strategy,
			r.NormalReturn,
			r.IntrabarReturn,
			r.Improvement,
			tradeMultiple,
			conclusion,
		)

		totalImprovement += r.Improvement
	}

	fmt.Println("└────────────────────┴──────────┴──────────┴──────────┴──────────┴──────────┘")
	fmt.Println("")

	// ========== 结论 ==========
	avgImprovement := totalImprovement / float64(len(results))

	fmt.Println("🎯 实验结论:")
	fmt.Println("")

	if improvedCount == len(results) && avgImprovement > 10 {
		fmt.Println("✅✅✅ 假设得到验证！")
		fmt.Println("")
		fmt.Printf("   平均改善幅度: %.2f%%\n", avgImprovement)
		fmt.Printf("   改善策略数: %d/%d\n", improvedCount, len(results))
		fmt.Println("")
		fmt.Println("   关键发现:")
		fmt.Println("   1. K线内模拟回测显著改善了回测结果")
		fmt.Println("   2. 交易次数增加了数倍，更接近实盘")
		fmt.Println("   3. 这证明了实盘盈利 vs 回测亏损的原因:")
		fmt.Println("      → 实盘是高频决策（毫秒级）")
		fmt.Println("      → 回测是低频决策（分钟级）")
		fmt.Println("      → 信息量差异导致结果差异")
		fmt.Println("")
		fmt.Println("   建议:")
		fmt.Println("   ✅ 使用 K线内模拟进行回测")
		fmt.Println("   ✅ 或使用更细粒度的数据（1秒K线、Tick数据）")
		fmt.Println("   ✅ 实盘策略应该继续保持高频决策")
	} else if improvedCount > 0 {
		fmt.Println("⚠️ 假设部分得到验证")
		fmt.Println("")
		fmt.Printf("   平均改善幅度: %.2f%%\n", avgImprovement)
		fmt.Printf("   改善策略数: %d/%d\n", improvedCount, len(results))
		fmt.Println("")
		fmt.Println("   说明:")
		fmt.Println("   - K线内模拟有一定改善，但不够显著")
		fmt.Println("   - 可能需要更细粒度的模拟（更多tick）")
		fmt.Println("   - 或者实盘盈利还有其他因素")
	} else {
		fmt.Println("❌ 假设未得到验证")
		fmt.Println("")
		fmt.Printf("   平均改善幅度: %.2f%%\n", avgImprovement)
		fmt.Println("")
		fmt.Println("   说明:")
		fmt.Println("   - K线内模拟没有改善回测结果")
		fmt.Println("   - 实盘盈利可能有其他原因:")
		fmt.Println("     • 订单簿信息")
		fmt.Println("     • 市场微观结构")
		fmt.Println("     • 实盘参数与回测不同")
		fmt.Println("     • 其他未知因素")
	}

	fmt.Println("")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("🎉 实验完成！")
	fmt.Println("")
}
