package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/optimizer"
	"quantmesh/backtest/optimrun"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

func deriveHedgeGroupTaskDefaults(cfg *config.Config, groupID, currentSymbol string, params map[string]interface{}) (string, string, map[string]interface{}, error) {
	if cfg == nil {
		return "", "", params, fmt.Errorf("配置不可用")
	}
	var group *config.BotGroup
	for i := range cfg.BotGroups {
		if cfg.BotGroups[i].ID == groupID {
			group = &cfg.BotGroups[i]
			break
		}
	}
	if group == nil {
		return "", "", params, fmt.Errorf("未找到对冲组: %s", groupID)
	}
	if len(group.BotIDs) < 2 {
		return "", "", params, fmt.Errorf("对冲组至少需要 2 个 Bot")
	}

	botByID := make(map[string]config.BotConfig, len(cfg.Bots))
	for i := range cfg.Bots {
		bot := cfg.Bots[i]
		id := bot.ID
		if id == "" {
			id = config.GenerateBotID(bot.Exchange, bot.Symbol, bot.GetMarketType())
		}
		botByID[id] = bot
	}

	groupBots := make([]config.BotConfig, 0, len(group.BotIDs))
	for _, botID := range group.BotIDs {
		bot, ok := botByID[botID]
		if !ok {
			return "", "", params, fmt.Errorf("对冲组内 Bot 不存在: %s", botID)
		}
		groupBots = append(groupBots, bot)
	}

	primary := groupBots[0]
	secondary := groupBots[1]
	if currentSymbol != "" {
		for i := range groupBots {
			if groupBots[i].Symbol == currentSymbol {
				primary = groupBots[i]
				for j := range groupBots {
					if j != i {
						secondary = groupBots[j]
						break
					}
				}
				break
			}
		}
	}

	if params == nil {
		params = make(map[string]interface{})
	}
	if _, ok := params["leg_a_symbol"]; !ok && primary.Symbol != "" {
		params["leg_a_symbol"] = primary.Symbol
	}
	if _, ok := params["leg_b_symbol"]; !ok && secondary.Symbol != "" {
		params["leg_b_symbol"] = secondary.Symbol
	}
	if _, ok := params["hedge_ratio"]; !ok && group.HedgeConfig.HedgeRatio > 0 {
		params["hedge_ratio"] = group.HedgeConfig.HedgeRatio
	}
	if _, ok := params["rebalance_interval"]; !ok && group.HedgeConfig.RebalanceInterval > 0 {
		params["rebalance_interval"] = group.HedgeConfig.RebalanceInterval
	}
	if _, ok := params["rebalance_threshold"]; !ok && group.HedgeConfig.MaxDrawdown > 0 {
		threshold := group.HedgeConfig.MaxDrawdown
		if threshold > 1 {
			threshold = threshold / 100
		}
		params["rebalance_threshold"] = threshold
	}
	return primary.ID, primary.Symbol, params, nil
}

// backtestTaskManager 回测任務管理器（由 main 注入）
var backtestTaskManager *backtest.TaskManager

// smartParamsService 智能參數推薦服務（由 main 注入）
var smartParamsService *backtest.SmartParamsService

// autoBacktestScheduler 自動回測調度器（由 main 注入）
var autoBacktestScheduler *backtest.AutoBacktestScheduler

// optimTaskManager 參數優化任務管理器（由 main 注入）
var optimTaskManager *optimrun.OptimTaskManager

// SetBacktestTaskManager 設置回测任務管理器
func SetBacktestTaskManager(m *backtest.TaskManager) {
	backtestTaskManager = m
}

// SetSmartParamsService 設置智能參數推薦服務
func SetSmartParamsService(s *backtest.SmartParamsService) {
	smartParamsService = s
}

// SetAutoBacktestScheduler 設置自動回測調度器
func SetAutoBacktestScheduler(s *backtest.AutoBacktestScheduler) {
	autoBacktestScheduler = s
}

// SetOptimTaskManager 設置參數優化任務管理器
func SetOptimTaskManager(m *optimrun.OptimTaskManager) {
	optimTaskManager = m
}

// BacktestRequest 回测请求
type BacktestRequest struct {
	Strategy       string    `json:"strategy" binding:"required"`        // "momentum", "mean_reversion", "trend_following"
	Symbol         string    `json:"symbol" binding:"required"`          // "BTCUSDT"
	Interval       string    `json:"interval" binding:"required"`        // "1m", "5m", "1h"
	StartTime      time.Time `json:"start_time" binding:"required"`      // 开始時间
	EndTime        time.Time `json:"end_time" binding:"required"`        // 結束時间
	InitialCapital float64   `json:"initial_capital" binding:"required"` // 初始资金
}

// BacktestResponse 回测响应
type BacktestResponse struct {
	Success    bool                     `json:"success"`
	Message    string                   `json:"message"`
	Result     *backtest.BacktestResult `json:"result,omitempty"`
	ReportPath string                   `json:"report_path,omitempty"`
}

