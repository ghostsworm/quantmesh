package web

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/backtest"
	"quantmesh/config"
	"quantmesh/logger"
)

// BotBacktestRequest Bot回测請求
type BotBacktestRequest struct {
	BotID       string    `json:"bot_id" binding:"required"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	DataDir     string    `json:"data_dir"`
	Commission  float64   `json:"commission"`
	Leverage    float64   `json:"leverage"`
}

// BotBacktestResponse Bot回测响应
type BotBacktestResponse struct {
	TaskID      string                      `json:"task_id"`
	Status      string                      `json:"status"`
	Message     string                      `json:"message"`
	BotConfig   *config.BotConfig           `json:"bot_config,omitempty"`
	BacktestConfig *backtest.EngineConfig   `json:"backtest_config,omitempty"`
}

// BotBacktestTask Bot回测任務
type BotBacktestTask struct {
	TaskID       string                               `json:"task_id"`
	BotID        string                               `json:"bot_id"`
	Status       string                               `json:"status"` // pending, running, completed, failed
	CreatedAt    time.Time                            `json:"created_at"`
	StartedAt    *time.Time                           `json:"started_at,omitempty"`
	CompletedAt  *time.Time                           `json:"completed_at,omitempty"`
	BotConfig    *config.BotConfig                    `json:"bot_config"`
	Engine       *backtest.MultiStrategyEngine        `json:"-"`
	Result       *backtest.MultiStrategyResult        `json:"result,omitempty"`
	Error        string                               `json:"error,omitempty"`
	Progress     float64                              `json:"progress"`
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

var (
	botBacktestTasks = make(map[string]*BotBacktestTask)
	botBacktestMu    sync.RWMutex
)

// postBotBacktestCreate 創建Bot回测任務
// POST /api/v2/bots/:id/backtest
func postBotBacktestCreate(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	// 獲取Bot配置
	if botManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	botDetail, ok := botManagerProvider.GetBot(botID)
	if !ok {
		respondError(c, http.StatusNotFound, "error.bot_not_found")
		return
	}

	// 解析請求
	var req BotBacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 檢查是否为空請求体（EOF）而非真正的錯誤
		if err.Error() != "EOF" && err.Error() != "unexpected EOF" {
			respondError(c, http.StatusBadRequest, "error.invalid_request")
			return
		}
		// 空請求体，使用預設配置
		req = BotBacktestRequest{
			BotID:      botID,
			DataDir:    "./data",
			Commission: 0.0004,
			Leverage:   1.0,
			StartDate:  time.Now().AddDate(0, -3, 0),
			EndDate:    time.Now(),
		}
	}

	// 驗證日期
	if req.StartDate.After(req.EndDate) {
		respondError(c, http.StatusBadRequest, "error.invalid_date_range")
		return
	}

	// 創建任務ID
	taskID := fmt.Sprintf("backtest_%s_%d", botID, time.Now().UnixNano())

	// 創建回测引擎配置
	engineConfig := &backtest.EngineConfig{
		Symbol:           botDetail.Config.Symbol,
		InitialCapital:   botDetail.Config.TotalAllocatedCapital,
		CommissionRate:   req.Commission,
		Leverage:         req.Leverage,
		StartDate:        req.StartDate,
		EndDate:          req.EndDate,
		DataDir:          req.DataDir,
		PositionMode:     botDetail.Config.Direction,
		EnableFunding:    true,
		MatcherConfig:    backtest.DefaultMatcherConfig(),
	}

	// 創建任務
	ctx, cancel := context.WithCancel(context.Background())
	task := &BotBacktestTask{
		TaskID:    taskID,
		BotID:     botID,
		Status:    "pending",
		CreatedAt: time.Now(),
		BotConfig: botDetail.Config,
		ctx:       ctx,
		cancel:    cancel,
	}

	// 存储任務
	botBacktestMu.Lock()
	botBacktestTasks[taskID] = task
	botBacktestMu.Unlock()

	// 异步執行回测
	go executeBotBacktest(task, engineConfig)

	// 返回响应
	c.JSON(http.StatusOK, BotBacktestResponse{
		TaskID:        taskID,
		Status:        "pending",
		Message:       "Backtest task created",
		BotConfig:     botDetail.Config,
		BacktestConfig: engineConfig,
	})
}

// executeBotBacktest 執行Bot回测
func executeBotBacktest(task *BotBacktestTask, engineConfig *backtest.EngineConfig) {
	task.mu.Lock()
	task.Status = "running"
	now := time.Now()
	task.StartedAt = &now
	task.mu.Unlock()

	// 檢查策略配置
	if len(task.BotConfig.Strategies) == 0 {
		task.mu.Lock()
		task.Status = "failed"
		task.Error = "No strategies configured for this bot"
		now := time.Now()
		task.CompletedAt = &now
		task.mu.Unlock()
		logger.Error("Bot backtest failed: no strategies configured")
		return
	}

	logger.Info("Starting bot backtest: task=%s, bot=%s, strategies=%d",
		task.TaskID, task.BotID, len(task.BotConfig.Strategies))

	// 創建回测引擎
	engine := backtest.NewMultiStrategyEngine(engineConfig)
	task.Engine = engine

	// 设置进度回調
	engine.SetProgressCallback(func(progress float64) {
		task.mu.Lock()
		task.Progress = progress
		task.mu.Unlock()
	})

	// 加載數據
	if err := engine.LoadData(); err != nil {
		task.mu.Lock()
		task.Status = "failed"
		task.Error = fmt.Sprintf("Failed to load data: %v", err)
		now := time.Now()
		task.CompletedAt = &now
		task.mu.Unlock()
		logger.Error("Bot backtest failed to load data: %v", err)
		return
	}

	// 創建策略实例
	for _, strategyCfg := range normalizeBotStrategies(task.BotConfig.Strategies) {
		strategy, err := createBacktestStrategy(strategyCfg, task.BotConfig)
		if err != nil {
			task.mu.Lock()
			task.Status = "failed"
			task.Error = fmt.Sprintf("Failed to create strategy %s: %v", strategyCfg.Type, err)
			now := time.Now()
			task.CompletedAt = &now
			task.mu.Unlock()
			logger.Error("Failed to create strategy: %v", err)
			return
		}

		if err := engine.AddStrategy(strategy); err != nil {
			task.mu.Lock()
			task.Status = "failed"
			task.Error = fmt.Sprintf("Failed to add strategy %s: %v", strategyCfg.Type, err)
			now := time.Now()
			task.CompletedAt = &now
			task.mu.Unlock()
			logger.Error("Failed to add strategy: %v", err)
			return
		}
	}

	// 運行回测
	result, err := engine.Run()
	if err != nil {
		task.mu.Lock()
		task.Status = "failed"
		task.Error = fmt.Sprintf("Backtest execution failed: %v", err)
		now := time.Now()
		task.CompletedAt = &now
		task.mu.Unlock()
		logger.Error("Bot backtest execution failed: %v", err)
		return
	}

	// 保存結果
	task.mu.Lock()
	task.Status = "completed"
	task.Result = result
	now = time.Now()
	task.CompletedAt = &now
	task.Progress = 100.0
	task.mu.Unlock()

	logger.Info("Bot backtest completed: task=%s, return=%.2f%%, trades=%d",
		task.TaskID, result.TotalReturnPct, result.TotalTrades)
}

// createBacktestStrategy 根据配置創建回测策略
func createBacktestStrategy(strategyCfg config.StrategyInstance, botCfg *config.BotConfig) (backtest.BacktestStrategy, error) {
	return backtest.CreateTaskBacktestStrategy(
		backtest.TaskStrategy{
			Type:   strategyCfg.Type,
			Weight: strategyCfg.Weight,
			Config: strategyCfg.Config,
		},
		backtest.StrategyExecutionContext{
			Symbol:       botCfg.Symbol,
			TotalCapital: botCfg.TotalAllocatedCapital,
		},
	)
}

func normalizeBotStrategies(strategies []config.StrategyInstance) []config.StrategyInstance {
	taskStrategies := make([]backtest.TaskStrategy, 0, len(strategies))
	for _, strategy := range strategies {
		taskStrategies = append(taskStrategies, backtest.TaskStrategy{
			Type:   strategy.Type,
			Weight: strategy.Weight,
			Config: strategy.Config,
		})
	}
	normalized := backtest.NormalizeTaskStrategies(taskStrategies)
	result := make([]config.StrategyInstance, 0, len(normalized))
	for _, strategy := range normalized {
		result = append(result, config.StrategyInstance{
			Type:   strategy.Type,
			Weight: strategy.Weight,
			Config: strategy.Config,
		})
	}
	return result
}

// createGridBacktestStrategy 創建网格回测策略
func createGridBacktestStrategy(strategyCfg config.StrategyInstance, botCfg *config.BotConfig) (backtest.BacktestStrategy, error) {
	// 提取网格参数
	gridCount := getIntParam(strategyCfg.Config, "grid_count", 50)
	gridSpacing := getFloatParam(strategyCfg.Config, "grid_spacing", 0.0025)
	gridLeverage := getIntParam(strategyCfg.Config, "grid_leverage", 5)

	// 創建网格回测策略
	strategy := backtest.NewGridBacktestStrategy(
		fmt.Sprintf("grid_%s", botCfg.Symbol),
		botCfg.Symbol,
		gridCount,
		gridSpacing,
		float64(gridLeverage),
		botCfg.TotalAllocatedCapital*strategyCfg.Weight,
	)

	return strategy, nil
}

// createDCABacktestStrategy 創建DCA回测策略
func createDCABacktestStrategy(strategyCfg config.StrategyInstance, botCfg *config.BotConfig) (backtest.BacktestStrategy, error) {
	baseOrderAmount := getFloatParam(strategyCfg.Config, "base_order_amount", 30.0)
	maxOrders := getIntParam(strategyCfg.Config, "max_orders", 10)

	strategy := backtest.NewDCABacktestStrategy(
		fmt.Sprintf("dca_%s", botCfg.Symbol),
		botCfg.Symbol,
		baseOrderAmount,
		float64(maxOrders),
		botCfg.TotalAllocatedCapital*strategyCfg.Weight,
	)

	return strategy, nil
}

// createMartingaleBacktestStrategy 創建马丁回测策略
func createMartingaleBacktestStrategy(strategyCfg config.StrategyInstance, botCfg *config.BotConfig) (backtest.BacktestStrategy, error) {
	baseOrderAmount := getFloatParam(strategyCfg.Config, "base_order_amount", 30.0)
	multiplier := getFloatParam(strategyCfg.Config, "multiplier", 2.0)
	maxOrders := getIntParam(strategyCfg.Config, "max_orders", 7)

	strategy := backtest.NewMartingaleBacktestStrategy(
		fmt.Sprintf("martingale_%s", botCfg.Symbol),
		botCfg.Symbol,
		baseOrderAmount,
		multiplier,
		float64(maxOrders),
		botCfg.TotalAllocatedCapital*strategyCfg.Weight,
	)

	return strategy, nil
}

// createTrendBacktestStrategy 創建趋势回测策略
func createTrendBacktestStrategy(strategyCfg config.StrategyInstance, botCfg *config.BotConfig) (backtest.BacktestStrategy, error) {
	lookback := getIntParam(strategyCfg.Config, "lookback", 20)

	strategy := backtest.NewTrendBacktestStrategy(
		fmt.Sprintf("trend_%s", botCfg.Symbol),
		botCfg.Symbol,
		botCfg.TotalAllocatedCapital*strategyCfg.Weight,
		lookback,
	)

	return strategy, nil
}

// createComboBacktestStrategy 創建组合回测策略
func createComboBacktestStrategy(strategyCfg config.StrategyInstance, botCfg *config.BotConfig) (backtest.BacktestStrategy, error) {
	// 组合策略需要包含子策略配置
	subStrategiesCfg := getSliceParam(strategyCfg.Config, "strategies")
	if len(subStrategiesCfg) == 0 {
		return nil, fmt.Errorf("combo strategy requires sub-strategies configuration")
	}

	var subStrategies []backtest.BacktestStrategy
	var weights []float64

	// 創建子策略
	for _, subCfg := range subStrategiesCfg {
		subStrategyInstance, ok := subCfg.(map[string]interface{})
		if !ok {
			continue
		}

		// 构造StrategyInstance
		subStrategyCfg := config.StrategyInstance{
			Type:   getStringParam(subStrategyInstance, "type"),
			Weight: getFloatParam(subStrategyInstance, "weight", 0.5),
			Config: subStrategyInstance,
		}

		subStrategy, err := createBacktestStrategy(subStrategyCfg, botCfg)
		if err != nil {
			logger.Warn("Failed to create sub-strategy: %v", err)
			continue
		}

		subStrategies = append(subStrategies, subStrategy)
		weights = append(weights, subStrategyCfg.Weight)
	}

	if len(subStrategies) == 0 {
		return nil, fmt.Errorf("no valid sub-strategies created")
	}

	strategy := backtest.NewComboBacktestStrategy(
		fmt.Sprintf("combo_%s", botCfg.Symbol),
		botCfg.Symbol,
		botCfg.TotalAllocatedCapital*strategyCfg.Weight,
		subStrategies,
		weights,
	)

	return strategy, nil
}

// getBotBacktestTask 獲取回测任務狀態
// GET /api/bot/backtest/:taskId
func getBotBacktestTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_task_id")
		return
	}

	botBacktestMu.RLock()
	task, ok := botBacktestTasks[taskID]
	botBacktestMu.RUnlock()

	if !ok {
		respondError(c, http.StatusNotFound, "error.task_not_found")
		return
	}

	task.mu.RLock()
	defer task.mu.RUnlock()

	c.JSON(http.StatusOK, task)
}

// getBotBacktestResult 獲取回测結果
// GET /api/bot/backtest/:taskId/result
func getBotBacktestResult(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_task_id")
		return
	}

	botBacktestMu.RLock()
	task, ok := botBacktestTasks[taskID]
	botBacktestMu.RUnlock()

	if !ok {
		respondError(c, http.StatusNotFound, "error.task_not_found")
		return
	}

	task.mu.RLock()
	defer task.mu.RUnlock()

	if task.Status != "completed" {
		respondError(c, http.StatusBadRequest, "error.backtest_not_completed")
		return
	}

	c.JSON(http.StatusOK, task.Result)
}

// deleteBotBacktestTask 删除回测任務
// DELETE /api/bot/backtest/:taskId
func deleteBotBacktestTask(c *gin.Context) {
	taskID := c.Param("taskId")
	if taskID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_task_id")
		return
	}

	botBacktestMu.Lock()
	defer botBacktestMu.Unlock()

	task, ok := botBacktestTasks[taskID]
	if !ok {
		respondError(c, http.StatusNotFound, "error.task_not_found")
		return
	}

	// 取消正在運行的任務
	task.mu.RLock()
	if task.Status == "running" && task.cancel != nil {
		task.cancel()
	}
	task.mu.RUnlock()

	delete(botBacktestTasks, taskID)

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"task_id": taskID,
	})
}

// listBotBacktestTasks 列出回测任務
// GET /api/v2/bots/:id/backtest/tasks
func listBotBacktestTasks(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	botBacktestMu.RLock()
	defer botBacktestMu.RUnlock()

	var tasks []*BotBacktestTask
	for _, task := range botBacktestTasks {
		if task.BotID == botID {
			tasks = append(tasks, task)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
		"count": len(tasks),
	})
}

// Helper functions for parameter extraction

func getIntParam(cfg map[string]interface{}, key string, defaultVal int) int {
	if val, ok := cfg[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case string:
			// Try to parse string
			var i int
			if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
				return i
			}
		}
	}
	return defaultVal
}

func getFloatParam(cfg map[string]interface{}, key string, defaultVal float64) float64 {
	if val, ok := cfg[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			var f float64
			if _, err := fmt.Sscanf(v, "%f", &f); err == nil {
				return f
			}
		}
	}
	return defaultVal
}

func getStringParam(cfg map[string]interface{}, key string) string {
	if val, ok := cfg[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

func getSliceParam(cfg map[string]interface{}, key string) []interface{} {
	if val, ok := cfg[key]; ok {
		if slice, ok := val.([]interface{}); ok {
			return slice
		}
	}
	return []interface{}{}
}

// BinanceDataDownloadRequest Binance數據下載請求
type BinanceDataDownloadRequest struct {
	Symbol   string `json:"symbol" binding:"required"`
	Interval string `json:"interval"`   // 1m, 5m, 15m, 1h, 4h, 1d
	StartDate string `json:"start_date"` // YYYY-MM or YYYY-MM-DD
	EndDate   string `json:"end_date"`   // YYYY-MM or YYYY-MM-DD
	DataDir   string `json:"data_dir"`
}

// BinanceDataInfoResponse 數據信息响应
type BinanceDataInfoResponse struct {
	Symbol          string    `json:"symbol"`
	Interval        string    `json:"interval"`
	KlinesFiles     []string  `json:"klines_files"`
	FundingFiles    []string  `json:"funding_files"`
	EarliestKline   int64     `json:"earliest_kline"`
	LatestKline     int64     `json:"latest_kline"`
	EarliestFunding int64     `json:"earliest_funding"`
	LatestFunding   int64     `json:"latest_funding"`
	KlinesSizeMB    float64   `json:"klines_size_mb"`
	FundingSizeMB   float64   `json:"funding_size_mb"`
}

// postBinanceDataDownload 下載數據
// POST /api/backtest/data/download
func postBinanceDataDownload(c *gin.Context) {
	var req BinanceDataDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request")
		return
	}

	// 设置預設值
	if req.Interval == "" {
		req.Interval = "1m"
	}
	if req.DataDir == "" {
		req.DataDir = "./data"
	}

	// 解析時間範圍
	startDate, endDate, err := backtest.ParseTimeRange(req.StartDate, req.EndDate)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_date_range")
		return
	}

	// 創建下載器
	downloader := backtest.NewBinanceDownloader(req.DataDir, req.Symbol, req.Interval)

	// 异步執行下載
	go func() {
		logger.Info("Starting data download: symbol=%s, interval=%s, start=%s, end=%s",
			req.Symbol, req.Interval, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
		if err := downloader.DownloadRange(startDate, endDate); err != nil {
			logger.Error("Data download failed: %v", err)
		} else {
			logger.Info("Data download completed: symbol=%s", req.Symbol)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"message": "Download started",
		"symbol":  req.Symbol,
		"start":   startDate.Format("2006-01-02"),
		"end":     endDate.Format("2006-01-02"),
	})
}

// getBinanceDataInfo 獲取數據信息
// GET /api/backtest/data/info
func getBinanceDataInfo(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	dataDir := c.Query("data_dir")

	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_symbol")
		return
	}

	if interval == "" {
		interval = "1m"
	}
	if dataDir == "" {
		dataDir = "./data"
	}

	downloader := backtest.NewBinanceDownloader(dataDir, symbol, interval)
	info, err := downloader.GetDataInfo()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.failed_to_get_data_info")
		return
	}

	// 轉換時間戳
	resp := BinanceDataInfoResponse{
		Symbol:          info.Symbol,
		Interval:        info.Interval,
		KlinesFiles:     info.KlinesFiles,
		FundingFiles:    info.FundingFiles,
		EarliestKline:   info.EarliestKline.Unix(),
		LatestKline:     info.LatestKline.Unix(),
		EarliestFunding: info.EarliestFunding.Unix(),
		LatestFunding:   info.LatestFunding.Unix(),
		KlinesSizeMB:    info.KlinesSizeMB,
		FundingSizeMB:   info.FundingSizeMB,
	}

	c.JSON(http.StatusOK, resp)
}

// getBinanceDataAvailability 檢查數據可用性
// GET /api/backtest/data/availability
func getBinanceDataAvailability(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	dataDir := c.Query("data_dir")

	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_symbol")
		return
	}

	if interval == "" {
		interval = "1m"
	}
	if dataDir == "" {
		dataDir = "./data"
	}

	downloader := backtest.NewBinanceDownloader(dataDir, symbol, interval)
	klinesAvailable, fundingAvailable, err := downloader.CheckAvailability()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.failed_to_check_availability")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"klines_available": klinesAvailable,
		"funding_available": fundingAvailable,
	})
}

// getBinanceLatestDataTime 獲取最新數據時間
// GET /api/backtest/data/latest
func getBinanceLatestDataTime(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	dataDir := c.Query("data_dir")

	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_symbol")
		return
	}

	if interval == "" {
		interval = "1m"
	}
	if dataDir == "" {
		dataDir = "./data"
	}

	downloader := backtest.NewBinanceDownloader(dataDir, symbol, interval)
	klinesTime, fundingTime, err := downloader.GetLatestDataTime()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.failed_to_get_latest_time")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":               true,
		"klines_latest":    klinesTime.Unix(),
		"funding_latest":   fundingTime.Unix(),
	})
}
