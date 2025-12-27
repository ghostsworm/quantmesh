package ai

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// PolymarketSignalAnalyzer 预测市场信号分析器
type PolymarketSignalAnalyzer struct {
	aiService     AIService
	cfg           *config.Config
	dataSourceMgr   *DataSourceManager
	
	lastAnalysis     *PolymarketSignalAnalysisResponse
	lastAnalysisTime time.Time
	mu               sync.RWMutex
	
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewPolymarketSignalAnalyzer 创建预测市场信号分析器
func NewPolymarketSignalAnalyzer(
	aiService AIService,
	cfg *config.Config,
	dataSourceMgr *DataSourceManager,
) *PolymarketSignalAnalyzer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &PolymarketSignalAnalyzer{
		aiService:     aiService,
		cfg:            cfg,
		dataSourceMgr: dataSourceMgr,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start 启动预测市场信号分析器
func (psa *PolymarketSignalAnalyzer) Start() {
	if !psa.cfg.AI.Modules.PolymarketSignal.Enabled {
		return
	}
	
	interval := time.Duration(psa.cfg.AI.Modules.PolymarketSignal.AnalysisInterval) * time.Second
	if interval <= 0 {
		interval = 300 * time.Second // 默认5分钟
	}
	
	go psa.analysisLoop(interval)
	logger.Info("✅ Polymarket预测市场信号分析器已启动 (分析间隔: %v)", interval)
	
	// 显示配置信息
	keywords := psa.cfg.AI.Modules.PolymarketSignal.Markets.Keywords
	if len(keywords) > 0 {
		logger.Info("📊 [Polymarket信号] 关注关键词: %v", keywords)
	}
	logger.Info("📊 [Polymarket信号] 最小流动性: %.0f USDC, 最小24h交易量: %.0f USDC", 
		psa.cfg.AI.Modules.PolymarketSignal.Markets.MinLiquidity,
		psa.cfg.AI.Modules.PolymarketSignal.Markets.MinVolume24h)
}

// Stop 停止预测市场信号分析器
func (psa *PolymarketSignalAnalyzer) Stop() {
	if psa.cancel != nil {
		psa.cancel()
	}
}

// analysisLoop 分析循环
func (psa *PolymarketSignalAnalyzer) analysisLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-psa.ctx.Done():
			return
		case <-ticker.C:
			if err := psa.performAnalysis(); err != nil {
				logger.Warn("⚠️ Polymarket信号分析失败: %v", err)
			}
		}
	}
}

// performAnalysis 执行分析
func (psa *PolymarketSignalAnalyzer) performAnalysis() error {
	_ = psa.cfg.Trading.Symbol // 保留供后续使用
	
	// 获取API URL
	apiURL := psa.cfg.AI.Modules.PolymarketSignal.APIURL
	if apiURL == "" {
		apiURL = DefaultPolymarketAPI
	}
	
	// 获取关键词
	keywords := psa.cfg.AI.Modules.PolymarketSignal.Markets.Keywords
	if len(keywords) == 0 {
		// 使用默认关键词
		keywords = []string{"bitcoin", "btc", "ethereum", "eth", "crypto", "regulation"}
	}
	
	// 获取市场列表
	markets, err := psa.dataSourceMgr.FetchPolymarketMarkets(apiURL, keywords)
	if err != nil {
		return fmt.Errorf("获取市场列表失败: %w", err)
	}
	
	if len(markets) == 0 {
		logger.Info("📊 [Polymarket信号] 未找到相关市场")
		return nil
	}
	
	logger.Info("📊 [Polymarket信号] 找到 %d 个相关市场", len(markets))
	
	// 筛选市场（流动性、交易量、到期时间）
	filteredMarkets := psa.filterMarkets(markets)
	if len(filteredMarkets) == 0 {
		logger.Info("📊 [Polymarket信号] 经过筛选后无有效市场")
		return nil
	}
	
	logger.Info("📊 [Polymarket信号] 筛选后剩余 %d 个有效市场", len(filteredMarkets))
	
	// 获取市场详细数据
	marketDataList := make([]*PolymarketMarketData, 0, len(filteredMarkets))
	for _, market := range filteredMarkets {
		data, err := psa.dataSourceMgr.FetchPolymarketMarketData(apiURL, market.ID)
		if err != nil {
			logger.Warn("⚠️ 获取市场数据失败 %s: %v", market.ID, err)
			continue
		}
		marketDataList = append(marketDataList, data)
	}
	
	if len(marketDataList) == 0 {
		return fmt.Errorf("无法获取市场数据")
	}
	
	// 生成信号
	signals := psa.generateSignals(filteredMarkets, marketDataList)
	if len(signals) == 0 {
		logger.Info("📊 [Polymarket信号] 未生成有效信号")
		return nil
	}
	
	// 聚合信号
	aggregateSignal, aggregateStrength, aggregateConfidence, reasoning := psa.aggregateSignals(signals)
	
	response := &PolymarketSignalAnalysisResponse{
		Signals:           signals,
		AggregateSignal:   aggregateSignal,
		AggregateStrength: aggregateStrength,
		AggregateConfidence: aggregateConfidence,
		Reasoning:         reasoning,
		Timestamp:         time.Now(),
	}
	
	psa.mu.Lock()
	psa.lastAnalysis = response
	psa.lastAnalysisTime = time.Now()
	psa.mu.Unlock()
	
	logger.Info("📊 [Polymarket信号] 聚合信号: %s, 强度: %.2f, 置信度: %.2f", 
		aggregateSignal, aggregateStrength, aggregateConfidence)
	logger.Info("📊 [Polymarket信号] 生成 %d 个有效信号", len(signals))
	
	return nil
}

