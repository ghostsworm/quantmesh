package strategy

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/logger"
	"quantmesh/position"
)

// MartingaleStrategy 马丁格尔策略
// 特点：
// 1. 加倍加仓机制：亏损时按倍数增加仓位
// 2. 风险递减控制：随着层数增加，递减加仓倍数
// 3. 最大层数限制：防止无限加仓
// 4. 反向马丁选项：盈利时加仓（适合趋势市）
type MartingaleStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg *MartingaleConfig

	// 价格数据
	priceHistory []float64
	candles      []indicators.Candle
	lastPrice    float64
	mu           sync.RWMutex

	// 仓位管理
	entries      []*MartingaleEntry // 入场记录
	totalCost    float64            // 总成本
	totalQty     float64            // 总持仓量
	avgEntryPrice float64           // 平均入场价
	
	// 方向
	direction    string // LONG/SHORT
	currentLevel int    // 当前马丁层级

	// 状态
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool

	// 统计
	stats *StrategyStatistics
}

// MartingaleConfig 马丁格尔配置
type MartingaleConfig struct {
	// 基础配置
	Symbol           string  `yaml:"symbol"`
	Direction        string  `yaml:"direction"`          // LONG/SHORT/BOTH
	InitialAmount    float64 `yaml:"initial_amount"`     // 初始金额 (USDT)
	
	// 马丁参数
	Multiplier       float64 `yaml:"multiplier"`         // 加仓倍数 (默认2.0)
	MaxLevels        int     `yaml:"max_levels"`         // 最大层数 (默认6)
	PriceStep        float64 `yaml:"price_step"`         // 加仓间距 (%)
	
	// 风险递减
	RiskDecay        bool    `yaml:"risk_decay"`         // 启用风险递减
	DecayFactor      float64 `yaml:"decay_factor"`       // 递减因子 (0.8-0.95)
	MinMultiplier    float64 `yaml:"min_multiplier"`     // 最小倍数 (1.0)
	
	// 止盈止损
	TakeProfit       float64 `yaml:"take_profit"`        // 止盈比例 (%)
	StopLoss         float64 `yaml:"stop_loss"`          // 止损比例 (%)
	TrailingStop     float64 `yaml:"trailing_stop"`      // 追踪止损 (%)
	
	// 反向马丁
	ReverseMartingale bool   `yaml:"reverse_martingale"` // 反向马丁（盈利时加仓）
	ReverseMultiplier float64 `yaml:"reverse_multiplier"` // 反向倍数
	
	// 冷却期
	CooldownEnabled  bool    `yaml:"cooldown_enabled"`   // 启用冷却期
	CooldownSeconds  int     `yaml:"cooldown_seconds"`   // 冷却时间（秒）
	
	// 趋势过滤
	TrendFilter      bool    `yaml:"trend_filter"`       // 启用趋势过滤
	TrendPeriod      int     `yaml:"trend_period"`       // 趋势周期
}

// MartingaleEntry 马丁入场记录
type MartingaleEntry struct {
	Level     int       // 层级
	Price     float64   // 入场价格
	Quantity  float64   // 数量
	Cost      float64   // 成本
	OrderID   int64     // 订单ID
	Status    string    // pending/filled/closed
	Timestamp time.Time // 时间戳
}

// NewMartingaleStrategy 创建马丁格尔策略
func NewMartingaleStrategy(
	name string,
	symbol string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *MartingaleStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	martinCfg := parseMartingaleConfig(strategyCfg)
	if symbol != "" {
		martinCfg.Symbol = symbol
	}

	strategy := &MartingaleStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  martinCfg,
		priceHistory: make([]float64, 0, 200),
		candles:      make([]indicators.Candle, 0, 200),
		entries:      make([]*MartingaleEntry, 0, martinCfg.MaxLevels),
		direction:    martinCfg.Direction,
		ctx:          ctx,
		cancel:       cancel,
		stats: &StrategyStatistics{
			TotalTrades: 0,
			WinRate:     0,
			TotalPnL:    0,
			TotalVolume: 0,
		},
	}

	return strategy
}

