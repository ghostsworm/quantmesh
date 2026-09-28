package strategy

import (
	"context"
	"fmt"
	"math"
	"strings"
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
	entries       []*MartingaleEntry // 入场記錄
	totalCost     float64            // 總成本
	totalQty      float64            // 總持倉量
	avgEntryPrice float64            // 平均入场價

	// 方向
	direction    string // LONG/SHORT
	currentLevel int    // 當前马丁层级

	// 状態
	ctx               context.Context
	cancel            context.CancelFunc
	isRunning         bool
	isClosing         bool
	closeOrderID      int64
	closeRequestedQty float64
	closeProgress     position.FillProgress
	closeRealizedPnL  float64

	// 统计
	stats *StrategyStatistics

	// 事件總線
	eventBus EventBus

	// 暂停標志
	isPaused          bool
	runtimeStateStore RuntimeStateStore
	runtimeStateErr   error
}

// MartingaleConfig 马丁格尔配置
type MartingaleConfig struct {
	// 基础配置
	Symbol        string  `yaml:"symbol"`
	Direction     string  `yaml:"direction"`      // LONG/SHORT/BOTH
	InitialAmount float64 `yaml:"initial_amount"` // 初始金額 (USDT)

	// 马丁参數
	Multiplier float64 `yaml:"multiplier"` // 加倉倍數（預設 2.0）
	MaxLevels  int     `yaml:"max_levels"` // 最大層數（預設 6）
	PriceStep  float64 `yaml:"price_step"` // 加倉间距 (%)

	// 风險遞减
	RiskDecay     bool    `yaml:"risk_decay"`     // 啟用风險遞减
	DecayFactor   float64 `yaml:"decay_factor"`   // 遞减因子 (0.8-0.95)
	MinMultiplier float64 `yaml:"min_multiplier"` // 最小倍數 (1.0)

	// 止盈止损
	TakeProfit   float64 `yaml:"take_profit"`   // 止盈比例 (%)
	StopLoss     float64 `yaml:"stop_loss"`     // 止损比例 (%)
	TrailingStop float64 `yaml:"trailing_stop"` // 追踪止损 (%)

	// 反向马丁
	ReverseMartingale bool    `yaml:"reverse_martingale"` // 反向马丁（盈利時加倉）
	ReverseMultiplier float64 `yaml:"reverse_multiplier"` // 反向倍數

	// 冷却期
	CooldownEnabled bool `yaml:"cooldown_enabled"` // 啟用冷却期
	CooldownSeconds int  `yaml:"cooldown_seconds"` // 冷却時间（秒）

	// 趨勢過濾
	TrendFilter bool `yaml:"trend_filter"` // 啟用趨勢過濾
	TrendPeriod int  `yaml:"trend_period"` // 趋势周期
}

