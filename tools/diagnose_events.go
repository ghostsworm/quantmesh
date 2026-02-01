package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"quantmesh/database"
)

func main() {
	fmt.Println("=== QuantMesh 日志系統完整診斷報告 ===\n")

	// 第一部分：檢查運行日志數據庫 (logs.db)
	fmt.Println("📁 第一部分：運行日志數據庫 (logs.db)")
	fmt.Println("=" + "===========================================")
	checkLogsDB()

	fmt.Println()

	// 第二部分：檢查事件中心數據庫 (quantmesh.db 中的 events 表)
	fmt.Println("📁 第二部分：事件中心數據庫 (quantmesh.db)")
	fmt.Println("=" + "===========================================")
	checkEventsDB()
}

// checkLogsDB 檢查運行日志數據庫
func checkLogsDB() {
	// 檢查 logs.db 文件是否存在
	logsDBPath := "./logs.db"
	if _, err := os.Stat(logsDBPath); os.IsNotExist(err) {
		fmt.Printf("❌ 日志數據庫文件不存在: %s\n", logsDBPath)
		fmt.Println("   這是運行日志為空的根本原因！")
		fmt.Println("   可能原因：")
		fmt.Println("   1. 程序啟動時工作目錄不正確")
		fmt.Println("   2. 日志存儲初始化失敗")
		fmt.Println("   3. 數據庫文件被刪除")
		return
	}

	// 獲取文件信息
	fileInfo, _ := os.Stat(logsDBPath)
	fmt.Printf("✅ 日志數據庫文件存在: %s\n", logsDBPath)
	fmt.Printf("   文件大小: %d bytes (%.2f KB)\n", fileInfo.Size(), float64(fileInfo.Size())/1024)
	fmt.Printf("   修改時間: %s\n", fileInfo.ModTime().Format("2006-01-02 15:04:05"))

	// 連接數據庫
	db, err := sql.Open("sqlite3", logsDBPath)
	if err != nil {
		fmt.Printf("❌ 無法打開日志數據庫: %v\n", err)
		return
	}
	defer db.Close()

	// 查詢日志總數
	var totalCount int
	err = db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&totalCount)
	if err != nil {
		fmt.Printf("❌ 查詢日志失敗: %v\n", err)
		return
	}
	fmt.Printf("   日志總數: %d\n", totalCount)

	if totalCount == 0 {
		fmt.Println("   ⚠️ 日志表為空！這就是運行日志頁面顯示空的原因")
		fmt.Println("   可能原因：")
		fmt.Println("   1. 日志被清理任務刪除了")
		fmt.Println("   2. logger.InitLogStorage() 未被調用")
		fmt.Println("   3. 日志寫入時發生錯誤")
		return
	}

	// 按級別統計
	rows, err := db.Query("SELECT level, COUNT(*) FROM logs GROUP BY level")
	if err == nil {
		fmt.Println("   按級別統計:")
		for rows.Next() {
			var level string
			var count int
			rows.Scan(&level, &count)
			fmt.Printf("     %s: %d\n", level, count)
		}
		rows.Close()
	}

	// 獲取最近日志
	fmt.Println("\n   最近 5 條日志:")
	rows, err = db.Query("SELECT timestamp, level, message FROM logs ORDER BY id DESC LIMIT 5")
	if err == nil {
		for rows.Next() {
			var timestamp, level, message string
			rows.Scan(&timestamp, &level, &message)
			// 截斷過長的消息
			if len(message) > 80 {
				message = message[:80] + "..."
			}
			fmt.Printf("     [%s] %s: %s\n", timestamp, level, message)
		}
		rows.Close()
	}

	// 獲取最早和最新日志時間
	var oldest, newest string
	db.QueryRow("SELECT MIN(timestamp), MAX(timestamp) FROM logs").Scan(&oldest, &newest)
	fmt.Printf("\n   時間範圍: %s ~ %s\n", oldest, newest)
}

