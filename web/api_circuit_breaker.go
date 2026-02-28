package web

import (
	"net/http"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/risk"

	"github.com/gin-gonic/gin"
)

// 全局熔断器实例，由 main.go 初始化后设置
var globalCircuitBreaker *risk.GlobalCircuitBreaker

// SetGlobalCircuitBreaker 设置全局熔断器实例
func SetGlobalCircuitBreaker(gcb *risk.GlobalCircuitBreaker) {
	globalCircuitBreaker = gcb
}

// GetGlobalCircuitBreakerConfig 获取全局熔断器配置（用于 API 返回）
func GetGlobalCircuitBreakerConfig() *config.CircuitBreakerConfig {
	if globalCircuitBreaker == nil {
		return &config.CircuitBreakerConfig{}
	}
	return globalCircuitBreaker.GetConfig()
}

// ManualTriggerRequest 手动触发熔断请求
type ManualTriggerRequest struct {
	TriggeredBy string `json:"triggered_by"` // 操作人
	Reason      string `json:"reason"`       // 触发原因
}

// ManualRecoverRequest 手动恢复请求
type ManualRecoverRequest struct {
	TriggeredBy string `json:"triggered_by"` // 操作人
}

// getCircuitBreakerStatus 获取全局熔断器状态
func getCircuitBreakerStatus(c *gin.Context) {
	if globalCircuitBreaker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "熔断器未初始化"})
		return
	}

	status := globalCircuitBreaker.GetStatus()
	c.JSON(http.StatusOK, gin.H{
		"status": status,
	})
}

// manualTriggerCircuitBreaker 手动触发熔断
func manualTriggerCircuitBreaker(c *gin.Context) {
	if globalCircuitBreaker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "熔断器未初始化"})
		return
	}

	var req ManualTriggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TriggeredBy == "" {
		req.TriggeredBy = "unknown"
	}

	globalCircuitBreaker.ManualTrigger(req.TriggeredBy, req.Reason)
	logger.Info("🚨 [全局熔断] 手动触发熔断，操作人: %s，原因: %s", req.TriggeredBy, req.Reason)

	c.JSON(http.StatusOK, gin.H{
		"status":      "tripped",
		"triggered_by": req.TriggeredBy,
		"reason":      req.Reason,
	})
}

// manualRecoverCircuitBreaker 手动恢复熔断
func manualRecoverCircuitBreaker(c *gin.Context) {
	if globalCircuitBreaker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "熔断器未初始化"})
		return
	}

	var req ManualRecoverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TriggeredBy == "" {
		req.TriggeredBy = "unknown"
	}

	if err := globalCircuitBreaker.ManualRecover(req.TriggeredBy); err != nil {
		logger.Warn("⚠️ [全局熔断] 手动恢复失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	logger.Info("♻️ [全局熔断] 手动恢复，操作人: %s", req.TriggeredBy)

	c.JSON(http.StatusOK, gin.H{
		"status":       "recovered",
		"triggered_by": req.TriggeredBy,
	})
}

// getCircuitBreakerEvents 获取熔断事件历史
func getCircuitBreakerEvents(c *gin.Context) {
	if globalCircuitBreaker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "熔断器未初始化"})
		return
	}

	// 获取限制参数，默认 50 条
	limit := 50
	if l, exists := c.GetQuery("limit"); exists {
		if parsed, err := parseIntParam(l, 1, 500); err == nil {
			limit = parsed
		}
	}

	events := globalCircuitBreaker.GetEvents(limit)
	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"count":  len(events),
	})
}

// updateCircuitBreakerMetrics 更新熔断器统计数据
func updateCircuitBreakerMetrics(c *gin.Context) {
	if globalCircuitBreaker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "熔断器未初始化"})
		return
	}

	var req struct {
		DailyPnL          float64 `json:"daily_pnl"`
		MaxDrawdown       float64 `json:"max_drawdown"`
		ConsecutiveLosses int     `json:"consecutive_losses"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	globalCircuitBreaker.UpdateMetrics(req.DailyPnL, req.MaxDrawdown, req.ConsecutiveLosses)
	logger.Debug("📊 [全局熔断] 更新统计数据: DailyPnL=%.2f, MaxDrawdown=%.2f%%, ConsecutiveLosses=%d",
		req.DailyPnL, req.MaxDrawdown, req.ConsecutiveLosses)

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

// reportAuthFailure 报告 API 认证失败
func reportAuthFailure(c *gin.Context) {
	if globalCircuitBreaker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "熔断器未初始化"})
		return
	}

	globalCircuitBreaker.ReportAuthFailure()
	c.JSON(http.StatusOK, gin.H{"status": "reported"})
}

// reportWebSocketDisconnect 报告 WebSocket 断线
func reportWebSocketDisconnect(c *gin.Context) {
	if globalCircuitBreaker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "熔断器未初始化"})
		return
	}

	globalCircuitBreaker.ReportWebSocketDisconnect()
	c.JSON(http.StatusOK, gin.H{"status": "reported"})
}

// reportWebSocketReconnected 报告 WebSocket 重连
func reportWebSocketReconnected(c *gin.Context) {
	if globalCircuitBreaker == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "熔断器未初始化"})
		return
	}

	globalCircuitBreaker.ReportWebSocketReconnected()
	c.JSON(http.StatusOK, gin.H{"status": "reported"})
}
