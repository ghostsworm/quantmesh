package strategy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/logger"
	"quantmesh/position"
)

// ComboStrategy 组合策略
// 特点：
// 1. 多空對冲：同時运行多头和空头策略
// 2. 市况自适应：根據市场状態自动切换策略权重
// 3. 策略组合：支援任意策略组合（如马丁+DCA）
// 4. 全時况覆盖：上涨、下跌、震荡行情均可盈利
type ComboStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg *ComboConfig

	// 子策略
	strategies    []Strategy
	strategyNames []string
	weights       []float64 // 各策略权重

	// 市场状態检测
	marketState   MarketState
	priceHistory  []float64
	candles       []indicators.Candle
	lastPrice     float64
	
	// 状態
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
	isPaused  bool // 暂停標志

	// 统计
	stats *StrategyStatistics

	// 事件總線
	eventBus EventBus
}

// MarketState 市场状態
type MarketState string

const (
	MarketBullish   MarketState = "bullish"   // 牛市（上涨趋势）
	MarketBearish   MarketState = "bearish"   // 熊市（下跌趋势）
	MarketSideways  MarketState = "sideways"  // 震荡市
	MarketVolatile  MarketState = "volatile"  // 高波动
)

// ComboConfig 组合策略配置
type ComboConfig struct {
	// 基础配置
	Symbol      string            `yaml:"symbol"`
	Strategies  []StrategyConfig  `yaml:"strategies"`  // 子策略配置
	
	// 市况检测
	MarketDetection    bool    `yaml:"market_detection"`     // 啟用市况检测
	TrendPeriod        int     `yaml:"trend_period"`         // 趋势周期
	VolatilityPeriod   int     `yaml:"volatility_period"`    // 波动率周期
	VolatilityThreshold float64 `yaml:"volatility_threshold"` // 高波动阈值
	
	// 权重調整
	AdaptiveWeights    bool    `yaml:"adaptive_weights"`     // 自适应权重
	RebalanceInterval  int     `yaml:"rebalance_interval"`   // 再平衡间隔（秒）
	
	// 對冲設置
	HedgeEnabled       bool    `yaml:"hedge_enabled"`        // 啟用對冲
	HedgeRatio         float64 `yaml:"hedge_ratio"`          // 對冲比例 (0.0-1.0)
	MaxDrawdown        float64 `yaml:"max_drawdown"`         // 最大回撤触发對冲
	
	// 风控
	TotalCapital       float64 `yaml:"total_capital"`        // 總资金
	MaxExposure        float64 `yaml:"max_exposure"`         // 最大敞口比例
}

// StrategyConfig 子策略配置
type StrategyConfig struct {
	Name       string                 `yaml:"name"`
	Type       string                 `yaml:"type"`       // dca/martingale/grid/trend
	Weight     float64                `yaml:"weight"`     // 权重
	Direction  string                 `yaml:"direction"`  // LONG/SHORT/BOTH
	Parameters map[string]interface{} `yaml:"parameters"`
	
	// 市况适配
	PreferredMarket []MarketState `yaml:"preferred_market"` // 适合的市况
}

// NewComboStrategy 創建组合策略
func NewComboStrategy(
	name string,
	symbol string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *ComboStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	comboCfg := parseComboConfig(strategyCfg)
	if symbol != "" {
		comboCfg.Symbol = symbol
	}

	combo := &ComboStrategy{
		name:          name,
		cfg:           cfg,
		executor:      executor,
		exchange:      exchange,
		strategyCfg:   comboCfg,
		strategies:    make([]Strategy, 0),
		strategyNames: make([]string, 0),
		weights:       make([]float64, 0),
		priceHistory:  make([]float64, 0, 200),
		candles:       make([]indicators.Candle, 0, 200),
		marketState:   MarketSideways,
		ctx:           ctx,
		cancel:        cancel,
		stats: &StrategyStatistics{
			TotalTrades: 0,
			WinRate:     0,
			TotalPnL:    0,
			TotalVolume: 0,
		},
	}

	// 創建子策略
	combo.initializeStrategies()

	return combo
}

