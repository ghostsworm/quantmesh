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
	fmt.Println("测試 AI 策略参數推算功能")
	fmt.Println("====================================")

	// 1. 加載配置文件
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		fmt.Printf("❌ 加載配置文件失败: %v\n", err)
		os.Exit(1)
	}

	// 2. 獲取 Gemini API Key
	geminiAPIKey := cfg.AI.GeminiAPIKey
	if geminiAPIKey == "" {
		geminiAPIKey = cfg.AI.APIKey
	}
	if geminiAPIKey == "" {
		fmt.Println("❌ 未找到 Gemini API Key，请在 config.yaml 中配置 ai.gemini_api_key")
		os.Exit(1)
	}
	fmt.Printf("✅ 已從配置文件读取 Gemini API Key: %s...\n", geminiAPIKey[:10])

	// 3. 初始化數據库（用於任務系统）
	fmt.Println("\n初始化數據库和任務系统...")
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
		fmt.Printf("❌ 初始化數據库失败: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	fmt.Println("✅ 數據库初始化成功")

	// 4. 初始化 AI 任務系统
	taskService := service.NewTaskService(db)
	aiService := service.NewAIService()
	taskProcessor := processor.NewTaskProcessor(taskService, aiService)

	// 設置全局任務服務
	ai.GlobalTaskService = taskService

	// 啟动任務处理器（后台运行）
	go taskProcessor.Start()
	defer taskProcessor.Stop()

	// 等待任務处理器啟动
	time.Sleep(2 * time.Second)
	fmt.Println("✅ AI 任務系统初始化成功")

	// 5. 創建 Gemini 客戶端
	client := ai.NewGeminiClient(geminiAPIKey)

	// 6. 准备测試數據
	// Binance 上總共 6000 USDT，BTC 和 ETH 各 3000 USDT
	fmt.Println("\n准备测試數據...")
	fmt.Println("  - 交易所: Binance")
	fmt.Println("  - 總资金: 6000 USDT")
	fmt.Println("  - BTCUSDT: 3000 USDT")
	fmt.Println("  - ETHUSDT: 3000 USDT")

	// 模拟當前價格（實際应該從交易所獲取）
	currentPrices := map[string]float64{
		"BTCUSDT": 95000.0, // 假設 BTC 價格 95000 USDT
		"ETHUSDT": 3500.0,  // 假設 ETH 價格 3500 USDT
	}

	req := &ai.GenerateConfigRequest{
		Exchange: "binance",
		Symbols:  []string{"BTCUSDT", "ETHUSDT"},
		SymbolCapitals: []ai.SymbolCapitalConfig{
			{Symbol: "BTCUSDT", Capital: 3000.0},
			{Symbol: "ETHUSDT", Capital: 3000.0},
		},
		CapitalMode:   "per_symbol",
		RiskProfile:    "balanced", // 平衡型风險偏好
		CurrentPrices: currentPrices,
	}

	// 7. 調用 AI 生成配置
	fmt.Println("\n开始調用 AI 生成策略配置...")
	fmt.Println("这可能需要几分钟時间，请耐心等待...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	startTime := time.Now()
	result, err := client.GenerateConfig(ctx, req)
	if err != nil {
		fmt.Printf("❌ AI 配置生成失败: %v\n", err)
		os.Exit(1)
	}
	duration := time.Since(startTime)

	fmt.Printf("✅ AI 配置生成成功！耗時: %v\n", duration)

	// 8. 調試：打印完整返回數據
	fmt.Println("\n【調試信息：完整返回數據結構】")
	fmt.Printf("  SymbolsConfig 數量: %d\n", len(result.SymbolsConfig))
	fmt.Printf("  GridConfig 數量: %d\n", len(result.GridConfig))
	fmt.Printf("  Allocation 數量: %d\n", len(result.Allocation))

	// 9. 显示結果
	fmt.Println("\n====================================")
	fmt.Println("AI 配置結果")
	fmt.Println("====================================")

	fmt.Println("\n【配置思路說明】")
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
				fmt.Printf("  總配比: %.1f%%\n", totalWeight*100)
			} else {
				fmt.Println("  ⚠️  未返回策略配比信息")
			}

			// 显示网格参數
			if symbolConfig.PriceInterval > 0 {
				fmt.Println("  网格参數:")
				fmt.Printf("    - 價格間隔: %.2f%%\n", symbolConfig.PriceInterval)
				fmt.Printf("    - 每單金額: %.2f USDT\n", symbolConfig.OrderQuantity)
				fmt.Printf("    - 買單窗口: %d\n", symbolConfig.BuyWindowSize)
				fmt.Printf("    - 賣單視窗: %d\n", symbolConfig.SellWindowSize)

				// 显示网格深度（如果有）
				if symbolConfig.GridRiskControl.Enabled && symbolConfig.GridRiskControl.MaxGridLayers > 0 {
					fmt.Printf("    - 最大网格层數: %d\n", symbolConfig.GridRiskControl.MaxGridLayers)
				}
			}
		}
	} else if len(result.GridConfig) > 0 {
		// 兼容舊格式
		fmt.Println("\n【网格配置详情】")
		for _, grid := range result.GridConfig {
			fmt.Printf("\n币种: %s\n", grid.Symbol)
			fmt.Printf("  價格間隔: %.2f%%\n", grid.PriceInterval)
			fmt.Printf("  每單金額: %.2f USDT\n", grid.OrderQuantity)
			fmt.Printf("  買單窗口: %d\n", grid.BuyWindowSize)
			fmt.Printf("  賣單視窗: %d\n", grid.SellWindowSize)

			if grid.GridRiskControl != nil && grid.GridRiskControl.MaxGridLayers > 0 {
				fmt.Printf("  最大网格层數: %d\n", grid.GridRiskControl.MaxGridLayers)
			}
		}

		if len(result.Allocation) > 0 {
			fmt.Println("\n【资金分配详情】")
			for _, alloc := range result.Allocation {
				fmt.Printf("  %s: %.2f USDT (%.1f%%)\n", alloc.Symbol, alloc.MaxAmountUSDT, alloc.MaxPercentage)
			}
		}
	}

	// 10. 驗证結果
	fmt.Println("\n====================================")
	fmt.Println("結果驗证")
	fmt.Println("====================================")

	success := true

	if len(result.SymbolsConfig) > 0 {
		totalCapital := 0.0
		for _, sc := range result.SymbolsConfig {
			totalCapital += sc.TotalAllocatedCapital
		}

		fmt.Printf("總分配资金: %.2f USDT (預期: 6000.00 USDT)\n", totalCapital)
		if totalCapital > 6000*1.05 || totalCapital < 6000*0.95 {
			fmt.Printf("⚠️  警告: 總分配资金與預期差异较大\n")
			success = false
		} else {
			fmt.Println("✅ 總分配资金符合預期")
		}

		// 驗证策略配比總和
		for _, sc := range result.SymbolsConfig {
			totalWeight := 0.0
			for _, s := range sc.Strategies {
				totalWeight += s.Weight
			}
			if totalWeight > 0 {
				fmt.Printf("\n%s 策略配比總和: %.1f%%\n", sc.Symbol, totalWeight*100)
				if totalWeight < 0.99 || totalWeight > 1.01 {
					fmt.Printf("⚠️  警告: %s 策略配比總和不為 100%%\n", sc.Symbol)
					success = false
				} else {
					fmt.Printf("✅ %s 策略配比總和正确\n", sc.Symbol)
				}
			}
		}
	}

	if success {
		fmt.Println("\n✅ 所有驗证通過！")
	} else {
		fmt.Println("\n⚠️  部分驗证未通過，但配置已生成")
	}

	fmt.Println("\n====================================")
	fmt.Println("测試完成!")
	fmt.Println("====================================")
}