// parseMartingaleConfig 解析马丁配置
func parseMartingaleConfig(cfg map[string]interface{}) *MartingaleConfig {
	martinCfg := &MartingaleConfig{
		// 默认值
		Symbol:            "BTCUSDT",
		Direction:         "LONG",
		InitialAmount:     100,
		Multiplier:        2.0,
		MaxLevels:         6,
		PriceStep:         2.0,
		RiskDecay:         true,
		DecayFactor:       0.9,
		MinMultiplier:     1.2,
		TakeProfit:        3.0,
		StopLoss:          15.0,
		TrailingStop:      1.0,
		ReverseMartingale: false,
		ReverseMultiplier: 1.5,
		CooldownEnabled:   true,
		CooldownSeconds:   60,
		TrendFilter:       true,
		TrendPeriod:       20,
	}

	if cfg == nil {
		return martinCfg
	}

	// 辅助函数：安全地从 map 中获取 float64
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

	// 辅助函数：安全地从 map 中获取 int
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

	// 从 map 中读取配置
	if v, ok := cfg["symbol"].(string); ok {
		martinCfg.Symbol = v
	}
	if v, ok := cfg["direction"].(string); ok {
		martinCfg.Direction = v
	}

	martinCfg.InitialAmount = getFloat("initial_amount", martinCfg.InitialAmount)
	martinCfg.Multiplier = getFloat("multiplier", martinCfg.Multiplier)
	martinCfg.MaxLevels = getInt("max_levels", martinCfg.MaxLevels)
	martinCfg.PriceStep = getFloat("price_step", martinCfg.PriceStep)

	if v, ok := cfg["risk_decay"].(bool); ok {
		martinCfg.RiskDecay = v
	}
	martinCfg.DecayFactor = getFloat("decay_factor", martinCfg.DecayFactor)
	martinCfg.MinMultiplier = getFloat("min_multiplier", martinCfg.MinMultiplier)
	martinCfg.TakeProfit = getFloat("take_profit", martinCfg.TakeProfit)
	martinCfg.StopLoss = getFloat("stop_loss", martinCfg.StopLoss)
	martinCfg.TrailingStop = getFloat("trailing_stop", martinCfg.TrailingStop)

	if v, ok := cfg["reverse_martingale"].(bool); ok {
		martinCfg.ReverseMartingale = v
	}
	martinCfg.ReverseMultiplier = getFloat("reverse_multiplier", martinCfg.ReverseMultiplier)

	if v, ok := cfg["cooldown_enabled"].(bool); ok {
		martinCfg.CooldownEnabled = v
	}
	martinCfg.CooldownSeconds = getInt("cooldown_seconds", martinCfg.CooldownSeconds)

	if v, ok := cfg["trend_filter"].(bool); ok {
		martinCfg.TrendFilter = v
	}
	martinCfg.TrendPeriod = getInt("trend_period", martinCfg.TrendPeriod)

	return martinCfg
}

// Name 返回策略名称
func (s *MartingaleStrategy) Name() string {
	return s.name
}

// Initialize 初始化策略
func (s *MartingaleStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	s.cfg = cfg
	s.executor = executor
	s.exchange = exchange
	return nil
}

// Start 启动策略
func (s *MartingaleStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	s.ctx = ctx
	s.isRunning = true
	s.mu.Unlock()

	logger.Info("✅ [%s] 马丁格尔策略已启动", s.name)
	logger.Info("📊 配置: 方向=%s, 初始金额=%.2f, 倍数=%.1f, 最大层数=%d",
		s.strategyCfg.Direction,
		s.strategyCfg.InitialAmount,
		s.strategyCfg.Multiplier,
		s.strategyCfg.MaxLevels)

	return nil
}

// Stop 停止策略
func (s *MartingaleStrategy) Stop() error {
	s.mu.Lock()
	s.isRunning = false
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	logger.Info("⏹️ [%s] 马丁格尔策略已停止", s.name)
	return nil
}