// parseComboConfig 解析组合配置
func parseComboConfig(cfg map[string]interface{}) *ComboConfig {
	comboCfg := &ComboConfig{
		Symbol:              "BTCUSDT",
		MarketDetection:     true,
		TrendPeriod:         20,
		VolatilityPeriod:    14,
		VolatilityThreshold: 3.0,
		AdaptiveWeights:     true,
		RebalanceInterval:   3600,
		HedgeEnabled:        true,
		HedgeRatio:          0.3,
		MaxDrawdown:         5.0,
		TotalCapital:        10000,
		MaxExposure:         0.8,
		Strategies:          make([]StrategyConfig, 0),
	}

	if cfg == nil {
		return comboCfg
	}

	// 辅助函數：安全地從 map 中獲取 float64
	getFloat := func(key string, defaultValue float64) float64 {
		if v, ok := cfg[key]; ok {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			case int64:
				return float64(val)
			}
		}
		return defaultValue
	}

	// 辅助函數：安全地從 map 中獲取 int
	getInt := func(key string, defaultValue int) int {
		if v, ok := cfg[key]; ok {
			switch val := v.(type) {
			case int:
				return val
			case float64:
				return int(val)
			case int64:
				return int(val)
			}
		}
		return defaultValue
	}

	if v, ok := cfg["symbol"].(string); ok {
		comboCfg.Symbol = v
	}

	comboCfg.MarketDetection = getBoolParam(cfg, "market_detection", comboCfg.MarketDetection)
	comboCfg.TrendPeriod = getInt("trend_period", comboCfg.TrendPeriod)
	comboCfg.VolatilityPeriod = getInt("volatility_period", comboCfg.VolatilityPeriod)
	comboCfg.VolatilityThreshold = getFloat("volatility_threshold", comboCfg.VolatilityThreshold)
	comboCfg.AdaptiveWeights = getBoolParam(cfg, "adaptive_weights", comboCfg.AdaptiveWeights)
	comboCfg.RebalanceInterval = getInt("rebalance_interval", comboCfg.RebalanceInterval)
	comboCfg.HedgeEnabled = getBoolParam(cfg, "hedge_enabled", comboCfg.HedgeEnabled)
	comboCfg.HedgeRatio = getFloat("hedge_ratio", comboCfg.HedgeRatio)
	comboCfg.MaxDrawdown = getFloat("max_drawdown", comboCfg.MaxDrawdown)
	comboCfg.TotalCapital = getFloat("total_capital", comboCfg.TotalCapital)
	comboCfg.MaxExposure = getFloat("max_exposure", comboCfg.MaxExposure)

	// 解析子策略配置
	if strategies, ok := cfg["strategies"].([]interface{}); ok {
		for _, s := range strategies {
			if stratMap, ok := s.(map[string]interface{}); ok {
				stratCfg := StrategyConfig{
					Name:       getStringParam(stratMap, "name", ""),
					Type:       getStringParam(stratMap, "type", "dca"),
					Weight:     getFloatParamCombo(stratMap, "weight", 1.0),
					Direction:  getStringParam(stratMap, "direction", "LONG"),
					Parameters: make(map[string]interface{}),
				}
				if params, ok := stratMap["parameters"].(map[string]interface{}); ok {
					stratCfg.Parameters = params
				}
				if preferred, ok := stratMap["preferred_market"].([]interface{}); ok {
					for _, p := range preferred {
						if ps, ok := p.(string); ok {
							stratCfg.PreferredMarket = append(stratCfg.PreferredMarket, MarketState(ps))
						}
					}
				}
				comboCfg.Strategies = append(comboCfg.Strategies, stratCfg)
			}
		}
	}

	// 如果沒有配置策略，新增預設組合
	if len(comboCfg.Strategies) == 0 {
		comboCfg.Strategies = []StrategyConfig{
			{
				Name:       "long_dca",
				Type:       "dca",
				Weight:     0.5,
				Direction:  "LONG",
				Parameters: map[string]interface{}{"base_order_amount": 100.0},
				PreferredMarket: []MarketState{MarketBullish, MarketSideways},
			},
			{
				Name:       "short_martingale",
				Type:       "martingale",
				Weight:     0.3,
				Direction:  "SHORT",
				Parameters: map[string]interface{}{"initial_amount": 50.0, "direction": "SHORT"},
				PreferredMarket: []MarketState{MarketBearish},
			},
			{
				Name:       "hedge_martingale",
				Type:       "martingale",
				Weight:     0.2,
				Direction:  "LONG",
				Parameters: map[string]interface{}{"initial_amount": 30.0, "reverse_martingale": true},
				PreferredMarket: []MarketState{MarketBullish, MarketVolatile},
			},
		}
	}

	return comboCfg
}