// filterMarkets 筛选市场
func (psa *PolymarketSignalAnalyzer) filterMarkets(markets []*PolymarketMarket) []*PolymarketMarket {
	filtered := make([]*PolymarketMarket, 0)
	now := time.Now()
	
	minLiquidity := psa.cfg.AI.Modules.PolymarketSignal.Markets.MinLiquidity
	minDaysToExpiry := psa.cfg.AI.Modules.PolymarketSignal.Markets.MinDaysToExpiry
	maxDaysToExpiry := psa.cfg.AI.Modules.PolymarketSignal.Markets.MaxDaysToExpiry
	
	for _, market := range markets {
		// 流动性筛选
		if market.Liquidity < minLiquidity {
			continue
		}
		
		// 到期时间筛选
		daysToExpiry := market.EndDate.Sub(now).Hours() / 24
		if daysToExpiry < float64(minDaysToExpiry) || daysToExpiry > float64(maxDaysToExpiry) {
			continue
		}
		
		filtered = append(filtered, market)
	}
	
	return filtered
}

// generateSignals 生成信号
func (psa *PolymarketSignalAnalyzer) generateSignals(
	markets []*PolymarketMarket,
	marketDataList []*PolymarketMarketData,
) []*PolymarketSignal {
	signals := make([]*PolymarketSignal, 0)
	
	buyThreshold := psa.cfg.AI.Modules.PolymarketSignal.SignalGeneration.BuyThreshold
	sellThreshold := psa.cfg.AI.Modules.PolymarketSignal.SignalGeneration.SellThreshold
	minSignalStrength := psa.cfg.AI.Modules.PolymarketSignal.SignalGeneration.MinSignalStrength
	minConfidence := psa.cfg.AI.Modules.PolymarketSignal.SignalGeneration.MinConfidence
	
	// 创建市场数据映射
	marketDataMap := make(map[string]*PolymarketMarketData)
	for _, data := range marketDataList {
		marketDataMap[data.MarketID] = data
	}
	
	for _, market := range markets {
		data, exists := marketDataMap[market.ID]
		if !exists {
			continue
		}
		
		// 使用YES价格作为概率
		probability := data.YesPrice
		
		// 计算信号强度（距离0.5的距离，归一化到0-1）
		signalStrength := abs(probability - 0.5) * 2
		
		if signalStrength < minSignalStrength {
			continue // 信号强度不足
		}
		
		// 计算置信度（基于流动性和交易量）
		liquidityWeight := market.Liquidity / 100000.0 // 假设10万USDC为满分
		if liquidityWeight > 1.0 {
			liquidityWeight = 1.0
		}
		
		volumeWeight := data.Volume24h / 10000.0 // 假设1万USDC为满分
		if volumeWeight > 1.0 {
			volumeWeight = 1.0
		}
		
		confidence := (liquidityWeight*0.6 + volumeWeight*0.4)
		
		if confidence < minConfidence {
			continue // 置信度不足
		}
		
		// 判断信号方向
		var signal string
		var reasoning string
		
		if probability >= buyThreshold {
			signal = "buy"
			reasoning = fmt.Sprintf("预测市场显示事件发生概率为 %.1f%%，看涨信号", probability*100)
		} else if probability <= sellThreshold {
			signal = "sell"
			reasoning = fmt.Sprintf("预测市场显示事件发生概率为 %.1f%%，看跌信号", probability*100)
		} else {
			signal = "hold"
			reasoning = fmt.Sprintf("预测市场显示事件发生概率为 %.1f%%，中性信号", probability*100)
		}
		
		// 判断相关性
		relevance := psa.determineRelevance(market.Question, market.Description)
		
		signals = append(signals, &PolymarketSignal{
			MarketID:    market.ID,
			Question:    market.Question,
			Probability: probability,
			Signal:      signal,
			Strength:    signalStrength,
			Confidence:  confidence,
			Reasoning:   reasoning,
			Relevance:   relevance,
		})
	}
	
	return signals
}

