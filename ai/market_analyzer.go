package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/position"
	"quantmesh/storage"
)

// MarketAnalyzer AI市场分析器
type MarketAnalyzer struct {
	aiService      AIService
	cfg            *config.Config
	priceMonitor   *monitor.PriceMonitor
	exchange       exchange.IExchange
	storage        storage.Storage
	positionManager *position.SuperPositionManager
	
	lastAnalysis *MarketAnalysisResponse
	lastAnalysisTime time.Time
	mu          sync.RWMutex
	
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewMarketAnalyzer 创建市场分析器
func NewMarketAnalyzer(
	aiService AIService,
	cfg *config.Config,
	priceMonitor *monitor.PriceMonitor,
	exchange exchange.IExchange,
	storage storage.Storage,
	positionManager *position.SuperPositionManager,
) *MarketAnalyzer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &MarketAnalyzer{
		aiService:      aiService,
		cfg:             cfg,
		priceMonitor:    priceMonitor,
		exchange:        exchange,
		storage:         storage,
		positionManager: positionManager,
		ctx:             ctx,
		cancel:          cancel,
	}
}

// Start 启动市场分析器
func (ma *MarketAnalyzer) Start() {
	if !ma.cfg.AI.Modules.MarketAnalysis.Enabled {
		return
	}
	
	interval := time.Duration(ma.cfg.AI.Modules.MarketAnalysis.UpdateInterval) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	
	go ma.analysisLoop(interval)
	logger.Info("✅ AI市场分析器已启动 (更新间隔: %v)", interval)
}

// Stop 停止市场分析器
func (ma *MarketAnalyzer) Stop() {
	if ma.cancel != nil {
		ma.cancel()
	}
}

// analysisLoop 分析循环
func (ma *MarketAnalyzer) analysisLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ma.ctx.Done():
			return
		case <-ticker.C:
			if err := ma.performAnalysis(); err != nil {
				logger.Warn("⚠️ AI市场分析失败: %v", err)
			}
		}
	}
}

// performAnalysis 执行分析
func (ma *MarketAnalyzer) performAnalysis() error {
	currentPrice := ma.priceMonitor.GetLastPrice()
	if currentPrice <= 0 {
		return nil // 价格未就绪
	}
	
	symbol := ma.cfg.Trading.Symbol
	
	// 获取K线数据
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	klines, err := ma.exchange.GetHistoricalKlines(ctx, symbol, "1m", 100)
	if err != nil {
		return fmt.Errorf("获取K线数据失败: %w", err)
	}
	
	// 转换为KlineData
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
	
	// 获取持仓数据
	var positions []PositionData
	if ma.positionManager != nil {
		// 从仓位管理器获取持仓
		slots := ma.positionManager.GetAllSlotsDetailed()
		currentPrice := ma.priceMonitor.GetLastPrice()
		for _, slot := range slots {
			if slot.PositionStatus == "FILLED" && slot.PositionQty > 0 {
				pnl := (currentPrice - slot.Price) * slot.PositionQty
				positions = append(positions, PositionData{
					Symbol:     symbol,
					Size:       slot.PositionQty,
					EntryPrice: slot.Price,
					PnL:        pnl,
				})
			}
		}
	}
	
	// 计算成交量
	volume := 0.0
	if len(klineData) > 0 {
		for _, k := range klineData {
			volume += k.Volume
		}
		volume /= float64(len(klineData)) // 平均成交量
	}
	
	// 获取价格历史
	priceHistory := make([]float64, 0, len(klineData))
	for _, k := range klineData {
		priceHistory = append(priceHistory, k.Close)
	}
	
	req := &MarketAnalysisRequest{
		Symbol:       symbol,
		CurrentPrice: currentPrice,
		Klines:       klineData,
		Positions:    positions,
		Volume:       volume,
		PriceHistory: priceHistory,
		Timestamp:    time.Now(),
	}
	
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	
	response, err := ma.aiService.AnalyzeMarket(ctx2, req)
	if err != nil {
		return fmt.Errorf("AI分析失败: %w", err)
	}
	
	ma.mu.Lock()
	ma.lastAnalysis = response
	ma.lastAnalysisTime = time.Now()
	ma.mu.Unlock()
	
	logger.Info("📊 [AI市场分析] 趋势: %s, 信号: %s, 置信度: %.2f", 
		response.Trend, response.Signal, response.Confidence)
	
	return nil
}

// GetLastAnalysis 获取最后一次分析结果
func (ma *MarketAnalyzer) GetLastAnalysis() *MarketAnalysisResponse {
	ma.mu.RLock()
	defer ma.mu.RUnlock()
	return ma.lastAnalysis
}

// GetLastAnalysisTime 获取最后一次分析时间
func (ma *MarketAnalyzer) GetLastAnalysisTime() time.Time {
	ma.mu.RLock()
	defer ma.mu.RUnlock()
	return ma.lastAnalysisTime
}

// TriggerAnalysis 手动触发分析
func (ma *MarketAnalyzer) TriggerAnalysis() error {
	return ma.performAnalysis()
}