func getStringParam(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

func getBoolParam(m map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return defaultVal
}

func getFloatParamCombo(m map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	if v, ok := m[key].(int); ok {
		return float64(v)
	}
	return defaultVal
}

// initializeStrategies 初始化子策略
func (s *ComboStrategy) initializeStrategies() {
	for _, stratCfg := range s.strategyCfg.Strategies {
		var strategy Strategy

		// 添加符号到参數
		stratCfg.Parameters["symbol"] = s.strategyCfg.Symbol

		switch stratCfg.Type {
		case "dca":
			strategy = NewDCAEnhancedStrategy(
				stratCfg.Name,
				s.strategyCfg.Symbol,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		case "martingale":
			stratCfg.Parameters["direction"] = stratCfg.Direction
			strategy = NewMartingaleStrategy(
				stratCfg.Name,
				s.strategyCfg.Symbol,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		case "trend":
			strategy = NewTrendFollowingStrategy(
				stratCfg.Name,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		case "mean_reversion":
			strategy = NewMeanReversionStrategy(
				stratCfg.Name,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		default:
			logger.Warn("⚠️ [%s] 未知策略類型: %s", s.name, stratCfg.Type)
			continue
		}

		if strategy != nil {
			s.strategies = append(s.strategies, strategy)
			s.strategyNames = append(s.strategyNames, stratCfg.Name)
			s.weights = append(s.weights, stratCfg.Weight)
		}
	}
}

// Name 回傳策略名稱
func (s *ComboStrategy) Name() string {
	return s.name
}

// Initialize 初始化策略
func (s *ComboStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	s.cfg = cfg
	s.executor = executor
	s.exchange = exchange
	return nil
}

// SetEventBus 設置事件總線
func (s *ComboStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus

	// 同步给所有子策略
	for _, strategy := range s.strategies {
		strategy.SetEventBus(bus)
	}
}

// Start 啟动策略
func (s *ComboStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	s.ctx = ctx
	s.isRunning = true
	s.mu.Unlock()

	// 啟动所有子策略
	for i, strategy := range s.strategies {
		if err := strategy.Start(ctx); err != nil {
			logger.Error("❌ [%s] 子策略 %s 啟动失败: %v", s.name, s.strategyNames[i], err)
		}
	}

	// 啟动市况检测循环
	if s.strategyCfg.MarketDetection {
		go s.marketDetectionLoop()
	}

	// 啟动权重再平衡循环
	if s.strategyCfg.AdaptiveWeights {
		go s.rebalanceLoop()
	}

	logger.Info("✅ [%s] 组合策略已啟动，子策略數量: %d", s.name, len(s.strategies))
	for i, name := range s.strategyNames {
		logger.Info("   - %s (权重: %.2f)", name, s.weights[i])
	}

	return nil
}

// Stop 停止策略
func (s *ComboStrategy) Stop() error {
	s.mu.Lock()
	s.isRunning = false
	s.mu.Unlock()

	// 停止所有子策略
	for i, strategy := range s.strategies {
		if err := strategy.Stop(); err != nil {
			logger.Error("❌ [%s] 子策略 %s 停止失败: %v", s.name, s.strategyNames[i], err)
		}
	}

	if s.cancel != nil {
		s.cancel()
	}

	logger.Info("⏹️ [%s] 组合策略已停止", s.name)
	return nil
}

// OnPriceChange 價格變化处理
func (s *ComboStrategy) OnPriceChange(price float64) error {
	s.mu.Lock()
	
	if s.isPaused {
		s.mu.Unlock()
		return nil
	}

	// 更新價格历史
	s.priceHistory = append(s.priceHistory, price)
	if len(s.priceHistory) > 200 {
		// 使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, 200)
		copy(newHistory, s.priceHistory[len(s.priceHistory)-200:])
		s.priceHistory = newHistory
	}
	s.lastPrice = price

	// 更新 K線
	s.updateCandle(price)
	
	s.mu.Unlock()

	// 傳遞给所有子策略
	for i, strategy := range s.strategies {
		// 根據权重和市况决定是否執行
		if s.shouldExecuteStrategy(i) {
			if err := strategy.OnPriceChange(price); err != nil {
				logger.Warn("⚠️ [%s] 子策略 %s 处理價格變化失败: %v",
					s.name, s.strategyNames[i], err)
			}
		}
	}

	return nil
}

// updateCandle 更新 K線
func (s *ComboStrategy) updateCandle(price float64) {
	now := time.Now().Unix()

	if len(s.candles) == 0 {
		s.candles = append(s.candles, indicators.Candle{
			Time:   now,
			Open:   price,
			High:   price,
			Low:    price,
			Close:  price,
			Volume: 1,
		})
		return
	}

	last := &s.candles[len(s.candles)-1]
	if now-last.Time >= 60 {
		s.candles = append(s.candles, indicators.Candle{
			Time:   now,
			Open:   price,
			High:   price,
			Low:    price,
			Close:  price,
			Volume: 1,
		})
		if len(s.candles) > 200 {
			// 使用 copy 而不是切片截取，避免記憶體泄漏
			newCandles := make([]indicators.Candle, 200)
			copy(newCandles, s.candles[len(s.candles)-200:])
			s.candles = newCandles
		}
	} else {
		last.Close = price
		if price > last.High {
			last.High = price
		}
		if price < last.Low {
			last.Low = price
		}
		last.Volume++
	}
}

// shouldExecuteStrategy 判断是否应該執行策略
func (s *ComboStrategy) shouldExecuteStrategy(index int) bool {
	if index >= len(s.strategyCfg.Strategies) {
		return true
	}

	stratCfg := s.strategyCfg.Strategies[index]
	
	// 如果没有指定首选市况，總是執行
	if len(stratCfg.PreferredMarket) == 0 {
		return true
	}

	// 检查當前市况是否匹配
	s.mu.RLock()
	currentMarket := s.marketState
	s.mu.RUnlock()

	for _, preferred := range stratCfg.PreferredMarket {
		if preferred == currentMarket {
			return true
		}
	}

	return false
}

// marketDetectionLoop 市况检测循环
func (s *ComboStrategy) marketDetectionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.detectMarketState()
		}
	}
}

// detectMarketState 检测市场状態
func (s *ComboStrategy) detectMarketState() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.priceHistory) < s.strategyCfg.TrendPeriod*2 {
		return
	}

	prices := s.priceHistory

	// 计算趋势
	shortPeriod := s.strategyCfg.TrendPeriod
	longPeriod := s.strategyCfg.TrendPeriod * 2

	shortMA := indicators.SMA(prices, shortPeriod)
	longMA := indicators.SMA(prices, longPeriod)

	if shortMA == nil || longMA == nil || len(shortMA) == 0 || len(longMA) == 0 {
		return
	}

	shortValue := shortMA[len(shortMA)-1]
	longValue := longMA[len(longMA)-1]

	// 计算波动率
	var volatility float64
	if len(s.candles) > s.strategyCfg.VolatilityPeriod {
		atr := indicators.NewATR(s.strategyCfg.VolatilityPeriod)
		atrValue := atr.CurrentATR(s.candles)
		if s.lastPrice > 0 {
			volatility = atrValue / s.lastPrice * 100
		}
	}

	// 判断市场状態
	previousState := s.marketState

	if volatility > s.strategyCfg.VolatilityThreshold {
		s.marketState = MarketVolatile
	} else if shortValue > longValue*1.02 { // 上涨趋势
		s.marketState = MarketBullish
	} else if shortValue < longValue*0.98 { // 下跌趋势
		s.marketState = MarketBearish
	} else {
		s.marketState = MarketSideways
	}

	if s.marketState != previousState {
		logger.Info("📊 [%s] 市场状態变化: %s -> %s (波动率: %.2f%%)",
			s.name, previousState, s.marketState, volatility)
	}
}