// runBacktest 运行回测
func runBacktest(c *gin.Context) {
	var req BacktestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, BacktestResponse{
			Success: false,
			Message: fmt.Sprintf("请求参數錯误: %v", err),
		})
		return
	}

	// 驗证策略名称
	validStrategies := map[string]bool{
		"momentum":        true,
		"mean_reversion":  true,
		"trend_following": true,
	}
	if !validStrategies[req.Strategy] {
		c.JSON(http.StatusBadRequest, BacktestResponse{
			Success: false,
			Message: fmt.Sprintf("不支援的策略: %s", req.Strategy),
		})
		return
	}

	// 驗证時间範圍
	if req.EndTime.Before(req.StartTime) {
		c.JSON(http.StatusBadRequest, BacktestResponse{
			Success: false,
			Message: "結束時间必須晚於开始時间",
		})
		return
	}

	logger.Info("📊 开始回测: 策略=%s, 交易對=%s, 周期=%s",
		req.Strategy, req.Symbol, req.Interval)

	// 獲取 Binance 配置
	binanceConfig := getBinanceConfig()

	// 1. 獲取歷史數據（优先缓存）
	candles, err := backtest.GetHistoricalData(
		req.Symbol,
		req.Interval,
		req.StartTime,
		req.EndTime,
		binanceConfig,
	)
	if err != nil {
		logger.Error("獲取歷史數據失败: %v", err)
		c.JSON(http.StatusInternalServerError, BacktestResponse{
			Success: false,
			Message: fmt.Sprintf("獲取歷史數據失败: %v", err),
		})
		return
	}

	if len(candles) == 0 {
		c.JSON(http.StatusBadRequest, BacktestResponse{
			Success: false,
			Message: "未獲取到历史數據",
		})
		return
	}

	logger.Info("✅ 獲取歷史數據成功: %d 根K線", len(candles))

	// 2. 創建策略适配器
	var strategy backtest.StrategyAdapter
	switch req.Strategy {
	case "momentum":
		strategy = backtest.NewMomentumAdapter()
	case "mean_reversion":
		strategy = backtest.NewMeanReversionAdapter()
	case "trend_following":
		strategy = backtest.NewTrendFollowingAdapter()
	}

	// 3. 創建回测器
	backtester := backtest.NewBacktester(
		req.Symbol,
		candles,
		strategy,
		req.InitialCapital,
	)

	// 4. 运行回测
	result, err := backtester.Run()
	if err != nil {
		logger.Error("回测失败: %v", err)
		c.JSON(http.StatusInternalServerError, BacktestResponse{
			Success: false,
			Message: fmt.Sprintf("回测失败: %v", err),
		})
		return
	}

	// 5. 生成报告
	reportPath, err := backtest.GenerateReport(result)
	if err != nil {
		logger.Warn("生成报告失败: %v", err)
	} else {
		logger.Info("📄 报告已生成: %s", reportPath)
	}

	// 6. 保存权益曲線
	equityPath, err := backtest.SaveEquityCurveCSV(result)
	if err != nil {
		logger.Warn("保存权益曲線失败: %v", err)
	} else {
		logger.Info("📈 权益曲線已保存: %s", equityPath)
	}

	logger.Info("✅ 回测完成: 總收益率=%.2f%%, 夏普比率=%.2f",
		result.Metrics.TotalReturn, result.Metrics.SharpeRatio)

	c.JSON(http.StatusOK, BacktestResponse{
		Success:    true,
		Message:    "回测完成",
		Result:     result,
		ReportPath: reportPath,
	})
}

// getCacheStats 獲取缓存统计
func getCacheStats(c *gin.Context) {
	stats, err := backtest.GetCacheStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("獲取缓存统计失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"stats":   stats,
	})
}

// listCache 列出所有缓存
func listCache(c *gin.Context) {
	caches, err := backtest.ListCache()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("列出缓存失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"caches":  caches,
	})
}

// deleteCache 刪除指定缓存
func deleteCache(c *gin.Context) {
	cacheKey := c.Param("key")
	if cacheKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缺少缓存键",
		})
		return
	}

	if err := backtest.DeleteCache(cacheKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("刪除缓存失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "缓存已刪除",
	})
}

// clearCache 清理所有缓存
func clearCache(c *gin.Context) {
	if err := backtest.ClearCache(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("清理缓存失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "所有缓存已清理",
	})
}

// getBinanceConfig 獲取 Binance 配置
func getBinanceConfig() map[string]string {
	return getExchangeConfig("binance")
}

// getExchangeConfig 根據交易所名称獲取配置（用於历史 K 線等公开接口）
func getExchangeConfig(exchange string) map[string]string {
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			switch exchange {
			case "binance":
				if exCfg, ok := cfg.Exchanges["binance"]; ok {
					return map[string]string{
						"api_key":    exCfg.APIKey,
						"secret_key": exCfg.SecretKey,
						"testnet":    fmt.Sprintf("%v", exCfg.Testnet),
					}
				}
			case "bitget":
				if exCfg, ok := cfg.Exchanges["bitget"]; ok {
					return map[string]string{
						"api_key":    exCfg.APIKey,
						"secret_key": exCfg.SecretKey,
						"passphrase": exCfg.Passphrase,
						"testnet":    fmt.Sprintf("%v", exCfg.Testnet),
					}
				}
			}
		}
	}
	// 默认配置（公开接口可不带鉴权）
	switch exchange {
	case "bitget":
		return map[string]string{
			"api_key":    "",
			"secret_key": "",
			"passphrase": "",
			"testnet":    "false",
		}
	default:
		return map[string]string{
			"api_key":    "",
			"secret_key": "",
			"testnet":    "false",
		}
	}
}

