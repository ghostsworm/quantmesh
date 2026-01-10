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
// 1. 多空对冲：同时运行多头和空头策略
// 2. 市况自适应：根据市场状态自动切换策略权重
// 3. 策略组合：支持任意策略组合（如马丁+DCA）
// 4. 全时况覆盖：上涨、下跌、震荡行情均可盈利
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

	// 市场状态检测
	marketState   MarketState
	priceHistory  []float64
	candles       []indicators.Candle
	lastPrice     float64
	
	// 状态
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool

	// 统计
	stats *StrategyStatistics
}

// MarketState 市场状态
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
	MarketDetection    bool    `yaml:"market_detection"`     // 启用市况检测
	TrendPeriod        int     `yaml:"trend_period"`         // 趋势周期
	VolatilityPeriod   int     `yaml:"volatility_period"`    // 波动率周期
	VolatilityThreshold float64 `yaml:"volatility_threshold"` // 高波动阈值
	
	// 权重调整
	AdaptiveWeights    bool    `yaml:"adaptive_weights"`     // 自适应权重
	RebalanceInterval  int     `yaml:"rebalance_interval"`   // 再平衡间隔（秒）
	
	// 对冲设置
	HedgeEnabled       bool    `yaml:"hedge_enabled"`        // 启用对冲
	HedgeRatio         float64 `yaml:"hedge_ratio"`          // 对冲比例 (0.0-1.0)
	MaxDrawdown        float64 `yaml:"max_drawdown"`         // 最大回撤触发对冲
	
	// 风控
	TotalCapital       float64 `yaml:"total_capital"`        // 总资金
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

// NewComboStrategy 创建组合策略
func NewComboStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *ComboStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	comboCfg := parseComboConfig(strategyCfg)

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

	// 创建子策略
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

	if v, ok := cfg["symbol"].(string); ok {
		comboCfg.Symbol = v
	}
	if v, ok := cfg["market_detection"].(bool); ok {
		comboCfg.MarketDetection = v
	}
	if v, ok := cfg["trend_period"].(int); ok {
		comboCfg.TrendPeriod = v
	}
	if v, ok := cfg["trend_period"].(float64); ok {
		comboCfg.TrendPeriod = int(v)
	}
	if v, ok := cfg["volatility_period"].(int); ok {
		comboCfg.VolatilityPeriod = v
	}
	if v, ok := cfg["volatility_period"].(float64); ok {
		comboCfg.VolatilityPeriod = int(v)
	}
	if v, ok := cfg["volatility_threshold"].(float64); ok {
		comboCfg.VolatilityThreshold = v
	}
	if v, ok := cfg["adaptive_weights"].(bool); ok {
		comboCfg.AdaptiveWeights = v
	}
	if v, ok := cfg["rebalance_interval"].(int); ok {
		comboCfg.RebalanceInterval = v
	}
	if v, ok := cfg["rebalance_interval"].(float64); ok {
		comboCfg.RebalanceInterval = int(v)
	}
	if v, ok := cfg["hedge_enabled"].(bool); ok {
		comboCfg.HedgeEnabled = v
	}
	if v, ok := cfg["hedge_ratio"].(float64); ok {
		comboCfg.HedgeRatio = v
	}
	if v, ok := cfg["max_drawdown"].(float64); ok {
		comboCfg.MaxDrawdown = v
	}
	if v, ok := cfg["total_capital"].(float64); ok {
		comboCfg.TotalCapital = v
	}
	if v, ok := cfg["max_exposure"].(float64); ok {
		comboCfg.MaxExposure = v
	}

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

	// 如果没有配置策略，添加默认组合
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

		// 添加符号到参数
		stratCfg.Parameters["symbol"] = s.strategyCfg.Symbol

		switch stratCfg.Type {
		case "dca":
			strategy = NewDCAEnhancedStrategy(
				stratCfg.Name,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		case "martingale":
			stratCfg.Parameters["direction"] = stratCfg.Direction
			strategy = NewMartingaleStrategy(
				stratCfg.Name,
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
			logger.Warn("⚠️ [%s] 未知策略类型: %s", s.name, stratCfg.Type)
			continue
		}

		if strategy != nil {
			s.strategies = append(s.strategies, strategy)
			s.strategyNames = append(s.strategyNames, stratCfg.Name)
			s.weights = append(s.weights, stratCfg.Weight)
		}
	}
}

// Name 返回策略名称
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