// OnPriceChange 价格变化处理
func (s *MartingaleStrategy) OnPriceChange(price float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return nil
	}

	// 更新价格历史
	s.priceHistory = append(s.priceHistory, price)
	if len(s.priceHistory) > 200 {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-200:]
	}
	s.lastPrice = price

	// 更新 K线
	s.updateCandle(price)

	// 检查止盈止损
	if err := s.checkTakeProfitStopLoss(price); err != nil {
		return err
	}

	// 检查是否需要开仓或加仓
	if len(s.entries) == 0 {
		// 首次开仓
		return s.openInitialPosition(price)
	}

	// 根据策略类型检查加仓
	if s.strategyCfg.ReverseMartingale {
		return s.checkReverseMartingale(price)
	}
	return s.checkMartingale(price)
}

// updateCandle 更新 K线
func (s *MartingaleStrategy) updateCandle(price float64) {
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

// openInitialPosition 开启初始仓位
func (s *MartingaleStrategy) openInitialPosition(price float64) error {
	// 趋势过滤
	if s.strategyCfg.TrendFilter && !s.checkTrendFilter() {
		return nil
	}

	side := "BUY"
	if s.direction == "SHORT" {
		side = "SELL"
	}

	quantity := s.strategyCfg.InitialAmount / price

	entry := &MartingaleEntry{
		Level:     0,
		Price:     price,
		Quantity:  quantity,
		Cost:      s.strategyCfg.InitialAmount,
		Status:    "pending",
		Timestamp: time.Now(),
	}

	// 下单
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     side,
		Quantity: quantity,
		Price:    price,
	})

	if err != nil {
		logger.Error("❌ [%s] 初始订单下单失败: %v", s.name, err)
		return err
	}

	entry.OrderID = order.OrderID
	entry.Status = "filled"
	s.entries = append(s.entries, entry)
	s.currentLevel = 1

	s.updateTotals()

	logger.Info("📈 [%s] 初始订单成交: 价格=%.2f, 数量=%.6f, 方向=%s",
		s.name, price, quantity, side)

	return nil
}

// checkMartingale 检查是否需要马丁加仓（亏损时加仓）
func (s *MartingaleStrategy) checkMartingale(price float64) error {
	if s.currentLevel >= s.strategyCfg.MaxLevels {
		return nil // 已达最大层数
	}

	lastEntry := s.entries[len(s.entries)-1]
	
	// 计算价格变化
	var priceChange float64
	if s.direction == "LONG" {
		priceChange = (lastEntry.Price - price) / lastEntry.Price * 100 // 价格下跌为正
	} else {
		priceChange = (price - lastEntry.Price) / lastEntry.Price * 100 // 价格上涨为正
	}

	// 检查是否达到加仓条件
	if priceChange < s.strategyCfg.PriceStep {
		return nil
	}

	// 计算加仓金额（考虑风险递减）
	multiplier := s.getMultiplier(s.currentLevel)
	amount := lastEntry.Cost * multiplier
	quantity := amount / price

	side := "BUY"
	if s.direction == "SHORT" {
		side = "SELL"
	}

	entry := &MartingaleEntry{
		Level:     s.currentLevel,
		Price:     price,
		Quantity:  quantity,
		Cost:      amount,
		Status:    "pending",
		Timestamp: time.Now(),
	}

	// 下单
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     side,
		Quantity: quantity,
		Price:    price,
	})

	if err != nil {
		logger.Error("❌ [%s] 马丁加仓 #%d 失败: %v", s.name, s.currentLevel, err)
		return err
	}

	entry.OrderID = order.OrderID
	entry.Status = "filled"
	s.entries = append(s.entries, entry)
	s.currentLevel++

	s.updateTotals()

	logger.Info("📉 [%s] 马丁加仓 #%d: 价格=%.2f, 数量=%.6f, 金额=%.2f, 倍数=%.2f, 平均成本=%.2f",
		s.name, entry.Level, price, quantity, amount, multiplier, s.avgEntryPrice)

	return nil
}