// MartingaleEntry 马丁入场記錄
type MartingaleEntry struct {
	Level             int     // 层级
	Price             float64 // 入场價格
	Quantity          float64 // 數量
	RequestedQuantity float64 // 委託數量（非已成交持倉）
	Cost              float64 // 成本
	OpeningFee        float64 // 已折算至计價币的入场手續費
	FillProgress      position.FillProgress
	OrderID           int64     // 订單ID
	Status            string    // pending/filled/closed
	Timestamp         time.Time // 時间戳
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
		direction:    normalizeMartingaleDirection(name, martinCfg.Direction),
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

// normalizeMartingaleDirection 马丁单策略只跑一條持倉腿：SHORT 做空，其余（含 BOTH/空值）按 LONG 处理
func normalizeMartingaleDirection(name, direction string) string {
	d := strings.ToUpper(strings.TrimSpace(direction))
	if d == position.PositionSideShort {
		return position.PositionSideShort
	}
	if d != "" && d != position.PositionSideLong {
		logger.Warn("⚠️ [%s] 马丁策略不支持方向 %q，按 LONG 运行", name, direction)
	}
	return position.PositionSideLong
}

// positionSide 本策略订單所屬持倉腿（显式写入 OrderRequest.PositionSide，供执行器区分开/平倉）
func (s *MartingaleStrategy) positionSide() string {
	if s.direction == position.PositionSideShort {
		return position.PositionSideShort
	}
	return position.PositionSideLong
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

func (s *MartingaleStrategy) effectiveBotID() string {
	if s.cfg != nil && strings.TrimSpace(s.cfg.Trading.BotID) != "" {
		return strings.TrimSpace(s.cfg.Trading.BotID)
	}
	exchangeName := "binance"
	if s.exchange != nil && strings.TrimSpace(s.exchange.GetName()) != "" {
		exchangeName = strings.ToLower(strings.TrimSpace(s.exchange.GetName()))
	}
	marketType := "futures"
	if s.cfg != nil && strings.TrimSpace(s.cfg.Trading.MarketType) != "" {
		marketType = strings.ToLower(strings.TrimSpace(s.cfg.Trading.MarketType))
	}
	return config.GenerateBotID(exchangeName, s.strategyCfg.Symbol, marketType)
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
	if ctx == nil {
		ctx = context.Background()
	}
	if s.runtimeStateStore == nil {
		return fmt.Errorf("martingale runtime state store is required")
	}
	if err := s.restoreRuntimeState(); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	oldCancel := s.cancel
	s.ctx = runCtx
	s.cancel = cancel
	s.isRunning = true
	s.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}

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
	cancel := s.cancel
	s.isRunning = false
	s.cancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	logger.Info("⏹️ [%s] 马丁格尔策略已停止", s.name)
	return nil
}

// IsRunning 回傳策略是否已成功啟动
func (s *MartingaleStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

// OnPriceChange 價格變化处理
func (s *MartingaleStrategy) OnPriceChange(price float64) error {
	return s.onPrice(price, true)
}

// OnPriceChangeRiskOnly 只更新行情並執行止盈止损，不开倉/加倉（组合策略門控時使用）
func (s *MartingaleStrategy) OnPriceChangeRiskOnly(price float64) error {
	return s.onPrice(price, false)
}

// onPrice 價格處理主流程；allowOpening=false 時跳过开倉/加倉。
// 暂停（如精度不足自动暂停）只禁止开倉/加倉，已有持倉的止盈止损照常执行。
func (s *MartingaleStrategy) onPrice(price float64, allowOpening bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning || s.isClosing {
		return nil
	}
	if s.runtimeStateErr != nil {
		return fmt.Errorf("martingale runtime state is not durable; new decisions are paused: %w", s.runtimeStateErr)
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

	// 已下平倉單、暂停、被門控、或仍有未成交的开倉單時，不再下新的开倉/加倉單
	if s.isClosing || s.isPaused || !allowOpening || s.hasPendingEntry() {
		return nil
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
					"symbol":         s.strategyCfg.Symbol,
					"strategy":       s.name,
					"order_amount":   s.strategyCfg.InitialAmount,
					"calculated_qty": quantity,
					"min_qty":        minQty,
					"price":          price,
					"action":         "pause",
					"reason":         "初始订單數量低於交易所最小精度",
				},
			})
		}
		return nil
	}
	quantity = quantityRounded

	entry := &MartingaleEntry{
		Level:             0,
		Price:             price,
		RequestedQuantity: quantity,
		Status:            entryStatusPending,
		Timestamp:         time.Now(),
	}

	// 下單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:       s.strategyCfg.Symbol,
		Side:         side,
		Quantity:     quantity,
		Price:        price,
		PostOnly:     true,
		PositionSide: s.positionSide(),
	})

	if err != nil {
		logger.Error("❌ [%s] 初始订單下單失败: %v", s.name, err)
		return err
	}
	if order == nil {
		logger.Debug("🔒 [%s] 初始订單被执行器跳过，等待下一轮", s.name)
		return nil
	}

	// S3：限價單下單成功≠成交，保持 pending，等成交回報再計入持倉
	entry.OrderID = order.OrderID
	s.entries = append(s.entries, entry)
	s.currentLevel = 1
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.requireMartingaleOrderReconciliation(&position.OrderUpdate{OrderID: order.OrderID}, "martingale order accepted but runtime state persistence failed")
		return err
	}

	logger.Info("📈 [%s:%s] [%s] 初始订單已挂單: 價格=%.2f, 數量=%.6f, 方向=%s",
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
					"symbol":         s.strategyCfg.Symbol,
					"strategy":       s.name,
					"level":          s.currentLevel,
					"order_amount":   amount,
					"calculated_qty": quantity,
					"min_qty":        minQty,
					"price":          price,
					"action":         "pause",
					"reason":         "马丁加倉數量低於交易所最小精度",
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
		Level:             s.currentLevel,
		Price:             price,
		RequestedQuantity: quantity,
		Status:            entryStatusPending,
		Timestamp:         time.Now(),
	}

	// 下單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:       s.strategyCfg.Symbol,
		Side:         side,
		Quantity:     quantity,
		Price:        price,
		PostOnly:     true,
		PositionSide: s.positionSide(),
	})

	if err != nil {
		logger.Error("❌ [%s] 马丁加倉 #%d 失败: %v", s.name, s.currentLevel, err)
		return err
	}
	if order == nil {
		logger.Debug("🔒 [%s] 马丁加倉 #%d 被执行器跳过，等待下一轮", s.name, s.currentLevel)
		return nil
	}

	// S3：保持 pending，等成交回報再計入持倉
	entry.OrderID = order.OrderID
	s.entries = append(s.entries, entry)
	s.currentLevel++
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.requireMartingaleOrderReconciliation(&position.OrderUpdate{OrderID: order.OrderID}, "martingale add order accepted but runtime state persistence failed")
		return err
	}

	logger.Info("📉 [%s] 马丁加倉 #%d 已挂單:價格=%.2f, 數量=%.6f, 金額=%.2f, 倍數=%.2f, 平均成本=%.2f",
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
		Level:             s.currentLevel,
		Price:             price,
		RequestedQuantity: quantity,
		Status:            entryStatusPending,
		Timestamp:         time.Now(),
	}

	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:       s.strategyCfg.Symbol,
		Side:         side,
		Quantity:     quantity,
		Price:        price,
		PostOnly:     true,
		PositionSide: s.positionSide(),
	})

	if err != nil {
		logger.Error("❌ [%s] 反向马丁加倉 #%d 失败: %v", s.name, s.currentLevel, err)
		return err
	}
	if order == nil {
		logger.Debug("🔒 [%s] 反向马丁加倉 #%d 被执行器跳过，等待下一轮", s.name, s.currentLevel)
		return nil
	}

	// S3：保持 pending，等成交回報再計入持倉
	entry.OrderID = order.OrderID
	s.entries = append(s.entries, entry)
	s.currentLevel++
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.requireMartingaleOrderReconciliation(&position.OrderUpdate{OrderID: order.OrderID}, "reverse martingale order accepted but runtime state persistence failed")
		return err
	}

	logger.Info("📈 [%s] 反向马丁加倉 #%d 已挂單:價格=%.2f, 數量=%.6f, 金額=%.2f",
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
		if entryHasFill(entry.Status) {
			s.totalCost += entry.Cost
			s.totalQty += entry.Quantity
		}
	}

	if s.totalQty > 0 {
		s.avgEntryPrice = s.totalCost / s.totalQty
	} else {
		s.avgEntryPrice = 0
	}
}

