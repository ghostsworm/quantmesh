package web

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"quantmesh/database"
	"quantmesh/logger"
	"quantmesh/position"
)

// PlanManagerProvider 仓位计划管理器提供者（由 main 注入）
type PlanManagerProvider interface {
	GetPlanManager() *position.PlanManager
}

var planManagerProviderVar PlanManagerProvider

// SetPlanManagerProvider 设置仓位计划管理器提供者
func SetPlanManagerProvider(p PlanManagerProvider) {
	if p != nil {
		planManagerProviderVar = p
	}
}

func getPlanManager(c *gin.Context) *position.PlanManager {
	if planManagerProviderVar == nil {
		return nil
	}
	return planManagerProviderVar.GetPlanManager()
}

// getCurrentPositionValueUSDT 根据 exchange:symbol 从 positionProviders 计算当前仓位价值（USDT）
func getCurrentPositionValueUSDT(exchange, symbol string) float64 {
	key := exchange + ":" + symbol
	providersMu.RLock()
	prov := positionProviders[key]
	providersMu.RUnlock()
	if prov == nil {
		return 0
	}
	slots := prov.GetAllSlots()
	var total float64
	for _, s := range slots {
		if s.PositionStatus == "FILLED" && s.PositionQty > 0 && s.Price > 0 {
			total += s.Price * s.PositionQty
		}
	}
	return total
}

// getPositionPlans 获取仓位计划列表
// GET /api/position-plans
func getPositionPlans(c *gin.Context) {
	pm := getPlanManager(c)
	if pm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "仓位计划服务不可用"})
		return
	}

	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	status := c.Query("status")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	if limit <= 0 {
		limit = 100
	}

	filter := &database.PositionPlanFilter{
		Exchange: exchange,
		Symbol:   symbol,
		Status:   status,
		Limit:    limit,
		Offset:   offset,
	}

	plans, err := pm.GetPlans(c.Request.Context(), filter)
	if err != nil {
		logger.Warn("获取仓位计划列表失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"plans":   plans,
	})
}

// getPositionPlanByID 获取单个仓位计划
// GET /api/position-plans/:id
func getPositionPlanByID(c *gin.Context) {
	pm := getPlanManager(c)
	if pm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "仓位计划服务不可用"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的计划 ID"})
		return
	}

	plan, err := pm.GetPlan(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if plan == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "计划不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"plan":    plan,
	})
}

// createPositionPlanRequest 创建计划请求体
type createPositionPlanRequest struct {
	Exchange         string  `json:"exchange" binding:"required"`
	Symbol           string  `json:"symbol" binding:"required"`
	StrategyID       string  `json:"strategyId"`
	TargetAmountUSDT float64 `json:"targetAmountUsdt" binding:"required"`
	NotifyOnComplete bool    `json:"notifyOnComplete"`
	AutoAdjustLimit  bool    `json:"autoAdjustLimit"`
}

// createPositionPlan 创建仓位计划
// POST /api/position-plans
func createPositionPlan(c *gin.Context) {
	pm := getPlanManager(c)
	if pm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "仓位计划服务不可用"})
		return
	}

	var req createPositionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	if req.TargetAmountUSDT < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "目标仓位不能为负数"})
		return
	}

	initialAmount := getCurrentPositionValueUSDT(req.Exchange, req.Symbol)

	plan := &database.PositionPlan{
		Exchange:         req.Exchange,
		Symbol:           req.Symbol,
		StrategyID:       req.StrategyID,
		TargetAmountUSDT: req.TargetAmountUSDT,
		InitialAmount:    initialAmount,
		NotifyOnComplete: req.NotifyOnComplete,
		AutoAdjustLimit:  req.AutoAdjustLimit,
	}

	created, err := pm.CreatePlan(c.Request.Context(), plan)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if created == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "当前仓位已是目标仓位，无需创建计划",
			"current": initialAmount,
			"target":  req.TargetAmountUSDT,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"plan":    created,
		"message": "计划已创建",
	})
}

// updatePositionPlanRequest 更新计划请求体（仅允许更新部分字段）
type updatePositionPlanRequest struct {
	TargetAmountUSDT *float64 `json:"targetAmountUsdt"`
	NotifyOnComplete *bool    `json:"notifyOnComplete"`
}

// updatePositionPlan 更新仓位计划
// PUT /api/position-plans/:id
func updatePositionPlan(c *gin.Context) {
	pm := getPlanManager(c)
	if pm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "仓位计划服务不可用"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的计划 ID"})
		return
	}

	var req updatePositionPlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误: " + err.Error()})
		return
	}

	plan, err := pm.GetPlan(c.Request.Context(), id)
	if err != nil || plan == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "计划不存在"})
		return
	}
	if plan.Status == position.PlanStatusCompleted || plan.Status == position.PlanStatusCancelled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "已完成或已取消的计划不能修改"})
		return
	}

	if req.TargetAmountUSDT != nil && *req.TargetAmountUSDT >= 0 {
		plan.TargetAmountUSDT = *req.TargetAmountUSDT
	}
	if req.NotifyOnComplete != nil {
		plan.NotifyOnComplete = *req.NotifyOnComplete
	}

	if err := pm.UpdatePlan(c.Request.Context(), plan); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"plan":    plan,
		"message": "计划已更新",
	})
}

// cancelPositionPlanRequest 取消计划请求体
type cancelPositionPlanRequest struct {
	RestoreLimit bool `json:"restoreLimit"` // 是否恢复原始资金限制
}

// cancelPositionPlan 取消仓位计划
// DELETE /api/position-plans/:id
func cancelPositionPlan(c *gin.Context) {
	pm := getPlanManager(c)
	if pm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "仓位计划服务不可用"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的计划 ID"})
		return
	}

	restoreLimit := false
	// 支持 query ?restoreLimit=true 或 body
	if c.Query("restoreLimit") == "true" {
		restoreLimit = true
	} else {
		var req cancelPositionPlanRequest
		_ = c.ShouldBindJSON(&req)
		restoreLimit = req.RestoreLimit
	}

	if err := pm.CancelPlan(c.Request.Context(), id, restoreLimit); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "计划已取消",
	})
}

// getPositionPlanCheck 检查当前仓位与目标差异
// GET /api/position-plans/check?exchange=xxx&symbol=xxx
func getPositionPlanCheck(c *gin.Context) {
	pm := getPlanManager(c)
	if pm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "仓位计划服务不可用"})
		return
	}

	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 exchange 或 symbol 参数"})
		return
	}

	currentAmount := getCurrentPositionValueUSDT(exchange, symbol)
	activePlan, err := pm.GetActivePlan(c.Request.Context(), exchange, symbol)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := gin.H{
		"success":       true,
		"exchange":      exchange,
		"symbol":        symbol,
		"currentAmount": currentAmount,
	}
	if activePlan != nil {
		resp["hasActivePlan"] = true
		resp["plan"] = activePlan
		resp["targetAmount"] = activePlan.TargetAmountUSDT
		resp["direction"] = activePlan.Direction
		resp["reached"] = pm.IsTargetReached(activePlan)
	} else {
		resp["hasActivePlan"] = false
	}

	c.JSON(http.StatusOK, resp)
}
