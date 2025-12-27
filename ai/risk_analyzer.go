package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/position"
)

// RiskAnalyzer AI风险分析器
type RiskAnalyzer struct {
	aiService    AIService
	cfg          *config.Config
	exchange     exchange.IExchange
	positionManager *position.SuperPositionManager
	
	lastAnalysis *RiskAnalysisResponse
	lastAnalysisTime time.Time
	mu          sync.RWMutex
	
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewRiskAnalyzer 创建风险分析器
func NewRiskAnalyzer(
	aiService AIService,
	cfg *config.Config,
	exchange exchange.IExchange,
	positionManager *position.SuperPositionManager,
) *RiskAnalyzer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &RiskAnalyzer{
		aiService:      aiService,
		cfg:             cfg,
		exchange:        exchange,
		positionManager: positionManager,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start 启动风险分析器
func (ra *RiskAnalyzer) Start() {
	if !ra.cfg.AI.Modules.RiskAnalysis.Enabled {
		return
	}
	
	interval := time.Duration(ra.cfg.AI.Modules.RiskAnalysis.AnalysisInterval) * time.Second
	if interval <= 0 {
		interval = 300 * time.Second // 默认5分钟
	}
	
	go ra.analysisLoop(interval)
	logger.Info("✅ AI风险分析器已启动 (分析间隔: %v)", interval)
}

// Stop 停止风险分析器
func (ra *RiskAnalyzer) Stop() {
	if ra.cancel != nil {
		ra.cancel()
	}
}

// analysisLoop 分析循环
func (ra *RiskAnalyzer) analysisLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ra.ctx.Done():
			return
		case <-ticker.C:
			if err := ra.performAnalysis(); err != nil {
				logger.Warn("⚠️ AI风险分析失败: %v", err)
			}
		}
	}
}

// performAnalysis 执行分析
func (ra *RiskAnalyzer) performAnalysis() error {
	symbol := ra.cfg.Trading.Symbol
	
	// 获取账户信息
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	account, err := ra.exchange.GetAccount(ctx)
	if err != nil {
		return fmt.Errorf("获取账户信息失败: %w", err)
	}
	
	accountBalance := 0.0
	usedMargin := 0.0
	if account != nil {
		accountBalance = account.TotalWalletBalance
		usedMargin = account.TotalMarginBalance - account.AvailableBalance
	}
	
	// 获取持仓
	positions, err := ra.exchange.GetPositions(ctx, symbol)
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}
	
	positionData := make([]PositionData, 0)
	if positions != nil {
		for _, p := range positions {
			positionData = append(positionData, PositionData{
				Symbol:     p.Symbol,
				Size:       p.Size,
				EntryPrice: p.EntryPrice,
				PnL:        p.UnrealizedPNL,
			})
		}
	}
	
	// 获取未完成订单
	openOrders, err := ra.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("获取未完成订单失败: %w", err)
	}
	
	openOrdersCount := 0
	if openOrders != nil {
		openOrdersCount = len(openOrders)
	}
	
	// 获取K线数据计算波动率
	klines, err := ra.exchange.GetHistoricalKlines(ctx, symbol, "1m", 100)
	if err != nil {
		return fmt.Errorf("获取K线数据失败: %w", err)
	}
	
	volatility := ra.calculateVolatility(klines)
	
	// 获取当前价格
	currentPrice := 0.0
	if len(klines) > 0 {
		currentPrice = klines[len(klines)-1].Close
	}
	
	klineData := make([]KlineData, 0, len(klines))
	for _, k := range klines {
		openTime := time.Unix(k.Timestamp/1000, 0)
		closeTime := openTime.Add(time.Minute) // 假设是1分钟K线
		klineData = append(klineData, KlineData{
			OpenTime:  openTime,
			CloseTime: closeTime,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
		})
	}
	
	req := &RiskAnalysisRequest{
		Symbol:           symbol,
		CurrentPrice:      currentPrice,
		Positions:         positionData,
		OpenOrders:       openOrdersCount,
		AccountBalance:   accountBalance,
		UsedMargin:       usedMargin,
		MarketVolatility: volatility,
		Klines:           klineData,
	}
	
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	
	response, err := ra.aiService.AnalyzeRisk(ctx2, req)
	if err != nil {
		return fmt.Errorf("AI分析失败: %w", err)
	}
	
	ra.mu.Lock()
	ra.lastAnalysis = response
	ra.lastAnalysisTime = time.Now()
	ra.mu.Unlock()
	
	logger.Info("🛡️ [AI风险分析] 风险等级: %s, 风险评分: %.2f", response.RiskLevel, response.RiskScore)
	
	if len(response.Warnings) > 0 {
		for _, warning := range response.Warnings {
			logger.Warn("⚠️ [AI风险警告] %s", warning)
		}
	}
	
	return nil
}

// calculateVolatility 计算波动率
func (ra *RiskAnalyzer) calculateVolatility(klines []*exchange.Candle) float64 {
	if len(klines) < 2 {
		return 0.0
	}
	
	var returns []float64
	for i := 1; i < len(klines); i++ {
		ret := (klines[i].Close - klines[i-1].Close) / klines[i-1].Close
		returns = append(returns, ret)
	}
	
	// 计算标准差
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	
	variance := 0.0
	for _, r := range returns {
		variance += (r - mean) * (r - mean)
	}
	variance /= float64(len(returns))
	
	volatility := variance * 100 // 转换为百分比
	
	return volatility
}

// GetLastAnalysis 获取最后一次分析结果
func (ra *RiskAnalyzer) GetLastAnalysis() *RiskAnalysisResponse {
	ra.mu.RLock()
	defer ra.mu.RUnlock()
	return ra.lastAnalysis
}

// GetLastAnalysisTime 获取最后一次分析时间
func (ra *RiskAnalyzer) GetLastAnalysisTime() time.Time {
	ra.mu.RLock()
	defer ra.mu.RUnlock()
	return ra.lastAnalysisTime
}