// checkReverseMartingale 检查反向马丁（盈利时加仓）
func (s *MartingaleStrategy) checkReverseMartingale(price float64) error {
	if s.currentLevel >= s.strategyCfg.MaxLevels {
		return nil
	}

	lastEntry := s.entries[len(s.entries)-1]

	// 计算盈利
	var profitPercent float64
	if s.direction == "LONG" {
		profitPercent = (price - lastEntry.Price) / lastEntry.Price * 100
	} else {
		profitPercent = (lastEntry.Price - price) / lastEntry.Price * 100
	}

	// 盈利达到阈值时加仓
	if profitPercent < s.strategyCfg.PriceStep {
		return nil
	}

	// 反向马丁使用固定倍数或递增倍数
	multiplier := s.strategyCfg.ReverseMultiplier
	amount := lastEntry.Cost * multiplier
	quantity := amount / price

	side := "BUY"
	if s.direction == "SHORT" {
		side = "SELL"
	}

	entry := &MartingaleEntry{
		Level:     s.currentLevel,
		Price:     price,
		Quantity:  quantity,
		Cost:      amount,
		Status:    "pending",
		Timestamp: time.Now(),
	}

	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     side,
		Quantity: quantity,
		Price:    price,
	})

	if err != nil {
		logger.Error("❌ [%s] 反向马丁加仓 #%d 失败: %v", s.name, s.currentLevel, err)
		return err
	}

	entry.OrderID = order.OrderID
	entry.Status = "filled"
	s.entries = append(s.entries, entry)
	s.currentLevel++

	s.updateTotals()

	logger.Info("📈 [%s] 反向马丁加仓 #%d: 价格=%.2f, 数量=%.6f, 金额=%.2f",
		s.name, entry.Level, price, quantity, amount)

	return nil
}

// getMultiplier 获取加仓倍数（考虑风险递减）
func (s *MartingaleStrategy) getMultiplier(level int) float64 {
	if !s.strategyCfg.RiskDecay {
		return s.strategyCfg.Multiplier
	}

	// 风险递减：倍数 = 基础倍数 * 递减因子^(层级-1)
	multiplier := s.strategyCfg.Multiplier * math.Pow(s.strategyCfg.DecayFactor, float64(level-1))

	// 确保不低于最小倍数
	if multiplier < s.strategyCfg.MinMultiplier {
		multiplier = s.strategyCfg.MinMultiplier
	}

	return multiplier
}

// updateTotals 更新总计
func (s *MartingaleStrategy) updateTotals() {
	s.totalCost = 0
	s.totalQty = 0

	for _, entry := range s.entries {
		if entry.Status == "filled" {
			s.totalCost += entry.Cost
			s.totalQty += entry.Quantity
		}
	}

	if s.totalQty > 0 {
		s.avgEntryPrice = s.totalCost / s.totalQty
	}
}

// checkTakeProfitStopLoss 检查止盈止损
func (s *MartingaleStrategy) checkTakeProfitStopLoss(price float64) error {
	if len(s.entries) == 0 || s.totalQty == 0 {
		return nil
	}

	// 计算盈亏
	var pnl, pnlPercent float64
	if s.direction == "LONG" {
		pnl = s.totalQty*price - s.totalCost
		pnlPercent = (price - s.avgEntryPrice) / s.avgEntryPrice * 100
	} else {
		pnl = s.totalCost - s.totalQty*price
		pnlPercent = (s.avgEntryPrice - price) / s.avgEntryPrice * 100
	}

	// 止盈
	if pnlPercent >= s.strategyCfg.TakeProfit {
		logger.Info("💰 [%s] 止盈触发: 盈利=%.2f%% (%.2f USDT)", s.name, pnlPercent, pnl)
		return s.closeAllPositions(price, "止盈")
	}

	// 止损
	if pnlPercent <= -s.strategyCfg.StopLoss {
		logger.Warn("🛑 [%s] 止损触发: 亏损=%.2f%% (%.2f USDT)", s.name, pnlPercent, pnl)
		return s.closeAllPositions(price, "止损")
	}

	return nil
}