// hasPendingEntry 是否存在未完全成交的开倉單
func (s *MartingaleStrategy) hasPendingEntry() bool {
	for _, entry := range s.entries {
		if entry.Status == entryStatusPending || entry.Status == entryStatusPartiallyFilled {
			return true
		}
	}
	return false
}

// cancelPendingEntries 撤销所有未完全成交的开倉單（撤單回報到達後再回滚入场記錄）
func (s *MartingaleStrategy) cancelPendingEntries() {
	ids := make([]int64, 0)
	for _, entry := range s.entries {
		if (entry.Status == entryStatusPending || entry.Status == entryStatusPartiallyFilled) && entry.OrderID > 0 {
			ids = append(ids, entry.OrderID)
		}
	}
	if len(ids) == 0 {
		return
	}
	if err := s.executor.BatchCancelOrders(ids); err != nil {
		logger.Warn("⚠️ [%s] 平倉前撤销未成交开倉單失败 (订單=%v): %v", s.name, ids, err)
	}
}

// checkTakeProfitStopLoss 检查止盈止损
func (s *MartingaleStrategy) checkTakeProfitStopLoss(price float64) error {
	if len(s.entries) == 0 || s.totalQty == 0 || s.avgEntryPrice <= 0 {
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

	// 判斷订單來源（止损/止盈）
	orderSource := "stop_loss"
	if !strings.Contains(reason, "止损") {
		orderSource = "normal"
	}

	// 先撤掉未成交的开倉單，避免平倉後又成交出孤兒倉位
	s.cancelPendingEntries()

	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:       s.strategyCfg.Symbol,
		Side:         side,
		Quantity:     s.totalQty,
		Price:        price,
		ReduceOnly:   true,
		PositionSide: s.positionSide(),
		PostOnly:     orderSource != "stop_loss",
		OrderSource:  orderSource,
	})

	if err != nil {
		logger.Error("❌ [%s] 平倉失败 (%s, 數量=%.6f, 價格=%.2f): %v", s.name, reason, s.totalQty, price, err)
		return fmt.Errorf("马丁策略 %s 平倉(%s)下單失败: %w", s.name, reason, err)
	}
	if order == nil {
		logger.Debug("🔒 [%s] 平倉单被执行器跳过，等待下一轮", s.name)
		return nil
	}

	s.isClosing = true
	s.closeOrderID = order.OrderID
	s.closeRequestedQty = s.totalQty
	s.closeProgress = position.FillProgress{}
	s.closeRealizedPnL = 0
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.requireMartingaleOrderReconciliation(&position.OrderUpdate{OrderID: order.OrderID}, "martingale close order accepted but runtime state persistence failed")
		return err
	}
	logger.Info("⏳ [%s] 平倉委託已提交 (%s): 订單ID=%d, 层數=%d；等待實際成交回報",
		s.name, reason, order.OrderID, len(s.entries))

	return nil
}

