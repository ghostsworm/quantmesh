package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quantmesh/storage"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// ========== 日志相关API ==========

var (
	// 日志存儲提供者（需要從main.go注入）
	logStorageProvider LogStorageProvider
)

// LogStorageProvider 日志存儲提供者接口
type LogStorageProvider interface {
	GetLogs(params storage.LogQueryParams) ([]*LogRecordResponse, int, error)
	CleanOldLogsByLevel(days int, levels []string) (int64, error)
	Vacuum() error
	GetLogStats() (map[string]interface{}, error)
}

// logStorageAdapter 日志存儲适配器
type logStorageAdapter struct {
	storage *storage.LogStorage
}

// NewLogStorageAdapter 創建日志存儲适配器
func NewLogStorageAdapter(ls *storage.LogStorage) LogStorageProvider {
	return &logStorageAdapter{storage: ls}
}

// GetLogs 實現 LogStorageProvider 接口
func (a *logStorageAdapter) GetLogs(params storage.LogQueryParams) ([]*LogRecordResponse, int, error) {
	logs, total, err := a.storage.GetLogs(params)
	if err != nil {
		return nil, 0, err
	}

	// 轉换為响应格式
	result := make([]*LogRecordResponse, len(logs))
	for i, log := range logs {
		result[i] = &LogRecordResponse{
			ID:        log.ID,
			Timestamp: utils.ToUTC8(log.Timestamp),
			Level:     log.Level,
			Message:   log.Message,
			BotID:     log.BotID,
		}
	}

	return result, total, nil
}

// CleanOldLogsByLevel 實現 LogStorageProvider 接口
func (a *logStorageAdapter) CleanOldLogsByLevel(days int, levels []string) (int64, error) {
	return a.storage.CleanOldLogsByLevel(days, levels)
}

// Vacuum 實現 LogStorageProvider 接口
func (a *logStorageAdapter) Vacuum() error {
	return a.storage.Vacuum()
}

// GetLogStats 實現 LogStorageProvider 接口
func (a *logStorageAdapter) GetLogStats() (map[string]interface{}, error) {
	return a.storage.GetLogStats()
}

// LogRecordResponse 日志記錄响应
type LogRecordResponse struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	BotID     string    `json:"bot_id,omitempty"`
}

// SetLogStorageProvider 設置日志存儲提供者
func SetLogStorageProvider(provider LogStorageProvider) {
	logStorageProvider = provider
}

// getLogs 獲取日志
// GET /api/logs
// 参數：
//   - start_time: 开始時间（可選，ISO 8601格式）
//   - end_time: 結束時间（可選，ISO 8601格式，默认當前時间）
//   - level: 日志级别（可選，DEBUG/INFO/WARN/ERROR/FATAL）
//   - keyword: 关键词搜索（可選）
//   - exchange / symbol / market_type: 可選，對 message 子串匹配（多條件 AND）
//   - bot_id: 可選，按 logs.bot_id 列精確匹配（舊數據為空則不命中）
//   - limit: 每页數量（可選，預設 100，最大 2000）
//   - offset: 偏移量（可選，默认0）
func getLogs(c *gin.Context) {
	if logStorageProvider == nil {
		c.JSON(http.StatusOK, gin.H{"logs": []interface{}{}, "total": 0})
		return
	}

	// 解析参數
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	level := c.Query("level")
	keyword := c.Query("keyword")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 如果没有指定开始時间，默认最近7天
	if startTime.IsZero() {
		startTime = endTime.AddDate(0, 0, -7)
	}

	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 2000 {
			limit = 2000
		}
	}

	offset := 0
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 查詢日志
	logs, total, err := logStorageProvider.GetLogs(storage.LogQueryParams{
		StartTime:  startTime,
		EndTime:    endTime,
		Level:      level,
		Keyword:    keyword,
		Limit:      limit,
		Offset:     offset,
		Exchange:   strings.TrimSpace(c.Query("exchange")),
		Symbol:     strings.TrimSpace(c.Query("symbol")),
		MarketType: strings.TrimSpace(c.Query("market_type")),
		BotID:      strings.TrimSpace(c.Query("bot_id")),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// cleanLogs 清理日志
// POST /api/logs/clean
// 参數：
//   - days: 保留天數（默认7天）
//   - levels: 要清理的日志级别列表，如 ["INFO", "WARN"]（可選，默认清理所有级别）
func cleanLogs(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存儲未初始化")
		return
	}

	var req struct {
		Days   int      `json:"days"`
		Levels []string `json:"levels"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_request")
		return
	}

	if req.Days <= 0 {
		req.Days = 7 // 默认7天
	}

	var rowsAffected int64
	var err error

	if len(req.Levels) > 0 {
		// 清理指定级别的日志
		rowsAffected, err = logStorageProvider.CleanOldLogsByLevel(req.Days, req.Levels)
	} else {
		// 清理所有级别的日志
		rowsAffected, err = logStorageProvider.CleanOldLogsByLevel(req.Days, []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"})
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"rows_affected": rowsAffected,
		"message":       fmt.Sprintf("已清理 %d 条日志", rowsAffected),
	})
}

// getLogStats 獲取日志统计信息
// GET /api/logs/stats
func getLogStats(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存儲未初始化")
		return
	}

	stats, err := logStorageProvider.GetLogStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// vacuumLogs 优化日志數據库
// POST /api/logs/vacuum
func vacuumLogs(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存儲未初始化")
		return
	}

	if err := logStorageProvider.Vacuum(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "數據库优化完成",
	})
}