// rebalanceLoop 权重再平衡循环
func (s *ComboStrategy) rebalanceLoop() {
	interval := time.Duration(s.strategyCfg.RebalanceInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.rebalanceWeights()
		}
	}
}

// rebalanceWeights 根據市况調整权重
func (s *ComboStrategy) rebalanceWeights() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.strategies) == 0 {
		return
	}

	// 根據當前市况調整权重
	for i, stratCfg := range s.strategyCfg.Strategies {
		if i >= len(s.weights) {
			break
		}

		baseWeight := stratCfg.Weight
		adjustedWeight := baseWeight

		// 检查策略是否适合當前市况
		isPreferred := false
		for _, preferred := range stratCfg.PreferredMarket {
			if preferred == s.marketState {
				isPreferred = true
				break
			}
		}

		if isPreferred {
			adjustedWeight = baseWeight * 1.5 // 增加权重
		} else if len(stratCfg.PreferredMarket) > 0 {
			adjustedWeight = baseWeight * 0.5 // 减少权重
		}

		// 限制权重範圍
		if adjustedWeight > 1.0 {
			adjustedWeight = 1.0
		}
		if adjustedWeight < 0.1 {
			adjustedWeight = 0.1
		}

		if s.weights[i] != adjustedWeight {
			logger.Info("⚖️ [%s] 調整策略 %s 权重: %.2f -> %.2f (市况: %s)",
				s.name, s.strategyNames[i], s.weights[i], adjustedWeight, s.marketState)
			s.weights[i] = adjustedWeight
		}
	}
}

