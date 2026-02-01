package strategy

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/indicators"
	"quantmesh/logger"
	"quantmesh/position"
)

// MartingaleStrategy 马丁格尔策略
// 特点：
// 1. 加倍加倉机制：亏损時按倍數增加倉位
// 2. 风險遞减控制：随着层數增加，遞减加倉倍數
// 3. 最大层數限制：防止無限加倉
// 4. 反向马丁选项：盈利時加倉（适合趋势市）
type MartingaleStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg *MartingaleConfig

	// 價格數據
	priceHistory []float64
	candles      []indicators.Candle
	lastPrice    float64
	mu           sync.RWMutex

	// 倉位管理
	entries      []*MartingaleEntry // 入场記錄
	totalCost    float64            // 總成本
	totalQty     float64            // 總持倉量
	avgEntryPrice float64           // 平均入场價
	
	// 方向
	direction    string // LONG/SHORT
	currentLevel int    // 當前马丁层级

	// 状態
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool

	// 统计
	stats *StrategyStatistics

	// 事件總線
	eventBus EventBus

	// 暂停標志
	isPaused bool
}

// MartingaleConfig 马丁格尔配置
type MartingaleConfig struct {
	// 基础配置
	Symbol           string  `yaml:"symbol"`
	Direction        string  `yaml:"direction"`          // LONG/SHORT/BOTH
	InitialAmount    float64 `yaml:"initial_amount"`     // 初始金額 (USDT)
	
	// 马丁参數
	Multiplier       float64 `yaml:"multiplier"`         // 加倉倍數（預設 2.0）
	MaxLevels        int     `yaml:"max_levels"`         // 最大層數（預設 6）
	PriceStep        float64 `yaml:"price_step"`         // 加倉间距 (%)
	
	// 风險遞减
	RiskDecay        bool    `yaml:"risk_decay"`         // 啟用风險遞减
	DecayFactor      float64 `yaml:"decay_factor"`       // 遞减因子 (0.8-0.95)
	MinMultiplier    float64 `yaml:"min_multiplier"`     // 最小倍數 (1.0)
	
	// 止盈止损
	TakeProfit       float64 `yaml:"take_profit"`        // 止盈比例 (%)
	StopLoss         float64 `yaml:"stop_loss"`          // 止损比例 (%)
	TrailingStop     float64 `yaml:"trailing_stop"`      // 追踪止损 (%)
	
	// 反向马丁
	ReverseMartingale bool   `yaml:"reverse_martingale"` // 反向马丁（盈利時加倉）
	ReverseMultiplier float64 `yaml:"reverse_multiplier"` // 反向倍數
	
	// 冷却期
	CooldownEnabled  bool    `yaml:"cooldown_enabled"`   // 啟用冷却期
	CooldownSeconds  int     `yaml:"cooldown_seconds"`   // 冷却時间（秒）
	
	// 趨勢過濾
	TrendFilter      bool    `yaml:"trend_filter"`       // 啟用趨勢過濾
	TrendPeriod      int     `yaml:"trend_period"`       // 趋势周期
}

// MartingaleEntry 马丁入场記錄
type MartingaleEntry struct {
	Level     int       // 层级
	Price     float64   // 入场價格
	Quantity  float64   // 數量
	Cost      float64   // 成本
	OrderID   int64     // 订單ID
	Status    string    // pending/filled/closed
	Timestamp time.Time // 時间戳
}

// NewMartingaleStrategy 創建马丁格尔策略
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
		// 預設值
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

	// 從 map 中读取配置
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

// Name 回傳策略名稱
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

// SetEventBus 設置事件總線
func (s *MartingaleStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus
}

