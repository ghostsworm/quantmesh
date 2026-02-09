//go:build tools

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"quantmesh/database"
)

func main() {
	fmt.Println("=== API 与數據庫一致性验证 ===\n")

	// 使用默認數據庫配置
	dbConfig := &database.Config{
		Type:            "sqlite",
		DSN:             "./data/quantmesh.db",
		MaxOpenConns:    100,
		MaxIdleConns:    10,
		ConnMaxLifetime: 3600 * time.Second,
		LogLevel:        "error",
	}

	// 連接數據庫
	db, err := database.NewDatabase(dbConfig)
	if err != nil {
		log.Fatalf("數據庫連接失败: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// 1. 直接查询數據庫统计
	fmt.Println("📊 直接數據庫查询:")
	stats, err := db.GetEventStats(ctx)
	if err != nil {
		log.Printf("数据库统计查询失败: %v", err)
	} else {
		fmt.Printf("   總數: %d\n", stats.TotalCount)
		fmt.Printf("   Critical: %d\n", stats.CriticalCount)
		fmt.Printf("   Warning: %d\n", stats.WarningCount)
		fmt.Printf("   Info: %d\n", stats.InfoCount)
		fmt.Printf("   24小时内: %d\n", stats.Last24HoursCount)
	}

	// 2. 測试不同條件的查询
	fmt.Println("\n🔍 分类查询测试:")
	
	// 全部事件
	allFilter := &database.EventFilter{Limit: 1000}
	allEvents, err := db.GetEvents(ctx, allFilter)
	if err != nil {
		log.Printf("查询全部事件失败: %v", err)
	} else {
		fmt.Printf("   全部事件: %d 個\n", len(allEvents))
	}

	// Critical 事件
	criticalFilter := &database.EventFilter{Severity: "critical", Limit: 1000}
	criticalEvents, err := db.GetEvents(ctx, criticalFilter)
	if err != nil {
		log.Printf("查询critical事件失败: %v", err)
	} else {
		fmt.Printf("   Critical 事件: %d 個\n", len(criticalEvents))
	}

	// Warning 事件
	warningFilter := &database.EventFilter{Severity: "warning", Limit: 1000}
	warningEvents, err := db.GetEvents(ctx, warningFilter)
	if err != nil {
		log.Printf("查询warning事件失败: %v", err)
	} else {
		fmt.Printf("   Warning 事件: %d 個\n", len(warningEvents))
	}

	// Info 事件
	infoFilter := &database.EventFilter{Severity: "info", Limit: 1000}
	infoEvents, err := db.GetEvents(ctx, infoFilter)
	if err != nil {
		log.Printf("查询info事件失败: %v", err)
	} else {
		fmt.Printf("   Info 事件: %d 個\n", len(infoEvents))
	}

	// 3. 測试API端点（如果服务正在运行）
	fmt.Println("\n🌐 API 端点测试:")
	
	// 測试统计API
	apiURL := "http://localhost:8080/api/events/stats"
	resp, err := http.Get(apiURL)
	if err != nil {
		fmt.Printf("   API 服务未运行或無法連接: %v\n", err)
	} else {
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("   無法讀取API響應: %v\n", err)
		} else {
			fmt.Printf("   API 统计響應: %s\n", string(body))
			
			// 解析API响应並与数据库对比
			var apiStats map[string]interface{}
			if err := json.Unmarshal(body, &apiStats); err == nil {
				if totalCount, ok := apiStats["total_count"].(float64); ok {
					fmt.Printf("   API vs DB 总数对比: %.0f vs %d\n", totalCount, stats.TotalCount)
				}
			}
		}
	}

	// 4. 檢查是否有重複或孤立记录
	fmt.Println("\n🔧 數據完整性检查:")
	
	// 检查是否有无效的严重程度值
	invalidSeverityFilter := &database.EventFilter{Limit: 1000}
	allEventsForValidation, err := db.GetEvents(ctx, invalidSeverityFilter)
	if err == nil {
		invalidCount := 0
		for _, event := range allEventsForValidation {
			if event.Severity != "critical" && event.Severity != "warning" && event.Severity != "info" {
				invalidCount++
				if invalidCount <= 5 { // 只显示前5个
					fmt.Printf("   無效严重程度: ID=%d, severity='%s'\n", event.ID, event.Severity)
				}
			}
		}
		if invalidCount > 5 {
			fmt.Printf("   ... 還有 %d 個無效严重程度記錄\n", invalidCount-5)
		}
		if invalidCount == 0 {
			fmt.Printf("   ✅ 所有事件的严重程度都是有效的\n")
		}
	}

	fmt.Println("\n✨ 验证完成")
}