// getBacktestStrategies 獲取策略列表及参數定义 GET /api/backtest/strategies
func getBacktestStrategies(c *gin.Context) {
	defs := backtest.GetAllStrategyDefinitions()
	c.JSON(http.StatusOK, gin.H{"success": true, "strategies": defs})
}

// getBacktestPreset 獲取交易對推荐配置 GET /api/backtest/presets/:symbol
func getBacktestPreset(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 symbol"})
		return
	}
	preset := backtest.GetSymbolPreset(symbol)
	c.JSON(http.StatusOK, gin.H{"success": true, "preset": preset})
}

// postCacheGenerate 生成 K 線缓存（异步） POST /api/backtest/cache/generate
func postCacheGenerate(c *gin.Context) {
	var req struct {
		Symbol    string `json:"symbol" binding:"required"`
		Interval  string `json:"interval" binding:"required"`
		StartDate string `json:"start_date" binding:"required"` // 2006-01-02
		EndDate   string `json:"end_date" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("参數錯误: %v", err)})
		return
	}
	startTime, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "start_date 格式应為 2006-01-02"})
		return
	}
	endTime, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_date 格式应為 2006-01-02"})
		return
	}
	if endTime.Before(startTime) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_date 必須晚於 start_date"})
		return
	}
	binanceConfig := getBinanceConfig()
	go func() {
		_, _ = backtest.GetHistoricalData(req.Symbol, req.Interval, startTime, endTime, binanceConfig)
		logger.Info("✅ K線缓存生成完成: %s %s %s ~ %s", req.Symbol, req.Interval, req.StartDate, req.EndDate)
	}()
	cacheKey := backtestCacheKeyFormat("binance", req.Symbol, req.Interval, req.StartDate, req.EndDate)
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   "已在后台生成缓存",
		"cache_key": cacheKey,
	})
}

// backtestCacheKeyFormat 与 backtest.GetHistoricalData 使用的缓存键一致（交易所_symbol_interval_start_end）
func backtestCacheKeyFormat(exchangeName, symbol, interval, startDate, endDate string) string {
	if exchangeName == "" {
		exchangeName = "binance"
	}
	return fmt.Sprintf("%s_%s_%s_%s_%s", exchangeName, symbol, interval, startDate, endDate)
}

// getCacheStatus 查詢指定缓存是否存在 GET /api/backtest/cache/status?symbol=&interval=&start_date=&end_date=
// 缓存键与回测拉取数据时一致，无缓存时直接运行回测也会自动从交易所拉取并写入缓存
func getCacheStatus(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	exchangeName := c.Query("exchange") // 可选，默认 binance
	if symbol == "" || interval == "" || startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 symbol, interval, start_date, end_date"})
		return
	}
	cacheKey := backtestCacheKeyFormat(exchangeName, symbol, interval, startDate, endDate)
	filename := filepath.Join("backtest", "cache", cacheKey+".csv")
	_, err := os.Stat(filename)
	exists := err == nil
	c.JSON(http.StatusOK, gin.H{"success": true, "cache_key": cacheKey, "exists": exists})
}

// postBacktestTasks 創建回测任務 POST /api/backtest/tasks
func postBacktestTasks(c *gin.Context) {
	if backtestTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "回测服務未初始化。請確保已啟用存儲（SQLite），並在「設置」中保存過配置後重啟服務；若為首次使用請先完成配置並保存。",
		})
		return
	}
	var req struct {
		Mode         backtest.TaskMode      `json:"mode"`
		BotID        string                 `json:"bot_id"`
		GroupID      string                 `json:"group_id"`
		Strategy     string                 `json:"strategy"`
		Strategies   []backtest.TaskStrategy `json:"strategies"`
		Symbol       string                 `json:"symbol"`
		Interval     string                 `json:"interval"`
		StartTime    time.Time              `json:"start_time"`
		EndTime      time.Time              `json:"end_time"`
		Params       map[string]interface{} `json:"params"`
		TotalCapital float64                `json:"total_capital" binding:"required"`
		// 数据来源相关字段
		DataSource string `json:"data_source"`
		KlineFile  string `json:"kline_file"`
		CacheName  string `json:"cache_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("参數錯误: %v", err)})
		return
	}
	if req.Mode == backtest.TaskModeHedgeGroup && req.GroupID != "" && configManager != nil {
		cfg, cfgErr := configManager.GetConfig()
		if cfgErr == nil && cfg != nil {
			primaryBotID, primarySymbol, mergedParams, deriveErr := deriveHedgeGroupTaskDefaults(cfg, req.GroupID, req.Symbol, req.Params)
			if deriveErr != nil {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": deriveErr.Error()})
				return
			}
			req.Params = mergedParams
			if req.BotID == "" {
				req.BotID = primaryBotID
			}
			if req.Symbol == "" {
				req.Symbol = primarySymbol
			}
		}
	}
	if req.Mode != backtest.TaskModeHedgeGroup && req.Strategy == "" && len(req.Strategies) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "strategy 或 strategies 必填"})
		return
	}
	if req.Mode == backtest.TaskModeHedgeGroup {
		if req.GroupID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "hedge_group 模式必须提供 group_id"})
			return
		}
		if req.Params == nil {
			req.Params = make(map[string]interface{})
		}
		if _, ok := req.Params["leg_b_symbol"]; !ok {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "hedge_group 模式必须提供 params.leg_b_symbol"})
			return
		}
	}

	// 按数据来源校验
	switch req.DataSource {
	case "kline_file":
		if req.KlineFile == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "使用K线文件时必须提供 kline_file"})
			return
		}
		// 校验文件状态
		if err := validateKlineFileForBacktest(req.KlineFile, c); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
	case "cache":
		if req.CacheName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "使用回测缓存时必须提供 cache_name"})
			return
		}
	default:
		// 默认或 "time_range": 使用原有校验
		if req.Symbol == "" || req.Interval == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "symbol 和 interval 必填"})
			return
		}
		if req.StartTime.IsZero() || req.EndTime.IsZero() {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "start_time 和 end_time 必填"})
			return
		}
		if req.EndTime.Before(req.StartTime) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_time 必須晚於 start_time"})
			return
		}
	}
	validStrategies := map[string]bool{
		"grid": true, "momentum": true, "mean_reversion": true,
		"trend_following": true, "dca": true, "martingale": true, "combo": true,
	}
	if req.Strategy != "" && !validStrategies[req.Strategy] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("不支援的策略: %s", req.Strategy)})
		return
	}
	for _, strategy := range req.Strategies {
		if !validStrategies[strategy.Type] {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("不支援的策略: %s", strategy.Type)})
			return
		}
	}
	if len(req.Strategies) > 0 && req.Mode == "" {
		req.Mode = backtest.TaskModeBotStrategies
	}
	task := &backtest.BacktestTask{
		Mode:         req.Mode,
		BotID:        req.BotID,
		GroupID:      req.GroupID,
		Strategy:     req.Strategy,
		Strategies:   req.Strategies,
		Symbol:       req.Symbol,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		Params:       req.Params,
		TotalCapital: req.TotalCapital,
		DataSource:   req.DataSource,
		KlineFile:    req.KlineFile,
		CacheName:    req.CacheName,
	}
	if task.Params == nil {
		task.Params = make(map[string]interface{})
	}
	if err := backtestTaskManager.CreateAndRun(task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "任務已創建並开始執行", "task_id": task.ID})
}

