package web

import (
	"net/http"

	"quantmesh/logger"
	"quantmesh/risk"

	"github.com/gin-gonic/gin"
)

// 全局紧急操作中心实例
var globalEmergencyCenter *risk.EmergencyCenter

// SetEmergencyCenter 设置全局紧急操作中心实例
func SetEmergencyCenter(ec *risk.EmergencyCenter) {
	globalEmergencyCenter = ec
}

// ExecuteScenarioRequest 执行场景请求
type ExecuteScenarioRequest struct {
	Scenario    string `json:"scenario"`     // 场景名称
	TriggeredBy string `json:"triggered_by"` // 操作人
	Reason      string `json:"reason"`       // 原因
}

// getEmergencyScenarios 获取所有紧急场景
func getEmergencyScenarios(c *gin.Context) {
	if globalEmergencyCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "紧急操作中心未初始化"})
		return
	}

	scenarios := globalEmergencyCenter.GetScenarios()
	c.JSON(http.StatusOK, gin.H{
		"scenarios": scenarios,
	})
}

// executeEmergencyScenario 执行紧急场景
func executeEmergencyScenario(c *gin.Context) {
	if globalEmergencyCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "紧急操作中心未初始化"})
		return
	}

	var req ExecuteScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Scenario == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "场景名称不能为空"})
		return
	}

	if req.TriggeredBy == "" {
		req.TriggeredBy = "unknown"
	}

	op, err := globalEmergencyCenter.ExecuteScenario(req.Scenario, req.TriggeredBy, req.Reason)
	if err != nil {
		logger.Error("❌ [紧急中心] 执行场景失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	logger.Warn("🚨 [紧急中心] 场景 %s 已触发，操作ID: %s，操作人: %s", req.Scenario, op.ID, req.TriggeredBy)

	c.JSON(http.StatusOK, gin.H{
		"operation_id": op.ID,
		"scenario":     op.Scenario,
		"status":       op.Status,
	})
}

// getEmergencyOperations 获取紧急操作历史
func getEmergencyOperations(c *gin.Context) {
	if globalEmergencyCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "紧急操作中心未初始化"})
		return
	}

	// 获取限制参数，默认 50 条
	limit := 50
	if l, exists := c.GetQuery("limit"); exists {
		if parsed, err := parseIntParam(l, 1, 500); err == nil {
			limit = parsed
		}
	}

	operations := globalEmergencyCenter.GetOperations(limit)
	c.JSON(http.StatusOK, gin.H{
		"operations": operations,
		"count":      len(operations),
	})
}

// getEmergencyModeStatus 获取紧急模式状态
func getEmergencyModeStatus(c *gin.Context) {
	if globalEmergencyCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "紧急操作中心未初始化"})
		return
	}

	isEmergencyMode := globalEmergencyCenter.IsEmergencyMode()
	c.JSON(http.StatusOK, gin.H{
		"emergency_mode": isEmergencyMode,
	})
}

// disableEmergencyMode 禁用紧急模式
func disableEmergencyMode(c *gin.Context) {
	if globalEmergencyCenter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "紧急操作中心未初始化"})
		return
	}

	var req struct {
		TriggeredBy string `json:"triggered_by"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TriggeredBy == "" {
		req.TriggeredBy = "unknown"
	}

	if err := globalEmergencyCenter.DisableEmergencyMode(req.TriggeredBy); err != nil {
		logger.Warn("⚠️ [紧急中心] 退出紧急模式失败: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	logger.Info("✅ [紧急中心] 已退出紧急模式，操作人: %s", req.TriggeredBy)

	c.JSON(http.StatusOK, gin.H{
		"status": "normal",
	})
}