// Start 啟动策略
func (s *MartingaleStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	s.ctx = ctx
	s.isRunning = true
	s.mu.Unlock()

	logger.Info("✅ [%s] 马丁格尔策略已啟动", s.name)
	logger.Info("📊 配置: 方向=%s, 初始金額=%.2f, 倍數=%.1f, 最大层數=%d",
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

// OnPriceChange 價格變化处理
func (s *MartingaleStrategy) OnPriceChange(price float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning || s.isPaused {
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

	// 检查止盈止损
	if err := s.checkTakeProfitStopLoss(price); err != nil {
		return err
	}

	// 检查是否需要开倉或加倉
	if len(s.entries) == 0 {
		// 首次开倉
		return s.openInitialPosition(price)
	}

	// 根據策略類型检查加倉
	if s.strategyCfg.ReverseMartingale {
		return s.checkReverseMartingale(price)
	}
	return s.checkMartingale(price)
}

// updateCandle 更新 K線
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

// openInitialPosition 开啟初始倉位
func (s *MartingaleStrategy) openInitialPosition(price float64) error {
	// 趨勢過濾
	if s.strategyCfg.TrendFilter && !s.checkTrendFilter() {
		return nil
	}

	side := "BUY"
	if s.direction == "SHORT" {
		side = "SELL"
	}

	quantity := s.strategyCfg.InitialAmount / price

	// 精度检查
	qDec := s.exchange.GetQuantityDecimals()
	quantityRounded := math.Floor(quantity*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))

	if quantityRounded <= 0 {
		minQty := math.Pow10(-qDec)
		logger.Error("🚨 [%s] 初始订單數量過小 (%.8f)，低於交易所最小精度 (%.8f)，策略已自动暂停！请在配置中調大 InitialAmount", s.name, quantity, minQty)
		s.isPaused = true
		
		// 发布事件
		if s.eventBus != nil {
			s.eventBus.Publish(&event.Event{
				Type:      event.EventTypePrecisionAdjustment,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"symbol":           s.strategyCfg.Symbol,
					"strategy":         s.name,
					"order_amount":     s.strategyCfg.InitialAmount,
					"calculated_qty":   quantity,
					"min_qty":          minQty,
					"price":            price,
					"action":           "pause",
					"reason":           "初始订單數量低於交易所最小精度",
				},
			})
		}
		return nil
	}
	quantity = quantityRounded

	entry := &MartingaleEntry{
		Level:     0,
		Price:     price,
		Quantity:  quantity,
		Cost:      s.strategyCfg.InitialAmount,
		Status:    "pending",
		Timestamp: time.Now(),
	}

	// 下單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     side,
		Quantity: quantity,
		Price:    price,
	})

	if err != nil {
		logger.Error("❌ [%s] 初始订單下單失败: %v", s.name, err)
		return err
	}

	entry.OrderID = order.OrderID
	entry.Status = "filled"
	s.entries = append(s.entries, entry)
	s.currentLevel = 1

	s.updateTotals()

	logger.Info("📈 [%s:%s] [%s] 初始订單成交: 價格=%.2f, 數量=%.6f, 方向=%s",
		s.exchange.GetName(), s.strategyCfg.Symbol, s.name, price, quantity, side)

	return nil
}

// checkMartingale 检查是否需要马丁加倉（亏损時加倉）
func (s *MartingaleStrategy) checkMartingale(price float64) error {
	if s.currentLevel >= s.strategyCfg.MaxLevels {
		return nil // 已达最大层數
	}

	lastEntry := s.entries[len(s.entries)-1]
	
	// 计算價格變化
	var priceChange float64
	if s.direction == "LONG" {
		priceChange = (lastEntry.Price - price) / lastEntry.Price * 100 // 價格下跌為正
	} else {
		priceChange = (price - lastEntry.Price) / lastEntry.Price * 100 // 價格上涨為正
	}

	// 检查是否达到加倉条件
	if priceChange < s.strategyCfg.PriceStep {
		return nil
	}

	// 计算加倉金額（考虑风險遞减）
	multiplier := s.getMultiplier(s.currentLevel)
	amount := lastEntry.Cost * multiplier
	quantity := amount / price

	// 精度检查
	qDec := s.exchange.GetQuantityDecimals()
	quantityRounded := math.Floor(quantity*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))

	if quantityRounded <= 0 {
		minQty := math.Pow10(-qDec)
		logger.Error("🚨 [%s] 马丁加倉 #%d 數量過小 (%.8f)，低於交易所最小精度 (%.8f)，策略已自动暂停！", s.name, s.currentLevel, quantity, minQty)
		s.isPaused = true
		
		// 发布事件
		if s.eventBus != nil {
			s.eventBus.Publish(&event.Event{
				Type:      event.EventTypePrecisionAdjustment,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"symbol":           s.strategyCfg.Symbol,
					"strategy":         s.name,
					"level":            s.currentLevel,
					"order_amount":     amount,
					"calculated_qty":   quantity,
					"min_qty":          minQty,
					"price":            price,
					"action":           "pause",
					"reason":           "马丁加倉數量低於交易所最小精度",
				},
			})
		}
		return nil
	}
	quantity = quantityRounded

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

	// 下單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     side,
		Quantity: quantity,
		Price:    price,
	})

	if err != nil {
		logger.Error("❌ [%s] 马丁加倉 #%d 失败: %v", s.name, s.currentLevel, err)
		return err
	}

	entry.OrderID = order.OrderID
	entry.Status = "filled"
	s.entries = append(s.entries, entry)
	s.currentLevel++

	s.updateTotals()

	logger.Info("📉 [%s] 马丁加倉 #%d: 價格=%.2f, 數量=%.6f, 金額=%.2f, 倍數=%.2f, 平均成本=%.2f",
		s.name, entry.Level, price, quantity, amount, multiplier, s.avgEntryPrice)

	return nil
}

