package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"quantmesh/backtest"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// backtestTaskManager 回测任务管理器（由 main 注入）
var backtestTaskManager *backtest.TaskManager

// SetBacktestTaskManager 设置回测任务管理器
func SetBacktestTaskManager(m *backtest.TaskManager) {
	backtestTaskManager = m
}

// BacktestRequest 回测请求
type BacktestRequest struct {
	Strategy       string    `json:"strategy" binding:"required"`        // "momentum", "mean_reversion", "trend_following"
	Symbol         string    `json:"symbol" binding:"required"`          // "BTCUSDT"
	Interval       string    `json:"interval" binding:"required"`        // "1m", "5m", "1h"
	StartTime      time.Time `json:"start_time" binding:"required"`      // 开始时间
	EndTime        time.Time `json:"end_time" binding:"required"`        // 结束时间
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
			Message: fmt.Sprintf("请求参数错误: %v", err),
		})
		return
	}

	// 验证策略名称
	validStrategies := map[string]bool{
		"momentum":        true,
		"mean_reversion":  true,
		"trend_following": true,
	}
	if !validStrategies[req.Strategy] {
		c.JSON(http.StatusBadRequest, BacktestResponse{
			Success: false,
			Message: fmt.Sprintf("不支持的策略: %s", req.Strategy),
		})
		return
	}

	// 验证时间范围
	if req.EndTime.Before(req.StartTime) {
		c.JSON(http.StatusBadRequest, BacktestResponse{
			Success: false,
			Message: "结束时间必须晚于开始时间",
		})
		return
	}

	logger.Info("📊 开始回测: 策略=%s, 交易对=%s, 周期=%s",
		req.Strategy, req.Symbol, req.Interval)

	// 获取 Binance 配置
	binanceConfig := getBinanceConfig()

	// 1. 获取历史数据（优先缓存）
	candles, err := backtest.GetHistoricalData(
		req.Symbol,
		req.Interval,
		req.StartTime,
		req.EndTime,
		binanceConfig,
	)
	if err != nil {
		logger.Error("获取历史数据失败: %v", err)
		c.JSON(http.StatusInternalServerError, BacktestResponse{
			Success: false,
			Message: fmt.Sprintf("获取历史数据失败: %v", err),
		})
		return
	}

	if len(candles) == 0 {
		c.JSON(http.StatusBadRequest, BacktestResponse{
			Success: false,
			Message: "未获取到历史数据",
		})
		return
	}

	logger.Info("✅ 获取历史数据成功: %d 根K线", len(candles))

	// 2. 创建策略适配器
	var strategy backtest.StrategyAdapter
	switch req.Strategy {
	case "momentum":
		strategy = backtest.NewMomentumAdapter()
	case "mean_reversion":
		strategy = backtest.NewMeanReversionAdapter()
	case "trend_following":
		strategy = backtest.NewTrendFollowingAdapter()
	}

	// 3. 创建回测器
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

	// 6. 保存权益曲线
	equityPath, err := backtest.SaveEquityCurveCSV(result)
	if err != nil {
		logger.Warn("保存权益曲线失败: %v", err)
	} else {
		logger.Info("📈 权益曲线已保存: %s", equityPath)
	}

	logger.Info("✅ 回测完成: 总收益率=%.2f%%, 夏普比率=%.2f",
		result.Metrics.TotalReturn, result.Metrics.SharpeRatio)

	c.JSON(http.StatusOK, BacktestResponse{
		Success:    true,
		Message:    "回测完成",
		Result:     result,
		ReportPath: reportPath,
	})
}