// checkEventsDB 檢查事件中心數據庫
func checkEventsDB() {
	// 使用默認數據庫配置 (SQLite)
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
		fmt.Printf("❌ 事件數據庫連接失敗: %v\n", err)
		return
	}
	defer db.Close()

	ctx := context.Background()

	// 1. 獲取總体统计
	stats, err := db.GetEventStats(ctx)
	if err != nil {
		log.Printf("獲取统计失败: %v", err)
	} else {
		fmt.Printf("📊 總体统计:\n")
		fmt.Printf("   總事件數: %d\n", stats.TotalCount)
		fmt.Printf("   嚴重事件: %d\n", stats.CriticalCount)
		fmt.Printf("   警告事件: %d\n", stats.WarningCount)
		fmt.Printf("   信息事件: %d\n", stats.InfoCount)
		fmt.Printf("   24小時內: %d\n\n", stats.Last24HoursCount)
	}

	// 2. 檢查严重程度分佈
	fmt.Printf("🔍 嚴重程度分析:\n")
	severityFilter := &database.EventFilter{Limit: 0}
	allEvents, err := db.GetEvents(ctx, severityFilter)
	if err != nil {
		log.Printf("獲取所有事件失败: %v", err)
	} else {
		severityCount := make(map[string]int)
		sourceCount := make(map[string]int)
		typeCount := make(map[string]int)
		invalidSeverity := 0
		emptyDetails := 0
		emptyMessage := 0

		for _, event := range allEvents {
			// 统计严重程度
			if event.Severity == "" {
				invalidSeverity++
				severityCount["[空值]"]++
			} else if event.Severity != "critical" && event.Severity != "warning" && event.Severity != "info" {
				invalidSeverity++
				severityCount[fmt.Sprintf("[無效:%s]", event.Severity)]++
			} else {
				severityCount[event.Severity]++
			}

			// 统计來源
			sourceCount[event.Source]++
			
			// 统计類型
			typeCount[event.Type]++

			// 检查空字段
			if event.Details == "" || event.Details == "{}" {
				emptyDetails++
			}
			if event.Message == "" {
				emptyMessage++
			}
		}

		// 顯示嚴重程度分佈
		for severity, count := range severityCount {
			fmt.Printf("   %s: %d\n", severity, count)
		}
		
		if invalidSeverity > 0 {
			fmt.Printf("   ⚠️  發現 %d 個無效嚴重程度值\n", invalidSeverity)
		}

		// 顯示空字段统计
		fmt.Printf("\n📝 數據完整性:\n")
		fmt.Printf("   空詳情字段: %d\n", emptyDetails)
		fmt.Printf("   空消息字段: %d\n", emptyMessage)

		// 顯示來源分佈
		fmt.Printf("\n📡 事件來源分佈:\n")
		sources := make([]string, 0, len(sourceCount))
		for source := range sourceCount {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		for _, source := range sources {
			fmt.Printf("   %s: %d\n", source, sourceCount[source])
		}

		// 顯示最频繁的事件類型
		fmt.Printf("\n🔢 最频繁的事件類型 (前10):\n")
		type typeCountPair struct {
			Type  string
			Count int
		}
		typeCounts := make([]typeCountPair, 0, len(typeCount))
		for t, c := range typeCount {
			typeCounts = append(typeCounts, typeCountPair{Type: t, Count: c})
		}
		sort.Slice(typeCounts, func(i, j int) bool {
			return typeCounts[i].Count > typeCounts[j].Count
		})
		
		for i, tc := range typeCounts {
			if i >= 10 {
				break
			}
			fmt.Printf("   %s: %d\n", tc.Type, tc.Count)
		}
	}

	// 3. 檢查最近的幾個事件样例
	fmt.Printf("\n🔍 最近事件样例 (前5個):\n")
	recentFilter := &database.EventFilter{Limit: 5}
	recentEvents, err := db.GetEvents(ctx, recentFilter)
	if err != nil {
		log.Printf("獲取最近事件失败: %v", err)
	} else {
		for i, event := range recentEvents {
			fmt.Printf("\n   事件 #%d:\n", i+1)
			fmt.Printf("     ID: %d\n", event.ID)
			fmt.Printf("     類型: %s\n", event.Type)
			fmt.Printf("     嚴重程度: %s\n", event.Severity)
			fmt.Printf("     來源: %s\n", event.Source)
			fmt.Printf("     標題: %s\n", event.Title)
			fmt.Printf("     消息: %s\n", event.Message)
			fmt.Printf("     詳情长度: %d 字符\n", len(event.Details))
			fmt.Printf("     時间: %s\n", event.CreatedAt.Format("2006-01-02 15:04:05"))
			
			// 嘗試解析詳情JSON
			if event.Details != "" && event.Details != "{}" {
				var details map[string]interface{}
				if err := json.Unmarshal([]byte(event.Details), &details); err != nil {
					fmt.Printf("     ⚠️  詳情JSON解析失败: %v\n", err)
				} else {
					fmt.Printf("     詳情字段數: %d\n", len(details))
				}
			}
		}
	}

	// 4. 提供修復建議
	fmt.Printf("\n🔧 修復建議:\n")
	fmt.Printf("   1. 執行數據清理，統一嚴重程度值為 critical/warning/info\n")
	fmt.Printf("   2. 為空詳情字段填充默认JSON: {}\n")
	fmt.Printf("   3. 為空消息字段填充默认消息\n")
	fmt.Printf("   4. 增强事件記錄的數據驗證邏輯\n")
	
	if len(os.Args) > 1 && os.Args[1] == "--fix" {
		fmt.Printf("\n🔨 開始執行修復...\n")
		performFixes(ctx, db)
	} else {
		fmt.Printf("\n💡 使用 --fix 參數執行自动修復\n")
	}

	// 打印診斷總結
	printDiagnosisSummary()
}