// closeAllPositions 平仓
func (s *MartingaleStrategy) closeAllPositions(price float64, reason string) error {
	if s.totalQty <= 0 {
		return nil
	}

	side := "SELL"
	if s.direction == "SHORT" {
		side = "BUY" // 空头平仓用买入
	}

	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     side,
		Quantity: s.totalQty,
		Price:    price,
	})

	if err != nil {
		logger.Error("❌ [%s] 平仓失败: %v", s.name, err)
		return err
	}

	// 计算盈亏
	var pnl float64
	if s.direction == "LONG" {
		pnl = s.totalQty*price - s.totalCost
	} else {
		pnl = s.totalCost - s.totalQty*price
	}

	// 更新统计
	s.stats.TotalTrades++
	s.stats.TotalPnL += pnl
	s.stats.TotalVolume += s.totalCost

	if pnl > 0 {
		winCount := s.stats.WinRate * float64(s.stats.TotalTrades-1)
		winCount++
		s.stats.WinRate = winCount / float64(s.stats.TotalTrades)
	} else {
		winCount := s.stats.WinRate * float64(s.stats.TotalTrades-1)
		s.stats.WinRate = winCount / float64(s.stats.TotalTrades)
	}

	logger.Info("✅ [%s] 平仓完成 (%s): 订单ID=%d, 层数=%d, 盈亏=%.2f USDT",
		s.name, reason, order.OrderID, len(s.entries), pnl)

	// 重置
	s.entries = make([]*MartingaleEntry, 0, s.strategyCfg.MaxLevels)
	s.totalCost = 0
	s.totalQty = 0
	s.avgEntryPrice = 0
	s.currentLevel = 0

	return nil
}

// checkTrendFilter 趋势过滤
func (s *MartingaleStrategy) checkTrendFilter() bool {
	if len(s.priceHistory) < s.strategyCfg.TrendPeriod*2 {
		return true // 数据不足，允许开仓
	}

	prices := s.priceHistory[len(s.priceHistory)-s.strategyCfg.TrendPeriod*2:]
	shortPeriod := s.strategyCfg.TrendPeriod
	longPeriod := s.strategyCfg.TrendPeriod * 2

	shortMA := indicators.SMA(prices, shortPeriod)
	longMA := indicators.SMA(prices, longPeriod)

	if shortMA == nil || longMA == nil || len(shortMA) == 0 || len(longMA) == 0 {
		return true
	}

	shortValue := shortMA[len(shortMA)-1]
	longValue := longMA[len(longMA)-1]

	if s.direction == "LONG" {
		return shortValue >= longValue // 上涨趋势做多
	}
	return shortValue <= longValue // 下跌趋势做空
}

// OnOrderUpdate 订单更新处理
func (s *MartingaleStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range s.entries {
		if entry.OrderID == update.OrderID {
			if update.Status == "FILLED" {
				entry.Status = "filled"
				logger.Info("📊 [%s] 订单 #%d 成交: 层级=%d", s.name, update.OrderID, entry.Level)
			} else if update.Status == "CANCELED" {
				entry.Status = "canceled"
				logger.Warn("⚠️ [%s] 订单 #%d 已取消: 层级=%d", s.name, update.OrderID, entry.Level)
			}
			break
		}
	}

	return nil
}

// GetPositions 获取持仓
func (s *MartingaleStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.totalQty <= 0 {
		return []*Position{}
	}

	var pnl float64
	if s.direction == "LONG" {
		pnl = s.totalQty*s.lastPrice - s.totalCost
	} else {
		pnl = s.totalCost - s.totalQty*s.lastPrice
	}

	return []*Position{
		{
			Symbol:       s.strategyCfg.Symbol,
			Size:         s.totalQty,
			EntryPrice:   s.avgEntryPrice,
			CurrentPrice: s.lastPrice,
			PnL:          pnl,
		},
	}
}

// GetOrders 获取订单
func (s *MartingaleStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orders := make([]*Order, 0, len(s.entries))
	for _, entry := range s.entries {
		side := "BUY"
		if s.direction == "SHORT" {
			side = "SELL"
		}
		orders = append(orders, &Order{
			OrderID:  entry.OrderID,
			Symbol:   s.strategyCfg.Symbol,
			Side:     side,
			Price:    entry.Price,
			Quantity: entry.Quantity,
			Status:   entry.Status,
		})
	}

	return orders
}

// GetStatistics 获取统计
func (s *MartingaleStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// GetLevelInfo 获取层级信息
func (s *MartingaleStrategy) GetLevelInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("当前层级: %d/%d, 总成本: %.2f, 总持仓: %.6f, 平均成本: %.2f",
		len(s.entries), s.strategyCfg.MaxLevels, s.totalCost, s.totalQty, s.avgEntryPrice)
}