// getBacktestTasks 獲取任務列表 GET /api/backtest/tasks?limit=50&offset=0
func getBacktestTasks(c *gin.Context) {
	if backtestTaskManager == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "tasks": []interface{}{}})
		return
	}
	store := backtestTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "tasks": []interface{}{}})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	tasks, err := store.ListBacktestTasks(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "tasks": tasks})
}

// getBacktestTaskByID 獲取任務详情 GET /api/backtest/tasks/:id
func getBacktestTaskByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	if backtestTaskManager == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	store := backtestTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	task, err := store.GetBacktestTask(id)
	if err != nil || task == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任務不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "task": task})
}

// getBacktestTaskResult 獲取結果 JSON GET /api/backtest/tasks/:id/result
func getBacktestTaskResult(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	if backtestTaskManager == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "結果不存在"})
		return
	}
	resultPath := filepath.Join("backtest", "results", id+".json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "結果文件不存在或未生成"})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// getBacktestTaskKlines 獲取任務對應時段的 K 線（供報告內走勢圖使用）GET /api/backtest/tasks/:id/klines
func getBacktestTaskKlines(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少任務 id"})
		return
	}
	if backtestTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "回測服務未初始化"})
		return
	}
	store := backtestTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲不可用"})
		return
	}
	task, err := store.GetBacktestTask(id)
	if err != nil || task == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "任務不存在"})
		return
	}
	var candles []*exchange.Candle

	// 根据任务的数据来源加载 K 线数据
	switch task.DataSource {
	case "kline_file":
		if task.KlineFile != "" {
			klineDataDir := backtestTaskManager.GetKlineDataDir()
			candles, _, err = backtest.LoadCandlesFromKlineFile(klineDataDir, task.KlineFile)
		} else {
			err = fmt.Errorf("任务未指定 K 线文件")
		}
	case "cache":
		if task.CacheName != "" {
			candles, err = backtest.LoadCandlesFromCache(task.CacheName)
		} else {
			err = fmt.Errorf("任务未指定缓存名称")
		}
	default:
		// 默认：从交易所获取历史数据
		binanceConfig := getBinanceConfig()
		candles, err = backtest.GetHistoricalData(task.Symbol, task.Interval, task.StartTime, task.EndTime, binanceConfig)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	klines := make([]gin.H, len(candles))
	for i, candle := range candles {
		klines[i] = gin.H{
			"time":   candle.Timestamp / 1000,
			"open":   candle.Open,
			"high":   candle.High,
			"low":    candle.Low,
			"close":  candle.Close,
			"volume": candle.Volume,
		}
	}
	c.JSON(http.StatusOK, gin.H{"klines": klines, "symbol": task.Symbol, "interval": task.Interval})
}