func performFixes(ctx context.Context, db database.Database) {
	fmt.Printf("修復功能尚未實現。請手动執行相應的SQL修復語句。\n")
	fmt.Printf("\n修復SQL參考:\n")
	
	fmt.Printf("-- 修復空的嚴重程度\n")
	fmt.Printf("UPDATE events SET severity = 'info' WHERE severity = '' OR severity IS NULL;\n\n")
	
	fmt.Printf("-- 修復空的詳情字段\n")
	fmt.Printf("UPDATE events SET details = '{}' WHERE details = '' OR details IS NULL;\n\n")
	
	fmt.Printf("-- 修復空的消息字段\n")
	fmt.Printf("UPDATE events SET message = title WHERE message = '' OR message IS NULL;\n\n")
	
	fmt.Printf("-- 修復非標準嚴重程度值\n")
	fmt.Printf("UPDATE events SET severity = 'info' WHERE severity NOT IN ('critical', 'warning', 'info');\n\n")
}

func printDiagnosisSummary() {
	fmt.Println("\n" + "=============================================")
	fmt.Println("📋 診斷總結")
	fmt.Println("=============================================")
	fmt.Println()
	fmt.Println("如果運行日志為空，請檢查：")
	fmt.Println("1. 確認 logs.db 文件存在且有內容")
	fmt.Println("2. 確認啟動日志中有 '✅ 日志存儲提供者已設置'")
	fmt.Println("3. 確認沒有看到 '[WARN] 初始化日志存儲失败'")
	fmt.Println()
	fmt.Println("如果事件中心為空，請檢查：")
	fmt.Println("1. 確認配置中 event_center.enabled: true")
	fmt.Println("2. 確認啟動日志中有 '✅ 事件中心已啟动'")
	fmt.Println("3. 確認有訂單成交或風控事件觸發")
}