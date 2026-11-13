package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/backtest"
	"quantmesh/backtest/optimizer"
	"quantmesh/backtest/optimrun"
)

// optimTaskManager 參數優化任務管理器（由 main 注入）
var optimTaskManager *optimrun.OptimTaskManager

// SetOptimTaskManager 設置參數優化任務管理器
func SetOptimTaskManager(m *optimrun.OptimTaskManager) {
	optimTaskManager = m
}

// postOptimTasks 創建參數優化任務 POST /api/backtest/optim/tasks
func postOptimTasks(c *gin.Context) {
	if optimTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "回測服務未初始化，請確保已啟用存儲並保存過配置。",
		})
		return
	}
	var req struct {
		Strategy     string                    `json:"strategy" binding:"required"`
		Symbol       string                    `json:"symbol" binding:"required"`
		Interval     string                    `json:"interval" binding:"required"`
		StartTime    time.Time                 `json:"start_time" binding:"required"`
		EndTime      time.Time                 `json:"end_time" binding:"required"`
		TotalCapital float64                   `json:"total_capital" binding:"required"`
		SearchSpace  backtest.OptimSearchSpace `json:"search_space"`
	}
	// Bound nested feature/funding configuration before JSON decoding or persistence.
	const maxOptimRequestBytes = 256 * 1024
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOptimRequestBytes)
	if err := c.ShouldBindJSON(&req); err != nil {
		status := http.StatusBadRequest
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"success": false, "message": fmt.Sprintf("參數錯誤: %v", err)})
		return
	}
	space := req.SearchSpace
	if space.Strategy != "" && space.Strategy != req.Strategy {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "search_space.strategy must match strategy"})
		return
	}
	space.Strategy = req.Strategy
	if len(space.Ranges) == 0 && space.ReplayWalkForward == nil && req.Strategy != "grid" {
		// Retain the existing default search only for non-grid legacy strategies.
		space.Ranges = make(map[string]backtest.OptimParamRange)
		for k, v := range optimizer.GetDefaultSearchSpace(req.Strategy).Ranges {
			space.Ranges[k] = backtest.OptimParamRange{Min: v.Min, Max: v.Max, Step: v.Step}
		}
	}

	task := &backtest.OptimTask{
		Strategy:     req.Strategy,
		Symbol:       req.Symbol,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		TotalCapital: req.TotalCapital,
		SearchSpace:  space,
	}
	if err := optimTaskManager.CreateAndRunContext(c.Request.Context(), task); err != nil {
		if errors.Is(err, optimrun.ErrInvalidOptimTask) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		if errors.Is(err, optimrun.ErrOptimizerCapacity) {
			c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "message": "已有参数优化任务运行中，请稍后重试。"})
			return
		}
		if errors.Is(err, context.Canceled) {
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			c.JSON(http.StatusGatewayTimeout, gin.H{"success": false, "message": "行情预检超时，请缩短时间范围后重试。"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "優化任務已創建", "task_id": task.ID})
}

// getOptimTasks 獲取參數優化任務列表 GET /api/backtest/optim/tasks
func getOptimTasks(c *gin.Context) {
	if optimTaskManager == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "tasks": []interface{}{}})
		return
	}
	store := optimTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "tasks": []interface{}{}})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tasks, err := store.ListOptimTasks(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "tasks": tasks})
}

// getOptimTaskByID 獲取參數優化任務詳情 GET /api/backtest/optim/tasks/:id
func getOptimTaskByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	if optimTaskManager == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	store := optimTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	task, err := store.GetOptimTask(id)
	if err != nil || task == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "task": task})
}

// getOptimTaskResult 獲取參數優化結果 GET /api/backtest/optim/tasks/:id/result
func getOptimTaskResult(c *gin.Context) {
	id := c.Param("id")
	if !optimrun.IsValidTaskID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	if optimTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "服務未初始化"})
		return
	}
	data, err := optimTaskManager.ReadResult(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "結果文件不存在或未生成"})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// deleteOptimTask 刪除參數優化任務 DELETE /api/backtest/optim/tasks/:id
func deleteOptimTask(c *gin.Context) {
	id := c.Param("id")
	if !optimrun.IsValidTaskID(id) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	if optimTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "服務未初始化"})
		return
	}
	store := optimTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲不可用"})
		return
	}
	const optimizerDeleteWait = 10 * time.Second
	ctx, cancel := context.WithTimeout(c.Request.Context(), optimizerDeleteWait)
	defer cancel()
	if err := optimTaskManager.DeleteTask(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已刪除"})
}

// getOptimSearchSpace 獲取策略默認搜索空間 GET /api/backtest/optim/space/:strategy
func getOptimSearchSpace(c *gin.Context) {
	strategy := c.Param("strategy")
	if strategy == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 strategy"})
		return
	}
	space := optimizer.GetDefaultSearchSpace(strategy)
	c.JSON(http.StatusOK, gin.H{"success": true, "search_space": space})
}