// getBacktestTaskReport 獲取/下載报告 Markdown GET /api/backtest/tasks/:id/report
func getBacktestTaskReport(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	reportPath := filepath.Join("backtest", "reports", id+".md")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "报告文件不存在或未生成"})
		return
	}
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", "attachment; filename="+id+".md")
	}
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", data)
}

// getBacktestTaskTradesExport 導出回測交易記錄 (CSV/JSON) GET /api/backtest/tasks/:id/trades/export
func getBacktestTaskTradesExport(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}

	// 獲取導出格式 (csv 或 json，默認 csv)
	format := c.Query("format")
	if format != "json" && format != "csv" {
		format = "csv"
	}

	// 讀取回測結果
	taskPath := filepath.Join("backtest", "tasks", id+".json")
	taskData, err := os.ReadFile(taskPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "回測任務不存在"})
		return
	}

	var task BacktestTask
	if err := json.Unmarshal(taskData, &task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "解析任務數據失敗"})
		return
	}

	if task.Result == nil || len(task.Result.Trades) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "無交易記錄"})
		return
	}

	trades := task.Result.Trades
	filename := fmt.Sprintf("backtest_trades_%s", id)

	// 根據格式返回數據
	if format == "json" {
		// JSON 格式
		jsonData, err := json.MarshalIndent(trades, "", "  ")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "生成 JSON 失敗"})
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.json", filename))
		c.Data(http.StatusOK, "application/json; charset=utf-8", jsonData)
		return
	}

	// CSV 格式
	var csvBuilder strings.Builder
	csvBuilder.WriteString("Timestamp,Type,Price,Quantity,Fee,PnL\n")
	for _, trade := range trades {
		timestamp := time.Unix(trade.Timestamp, 0).Format("2006-01-02 15:04:05")
		csvBuilder.WriteString(fmt.Sprintf("%s,%s,%.4f,%.6f,%.4f,%.4f\n",
			timestamp, trade.Type, trade.Price, trade.Quantity, trade.Fee, trade.PnL))
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.csv", filename))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(csvBuilder.String()))
}


// deleteBacktestTask 刪除任務 DELETE /api/backtest/tasks/:id
func deleteBacktestTask(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	if backtestTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "回测服務未初始化。請確保已啟用存儲（SQLite），並在「設置」中保存過配置後重啟服務。",
		})
		return
	}
	store := backtestTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲不可用"})
		return
	}
	if err := store.DeleteBacktestTask(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	_ = os.Remove(filepath.Join("backtest", "results", id+".json"))
	_ = os.Remove(filepath.Join("backtest", "reports", id+".md"))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已刪除"})
}

// BacktestExchangeInfo 回测交易所信息
type BacktestExchangeInfo struct {
	Exchange     string   `json:"exchange"`
	MarketTypes  []string `json:"market_types"`  // 支援的市場類型：spot, futures
	IsConfigured bool     `json:"is_configured"` // 是否已在 config 中配置
}

// getBacktestExchanges 獲取可用於回测的交易所列表 GET /api/backtest/exchanges
func getBacktestExchanges(c *gin.Context) {
	exchanges := make([]BacktestExchangeInfo, 0)
	configuredExchanges := make(map[string]bool)

	// 從配置中讀取已配置的交易所
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			for ex := range cfg.Exchanges {
				if ex != "" {
					configuredExchanges[ex] = true
				}
			}
		}
	}

	// 定義支援的交易所及其市場類型
	supportedExchanges := map[string][]string{
		"binance": {"spot", "futures"},
		"bitget":  {"spot", "futures"},
		"okx":     {"spot", "futures"},
		"bybit":   {"spot", "futures"},
		"gate":    {"spot", "futures"},
	}

	for ex, marketTypes := range supportedExchanges {
		exchanges = append(exchanges, BacktestExchangeInfo{
			Exchange:     ex,
			MarketTypes:  marketTypes,
			IsConfigured: configuredExchanges[ex],
		})
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "exchanges": exchanges})
}

// BacktestSymbolInfo 回测交易對信息
type BacktestSymbolInfo struct {
	Symbol       string `json:"symbol"`
	Exchange     string `json:"exchange"`
	MarketType   string `json:"market_type"`   // spot 或 futures
	IsConfigured bool   `json:"is_configured"` // 是否已在 config 中配置
}