// OnOrderUpdate 订單更新处理
func (s *ComboStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// 傳遞给所有子策略
	for _, strategy := range s.strategies {
		if err := strategy.OnOrderUpdate(update); err != nil {
			logger.Warn("⚠️ [%s] 子策略处理订單更新失败: %v", s.name, err)
		}
	}
	return nil
}

// GetPositions 獲取所有持倉
func (s *ComboStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()

	positions := make([]*Position, 0)
	for _, strategy := range s.strategies {
		positions = append(positions, strategy.GetPositions()...)
	}
	return positions
}

// GetOrders 獲取所有订單
func (s *ComboStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orders := make([]*Order, 0)
	for _, strategy := range s.strategies {
		orders = append(orders, strategy.GetOrders()...)
	}
	return orders
}

// GetStatistics 獲取统计
func (s *ComboStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 彙總所有子策略统计
	totalStats := &StrategyStatistics{}
	for _, strategy := range s.strategies {
		subStats := strategy.GetStatistics()
		totalStats.TotalTrades += subStats.TotalTrades
		totalStats.TotalPnL += subStats.TotalPnL
		totalStats.TotalVolume += subStats.TotalVolume
	}

	if totalStats.TotalTrades > 0 {
		// 计算總胜率（加权平均）
		totalWins := 0.0
		for _, strategy := range s.strategies {
			subStats := strategy.GetStatistics()
			totalWins += subStats.WinRate * float64(subStats.TotalTrades)
		}
		totalStats.WinRate = totalWins / float64(totalStats.TotalTrades)
	}

	return totalStats
}

// GetMarketState 獲取當前市场状態
func (s *ComboStrategy) GetMarketState() MarketState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.marketState
}

// GetStrategyWeights 獲取策略权重
func (s *ComboStrategy) GetStrategyWeights() map[string]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	weights := make(map[string]float64)
	for i, name := range s.strategyNames {
		if i < len(s.weights) {
			weights[name] = s.weights[i]
		}
	}
	return weights
}

// GetInfo 獲取组合策略信息
func (s *ComboStrategy) GetInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("市场状態: %s, 子策略數: %d, 總持倉: %d",
		s.marketState, len(s.strategies), len(s.GetPositions()))
}
