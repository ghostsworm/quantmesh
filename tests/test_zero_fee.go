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

	fmt.Println("🔬 零手续费回测實驗 (ETCUSDT)")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")
	fmt.Println("目的: 排除手续费影响，驗证高频决策的效果")
	fmt.Println("假設: 手续费 = 0% (ETCUSDT 零手续费)")
	fmt.Println("")

	// 加載配置
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("❌ 加載配置失败: %v", err)
	}

	binanceConfig, ok := cfg.Exchanges["binance"]
	if !ok {
		log.Fatalf("❌ Binance 配置未找到")
	}

	// 测試参數
	symbol := "ETCUSDT"
	interval := "3m"
	days := 7
	initialCapital := 10000.0

	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -days)

	fmt.Printf("📊 测試参數: %s, %s, %d天, $%.0f\n", symbol, interval, days, initialCapital)
	fmt.Println("")

	// 獲取歷史數據
	fmt.Println("📥 獲取歷史數據...")
	candles, err := backtest.GetHistoricalData(symbol, interval, startTime, endTime, map[string]string{
		"api_key":    binanceConfig.APIKey,
		"secret_key": binanceConfig.SecretKey,
		"testnet":    fmt.Sprintf("%t", binanceConfig.Testnet),
	})
	if err != nil {
		log.Fatalf("❌ 獲取歷史數據失败: %v", err)
	}
	fmt.Printf("✅ 獲取到 %d 根 K 線\n", len(candles))
	fmt.Println("")

	// 测試所有策略
	strategies := []struct {
		Name    string
		Adapter func() backtest.StrategyAdapter
	}{
		{"动量策略", func() backtest.StrategyAdapter { return backtest.NewMomentumAdapter() }},
		{"均值回归策略", func() backtest.StrategyAdapter { return backtest.NewMeanReversionAdapter() }},
		{"趋势跟踪策略", func() backtest.StrategyAdapter { return backtest.NewTrendFollowingAdapter() }},
	}

	type Result struct {
		Strategy       string
		NormalReturn   float64
		NormalTrades   int
		IntrabarReturn float64
		IntrabarTrades int
		Improvement    float64
	}

	results := make([]Result, 0)

	for _, s := range strategies {
		fmt.Println("")
		fmt.Println("=" + string(make([]rune, 80)))
		fmt.Printf("🧪 测試策略: %s\n", s.Name)
		fmt.Println("=" + string(make([]rune, 80)))
		fmt.Println("")

		// ========== 测試1: 普通回测（有手续费 0.04%）==========
		fmt.Println("📊 测試 1: 普通回测（手续费 0.04%）")
		adapter1 := s.Adapter()
		normalBT := backtest.NewBacktester(symbol, candles, adapter1, initialCapital)
		// 保持默认手续费
		normalResult, err := normalBT.Run()
		if err != nil {
			log.Printf("❌ 普通回测失败: %v", err)
			continue
		}

		fmt.Printf("✅ 普通回测完成\n")
		fmt.Printf("   總收益率: %.2f%%\n", normalResult.Metrics.TotalReturn)
		fmt.Printf("   交易次數: %d 笔\n", normalResult.Metrics.TotalTrades)
		fmt.Printf("   胜率: %.2f%%\n", normalResult.Metrics.WinRate)
		fmt.Println("")

		// ========== 测試2: 普通回测（零手续费）==========
		fmt.Println("📊 测試 2: 普通回测（零手续费）")
		adapter2 := s.Adapter()
		normalZeroBT := backtest.NewBacktester(symbol, candles, adapter2, initialCapital)
		normalZeroBT.SetFees(0, 0, 0) // 設置零手续费
		normalZeroResult, err := normalZeroBT.Run()
		if err != nil {
			log.Printf("❌ 零手续费回测失败: %v", err)
			continue
		}

		fmt.Printf("✅ 零手续费回测完成\n")
		fmt.Printf("   總收益率: %.2f%%\n", normalZeroResult.Metrics.TotalReturn)
		fmt.Printf("   交易次數: %d 笔\n", normalZeroResult.Metrics.TotalTrades)
		fmt.Printf("   胜率: %.2f%%\n", normalZeroResult.Metrics.WinRate)
		fmt.Println("")

		// ========== 测試3: K線内模拟（零手续费，60次/K線）==========
		fmt.Println("📊 测試 3: K線内模拟（零手续费，60 ticks/K線）")
		adapter3 := s.Adapter()
		intrabarBT := backtest.NewIntrabarBacktester(symbol, candles, adapter3, initialCapital, 60)
		intrabarBT.SetFees(0, 0, 0) // 設置零手续费
		intrabarResult, err := intrabarBT.Run()
		if err != nil {
			log.Printf("❌ K線内模拟失败: %v", err)
			continue
		}

		fmt.Printf("✅ K線内模拟完成\n")
		fmt.Printf("   總收益率: %.2f%%\n", intrabarResult.Metrics.TotalReturn)
		fmt.Printf("   交易次數: %d 笔\n", intrabarResult.Metrics.TotalTrades)
		fmt.Printf("   胜率: %.2f%%\n", intrabarResult.Metrics.WinRate)
		fmt.Println("")

		// ========== 對比分析 ==========
		improvement := intrabarResult.Metrics.TotalReturn - normalZeroResult.Metrics.TotalReturn

		fmt.Println("📈 對比結果:")
		fmt.Printf("   有手续费: %.2f%%\n", normalResult.Metrics.TotalReturn)
		fmt.Printf("   零手续费（普通）: %.2f%%\n", normalZeroResult.Metrics.TotalReturn)
		fmt.Printf("   零手续费（K線内模拟）: %.2f%% (", intrabarResult.Metrics.TotalReturn)
		if improvement > 0 {
			fmt.Printf("+%.2f%% ✅)\n", improvement)
		} else {
			fmt.Printf("%.2f%% ❌)\n", improvement)
		}

		fmt.Printf("   交易次數: %d → %d 笔 (%.1fx)\n",
			normalZeroResult.Metrics.TotalTrades,
			intrabarResult.Metrics.TotalTrades,
			float64(intrabarResult.Metrics.TotalTrades)/float64(normalZeroResult.Metrics.TotalTrades))

		fmt.Printf("   胜率: %.2f%% → %.2f%%\n",
			normalZeroResult.Metrics.WinRate, intrabarResult.Metrics.WinRate)

		fmt.Println("")

		// 保存結果
		results = append(results, Result{
			Strategy:       s.Name,
			NormalReturn:   normalZeroResult.Metrics.TotalReturn,
			NormalTrades:   normalZeroResult.Metrics.TotalTrades,
			IntrabarReturn: intrabarResult.Metrics.TotalReturn,
			IntrabarTrades: intrabarResult.Metrics.TotalTrades,
			Improvement:    improvement,
		})
	}

	// ========== 總結 ==========
	fmt.Println("")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("📊 零手续费實驗總結")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")

	fmt.Println("┌────────────────────┬──────────┬──────────┬──────────┬──────────┬──────────┐")
	fmt.Println("│ 策略               │ 普通回测 │ K線内模拟│ 改善幅度 │ 交易倍數 │ 結論     │")
	fmt.Println("├────────────────────┼──────────┼──────────┼──────────┼──────────┼──────────┤")

	totalImprovement := 0.0
	improvedCount := 0

	for _, r := range results {
		conclusion := ""
		if r.Improvement > 20 {
			conclusion = "显著改善 ✅✅"
			improvedCount++
		} else if r.Improvement > 10 {
			conclusion = "明显改善 ✅"
			improvedCount++
		} else if r.Improvement > 5 {
			conclusion = "有改善 ⚠️"
			improvedCount++
		} else if r.Improvement > 0 {
			conclusion = "略有改善 ⚠️"
			improvedCount++
		} else {
			conclusion = "無改善 ❌"
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

	// ========== 結論 ==========
	avgImprovement := totalImprovement / float64(len(results))

	fmt.Println("🎯 實驗結論:")
	fmt.Println("")

	if improvedCount == len(results) && avgImprovement > 10 {
		fmt.Println("✅✅✅ 假設得到驗证！（在零手续费条件下）")
		fmt.Println("")
		fmt.Printf("   平均改善幅度: %.2f%%\n", avgImprovement)
		fmt.Printf("   改善策略數: %d/%d\n", improvedCount, len(results))
		fmt.Println("")
		fmt.Println("   关键发現:")
		fmt.Println("   1. 在零手续费条件下，K線内模拟显著改善了回测結果")
		fmt.Println("   2. 这证明了高频决策确實能捕捉更多交易机會")
		fmt.Println("   3. 之前的亏损主要是手续费累积導致的")
		fmt.Println("")
		fmt.Println("   實盘盈利的原因:")
		fmt.Println("   ✅ ETCUSDT 零手续费")
		fmt.Println("   ✅ 高频决策捕捉更多机會")
		fmt.Println("   ✅ 策略本身是有效的")
		fmt.Println("")
		fmt.Println("   建议:")
		fmt.Println("   ✅ 优先交易零手续费币种（ETCUSDT 等）")
		fmt.Println("   ✅ 或使用 BNB 抵扣 + VIP 等级降低手续费")
		fmt.Println("   ✅ 继续使用高频决策策略")
	} else if improvedCount > 0 {
		fmt.Println("⚠️ 假設部分得到驗证")
		fmt.Println("")
		fmt.Printf("   平均改善幅度: %.2f%%\n", avgImprovement)
		fmt.Printf("   改善策略數: %d/%d\n", improvedCount, len(results))
		fmt.Println("")
		fmt.Println("   說明:")
		fmt.Println("   - 零手续费下有改善，但不够显著")
		fmt.Println("   - 可能还有其他因素影响")
		fmt.Println("   - 需要進一步优化策略参數")
	} else {
		fmt.Println("❌ 假設未得到驗證（即使在零手续费下）")
		fmt.Println("")
		fmt.Printf("   平均改善幅度: %.2f%%\n", avgImprovement)
		fmt.Println("")
		fmt.Println("   說明:")
		fmt.Println("   - 即使零手续费，K線内模拟也没有改善")
		fmt.Println("   - 问题不在於手续费，而在於策略本身")
		fmt.Println("   - 高频决策可能產生更多假信号")
		fmt.Println("   - 需要重新审視策略逻辑")
	}

	fmt.Println("")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("🎉 實驗完成！")
	fmt.Println("")
}
