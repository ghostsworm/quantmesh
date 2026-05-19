package web

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/optimizer"
	"quantmesh/exchange"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// optimizerTask 优化任務（記憶體存儲）
type optimizerTask struct {
	ID        string
	Status    string // pending, running, completed, failed, stopped
	Progress  int
	Result    *optimizer.OptimResult
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
	cancel    context.CancelFunc
}

var (
	optimizerTasks   = make(map[string]*optimizerTask)
	optimizerTasksMu sync.RWMutex
)

// OptimizerRunRequest 优化运行请求
type OptimizerRunRequest struct {
	Exchange       string                     `json:"exchange"` // binance, bitget，預設 binance
	Symbol         string                     `json:"symbol" binding:"required"`
	Interval       string                     `json:"interval" binding:"required"`
	StartTime      time.Time                  `json:"start_time" binding:"required"`
	EndTime        time.Time                  `json:"end_time" binding:"required"`
	InitialCapital float64                    `json:"initial_capital" binding:"required"`
	SearchSpace    optimizer.OptimSearchSpace `json:"search_space" binding:"required"`
	Config         optimizer.OptimConfig      `json:"config" binding:"required"`
}

// postOptimizerRun 啟动优化任務 POST /api/optimizer/run
func postOptimizerRun(c *gin.Context) {
	var req OptimizerRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("参數錯误: %v", err)})
		return
	}
	if req.EndTime.Before(req.StartTime) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_time 必須晚於 start_time"})
		return
	}
	if req.InitialCapital <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "initial_capital 必須大於 0"})
		return
	}
	if err := optimizer.ValidateSearchSpace(req.SearchSpace); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := optimizer.ValidateOptimConfig(req.Config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	validMethods := map[string]bool{"grid": true, "bayesian": true, "genetic": true}
	if !validMethods[req.Config.Method] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("不支援的优化方法: %s", req.Config.Method)})
		return
	}

	exchangeName := req.Exchange
	if exchangeName == "" {
		exchangeName = "binance"
	}
	validExchanges := map[string]bool{"binance": true, "bitget": true}
	if !validExchanges[exchangeName] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("不支援的交易所: %s", exchangeName)})
		return
	}

	// 立即創建任務並返回，數據獲取移到後台執行（避免 HTTP 請求超時）
	taskID := fmt.Sprintf("opt_%d", time.Now().UnixMilli())
	ctx, cancel := context.WithCancel(context.Background())
	task := &optimizerTask{
		ID:        taskID,
		Status:    "pending",
		Progress:  0,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		cancel:    cancel,
	}
	optimizerTasksMu.Lock()
	optimizerTasks[taskID] = task
	optimizerTasksMu.Unlock()

	// 異步執行數據獲取和优化任務
	go runOptimizerTaskWithDataFetch(ctx, taskID, exchangeName, req.Symbol, req.Interval, req.StartTime, req.EndTime, req.SearchSpace, req.Config, req.InitialCapital)

	c.JSON(http.StatusOK, gin.H{"success": true, "message": "优化任務已創建，正在獲取歷史數據...", "task_id": taskID})
}

// runOptimizerTaskWithDataFetch 包含數據獲取的完整任務流程
func runOptimizerTaskWithDataFetch(ctx context.Context, taskID, exchangeName, symbol, interval string, startTime, endTime time.Time, space optimizer.OptimSearchSpace, config optimizer.OptimConfig, initialCapital float64) {
	// 更新任務状態為 "loading_data"
	optimizerTasksMu.Lock()
	task, ok := optimizerTasks[taskID]
	if !ok {
		optimizerTasksMu.Unlock()
		return
	}
	task.Status = "loading_data"
	task.UpdatedAt = time.Now()
	optimizerTasksMu.Unlock()

	// 獲取历史數據
	exConfig := getExchangeConfig(exchangeName)
	candles, err := backtest.GetHistoricalDataEx(exchangeName, symbol, interval, startTime, endTime, exConfig)

	// 检查是否被取消
	if ctx.Err() != nil {
		optimizerTasksMu.Lock()
		if t, ok := optimizerTasks[taskID]; ok {
			t.Status = "stopped"
			t.UpdatedAt = time.Now()
		}
		optimizerTasksMu.Unlock()
		return
	}

	if err != nil {
		logger.Error("优化器獲取歷史數據失败: %v", err)
		optimizerTasksMu.Lock()
		if t, ok := optimizerTasks[taskID]; ok {
			t.Status = "failed"
			t.Error = fmt.Sprintf("獲取歷史數據失败: %v", err)
			t.UpdatedAt = time.Now()
		}
		optimizerTasksMu.Unlock()
		return
	}

	if len(candles) == 0 {
		optimizerTasksMu.Lock()
		if t, ok := optimizerTasks[taskID]; ok {
			t.Status = "failed"
			t.Error = "未獲取到历史 K 線數據"
			t.UpdatedAt = time.Now()
		}
		optimizerTasksMu.Unlock()
		return
	}

	logger.Info("优化任務 %s: 已獲取 %d 根K線數據，開始执行优化...", taskID, len(candles))

	// 執行优化任務
	runOptimizerTask(ctx, taskID, symbol, candles, space, config, initialCapital)
}