// getBacktestSymbols 獲取可用於回测的交易對列表 GET /api/backtest/symbols?exchange=binance&market_type=futures
func getBacktestSymbols(c *gin.Context) {
	exchange := c.Query("exchange")
	marketType := c.Query("market_type")

	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 exchange 参數"})
		return
	}
	if marketType == "" {
		marketType = "futures" // 預設合約
	}

	symbols := make([]BacktestSymbolInfo, 0)
	configuredSymbols := make(map[string]bool)

	// 從配置中讀取已配置的交易對
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			for _, sym := range cfg.Trading.Symbols {
				symExchange := sym.Exchange
				if symExchange == "" {
					symExchange = cfg.App.CurrentExchange
				}
				symMarketType := sym.MarketType
				if symMarketType == "" {
					symMarketType = "futures"
				}
				if symExchange == exchange && symMarketType == marketType {
					configuredSymbols[sym.Symbol] = true
				}
			}
		}
	}

	// 常用交易對列表（按市場類型分類）
	commonFuturesSymbols := []string{
		"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT",
		"DOGEUSDT", "ADAUSDT", "LTCUSDT", "LINKUSDT", "DOTUSDT",
		"MATICUSDT", "AVAXUSDT", "UNIUSDT", "ATOMUSDT", "APTUSDT",
		"ARBUSDT", "OPUSDT", "SUIUSDT", "PEPEUSDT", "SHIBUSDT",
	}
	commonSpotSymbols := []string{
		"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT",
		"DOGEUSDT", "ADAUSDT", "LTCUSDT", "LINKUSDT", "DOTUSDT",
		"PAXGUSDT", "XAUUSDT", // 黃金相關
	}

	var symbolList []string
	if marketType == "spot" {
		symbolList = commonSpotSymbols
	} else {
		symbolList = commonFuturesSymbols
	}

	// 首先添加已配置的交易對（排在前面）
	for sym := range configuredSymbols {
		symbols = append(symbols, BacktestSymbolInfo{
			Symbol:       sym,
			Exchange:     exchange,
			MarketType:   marketType,
			IsConfigured: true,
		})
	}

	// 然後添加常用交易對（未配置的）
	for _, sym := range symbolList {
		if !configuredSymbols[sym] {
			symbols = append(symbols, BacktestSymbolInfo{
				Symbol:       sym,
				Exchange:     exchange,
				MarketType:   marketType,
				IsConfigured: false,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "symbols": symbols})
}

// getBacktestConfigParams 獲取已配置交易對的策略参數 GET /api/backtest/config-params?exchange=binance&symbol=BTCUSDT&strategy=grid
func getBacktestConfigParams(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	strategy := c.Query("strategy")

	if symbol == "" || strategy == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 symbol 或 strategy 参數"})
		return
	}

	params := make(map[string]interface{})
	found := false

	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			// 查找匹配的交易對配置
			for _, sym := range cfg.Trading.Symbols {
				symExchange := sym.Exchange
				if symExchange == "" {
					symExchange = cfg.App.CurrentExchange
				}
				// 匹配交易對（如果指定了 exchange 則也要匹配）
				if sym.Symbol == symbol && (exchange == "" || symExchange == exchange) {
					found = true

					// 根據策略類型提取相關参數
					switch strategy {
					case "grid":
						// 網格策略参數
						params["order_quantity"] = sym.OrderQuantity
						params["price_interval"] = sym.PriceInterval
						params["buy_window_size"] = sym.BuyWindowSize
						params["sell_window_size"] = sym.SellWindowSize
						// 從策略實例中讀取更多参數
						for _, s := range sym.Strategies {
							if s.Type == "grid" {
								for k, v := range s.Config {
									params[k] = v
								}
								if sym.TotalAllocatedCapital > 0 {
									params["total_capital"] = sym.TotalAllocatedCapital * s.Weight
								}
								break
							}
						}
						// 風控参數
						if sym.GridRiskControl.Enabled {
							params["max_grid_layers"] = sym.GridRiskControl.MaxGridLayers
							params["stop_loss_ratio"] = sym.GridRiskControl.StopLossRatio
						}
					case "dca":
						for _, s := range sym.Strategies {
							if s.Type == "dca" || s.Type == "dca_enhanced" {
								for k, v := range s.Config {
									params[k] = v
								}
								if sym.TotalAllocatedCapital > 0 {
									params["total_capital"] = sym.TotalAllocatedCapital * s.Weight
								}
								break
							}
						}
					case "martingale":
						for _, s := range sym.Strategies {
							if s.Type == "martingale" {
								for k, v := range s.Config {
									params[k] = v
								}
								if sym.TotalAllocatedCapital > 0 {
									params["total_capital"] = sym.TotalAllocatedCapital * s.Weight
								}
								break
							}
						}
					default:
						// 其他策略：嘗試從策略實例中讀取
						for _, s := range sym.Strategies {
							if s.Type == strategy {
								for k, v := range s.Config {
									params[k] = v
								}
								if sym.TotalAllocatedCapital > 0 {
									params["total_capital"] = sym.TotalAllocatedCapital * s.Weight
								}
								break
							}
						}
					}

					// 如果沒有找到策略實例但有總资金，設置預設
					if params["total_capital"] == nil && sym.TotalAllocatedCapital > 0 {
						params["total_capital"] = sym.TotalAllocatedCapital
					}

					break
				}
			}

			// 如果沒有找到特定配置，嘗試從全局策略配置中讀取
			if !found && cfg.Strategies.Enabled {
				if stratCfg, ok := cfg.Strategies.Configs[strategy]; ok && stratCfg.Enabled {
					for k, v := range stratCfg.Config {
						params[k] = v
					}
					found = true
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"found":   found,
		"params":  params,
	})
}