// getCacheStats 获取缓存统计
func getCacheStats(c *gin.Context) {
	stats, err := backtest.GetCacheStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": fmt.Sprintf("获取缓存统计失败: %v", err),
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

// deleteCache 删除指定缓存
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
			"message": fmt.Sprintf("删除缓存失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "缓存已删除",
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

// getBinanceConfig 获取 Binance 配置
func getBinanceConfig() map[string]string {
	return getExchangeConfig("binance")
}

// getExchangeConfig 根据交易所名称获取配置（用于历史 K 线等公开接口）
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

// getBacktestStrategies 获取策略列表及参数定义 GET /api/backtest/strategies
func getBacktestStrategies(c *gin.Context) {
	defs := backtest.GetAllStrategyDefinitions()
	c.JSON(http.StatusOK, gin.H{"success": true, "strategies": defs})
}

// getBacktestPreset 获取交易对推荐配置 GET /api/backtest/presets/:symbol
func getBacktestPreset(c *gin.Context) {
	symbol := c.Param("symbol")
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 symbol"})
		return
	}
	preset := backtest.GetSymbolPreset(symbol)
	c.JSON(http.StatusOK, gin.H{"success": true, "preset": preset})
}

// postCacheGenerate 生成 K 线缓存（异步） POST /api/backtest/cache/generate
func postCacheGenerate(c *gin.Context) {
	var req struct {
		Symbol    string `json:"symbol" binding:"required"`
		Interval  string `json:"interval" binding:"required"`
		StartDate string `json:"start_date" binding:"required"` // 2006-01-02
		EndDate   string `json:"end_date" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("参数错误: %v", err)})
		return
	}
	startTime, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "start_date 格式应为 2006-01-02"})
		return
	}
	endTime, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_date 格式应为 2006-01-02"})
		return
	}
	if endTime.Before(startTime) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_date 必须晚于 start_date"})
		return
	}
	binanceConfig := getBinanceConfig()
	go func() {
		_, _ = backtest.GetHistoricalData(req.Symbol, req.Interval, startTime, endTime, binanceConfig)
		logger.Info("✅ K线缓存生成完成: %s %s %s ~ %s", req.Symbol, req.Interval, req.StartDate, req.EndDate)
	}()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "已在后台生成缓存",
		"cache_key": fmt.Sprintf("%s_%s_%s_%s", req.Symbol, req.Interval, req.StartDate, req.EndDate),
	})
}

// getCacheStatus 查询指定缓存是否存在 GET /api/backtest/cache/status?symbol=&interval=&start_date=&end_date=
func getCacheStatus(c *gin.Context) {
	symbol := c.Query("symbol")
	interval := c.Query("interval")
	startDate := c.Query("start_date")
	endDate := c.Query("end_date")
	if symbol == "" || interval == "" || startDate == "" || endDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 symbol, interval, start_date, end_date"})
		return
	}
	cacheKey := fmt.Sprintf("%s_%s_%s_%s", symbol, interval, startDate, endDate)
	filename := filepath.Join("backtest", "cache", cacheKey+".csv")
	_, err := os.Stat(filename)
	exists := err == nil
	c.JSON(http.StatusOK, gin.H{"success": true, "cache_key": cacheKey, "exists": exists})
}

// postBacktestTasks 创建回测任务 POST /api/backtest/tasks
func postBacktestTasks(c *gin.Context) {
	if backtestTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "回测服务未初始化"})
		return
	}
	var req struct {
		Strategy     string                 `json:"strategy" binding:"required"`
		Symbol       string                 `json:"symbol" binding:"required"`
		Interval     string                 `json:"interval" binding:"required"`
		StartTime    time.Time              `json:"start_time" binding:"required"`
		EndTime      time.Time              `json:"end_time" binding:"required"`
		Params       map[string]interface{} `json:"params"`
		TotalCapital float64                `json:"total_capital" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("参数错误: %v", err)})
		return
	}
	if req.EndTime.Before(req.StartTime) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "end_time 必须晚于 start_time"})
		return
	}
	validStrategies := map[string]bool{
		"grid": true, "momentum": true, "mean_reversion": true,
		"trend_following": true, "dca": true, "martingale": true, "combo": true,
	}
	if !validStrategies[req.Strategy] {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": fmt.Sprintf("不支持的策略: %s", req.Strategy)})
		return
	}
	task := &backtest.BacktestTask{
		Strategy:     req.Strategy,
		Symbol:       req.Symbol,
		Interval:     req.Interval,
		StartTime:    req.StartTime,
		EndTime:      req.EndTime,
		Params:       req.Params,
		TotalCapital: req.TotalCapital,
	}
	if task.Params == nil {
		task.Params = make(map[string]interface{})
	}
	if err := backtestTaskManager.CreateAndRun(task); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "任务已创建并开始执行", "task_id": task.ID})
}

// getBacktestTasks 获取任务列表 GET /api/backtest/tasks?limit=50&offset=0
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

// getBacktestTaskByID 获取任务详情 GET /api/backtest/tasks/:id
func getBacktestTaskByID(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任务 id"})
		return
	}
	if backtestTaskManager == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}
	store := backtestTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}
	task, err := store.GetBacktestTask(id)
	if err != nil || task == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "任务不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "task": task})
}

// getBacktestTaskResult 获取结果 JSON GET /api/backtest/tasks/:id/result
func getBacktestTaskResult(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任务 id"})
		return
	}
	if backtestTaskManager == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "结果不存在"})
		return
	}
	resultPath := filepath.Join("backtest", "results", id+".json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "结果文件不存在或未生成"})
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// getBacktestTaskReport 获取/下载报告 Markdown GET /api/backtest/tasks/:id/report
func getBacktestTaskReport(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任务 id"})
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

// deleteBacktestTask 删除任务 DELETE /api/backtest/tasks/:id
func deleteBacktestTask(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少任务 id"})
		return
	}
	if backtestTaskManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "回测服务未初始化"})
		return
	}
	store := backtestTaskManager.GetStore()
	if store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存储不可用"})
		return
	}
	if err := store.DeleteBacktestTask(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	_ = os.Remove(filepath.Join("backtest", "results", id+".json"))
	_ = os.Remove(filepath.Join("backtest", "reports", id+".md"))
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "已删除"})
}
