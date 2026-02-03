package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"quantmesh/monitor"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

var klineCollector *monitor.KlineCollector

// SetKlineCollector 设置K线收集器（从main.go注入）
func SetKlineCollector(collector *monitor.KlineCollector) {
	klineCollector = collector
}

// listKlineFilesHandler 列出所有K线数据文件
// GET /api/kline-files
func listKlineFilesHandler(c *gin.Context) {
	if klineCollector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "K线收集器未初始化"})
		return
	}

	files, err := klineCollector.ListFiles()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"files":   files,
	})
}

// protectKlineFileHandler 保护K线文件
// POST /api/kline-files/:filename/protect
func protectKlineFileHandler(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	if err := storageProv.GetStorage().ProtectKlineFile(filename); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "文件已保护",
	})
}

// unprotectKlineFileHandler 取消保护K线文件
// DELETE /api/kline-files/:filename/protect
func unprotectKlineFileHandler(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	if err := storageProv.GetStorage().UnprotectKlineFile(filename); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "文件保护已取消",
	})
}

// downloadKlineFileHandler 下载K线文件
// GET /api/kline-files/:filename/download
func downloadKlineFileHandler(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件名不能为空"})
		return
	}

	// 安全检查：防止路径遍历攻击
	if strings.Contains(filename, "..") || strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的文件名"})
		return
	}

	if klineCollector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "K线收集器未初始化"})
		return
	}

	filepath := filepath.Join(klineCollector.GetDataDir(), filename)
	c.File(filepath)
}

// listAvailableKlineFilesHandler 列出可用于回测的 K 线文件
// GET /api/kline-files/available?exchange=&symbol=&interval=
func listAvailableKlineFilesHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	sqliteStorage, ok := storageProv.GetStorage().(*storage.SQLiteStorage)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "需要 SQLite 存储服务"})
		return
	}

	// 解析查询参数
	filter := &storage.KlineFileFilter{
		Exchange: c.Query("exchange"),
		Symbol:   c.Query("symbol"),
		Interval: c.Query("interval"),
		Status:   "completed", // 只返回已完成的文件
		Limit:    1000,        // 限制返回数量
	}

	files, err := sqliteStorage.ListKlineFiles(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("查询文件列表失败: %v", err)})
		return
	}

	// 转换为前端需要的格式
	result := make([]gin.H, len(files))
	for i, kf := range files {
		timeRange := ""
		if kf.EndTime != nil {
			if kf.StartTime.Format("2006-01-02") == kf.EndTime.Format("2006-01-02") {
				// 单日文件
				timeRange = kf.StartTime.Format("2006-01-02")
			} else {
				// 时间段文件
				timeRange = fmt.Sprintf("%s ~ %s", kf.StartTime.Format("2006-01-02"), kf.EndTime.Format("2006-01-02"))
			}
		} else {
			// 采集中的文件（理论上这里不会出现，因为我们过滤了 status=completed）
			timeRange = kf.StartTime.Format("2006-01-02") + " (采集中)"
		}

		result[i] = gin.H{
			"id":         kf.ID,
			"filename":   kf.Filename,
			"exchange":   kf.Exchange,
			"symbol":     kf.Symbol,
			"interval":   kf.Interval,
			"time_range": timeRange,
			"start_time": kf.StartTime.Format(time.RFC3339),
			"end_time": func() string {
				if kf.EndTime != nil {
					return kf.EndTime.Format(time.RFC3339)
				} else {
					return ""
				}
			}(),
			"status":       kf.Status,
			"has_depth":    kf.HasDepth,
			"candle_count": kf.CandleCount,
			"file_size":    kf.FileSize,
			"source":       kf.Source,
			"created_at":   kf.CreatedAt.Format(time.RFC3339),
			"updated_at":   kf.UpdatedAt.Format(time.RFC3339),
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"files":   result,
	})
}