// ========== 智能參數推薦和自動回測 API ==========

// SmartParamsRequest 智能參數推薦請求
type SmartParamsRequest struct {
	Exchange     string  `json:"exchange" binding:"required"`
	MarketType   string  `json:"market_type" binding:"required"`
	Symbol       string  `json:"symbol" binding:"required"`
	Strategy     string  `json:"strategy" binding:"required"`
	TotalCapital float64 `json:"total_capital" binding:"required"`
}

// getSmartParamsRecommendation 獲取智能參數推薦 GET /api/backtest/smart-params
func getSmartParamsRecommendation(c *gin.Context) {
	if smartParamsService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "智能參數服務未初始化",
		})
		return
	}

	exchange := c.Query("exchange")
	marketType := c.Query("market_type")
	symbol := c.Query("symbol")
	strategy := c.Query("strategy")
	totalCapitalStr := c.Query("total_capital")

	if exchange == "" {
		exchange = "binance"
	}
	if marketType == "" {
		marketType = "futures"
	}
	if symbol == "" || strategy == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缺少必要參數: symbol, strategy",
		})
		return
	}

	totalCapital := 10000.0
	if totalCapitalStr != "" {
		if v, err := strconv.ParseFloat(totalCapitalStr, 64); err == nil && v > 0 {
			totalCapital = v
		}
	}

	ctx := c.Request.Context()
	recommendation, err := smartParamsService.GetRecommendation(
		ctx, exchange, marketType, symbol, strategy, totalCapital,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("獲取智能推薦失敗: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"recommendation": recommendation,
	})
}

// postSmartParamsRecommendation 獲取智能參數推薦 POST /api/backtest/smart-params
func postSmartParamsRecommendation(c *gin.Context) {
	if smartParamsService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "智能參數服務未初始化",
		})
		return
	}

	var req SmartParamsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": fmt.Sprintf("參數錯誤: %v", err),
		})
		return
	}

	ctx := c.Request.Context()
	recommendation, err := smartParamsService.GetRecommendation(
		ctx, req.Exchange, req.MarketType, req.Symbol, req.Strategy, req.TotalCapital,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("獲取智能推薦失敗: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"recommendation": recommendation,
	})
}

// getMultipleSmartParams 獲取多個策略的智能推薦 GET /api/backtest/smart-params/multiple
func getMultipleSmartParams(c *gin.Context) {
	if smartParamsService == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "智能參數服務未初始化",
		})
		return
	}

	exchange := c.DefaultQuery("exchange", "binance")
	marketType := c.DefaultQuery("market_type", "futures")
	symbol := c.Query("symbol")

	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缺少必要參數: symbol",
		})
		return
	}

	totalCapital := 10000.0
	if v, err := strconv.ParseFloat(c.DefaultQuery("total_capital", "10000"), 64); err == nil && v > 0 {
		totalCapital = v
	}

	// 獲取所有策略推薦
	strategies := []string{"grid", "momentum", "mean_reversion", "trend_following", "dca", "martingale"}

	ctx := c.Request.Context()
	recommendations, err := smartParamsService.GetMultipleRecommendations(
		ctx, exchange, marketType, symbol, strategies, totalCapital,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("獲取智能推薦失敗: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":         true,
		"recommendations": recommendations,
	})
}

// getPrecomputedResults 獲取預計算回測結果 GET /api/backtest/precomputed
func getPrecomputedResults(c *gin.Context) {
	if autoBacktestScheduler == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"results": []interface{}{},
			"message": "自動回測服務未啟用",
		})
		return
	}

	symbol := c.Query("symbol")
	onlyReady := c.Query("only_ready") == "1" || c.Query("only_ready") == "true"

	var results []*backtest.PrecomputedResult

	if symbol != "" {
		results = autoBacktestScheduler.GetResultsBySymbol(symbol)
	} else if onlyReady {
		results = autoBacktestScheduler.GetReadyResults()
	} else {
		results = autoBacktestScheduler.GetPrecomputedResults()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"results": results,
		"count":   len(results),
	})
}

// getPrecomputedResult 獲取特定預計算結果 GET /api/backtest/precomputed/:symbol/:strategy
func getPrecomputedResult(c *gin.Context) {
	if autoBacktestScheduler == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "自動回測服務未啟用",
		})
		return
	}

	symbol := c.Param("symbol")
	strategy := c.Param("strategy")
	exchange := c.DefaultQuery("exchange", "binance")

	if symbol == "" || strategy == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "缺少必要參數",
		})
		return
	}

	result := autoBacktestScheduler.GetPrecomputedResult(symbol, exchange, strategy)
	if result == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "未找到預計算結果",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"result":  result,
	})
}

