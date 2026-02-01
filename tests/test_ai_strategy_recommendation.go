//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"quantmesh/ai"
	"quantmesh/ai/processor"
	"quantmesh/ai/service"
	"quantmesh/config"
	"quantmesh/database"
)

func main() {
	fmt.Println("====================================")
	fmt.Println("测试 AI 策略参数推算功能")
	fmt.Println("====================================")

	// 1. 加载配置文件
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		fmt.Printf("❌ 加载配置文件失败: %v\n", err)
		os.Exit(1)
	}

	// 2. 获取 Gemini API Key
	geminiAPIKey := cfg.AI.GeminiAPIKey
	if geminiAPIKey == "" {
		geminiAPIKey = cfg.AI.APIKey
	}
	if geminiAPIKey == "" {
		fmt.Println("❌ 未找到 Gemini API Key，请在 config.yaml 中配置 ai.gemini_api_key")
		os.Exit(1)
	}
	fmt.Printf("✅ 已从配置文件读取 Gemini API Key: %s...\n", geminiAPIKey[:10])

	// 3. 初始化数据库（用于任务系统）
	fmt.Println("\n初始化数据库和任务系统...")
	dbConfig := &database.Config{
		Type:            cfg.Database.Type,
		DSN:             cfg.Database.DSN,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: time.Duration(cfg.Database.ConnMaxLifetime) * time.Second,
		LogLevel:        cfg.Database.LogLevel,
	}

	db, err := database.NewDatabase(dbConfig)
	if err != nil {
		fmt.Printf("❌ 初始化数据库失败: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	fmt.Println("✅ 数据库初始化成功")

	// 4. 初始化 AI 任务系统
	taskService := service.NewTaskService(db)
	aiService := service.NewAIService()
	taskProcessor := processor.NewTaskProcessor(taskService, aiService)

	// 设置全局任务服务
	ai.GlobalTaskService = taskService

	// 启动任务处理器（后台运行）
	go taskProcessor.Start()
	defer taskProcessor.Stop()

	// 等待任务处理器启动
	time.Sleep(2 * time.Second)
	fmt.Println("✅ AI 任务系统初始化成功")

	// 5. 创建 Gemini 客户端
	client := ai.NewGeminiClient(geminiAPIKey)

	// 6. 准备测试数据
	// Binance 上总共 6000 USDT，BTC 和 ETH 各 3000 USDT
	fmt.Println("\n准备测试数据...")
	fmt.Println("  - 交易所: Binance")
	fmt.Println("  - 总资金: 6000 USDT")
	fmt.Println("  - BTCUSDT: 3000 USDT")
	fmt.Println("  - ETHUSDT: 3000 USDT")

	// 模拟当前价格（实际应该从交易所获取）
	currentPrices := map[string]float64{
		"BTCUSDT": 95000.0, // 假设 BTC 价格 95000 USDT
		"ETHUSDT": 3500.0,  // 假设 ETH 价格 3500 USDT
	}

	req := &ai.GenerateConfigRequest{
		Exchange: "binance",
		Symbols:  []string{"BTCUSDT", "ETHUSDT"},
		SymbolCapitals: []ai.SymbolCapitalConfig{
			{Symbol: "BTCUSDT", Capital: 3000.0},
			{Symbol: "ETHUSDT", Capital: 3000.0},
		},
		CapitalMode:   "per_symbol",
		RiskProfile:    "balanced", // 平衡型风险偏好
		CurrentPrices: currentPrices,
	}

	// 7. 调用 AI 生成配置
	fmt.Println("\n开始调用 AI 生成策略配置...")
	fmt.Println("这可能需要几分钟时间，请耐心等待...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	startTime := time.Now()
	result, err := client.GenerateConfig(ctx, req)
	if err != nil {
		fmt.Printf("❌ AI 配置生成失败: %v\n", err)
		os.Exit(1)
	}
	duration := time.Since(startTime)

	fmt.Printf("✅ AI 配置生成成功！耗时: %v\n", duration)

	// 8. 调试：打印完整返回数据
	fmt.Println("\n【调试信息：完整返回数据结构】")
	fmt.Printf("  SymbolsConfig 数量: %d\n", len(result.SymbolsConfig))
	fmt.Printf("  GridConfig 数量: %d\n", len(result.GridConfig))
	fmt.Printf("  Allocation 数量: %d\n", len(result.Allocation))

	// 9. 显示结果
	fmt.Println("\n====================================")
	fmt.Println("AI 配置结果")
	fmt.Println("====================================")

	fmt.Println("\n【配置思路说明】")
	fmt.Println(result.Explanation)

	// 显示策略配比
	if len(result.SymbolsConfig) > 0 {
		fmt.Println("\n【策略配比详情】")
		for _, symbolConfig := range result.SymbolsConfig {
			fmt.Printf("\n币种: %s\n", symbolConfig.Symbol)
			fmt.Printf("  分配资金: %.2f USDT\n", symbolConfig.TotalAllocatedCapital)

			if len(symbolConfig.Strategies) > 0 {
				fmt.Println("  策略组合:")
				totalWeight := 0.0
				for _, strategy := range symbolConfig.Strategies {
					weightPercent := strategy.Weight * 100
					totalWeight += strategy.Weight
					fmt.Printf("    - %s: %.1f%%\n", strategy.Type, weightPercent)
				}
				fmt.Printf("  总配比: %.1f%%\n", totalWeight*100)
			} else {
				fmt.Println("  ⚠️  未返回策略配比信息")
			}

			// 显示网格参数
			if symbolConfig.PriceInterval > 0 {
				fmt.Println("  网格参数:")
				fmt.Printf("    - 价格间隔: %.2f%%\n", symbolConfig.PriceInterval)
				fmt.Printf("    - 每单金额: %.2f USDT\n", symbolConfig.OrderQuantity)
				fmt.Printf("    - 买单窗口: %d\n", symbolConfig.BuyWindowSize)
				fmt.Printf("    - 卖单窗口: %d\n", symbolConfig.SellWindowSize)

				// 显示网格深度（如果有）
				if symbolConfig.GridRiskControl.Enabled && symbolConfig.GridRiskControl.MaxGridLayers > 0 {
					fmt.Printf("    - 最大网格层数: %d\n", symbolConfig.GridRiskControl.MaxGridLayers)
				}
			}
		}
	} else if len(result.GridConfig) > 0 {
		// 兼容旧格式
		fmt.Println("\n【网格配置详情】")
		for _, grid := range result.GridConfig {
			fmt.Printf("\n币种: %s\n", grid.Symbol)
			fmt.Printf("  价格间隔: %.2f%%\n", grid.PriceInterval)
			fmt.Printf("  每单金额: %.2f USDT\n", grid.OrderQuantity)
			fmt.Printf("  买单窗口: %d\n", grid.BuyWindowSize)
			fmt.Printf("  卖单窗口: %d\n", grid.SellWindowSize)

			if grid.GridRiskControl != nil && grid.GridRiskControl.MaxGridLayers > 0 {
				fmt.Printf("  最大网格层数: %d\n", grid.GridRiskControl.MaxGridLayers)
			}
		}

		if len(result.Allocation) > 0 {
			fmt.Println("\n【资金分配详情】")
			for _, alloc := range result.Allocation {
				fmt.Printf("  %s: %.2f USDT (%.1f%%)\n", alloc.Symbol, alloc.MaxAmountUSDT, alloc.MaxPercentage)
			}
		}
	}

	// 10. 验证结果
	fmt.Println("\n====================================")
	fmt.Println("结果验证")
	fmt.Println("====================================")

	success := true

	if len(result.SymbolsConfig) > 0 {
		totalCapital := 0.0
		for _, sc := range result.SymbolsConfig {
			totalCapital += sc.TotalAllocatedCapital
		}

		fmt.Printf("总分配资金: %.2f USDT (预期: 6000.00 USDT)\n", totalCapital)
		if totalCapital > 6000*1.05 || totalCapital < 6000*0.95 {
			fmt.Printf("⚠️  警告: 总分配资金与预期差异较大\n")
			success = false
		} else {
			fmt.Println("✅ 总分配资金符合预期")
		}

		// 验证策略配比总和
		for _, sc := range result.SymbolsConfig {
			totalWeight := 0.0
			for _, s := range sc.Strategies {
				totalWeight += s.Weight
			}
			if totalWeight > 0 {
				fmt.Printf("\n%s 策略配比总和: %.1f%%\n", sc.Symbol, totalWeight*100)
				if totalWeight < 0.99 || totalWeight > 1.01 {
					fmt.Printf("⚠️  警告: %s 策略配比总和不为 100%%\n", sc.Symbol)
					success = false
				} else {
					fmt.Printf("✅ %s 策略配比总和正确\n", sc.Symbol)
				}
			}
		}
	}

	if success {
		fmt.Println("\n✅ 所有验证通过！")
	} else {
		fmt.Println("\n⚠️  部分验证未通过，但配置已生成")
	}

	fmt.Println("\n====================================")
	fmt.Println("测试完成!")
	fmt.Println("====================================")
}