func runOptimizerTask(ctx context.Context, taskID, symbol string, candles []*exchange.Candle, space optimizer.OptimSearchSpace, config optimizer.OptimConfig, initialCapital float64) {
	optimizerTasksMu.Lock()
	t, ok := optimizerTasks[taskID]
	if !ok {
		optimizerTasksMu.Unlock()
		return
	}
	t.Status = "running"
	t.Progress = 0
	t.UpdatedAt = time.Now()
	optimizerTasksMu.Unlock()

	var opt optimizer.Optimizer
	switch config.Method {
	case "grid":
		opt = &optimizer.GridSearchOptimizer{}
	case "bayesian":
		opt = optimizer.NewBayesianOptimizer()
	case "genetic":
		opt = optimizer.NewGeneticOptimizer()
	default:
		opt = &optimizer.GridSearchOptimizer{}
	}

	result, err := opt.Run(ctx, symbol, candles, space, config, initialCapital)
	optimizerTasksMu.Lock()
	defer optimizerTasksMu.Unlock()
	task, ok := optimizerTasks[taskID]
	if !ok {
		return
	}
	task.UpdatedAt = time.Now()
	if err != nil {
		if ctx.Err() != nil {
			task.Status = "stopped"
		} else {
			task.Status = "failed"
			task.Error = err.Error()
		}
		return
	}
	task.Status = "completed"
	task.Progress = 100
	task.Result = result
	logger.Info("优化任務 %s 完成: hold_out=%v fee_rate=%.6f slippage=%.6f best_score=%.6f",
		taskID, result.HoldOutEnabled, result.FeeRateUsed, result.SlippageUsed, result.BestScore)
}

// getOptimizerStatus 查詢优化任務状態 GET /api/optimizer/status/:id
func getOptimizerStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	optimizerTasksMu.RLock()
	task, ok := optimizerTasks[id]
	optimizerTasksMu.RUnlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"task_id":    task.ID,
		"status":     task.Status,
		"progress":   task.Progress,
		"error":      task.Error,
		"created_at": task.CreatedAt,
		"updated_at": task.UpdatedAt,
	})
}

// getOptimizerResult 獲取优化結果 GET /api/optimizer/result/:id
func getOptimizerResult(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	optimizerTasksMu.RLock()
	task, ok := optimizerTasks[id]
	optimizerTasksMu.RUnlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	if task.Status != "completed" {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  task.Status,
			"result":  nil,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"status":  task.Status,
		"result":  task.Result,
	})
}

// getOptimizerPrice 獲取交易對當前價格 GET /api/optimizer/price?exchange=&symbol=
func getOptimizerPrice(c *gin.Context) {
	exchangeName := c.DefaultQuery("exchange", "binance")
	symbol := c.Query("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 symbol"})
		return
	}
	// Binance 公开 API 無需鉴权
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%s", symbol)
	var data struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	if err := fetchPublicJSON(c.Request.Context(), url, &data); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析價格失败"})
		return
	}
	var price float64
	if _, err := fmt.Sscanf(data.Price, "%f", &price); err != nil || price <= 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "無法獲取有效價格"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"price": price, "symbol": symbol, "exchange": exchangeName})
}

// postOptimizerStop 停止优化任務 POST /api/optimizer/stop/:id
func postOptimizerStop(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	optimizerTasksMu.Lock()
	task, ok := optimizerTasks[id]
	optimizerTasksMu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	if task.cancel != nil {
		task.cancel()
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已发送停止请求"})
}