// Start 启动策略
func (s *ComboStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	s.ctx = ctx
	s.isRunning = true
	s.mu.Unlock()

	// 启动所有子策略
	for i, strategy := range s.strategies {
		if err := strategy.Start(ctx); err != nil {
			logger.Error("❌ [%s] 子策略 %s 启动失败: %v", s.name, s.strategyNames[i], err)
		}
	}

	// 启动市况检测循环
	if s.strategyCfg.MarketDetection {
		go s.marketDetectionLoop()
	}

	// 启动权重再平衡循环
	if s.strategyCfg.AdaptiveWeights {
		go s.rebalanceLoop()
	}

	logger.Info("✅ [%s] 组合策略已启动，子策略数量: %d", s.name, len(s.strategies))
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

// OnPriceChange 价格变化处理
func (s *ComboStrategy) OnPriceChange(price float64) error {
	s.mu.Lock()
	
	// 更新价格历史
	s.priceHistory = append(s.priceHistory, price)
	if len(s.priceHistory) > 200 {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-200:]
	}
	s.lastPrice = price

	// 更新 K线
	s.updateCandle(price)
	
	s.mu.Unlock()

	// 传递给所有子策略
	for i, strategy := range s.strategies {
		// 根据权重和市况决定是否执行
		if s.shouldExecuteStrategy(i) {
			if err := strategy.OnPriceChange(price); err != nil {
				logger.Warn("⚠️ [%s] 子策略 %s 处理价格变化失败: %v",
					s.name, s.strategyNames[i], err)
			}
		}
	}

	return nil
}

// updateCandle 更新 K线
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
			s.candles = s.candles[len(s.candles)-200:]
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

// shouldExecuteStrategy 判断是否应该执行策略
func (s *ComboStrategy) shouldExecuteStrategy(index int) bool {
	if index >= len(s.strategyCfg.Strategies) {
		return true
	}

	stratCfg := s.strategyCfg.Strategies[index]
	
	// 如果没有指定首选市况，总是执行
	if len(stratCfg.PreferredMarket) == 0 {
		return true
	}

	// 检查当前市况是否匹配
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

// detectMarketState 检测市场状态
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

	// 判断市场状态
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
		logger.Info("📊 [%s] 市场状态变化: %s -> %s (波动率: %.2f%%)",
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

// rebalanceWeights 根据市况调整权重
func (s *ComboStrategy) rebalanceWeights() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.strategies) == 0 {
		return
	}

	// 根据当前市况调整权重
	for i, stratCfg := range s.strategyCfg.Strategies {
		if i >= len(s.weights) {
			break
		}

		baseWeight := stratCfg.Weight
		adjustedWeight := baseWeight

		// 检查策略是否适合当前市况
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

		// 限制权重范围
		if adjustedWeight > 1.0 {
			adjustedWeight = 1.0
		}
		if adjustedWeight < 0.1 {
			adjustedWeight = 0.1
		}

		if s.weights[i] != adjustedWeight {
			logger.Info("⚖️ [%s] 调整策略 %s 权重: %.2f -> %.2f (市况: %s)",
				s.name, s.strategyNames[i], s.weights[i], adjustedWeight, s.marketState)
			s.weights[i] = adjustedWeight
		}
	}
}

// OnOrderUpdate 订单更新处理
func (s *ComboStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// 传递给所有子策略
	for _, strategy := range s.strategies {
		if err := strategy.OnOrderUpdate(update); err != nil {
			logger.Warn("⚠️ [%s] 子策略处理订单更新失败: %v", s.name, err)
		}
	}
	return nil
}

// GetPositions 获取所有持仓
func (s *ComboStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()

	positions := make([]*Position, 0)
	for _, strategy := range s.strategies {
		positions = append(positions, strategy.GetPositions()...)
	}
	return positions
}

// GetOrders 获取所有订单
func (s *ComboStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orders := make([]*Order, 0)
	for _, strategy := range s.strategies {
		orders = append(orders, strategy.GetOrders()...)
	}
	return orders
}

// GetStatistics 获取统计
func (s *ComboStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 汇总所有子策略统计
	totalStats := &StrategyStatistics{}
	for _, strategy := range s.strategies {
		subStats := strategy.GetStatistics()
		totalStats.TotalTrades += subStats.TotalTrades
		totalStats.TotalPnL += subStats.TotalPnL
		totalStats.TotalVolume += subStats.TotalVolume
	}

	if totalStats.TotalTrades > 0 {
		// 计算总胜率（加权平均）
		totalWins := 0.0
		for _, strategy := range s.strategies {
			subStats := strategy.GetStatistics()
			totalWins += subStats.WinRate * float64(subStats.TotalTrades)
		}
		totalStats.WinRate = totalWins / float64(totalStats.TotalTrades)
	}

	return totalStats
}

// GetMarketState 获取当前市场状态
func (s *ComboStrategy) GetMarketState() MarketState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.marketState
}

// GetStrategyWeights 获取策略权重
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

// GetInfo 获取组合策略信息
func (s *ComboStrategy) GetInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("市场状态: %s, 子策略数: %d, 总持仓: %d",
		s.marketState, len(s.strategies), len(s.GetPositions()))
}