func (s *MartingaleStrategy) resetPositionState() {
	s.entries = make([]*MartingaleEntry, 0, s.strategyCfg.MaxLevels)
	s.totalCost = 0
	s.totalQty = 0
	s.avgEntryPrice = 0
	s.currentLevel = 0
	s.isClosing = false
	s.closeOrderID = 0
	s.closeRequestedQty = 0
	s.closeProgress = position.FillProgress{}
	s.closeRealizedPnL = 0
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

	if update == nil || update.OrderID == 0 {
		return nil
	}

	if s.isClosing && update.OrderID == s.closeOrderID {
		terminal := signalOrderStatusFilled(update.Status) || signalOrderStatusTerminal(update.Status)
		if signalOrderStatusFilled(update.Status) && update.ExecutedQty <= 0 {
			return nil
		}
		if update.ExecutedQty < s.closeProgress.Quantity {
			return nil
		}
		var actualFilled float64
		oldProgress := s.closeProgress.Quantity
		deltaQty := update.ExecutedQty - oldProgress
		if deltaQty > s.totalQty+entryQtyEpsilon {
			return nil
		}
		realizedDelta := 0.0
		if update.ExecutedQty > s.closeProgress.Quantity {
			if update.AvgPrice <= 0 {
				// Do not reduce inventory or book PnL against an unverified price.
				return nil
			}
			nextProgress := s.closeProgress
			delta, executionPrice := nextProgress.Advance(update.ExecutedQty, update.AvgPrice, 0)
			if delta <= 0 || math.Abs(delta-deltaQty) > entryQtyEpsilon {
				return nil
			}
			closeFee, feeKnown := commissionInQuote(s.exchange, update.Commission, update.CommissionAsset, executionPrice)
			if !feeKnown {
				s.requireMartingaleOrderReconciliation(update, "martingale close fee cannot be valued in quote asset")
				return nil
			}
			basis := s.avgEntryPrice
			realizedDelta = delta * (executionPrice - basis)
			if s.direction == "SHORT" {
				realizedDelta = -realizedDelta
			}
			openingFee := s.openingFeeTotal() * delta / s.totalQty
			realizedDelta -= closeFee + openingFee
			s.closeProgress = nextProgress
			actualFilled = s.closeProgress.Quantity
			s.closeRealizedPnL += realizedDelta
			s.stats.TotalPnL += realizedDelta
			s.stats.TotalVolume += delta * executionPrice
		}
		if terminal && s.closeProgress.Quantity > 0 {
			actualFilled = s.closeProgress.Quantity
			s.recordCloseResult(s.closeRealizedPnL)
		}
		if deltaQty > 0 {
			s.reduceAllEntries(deltaQty)
		}
		if terminal {
			logger.Info("✅ [%s] 平倉單 #%d 已終結 (%s, 實際成交 %.8f)，剩餘持倉 %.8f",
				s.name, update.OrderID, update.Status, actualFilled, s.totalQty)
			s.isClosing = false
			s.closeOrderID = 0
			s.closeRequestedQty = 0
			s.closeProgress = position.FillProgress{}
			s.closeRealizedPnL = 0
		}
		return s.persistRuntimeStateLocked()
	}

	for _, entry := range s.entries {
		if entry.OrderID == update.OrderID {
			s.handleEntryOrderUpdate(entry, update)
			return s.persistRuntimeStateLocked()
		}
	}

	return nil
}

func (s *MartingaleStrategy) recordCloseResult(pnl float64) {
	s.stats.TotalTrades++
	wins := s.stats.WinRate * float64(s.stats.TotalTrades-1)
	if pnl > 0 {
		wins++
	}
	s.stats.WinRate = wins / float64(s.stats.TotalTrades)
}

func (s *MartingaleStrategy) openingFeeTotal() float64 {
	var total float64
	for _, entry := range s.entries {
		if entryHasFill(entry.Status) {
			total += entry.OpeningFee
		}
	}
	return total
}