// triggerPrecompute 觸發預計算 POST /api/backtest/precomputed/trigger
func triggerPrecompute(c *gin.Context) {
	if autoBacktestScheduler == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "自動回測服務未啟用",
		})
		return
	}

	var req struct {
		Symbol     string `json:"symbol" binding:"required"`
		Exchange   string `json:"exchange"`
		MarketType string `json:"market_type"`
		Strategy   string `json:"strategy" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": fmt.Sprintf("參數錯誤: %v", err),
		})
		return
	}

	if req.Exchange == "" {
		req.Exchange = "binance"
	}
	if req.MarketType == "" {
		req.MarketType = "futures"
	}

	if err := autoBacktestScheduler.TriggerPrecompute(req.Symbol, req.Exchange, req.MarketType, req.Strategy); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("觸發預計算失敗: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已觸發預計算任務",
	})
}

// getAutoSchedulerStatus 獲取自動調度器狀態 GET /api/backtest/scheduler/status
func getAutoSchedulerStatus(c *gin.Context) {
	if autoBacktestScheduler == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"enabled": false,
			"running": false,
		})
		return
	}

	config := autoBacktestScheduler.GetConfig()
	results := autoBacktestScheduler.GetPrecomputedResults()

	readyCount := 0
	runningCount := 0
	for _, r := range results {
		if r.IsReady {
			readyCount++
		}
		if r.TaskStatus == "running" || r.TaskStatus == "pending" {
			runningCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":           true,
		"enabled":           config.Enabled,
		"running":           autoBacktestScheduler.IsRunning(),
		"schedule_interval": config.ScheduleInterval.String(),
		"total_tasks":       len(results),
		"ready_count":       readyCount,
		"running_count":     runningCount,
		"symbols":           config.Symbols,
	})
}

// ---- 參數優化 API ----

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
		Strategy     string    `json:"strategy" binding:"required"`
		Symbol       string    `json:"symbol" binding:"required"`
		Interval     string    `json:"interval" binding:"required"`
		StartTime    time.Time `json:"start_time" binding:"required"`
		EndTime      time.Time `json:"end_time" binding:"required"`
		TotalCapital float64   `json:"total_capital" binding:"required"`
		SearchSpace  struct {
			Strategy string `json:"strategy"`
			Ranges   map[string]struct {
				Min  float64 `json:"min"`
				Max  float64 `json:"max"`
				Step float64 `json:"step"`
			} `json:"ranges"`
		} `json:"search_space"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("參數錯誤: %v", err)})
		return
	}
	if req.EndTime.Before(req.StartTime) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_time 必須晚於 start_time"})
		return
	}
	validStrategies := map[string]bool{
		"grid": true, "momentum": true, "mean_reversion": true,
		"trend_following": true, "dca": true, "martingale": true,
	}
	if !validStrategies[req.Strategy] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("不支援的策略: %s", req.Strategy)})
		return
	}
	space := backtest.OptimSearchSpace{Strategy: req.Strategy, Ranges: make(map[string]backtest.OptimParamRange)}
	if len(req.SearchSpace.Ranges) > 0 {
		for k, v := range req.SearchSpace.Ranges {
			space.Ranges[k] = backtest.OptimParamRange{Min: v.Min, Max: v.Max, Step: v.Step}
		}
	} else {
		// 使用默认搜索空间
		defaultSpace := optimizer.GetDefaultSearchSpace(req.Strategy)
		for k, v := range defaultSpace.Ranges {
			space.Ranges[k] = backtest.OptimParamRange{Min: v.Min, Max: v.Max, Step: v.Step}
		}
	}
	space.Strategy = req.Strategy

	task := &backtest.OptimTask{
		Strategy:     req.Strategy,
		Symbol:       req.Symbol,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		TotalCapital: req.TotalCapital,
		SearchSpace:  space,
	}
	if err := optimTaskManager.CreateAndRun(task); err != nil {
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
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任務 id"})
		return
	}
	if optimTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "服務未初始化"})
		return
	}
	resultPath := filepath.Join("backtest", "optim_results", id+".json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "結果文件不存在或未生成"})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// deleteOptimTask 刪除參數優化任務 DELETE /api/backtest/optim/tasks/:id
func deleteOptimTask(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
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
	if err := store.DeleteOptimTask(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	// 可選：刪除結果文件
	_ = os.Remove(filepath.Join("backtest", "optim_results", id+".json"))
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

// validateKlineFileForBacktest 校验 K 线文件是否可用于回测
func validateKlineFileForBacktest(filename string, c *gin.Context) error {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		return nil // 无存储服务时跳过校验
	}

	sqliteStorage, ok := storageProv.GetStorage().(*storage.SQLiteStorage)
	if !ok {
		return nil // 非 SQLite 存储时跳过校验
	}

	kf, err := sqliteStorage.GetKlineFileByFilename(filename)
	if err != nil {
		return fmt.Errorf("查询文件记录失败: %v", err)
	}
	if kf == nil {
		return fmt.Errorf("文件 %s 未在数据库中记录，请检查文件是否存在", filename)
	}

	switch kf.Status {
	case "collecting":
		return fmt.Errorf("文件 %s 正在采集中，暂不可用于回测，请等待采集完成", filename)
	case "error":
		return fmt.Errorf("文件 %s 采集出错，不可用于回测", filename)
	case "completed":
		return nil // 正常情况
	default:
		return fmt.Errorf("文件 %s 状态异常: %s", filename, kf.Status)
	}
}
