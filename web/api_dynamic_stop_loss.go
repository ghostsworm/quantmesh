package web

import (
	"math"
	"net/http"
	"strings"

	"quantmesh/logger"
	"quantmesh/risk"

	"github.com/gin-gonic/gin"
)

// 全局动态止损管理器实例
var globalDynamicStopLossManager *risk.DynamicStopLossManager

// SetDynamicStopLossManager 设置全局动态止损管理器实例
func SetDynamicStopLossManager(dslm *risk.DynamicStopLossManager) {
	globalDynamicStopLossManager = dslm
}

// getDynamicStopLossSlots 获取动态止损槽位
func getDynamicStopLossSlots(c *gin.Context) {
	if globalDynamicStopLossManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "动态止损管理器未初始化"})
		return
	}

	slots := globalDynamicStopLossManager.GetActiveSlots()
	c.JSON(http.StatusOK, gin.H{
		"slots": slots,
		"count": len(slots),
	})
}

// getDynamicStopLossStats 获取动态止损统计信息
func getDynamicStopLossStats(c *gin.Context) {
	if globalDynamicStopLossManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "动态止损管理器未初始化"})
		return
	}

	totalAdjustments := globalDynamicStopLossManager.GetTotalAdjustments()
	c.JSON(http.StatusOK, gin.H{
		"total_adjustments": totalAdjustments,
	})
}

// AdjustStopLossRequest 手动调整止损请求
type AdjustStopLossRequest struct {
	BotID       string  `json:"bot_id"`
	NewStopLoss float64 `json:"new_stop_loss"`
	Reason      string  `json:"reason"`
}

// adjustStopLoss 手动调整止损
func adjustStopLoss(c *gin.Context) {
	if globalDynamicStopLossManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "动态止损管理器未初始化"})
		return
	}

	var req AdjustStopLossRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	req.BotID = strings.TrimSpace(req.BotID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.BotID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bot_id is required"})
		return
	}
	if math.IsNaN(req.NewStopLoss) || math.IsInf(req.NewStopLoss, 0) || req.NewStopLoss <= 0 || req.NewStopLoss > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "new_stop_loss must be between 0 and 1"})
		return
	}

	logger.Warn("⚠️ [动态止损] 手动调整止损接口尚未实现，已拒绝请求: BotID=%s, NewStopLoss=%.4f, Reason=%s",
		req.BotID, req.NewStopLoss, req.Reason)

	c.JSON(http.StatusNotImplemented, gin.H{
		"error":         "manual dynamic stop-loss adjustment is not implemented",
		"bot_id":        req.BotID,
		"new_stop_loss": req.NewStopLoss,
	})
}
