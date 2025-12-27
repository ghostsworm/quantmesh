package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/storage"
)

// ParameterOptimizer AI参数优化器
type ParameterOptimizer struct {
	aiService AIService
	cfg       *config.Config
	storage   storage.Storage
	
	lastOptimization *ParameterOptimizationResponse
	lastOptimizationTime time.Time
	mu          sync.RWMutex
	
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewParameterOptimizer 创建参数优化器
func NewParameterOptimizer(
	aiService AIService,
	cfg *config.Config,
	storage storage.Storage,
) *ParameterOptimizer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &ParameterOptimizer{
		aiService: aiService,
		cfg:        cfg,
		storage:    storage,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start 启动参数优化器
func (po *ParameterOptimizer) Start() {
	if !po.cfg.AI.Modules.ParameterOptimization.Enabled {
		return
	}
	
	interval := time.Duration(po.cfg.AI.Modules.ParameterOptimization.OptimizationInterval) * time.Second
	if interval <= 0 {
		interval = 3600 * time.Second // 默认1小时
	}
	
	go po.optimizationLoop(interval)
	logger.Info("✅ AI参数优化器已启动 (优化间隔: %v)", interval)
}

// Stop 停止参数优化器
func (po *ParameterOptimizer) Stop() {
	if po.cancel != nil {
		po.cancel()
	}
}

// optimizationLoop 优化循环
func (po *ParameterOptimizer) optimizationLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-po.ctx.Done():
			return
		case <-ticker.C:
			if err := po.performOptimization(); err != nil {
				logger.Warn("⚠️ AI参数优化失败: %v", err)
			}
		}
	}
}

// performOptimization 执行优化
func (po *ParameterOptimizer) performOptimization() error {
	symbol := po.cfg.Trading.Symbol
	
	// 获取历史交易数据（最近7天）
	endTime := time.Now()
	startTime := endTime.Add(-7 * 24 * time.Hour)
	
	trades, err := po.storage.QueryTrades(startTime, endTime, 1000, 0)
	if err != nil {
		return fmt.Errorf("获取交易数据失败: %w", err)
	}
	
	// 转换为TradeData
	tradeData := make([]TradeData, 0, len(trades))
	for _, t := range trades {
		if t != nil {
			tradeData = append(tradeData, TradeData{
				BuyPrice:  t.BuyPrice,
				SellPrice: t.SellPrice,
				Quantity:  t.Quantity,
				PnL:       t.PnL,
				Timestamp: t.CreatedAt,
			})
		}
	}
	
	// 计算性能指标
	performance := po.calculatePerformance(tradeData)
	
	// 当前参数
	currentParams := TradingParams{
		PriceInterval: po.cfg.Trading.PriceInterval,
		BuyWindowSize: po.cfg.Trading.BuyWindowSize,
		SellWindowSize: po.cfg.Trading.SellWindowSize,
		OrderQuantity: po.cfg.Trading.OrderQuantity,
	}
	
	req := &ParameterOptimizationRequest{
		Symbol:           symbol,
		CurrentParams:    currentParams,
		HistoricalTrades: tradeData,
		Performance:      performance,
		MarketCondition:  "normal", // 可以从市场分析器获取
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	response, err := po.aiService.OptimizeParameters(ctx, req)
	if err != nil {
		return fmt.Errorf("AI优化失败: %w", err)
	}
	
	po.mu.Lock()
	po.lastOptimization = response
	po.lastOptimizationTime = time.Now()
	po.mu.Unlock()
	
	logger.Info("🔧 [AI参数优化] 推荐参数: 价格间隔=%.2f, 买单窗口=%d, 卖单窗口=%d, 订单金额=%.2f, 预期改进=%.2f%%",
		response.RecommendedParams.PriceInterval,
		response.RecommendedParams.BuyWindowSize,
		response.RecommendedParams.SellWindowSize,
		response.RecommendedParams.OrderQuantity,
		response.ExpectedImprovement)
	
	// 如果启用自动应用，更新配置
	if po.cfg.AI.Modules.ParameterOptimization.AutoApply && response.Confidence > 0.7 {
		po.applyOptimization(response)
	}
	
	return nil
}

// calculatePerformance 计算性能指标
func (po *ParameterOptimizer) calculatePerformance(trades []TradeData) PerformanceData {
	if len(trades) == 0 {
		return PerformanceData{}
	}
	
	totalTrades := len(trades)
	totalPnL := 0.0
	totalVolume := 0.0
	winCount := 0
	maxDrawdown := 0.0
	peak := 0.0
	runningPnL := 0.0
	
	for _, trade := range trades {
		totalPnL += trade.PnL
		totalVolume += trade.Quantity * trade.BuyPrice
		runningPnL += trade.PnL
		
		if trade.PnL > 0 {
			winCount++
		}
		
		if runningPnL > peak {
			peak = runningPnL
		}
		
		drawdown := peak - runningPnL
		if drawdown > maxDrawdown {
			maxDrawdown = drawdown
		}
	}
	
	winRate := float64(winCount) / float64(totalTrades)
	
	// 计算夏普比率（简化版）
	sharpeRatio := 0.0
	if totalTrades > 1 {
		avgReturn := totalPnL / float64(totalTrades)
		// 简化计算，实际应该使用标准差
		if avgReturn > 0 {
			sharpeRatio = avgReturn * 10 // 简化
		}
	}
	
	return PerformanceData{
		TotalTrades:  totalTrades,
		WinRate:      winRate,
		TotalPnL:     totalPnL,
		TotalVolume:  totalVolume,
		MaxDrawdown:  maxDrawdown / peak * 100, // 百分比
		SharpeRatio:  sharpeRatio,
	}
}

// applyOptimization 应用优化结果
func (po *ParameterOptimizer) applyOptimization(response *ParameterOptimizationResponse) {
	logger.Info("🔧 [AI参数优化] 自动应用优化结果（置信度: %.2f）", response.Confidence)
	
	// 更新配置（注意：这里只是更新内存中的配置，不会保存到文件）
	po.cfg.Trading.PriceInterval = response.RecommendedParams.PriceInterval
	po.cfg.Trading.BuyWindowSize = response.RecommendedParams.BuyWindowSize
	po.cfg.Trading.SellWindowSize = response.RecommendedParams.SellWindowSize
	po.cfg.Trading.OrderQuantity = response.RecommendedParams.OrderQuantity
	
	logger.Info("✅ [AI参数优化] 参数已更新")
}

// GetLastOptimization 获取最后一次优化结果
func (po *ParameterOptimizer) GetLastOptimization() *ParameterOptimizationResponse {
	po.mu.RLock()
	defer po.mu.RUnlock()
	return po.lastOptimization
}