func (s *MartingaleStrategy) requireMartingaleOrderReconciliation(update *position.OrderUpdate, reason string) {
	if tracker, ok := s.executor.(interface {
		MarkOrderReconciliationRequired(int64, string, string) error
	}); ok {
		if err := tracker.MarkOrderReconciliationRequired(update.OrderID, update.ClientOrderID, reason); err != nil {
			logger.Error("[%s] 马丁手续费无法估值且持久化对账锁失败: order=%d err=%v", s.name, update.OrderID, err)
		}
	} else {
		logger.Error("[%s] 马丁手续费无法估值且执行器不支持持久化对账锁: order=%d reason=%s", s.name, update.OrderID, reason)
	}
}

// handleEntryOrderUpdate 处理开倉/加倉單回報：按實際成交數量/均價計入持倉；未成交即終止则回滚（S3）
func (s *MartingaleStrategy) handleEntryOrderUpdate(entry *MartingaleEntry, update *position.OrderUpdate) {
	qty := update.ExecutedQty
	if qty < entry.FillProgress.Quantity || (signalOrderStatusFilled(update.Status) && qty <= 0) {
		return
	}
	if qty > entry.FillProgress.Quantity {
		if update.AvgPrice <= 0 {
			return
		}
		nextProgress := entry.FillProgress
		delta, price := nextProgress.Advance(qty, update.AvgPrice, 0)
		fee, feeKnown := commissionInQuote(s.exchange, update.Commission, update.CommissionAsset, price)
		if !feeKnown {
			s.requireMartingaleOrderReconciliation(update, "martingale entry fee cannot be valued in quote asset")
			return
		}
		if delta <= 0 {
			return
		}
		entry.Quantity += delta
		entry.Cost += delta * price
		entry.Price = entry.Cost / entry.Quantity
		entry.OpeningFee += fee
		entry.FillProgress = nextProgress
	}
	if signalOrderStatusFilled(update.Status) || signalOrderStatusTerminal(update.Status) {
		if entry.FillProgress.Quantity > 0 {
			entry.Status = entryStatusFilled
		} else {
			for i, candidate := range s.entries {
				if candidate == entry {
					s.entries = append(s.entries[:i], s.entries[i+1:]...)
					break
				}
			}
			s.currentLevel = len(s.entries)
		}
	} else if entry.FillProgress.Quantity > 0 {
		entry.Status = entryStatusPartiallyFilled
	}
	s.updateTotals()
	logger.Info("📊 [%s] 订單 #%d %s: 层级=%d, 成交數量=%.6f, 均價=%.2f, 平均成本=%.2f, 开倉费=%.6f",
		s.name, update.OrderID, update.Status, entry.Level, entry.Quantity, entry.Price, s.avgEntryPrice, entry.OpeningFee)
}

// reduceAllEntries 平倉單部分成交後被撤：按比例缩减已成交入场記錄
func (s *MartingaleStrategy) reduceAllEntries(qty float64) {
	if qty <= 0 || s.totalQty <= 0 {
		return
	}
	if qty >= s.totalQty-entryQtyEpsilon {
		s.resetPositionState()
		return
	}
	remainRatio := (s.totalQty - qty) / s.totalQty
	for _, entry := range s.entries {
		if entryHasFill(entry.Status) {
			entry.Quantity *= remainRatio
			entry.Cost *= remainRatio
			entry.OpeningFee *= remainRatio
		}
	}
	s.updateTotals()
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
		pnl = s.totalQty*s.lastPrice - s.totalCost - s.openingFeeTotal()
	} else {
		pnl = s.totalCost - s.totalQty*s.lastPrice - s.openingFeeTotal()
	}

	return []*Position{
		{
			Symbol:       s.strategyCfg.Symbol,
			Size:         s.totalQty,
			EntryPrice:   s.avgEntryPrice,
			OpeningFee:   s.openingFeeTotal(),
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
			Quantity: max(entry.Quantity, entry.RequestedQuantity),
			Status:   entry.Status,
		})
	}

	return orders
}

// GetStatistics 獲取统计
func (s *MartingaleStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.stats == nil {
		return &StrategyStatistics{}
	}
	statsCopy := *s.stats
	return &statsCopy
}

// GetLevelInfo 獲取层级信息
func (s *MartingaleStrategy) GetLevelInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("當前层级: %d/%d, 總成本: %.2f, 總持倉: %.6f, 平均成本: %.2f",
		len(s.entries), s.strategyCfg.MaxLevels, s.totalCost, s.totalQty, s.avgEntryPrice)
}

// GetVisualizationData 獲取策略可视化數據
func (s *MartingaleStrategy) GetVisualizationData() map[string]interface{} {
	// TODO: 实现Martingale策略的可视化数据
	return make(map[string]interface{})
}
