package web

import (
	"fmt"
	"net/http"
	"time"

	"quantmesh/backtest"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

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
	// 从全局配置获取 Binance 配置
	// 这里简化处理，实际应该从配置文件读取
	return map[string]string{
		"api_key":    "",
		"secret_key": "",
		"testnet":    "false",
	}
}