// checkReverseMartingale 检查反向马丁（盈利時加倉）
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

	// 盈利達到阈值時加倉
	if profitPercent < s.strategyCfg.PriceStep {
		return nil
	}

	// 反向马丁使用固定倍數或遞增倍數
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
		logger.Error("❌ [%s] 反向马丁加倉 #%d 失败: %v", s.name, s.currentLevel, err)
		return err
	}

	entry.OrderID = order.OrderID
	entry.Status = "filled"
	s.entries = append(s.entries, entry)
	s.currentLevel++

	s.updateTotals()

	logger.Info("📈 [%s] 反向马丁加倉 #%d: 價格=%.2f, 數量=%.6f, 金額=%.2f",
		s.name, entry.Level, price, quantity, amount)

	return nil
}

// getMultiplier 獲取加倉倍數（考虑风險遞减）
func (s *MartingaleStrategy) getMultiplier(level int) float64 {
	if !s.strategyCfg.RiskDecay {
		return s.strategyCfg.Multiplier
	}

	// 风險遞减：倍數 = 基础倍數 * 遞减因子^(层级-1)
	multiplier := s.strategyCfg.Multiplier * math.Pow(s.strategyCfg.DecayFactor, float64(level-1))

	// 确保不低於最小倍數
	if multiplier < s.strategyCfg.MinMultiplier {
		multiplier = s.strategyCfg.MinMultiplier
	}

	return multiplier
}

// updateTotals 更新總计
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

// closeAllPositions 平倉
func (s *MartingaleStrategy) closeAllPositions(price float64, reason string) error {
	if s.totalQty <= 0 {
		return nil
	}

	side := "SELL"
	if s.direction == "SHORT" {
		side = "BUY" // 空头平倉用買入
	}

	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     side,
		Quantity: s.totalQty,
		Price:    price,
	})

	if err != nil {
		logger.Error("❌ [%s] 平倉失败: %v", s.name, err)
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

	logger.Info("✅ [%s] 平倉完成 (%s): 订單ID=%d, 层數=%d, 盈亏=%.2f USDT",
		s.name, reason, order.OrderID, len(s.entries), pnl)

	// 重置
	s.entries = make([]*MartingaleEntry, 0, s.strategyCfg.MaxLevels)
	s.totalCost = 0
	s.totalQty = 0
	s.avgEntryPrice = 0
	s.currentLevel = 0

	return nil
}

// checkTrendFilter 趨勢過濾
func (s *MartingaleStrategy) checkTrendFilter() bool {
	if len(s.priceHistory) < s.strategyCfg.TrendPeriod*2 {
		return true // 數據不足，允許开倉
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

// OnOrderUpdate 订單更新处理
func (s *MartingaleStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range s.entries {
		if entry.OrderID == update.OrderID {
			if update.Status == "FILLED" {
				entry.Status = "filled"
				logger.Info("📊 [%s] 订單 #%d 成交: 层级=%d", s.name, update.OrderID, entry.Level)
			} else if update.Status == "CANCELED" {
				entry.Status = "canceled"
				logger.Warn("⚠️ [%s] 订單 #%d 已取消: 层级=%d", s.name, update.OrderID, entry.Level)
			}
			break
		}
	}

	return nil
}

// GetPositions 獲取持倉
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

// GetOrders 獲取訂單
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

// GetStatistics 獲取统计
func (s *MartingaleStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// GetLevelInfo 獲取层级信息
func (s *MartingaleStrategy) GetLevelInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("當前层级: %d/%d, 總成本: %.2f, 總持倉: %.6f, 平均成本: %.2f",
		len(s.entries), s.strategyCfg.MaxLevels, s.totalCost, s.totalQty, s.avgEntryPrice)
}