// aggregateSignals 聚合信号
func (psa *PolymarketSignalAnalyzer) aggregateSignals(signals []*PolymarketSignal) (string, float64, float64, string) {
	if len(signals) == 0 {
		return "hold", 0.0, 0.0, "无有效信号"
	}
	
	// 按相关性加权
	buyWeight := 0.0
	sellWeight := 0.0
	totalWeight := 0.0
	
	var reasons []string
	
	for _, signal := range signals {
		// 相关性权重
		relevanceWeight := 1.0
		if signal.Relevance == "high" {
			relevanceWeight = 1.5
		} else if signal.Relevance == "low" {
			relevanceWeight = 0.5
		}
		
		// 综合权重 = 信号强度 × 置信度 × 相关性权重
		weight := signal.Strength * signal.Confidence * relevanceWeight
		
		if signal.Signal == "buy" {
			buyWeight += weight
		} else if signal.Signal == "sell" {
			sellWeight += weight
		}
		
		totalWeight += weight
		
		if signal.Relevance == "high" {
			reasons = append(reasons, fmt.Sprintf("%s (概率: %.1f%%, 强度: %.2f)", 
				signal.Question, signal.Probability*100, signal.Strength))
		}
	}
	
	if totalWeight == 0 {
		return "hold", 0.0, 0.0, "权重计算失败"
	}
	
	// 归一化权重
	buyRatio := buyWeight / totalWeight
	sellRatio := sellWeight / totalWeight
	
	// 计算聚合信号
	var aggregateSignal string
	var aggregateStrength float64
	
	if buyRatio > 0.6 {
		aggregateSignal = "buy"
		aggregateStrength = buyRatio
	} else if sellRatio > 0.6 {
		aggregateSignal = "sell"
		aggregateStrength = sellRatio
	} else {
		aggregateSignal = "hold"
		aggregateStrength = 1.0 - abs(buyRatio - sellRatio)
	}
	
	// 计算平均置信度
	avgConfidence := 0.0
	for _, signal := range signals {
		avgConfidence += signal.Confidence
	}
	avgConfidence /= float64(len(signals))
	
	reasoning := fmt.Sprintf("聚合 %d 个信号: 买入权重 %.1f%%, 卖出权重 %.1f%%", 
		len(signals), buyRatio*100, sellRatio*100)
	if len(reasons) > 0 {
		maxReasons := len(reasons)
		if maxReasons > 3 {
			maxReasons = 3
		}
		reasoning += fmt.Sprintf("; 关键市场: %s", strings.Join(reasons[:maxReasons], "; "))
	}
	
	return aggregateSignal, aggregateStrength, avgConfidence, reasoning
}

// determineRelevance 判断相关性
func (psa *PolymarketSignalAnalyzer) determineRelevance(question, description string) string {
	text := strings.ToLower(question + " " + description)
	
	// 高相关性关键词
	highRelevanceKeywords := []string{"bitcoin", "btc", "ethereum", "eth", "crypto", "cryptocurrency", 
		"regulation", "sec", "cftc", "ban", "approve", "etf", "halving"}
	
	// 中相关性关键词
	mediumRelevanceKeywords := []string{"stock", "market", "economy", "inflation", "fed", "interest rate"}
	
	highCount := 0
	mediumCount := 0
	
	for _, keyword := range highRelevanceKeywords {
		if strings.Contains(text, keyword) {
			highCount++
		}
	}
	
	for _, keyword := range mediumRelevanceKeywords {
		if strings.Contains(text, keyword) {
			mediumCount++
		}
	}
	
	if highCount >= 2 {
		return "high"
	} else if highCount >= 1 || mediumCount >= 2 {
		return "medium"
	}
	
	return "low"
}

// GetLastAnalysis 获取最后一次分析结果
func (psa *PolymarketSignalAnalyzer) GetLastAnalysis() *PolymarketSignalAnalysisResponse {
	psa.mu.RLock()
	defer psa.mu.RUnlock()
	return psa.lastAnalysis
}

// GetLastAnalysisTime 获取最后一次分析时间
func (psa *PolymarketSignalAnalyzer) GetLastAnalysisTime() time.Time {
	psa.mu.RLock()
	defer psa.mu.RUnlock()
	return psa.lastAnalysisTime
}

// 辅助函数
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

