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

	fmt.Println("🔬 K线内模拟回测快速验证")
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

	// 测试参数 - 使用更少的数据
	symbol := "BTCUSDT"
	interval := "3m"
	days := 3 // 只测试 3 天
	initialCapital := 10000.0

	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -days)

	fmt.Printf("📊 测试参数: %s, %s, %d天, $%.0f\n", symbol, interval, days, initialCapital)
	fmt.Println("")

	// 获取历史数据
	fmt.Println("📥 获取历史数据...")
	candles, err := backtest.GetHistoricalData(symbol, interval, startTime, endTime, map[string]string{
		"api_key":    binanceConfig.APIKey,
		"secret_key": binanceConfig.SecretKey,
		"testnet":    fmt.Sprintf("%t", binanceConfig.Testnet),
	})
	if err != nil {
		log.Fatalf("❌ 获取历史数据失败: %v", err)
	}
	fmt.Printf("✅ 获取到 %d 根 K 线\n", len(candles))
	fmt.Println("")

	// 只测试趋势跟踪策略
	fmt.Println("🧪 测试策略: 趋势跟踪")
	fmt.Println("")

	// ========== 测试1: 普通回测 ==========
	fmt.Println("📊 测试 1: 普通回测")
	adapter1 := backtest.NewTrendFollowingAdapter()
	normalBT := backtest.NewBacktester(symbol, candles, adapter1, initialCapital)
	normalResult, err := normalBT.Run()
	if err != nil {
		log.Fatalf("❌ 普通回测失败: %v", err)
	}

	fmt.Printf("✅ 普通回测完成\n")
	fmt.Printf("   总收益率: %.2f%%\n", normalResult.Metrics.TotalReturn)
	fmt.Printf("   交易次数: %d 笔\n", normalResult.Metrics.TotalTrades)
	fmt.Println("")

	// ========== 测试2: K线内模拟（10次/K线）==========
	fmt.Println("📊 测试 2: K线内模拟 (10 ticks/K线)")
	adapter2 := backtest.NewTrendFollowingAdapter()
	intrabar10BT := backtest.NewIntrabarBacktester(symbol, candles, adapter2, initialCapital, 10)
	intrabar10Result, err := intrabar10BT.Run()
	if err != nil {
		log.Fatalf("❌ K线内模拟失败: %v", err)
	}

	fmt.Printf("✅ K线内模拟完成\n")
	fmt.Printf("   总收益率: %.2f%%\n", intrabar10Result.Metrics.TotalReturn)
	fmt.Printf("   交易次数: %d 笔\n", intrabar10Result.Metrics.TotalTrades)
	fmt.Println("")

	// ========== 对比 ==========
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("📈 对比结果")
	fmt.Println("=" + string(make([]rune, 80)))
	fmt.Println("")

	improvement := intrabar10Result.Metrics.TotalReturn - normalResult.Metrics.TotalReturn

	fmt.Printf("收益率: %.2f%% → %.2f%% (",
		normalResult.Metrics.TotalReturn, intrabar10Result.Metrics.TotalReturn)
	if improvement > 0 {
		fmt.Printf("+%.2f%% ✅)\n", improvement)
	} else {
		fmt.Printf("%.2f%% ❌)\n", improvement)
	}

	fmt.Printf("交易次数: %d → %d 笔 (%.1fx)\n",
		normalResult.Metrics.TotalTrades,
		intrabar10Result.Metrics.TotalTrades,
		float64(intrabar10Result.Metrics.TotalTrades)/float64(normalResult.Metrics.TotalTrades))

	fmt.Println("")

	if improvement > 5 {
		fmt.Println("✅ 假设得到验证！K线内模拟显著改善了回测结果")
	} else if improvement > 0 {
		fmt.Println("⚠️ 假设部分验证，有改善但不显著")
	} else {
		fmt.Println("❌ 假设未验证，需要进一步分析")
	}

	fmt.Println("")
	fmt.Println("🎉 测试完成！")
}
