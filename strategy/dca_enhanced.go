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

// DCAEnhancedStrategy 增强型 DCA (定投) 策略
// 特点：
// 1. ATR 动態间距管理：根據市場波動率自动調整買入间距
// 2. 三重止盈机制：首單止盈、尾單止盈、全倉止盈
// 3. 50层精细化倉位管理：多达50层的加倉控制
// 4. 防瀑布式下跌保护：在极端下跌時暂停加倉
type DCAEnhancedStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg *DCAEnhancedConfig

	// 價格數據
	priceHistory []float64
	candles      []indicators.Candle
	lastPrice    float64
	mu           sync.RWMutex

	// 倉位管理
	layers       []*DCALayer // 分层倉位
	totalCost    float64     // 總成本
	totalQty     float64     // 總持倉量
	avgEntryPrice float64    // 平均入场價
	maxLayers    int         // 最大层數
	currentLayer int         // 當前层數

	// ATR 动態间距
	atr           *indicators.ATR
	baseInterval  float64 // 基础间距
	dynamicInterval float64 // 动態间距

	// 止盈追踪
	highestProfit float64 // 最高盈利点
	takeProfitTriggered bool // 是否触发止盈追踪

	// 状態
	ctx           context.Context
	cancel        context.CancelFunc
	isRunning     bool
	isPaused      bool // 暂停加倉（瀑布下跌保护）
	pauseUntil    time.Time

	// 统计
	stats *StrategyStatistics

	// 事件總線
	eventBus EventBus
}

// DCAEnhancedConfig 增强型 DCA 配置
type DCAEnhancedConfig struct {
	// 基础配置
	Symbol          string  `yaml:"symbol"`
	BaseOrderAmount float64 `yaml:"base_order_amount"` // 基础订單金額 (USDT)
	SafetyOrderAmount float64 `yaml:"safety_order_amount"` // 安全订單金額 (USDT)
	MaxSafetyOrders int     `yaml:"max_safety_orders"` // 最大安全订單數 (最多50层)
	
	// ATR 动態间距
	ATRPeriod       int     `yaml:"atr_period"`        // ATR 周期
	ATRMultiplier   float64 `yaml:"atr_multiplier"`    // ATR 乘數
	MinPriceStep    float64 `yaml:"min_price_step"`    // 最小價格间距 (%)
	MaxPriceStep    float64 `yaml:"max_price_step"`    // 最大價格间距 (%)
	
	// 倉位遞增
	SafetyOrderScale float64 `yaml:"safety_order_scale"` // 安全订單遞增倍數 (1.0-2.0)
	SafetyOrderStep  float64 `yaml:"safety_order_step"`  // 安全订單间距遞增 (1.0-2.0)
	
	// 三重止盈
	FirstOrderTakeProfit float64 `yaml:"first_order_take_profit"` // 首單止盈比例 (%)
	LastOrderTakeProfit  float64 `yaml:"last_order_take_profit"`  // 尾單止盈比例 (%)
	TotalTakeProfit      float64 `yaml:"total_take_profit"`       // 全倉止盈比例 (%)
	TrailingTakeProfit   float64 `yaml:"trailing_take_profit"`    // 追踪止盈回撤比例 (%)
	TrailingActivation   float64 `yaml:"trailing_activation"`     // 追踪止盈激活阈值 (%)
	
	// 止损
	StopLoss         float64 `yaml:"stop_loss"`          // 止损比例 (%)
	TrailingStopLoss float64 `yaml:"trailing_stop_loss"` // 追踪止损比例 (%)
	
	// 防瀑布保护
	CascadeProtection     bool    `yaml:"cascade_protection"`      // 啟用瀑布保护
	CascadeDropThreshold  float64 `yaml:"cascade_drop_threshold"`  // 瀑布下跌阈值 (%)
	CascadePauseDuration  int     `yaml:"cascade_pause_duration"`  // 暂停時长 (秒)
	
	// 趨勢過濾
	TrendFilterEnabled bool   `yaml:"trend_filter_enabled"` // 啟用趨勢過濾
	TrendMethod        string `yaml:"trend_method"`         // 趋势判断方法 (ma/ema/macd)
	TrendPeriod        int    `yaml:"trend_period"`         // 趋势周期
}

// DCALayer 分层倉位
type DCALayer struct {
	Index       int       // 层级索引
	Price       float64   // 入场價格
	Quantity    float64   // 持倉數量
	Cost        float64   // 成本
	OrderID     int64     // 订單ID
	Status      string    // 状態: pending/filled/closed
	FilledAt    time.Time // 成交時间
}

// NewDCAEnhancedStrategy 創建增强型 DCA 策略
func NewDCAEnhancedStrategy(
	name string,
	symbol string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *DCAEnhancedStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	dcaCfg := parseDCAConfig(strategyCfg)
	if symbol != "" {
		dcaCfg.Symbol = symbol
	}

	strategy := &DCAEnhancedStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  dcaCfg,
		priceHistory: make([]float64, 0, 200),
		candles:      make([]indicators.Candle, 0, 200),
		layers:       make([]*DCALayer, 0, dcaCfg.MaxSafetyOrders+1),
		maxLayers:    dcaCfg.MaxSafetyOrders + 1,
		atr:          indicators.NewATR(dcaCfg.ATRPeriod),
		baseInterval: dcaCfg.MinPriceStep,
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

// parseDCAConfig 解析 DCA 配置
func parseDCAConfig(cfg map[string]interface{}) *DCAEnhancedConfig {
	dcaCfg := &DCAEnhancedConfig{
		// 默认值
		Symbol:               "BTCUSDT",
		BaseOrderAmount:      100,
		SafetyOrderAmount:    200,
		MaxSafetyOrders:      50,
		ATRPeriod:            14,
		ATRMultiplier:        1.5,
		MinPriceStep:         1.0,
		MaxPriceStep:         5.0,
		SafetyOrderScale:     1.05,
		SafetyOrderStep:      1.0,
		FirstOrderTakeProfit: 1.0,
		LastOrderTakeProfit:  0.5,
		TotalTakeProfit:      2.0,
		TrailingTakeProfit:   0.5,
		TrailingActivation:   1.0,
		StopLoss:             10.0,
		TrailingStopLoss:     2.0,
		CascadeProtection:    true,
		CascadeDropThreshold: 5.0,
		CascadePauseDuration: 300,
		TrendFilterEnabled:   true,
		TrendMethod:          "ema",
		TrendPeriod:          20,
	}

	if cfg == nil {
		return dcaCfg
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
		dcaCfg.Symbol = v
	}

	dcaCfg.BaseOrderAmount = getFloat("base_order_amount", dcaCfg.BaseOrderAmount)
	dcaCfg.SafetyOrderAmount = getFloat("safety_order_amount", dcaCfg.SafetyOrderAmount)
	dcaCfg.MaxSafetyOrders = getInt("max_safety_orders", dcaCfg.MaxSafetyOrders)
	dcaCfg.ATRPeriod = getInt("atr_period", dcaCfg.ATRPeriod)
	dcaCfg.ATRMultiplier = getFloat("atr_multiplier", dcaCfg.ATRMultiplier)
	dcaCfg.MinPriceStep = getFloat("min_price_step", dcaCfg.MinPriceStep)
	dcaCfg.MaxPriceStep = getFloat("max_price_step", dcaCfg.MaxPriceStep)
	dcaCfg.SafetyOrderScale = getFloat("safety_order_scale", dcaCfg.SafetyOrderScale)
	dcaCfg.SafetyOrderStep = getFloat("safety_order_step", dcaCfg.SafetyOrderStep)
	dcaCfg.FirstOrderTakeProfit = getFloat("first_order_take_profit", dcaCfg.FirstOrderTakeProfit)
	dcaCfg.LastOrderTakeProfit = getFloat("last_order_take_profit", dcaCfg.LastOrderTakeProfit)
	dcaCfg.TotalTakeProfit = getFloat("total_take_profit", dcaCfg.TotalTakeProfit)
	dcaCfg.TrailingTakeProfit = getFloat("trailing_take_profit", dcaCfg.TrailingTakeProfit)
	dcaCfg.TrailingActivation = getFloat("trailing_activation", dcaCfg.TrailingActivation)
	dcaCfg.StopLoss = getFloat("stop_loss", dcaCfg.StopLoss)
	dcaCfg.TrailingStopLoss = getFloat("trailing_stop_loss", dcaCfg.TrailingStopLoss)

	if v, ok := cfg["cascade_protection"].(bool); ok {
		dcaCfg.CascadeProtection = v
	}
	dcaCfg.CascadeDropThreshold = getFloat("cascade_drop_threshold", dcaCfg.CascadeDropThreshold)
	dcaCfg.CascadePauseDuration = getInt("cascade_pause_duration", dcaCfg.CascadePauseDuration)

	if v, ok := cfg["trend_filter_enabled"].(bool); ok {
		dcaCfg.TrendFilterEnabled = v
	}
	if v, ok := cfg["trend_method"].(string); ok {
		dcaCfg.TrendMethod = v
	}
	dcaCfg.TrendPeriod = getInt("trend_period", dcaCfg.TrendPeriod)

	return dcaCfg
}

// Name 返回策略名称
func (s *DCAEnhancedStrategy) Name() string {
	return s.name
}

// Initialize 初始化策略
func (s *DCAEnhancedStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	s.cfg = cfg
	s.executor = executor
	s.exchange = exchange
	return nil
}

// SetEventBus 設置事件總線
func (s *DCAEnhancedStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus
}

// Start 啟动策略
func (s *DCAEnhancedStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	s.ctx = ctx
	s.isRunning = true
	s.mu.Unlock()

	logger.Info("✅ [%s] 增强型 DCA 策略已啟动", s.name)
	logger.Info("📊 配置: 最大层數=%d, 基础订單=%.2f, ATR周期=%d",
		s.strategyCfg.MaxSafetyOrders+1,
		s.strategyCfg.BaseOrderAmount,
		s.strategyCfg.ATRPeriod)

	return nil
}

// roundPrice 根據交易所精度格式化價格
func (s *DCAEnhancedStrategy) roundPrice(price float64) float64 {
	decimals := s.exchange.GetPriceDecimals()
	multiplier := math.Pow(10, float64(decimals))
	return math.Round(price*multiplier) / multiplier
}

// Stop 停止策略
func (s *DCAEnhancedStrategy) Stop() error {
	s.mu.Lock()
	s.isRunning = false
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	logger.Info("⏹️ [%s] 增强型 DCA 策略已停止", s.name)
	return nil
}

// OnPriceChange 價格變化处理
func (s *DCAEnhancedStrategy) OnPriceChange(price float64) error {
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

	// 更新 K線數據（简化处理，實際应該從 K線流獲取）
	s.updateCandle(price)

	// 计算动態间距
	s.calculateDynamicInterval()

	// 检查瀑布保护
	if s.isPaused && time.Now().Before(s.pauseUntil) {
		return nil
	}
	s.isPaused = false

	// 检查瀑布下跌
	if s.strategyCfg.CascadeProtection && s.detectCascadeDrop() {
		s.isPaused = true
		s.pauseUntil = time.Now().Add(time.Duration(s.strategyCfg.CascadePauseDuration) * time.Second)
		logger.Warn("⚠️ [%s] 检测到瀑布式下跌，暂停加倉 %d 秒", s.name, s.strategyCfg.CascadePauseDuration)
		return nil
	}

	// 检查止盈止损
	if err := s.checkTakeProfitStopLoss(price); err != nil {
		return err
	}

	// 检查是否需要开倉或加倉
	if len(s.layers) == 0 {
		// 首次开倉
		return s.openBaseOrder(price)
	}

	// 检查是否需要加倉
	return s.checkSafetyOrder(price)
}

// updateCandle 更新 K線數據
func (s *DCAEnhancedStrategy) updateCandle(price float64) {
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
	// 简化处理：每分钟一根 K線
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

// calculateDynamicInterval 计算动態间距
func (s *DCAEnhancedStrategy) calculateDynamicInterval() {
	if len(s.candles) < s.strategyCfg.ATRPeriod+1 {
		s.dynamicInterval = s.strategyCfg.MinPriceStep
		return
	}

	// 使用 ATR 计算动態间距
	atrValue := s.atr.CurrentATR(s.candles)
	if atrValue == 0 || s.lastPrice == 0 {
		s.dynamicInterval = s.strategyCfg.MinPriceStep
		return
	}

	// 动態间距 = ATR / 當前價格 * 100 * 乘數
	dynamicStep := (atrValue / s.lastPrice) * 100 * s.strategyCfg.ATRMultiplier

	// 限制在最小和最大间距之间
	s.dynamicInterval = math.Max(s.strategyCfg.MinPriceStep, math.Min(dynamicStep, s.strategyCfg.MaxPriceStep))
}

// detectCascadeDrop 检测瀑布式下跌
func (s *DCAEnhancedStrategy) detectCascadeDrop() bool {
	if len(s.priceHistory) < 10 {
		return false
	}

	// 计算最近10個價格的最大跌幅
	recent := s.priceHistory[len(s.priceHistory)-10:]
	maxPrice := recent[0]
	for _, p := range recent {
		if p > maxPrice {
			maxPrice = p
		}
	}

	currentDrop := (maxPrice - s.lastPrice) / maxPrice * 100
	return currentDrop >= s.strategyCfg.CascadeDropThreshold
}

// openBaseOrder 开啟基础订單
func (s *DCAEnhancedStrategy) openBaseOrder(price float64) error {
	// 检查趨勢過濾
	if s.strategyCfg.TrendFilterEnabled && !s.isTrendUp() {
		logger.Info("📊 [%s] 趋势向下，暂不开倉", s.name)
		return nil
	}

	// 格式化價格
	orderPrice := s.roundPrice(price)
	quantity := s.strategyCfg.BaseOrderAmount / orderPrice

	// 🔥 精度处理：根據交易所要求的精度截断數量
	qDec := s.exchange.GetQuantityDecimals()
	quantity = math.Floor(quantity*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))

	if quantity <= 0 {
		minQty := math.Pow10(-qDec)
		logger.Error("🚨 [%s] 基础订單數量過小 (%.8f)，低於交易所最小精度 (%.8f)，策略已自动暂停！请在配置中調大 BaseOrderAmount", s.name, quantity, minQty)
		s.isPaused = true
		
		// 发布事件
		if s.eventBus != nil {
			s.eventBus.Publish(&event.Event{
				Type:      event.EventTypePrecisionAdjustment,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"symbol":           s.strategyCfg.Symbol,
					"strategy":         s.name,
					"order_amount":     s.strategyCfg.BaseOrderAmount,
					"calculated_qty":   quantity,
					"min_qty":          minQty,
					"price":            orderPrice,
					"action":           "pause",
					"reason":           "基础订單數量低於交易所最小精度",
				},
			})
		}
		return nil
	}

	layer := &DCALayer{
		Index:    0,
		Price:    orderPrice,
		Quantity: quantity,
		Cost:     s.strategyCfg.BaseOrderAmount,
		Status:   "pending",
	}

	// 下單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     "BUY",
		Quantity: quantity,
		Price:    orderPrice,
	})

	if err != nil {
		logger.Error("❌ [%s] 基础订單下單失败: %v", s.name, err)
		return err
	}

	layer.OrderID = order.OrderID
	layer.Status = "filled"
	layer.FilledAt = time.Now()
	s.layers = append(s.layers, layer)
	s.currentLayer = 1

	s.updateTotals()

	logger.Info("📈 [%s:%s] [%s] 基础订單成交: 價格=%.2f, 數量=%.6f, 成本=%.2f",
		s.exchange.GetName(), s.strategyCfg.Symbol, s.name, price, quantity, s.strategyCfg.BaseOrderAmount)

	return nil
}

// checkSafetyOrder 检查是否需要下安全订單
func (s *DCAEnhancedStrategy) checkSafetyOrder(price float64) error {
	if s.currentLayer >= s.maxLayers {
		return nil // 已达最大层數
	}

	// 计算需要的下跌幅度
	lastLayer := s.layers[len(s.layers)-1]
	requiredDrop := s.getRequiredDrop(s.currentLayer)

	dropPercent := (lastLayer.Price - price) / lastLayer.Price * 100

	if dropPercent < requiredDrop {
		return nil // 未达到加倉条件
	}

	// 计算安全订單金額（遞增）
	orderAmount := s.strategyCfg.SafetyOrderAmount * math.Pow(s.strategyCfg.SafetyOrderScale, float64(s.currentLayer-1))
	orderPrice := s.roundPrice(price)
	quantity := orderAmount / orderPrice

	// 🔥 精度处理：根據交易所要求的精度截断數量
	qDec := s.exchange.GetQuantityDecimals()
	quantity = math.Floor(quantity*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))

	if quantity <= 0 {
		minQty := math.Pow10(-qDec)
		logger.Error("🚨 [%s] 安全订單 #%d 數量過小 (%.8f)，低於交易所最小精度 (%.8f)，策略已自动暂停！", s.name, s.currentLayer, quantity, minQty)
		s.isPaused = true
		
		// 发布事件
		if s.eventBus != nil {
			s.eventBus.Publish(&event.Event{
				Type:      event.EventTypePrecisionAdjustment,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"symbol":           s.strategyCfg.Symbol,
					"strategy":         s.name,
					"layer":            s.currentLayer,
					"order_amount":     orderAmount,
					"calculated_qty":   quantity,
					"min_qty":          minQty,
					"price":            orderPrice,
					"action":           "pause",
					"reason":           "安全订單數量低於交易所最小精度",
				},
			})
		}
		return nil
	}

	layer := &DCALayer{
		Index:    s.currentLayer,
		Price:    orderPrice,
		Quantity: quantity,
		Cost:     orderAmount,
		Status:   "pending",
	}

	// 下單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     "BUY",
		Quantity: quantity,
		Price:    orderPrice,
	})

	if err != nil {
		logger.Error("❌ [%s] 安全订單 #%d 下單失败: %v", s.name, s.currentLayer, err)
		return err
	}

	layer.OrderID = order.OrderID
	layer.Status = "filled"
	layer.FilledAt = time.Now()
	s.layers = append(s.layers, layer)
	s.currentLayer++

	s.updateTotals()

	logger.Info("📉 [%s:%s] [%s] 安全订單 #%d 成交: 價格=%.2f, 數量=%.6f, 成本=%.2f, 平均成本=%.2f",
		s.exchange.GetName(), s.strategyCfg.Symbol, s.name, layer.Index, price, quantity, orderAmount, s.avgEntryPrice)

	return nil
}

// getRequiredDrop 獲取需要的下跌幅度（考虑 ATR 動態調整和遞增）
func (s *DCAEnhancedStrategy) getRequiredDrop(layerIndex int) float64 {
	// 基础间距使用动態 ATR 间距
	baseStep := s.dynamicInterval

	// 应用遞增系數
	requiredDrop := baseStep * math.Pow(s.strategyCfg.SafetyOrderStep, float64(layerIndex-1))

	return requiredDrop
}

// updateTotals 更新總计數據
func (s *DCAEnhancedStrategy) updateTotals() {
	s.totalCost = 0
	s.totalQty = 0

	for _, layer := range s.layers {
		if layer.Status == "filled" {
			s.totalCost += layer.Cost
			s.totalQty += layer.Quantity
		}
	}

	if s.totalQty > 0 {
		s.avgEntryPrice = s.totalCost / s.totalQty
	}
}

// checkTakeProfitStopLoss 检查止盈止损
func (s *DCAEnhancedStrategy) checkTakeProfitStopLoss(price float64) error {
	if len(s.layers) == 0 || s.totalQty == 0 {
		return nil
	}

	// 计算當前盈亏
	currentValue := s.totalQty * price
	pnl := currentValue - s.totalCost
	pnlPercent := pnl / s.totalCost * 100

	// 更新最高盈利点
	if pnlPercent > s.highestProfit {
		s.highestProfit = pnlPercent
	}

	// 1. 首單止盈检查
	if len(s.layers) == 1 && pnlPercent >= s.strategyCfg.FirstOrderTakeProfit {
		logger.Info("💰 [%s] 首單止盈触发: 盈利=%.2f%%", s.name, pnlPercent)
		return s.closeAllPositions(price, "首單止盈")
	}

	// 2. 尾單止盈检查
	if len(s.layers) > 1 {
		lastLayer := s.layers[len(s.layers)-1]
		lastPnlPercent := (price - lastLayer.Price) / lastLayer.Price * 100
		if lastPnlPercent >= s.strategyCfg.LastOrderTakeProfit {
			logger.Info("💰 [%s] 尾單止盈触发: 尾單盈利=%.2f%%", s.name, lastPnlPercent)
			return s.closeAllPositions(price, "尾單止盈")
		}
	}

	// 3. 全倉止盈检查
	if pnlPercent >= s.strategyCfg.TotalTakeProfit {
		logger.Info("💰 [%s] 全倉止盈触发: 總盈利=%.2f%%", s.name, pnlPercent)
		return s.closeAllPositions(price, "全倉止盈")
	}

	// 4. 追踪止盈
	if !s.takeProfitTriggered && pnlPercent >= s.strategyCfg.TrailingActivation {
		s.takeProfitTriggered = true
		logger.Info("🎯 [%s] 追踪止盈激活: 當前盈利=%.2f%%", s.name, pnlPercent)
	}

	if s.takeProfitTriggered {
		drawdown := s.highestProfit - pnlPercent
		if drawdown >= s.strategyCfg.TrailingTakeProfit {
			logger.Info("💰 [%s] 追踪止盈触发: 最高盈利=%.2f%%, 回撤=%.2f%%",
				s.name, s.highestProfit, drawdown)
			return s.closeAllPositions(price, "追踪止盈")
		}
	}

	// 5. 止损检查
	if pnlPercent <= -s.strategyCfg.StopLoss {
		logger.Warn("🛑 [%s] 止损触发: 亏损=%.2f%%", s.name, pnlPercent)
		return s.closeAllPositions(price, "止损")
	}

	return nil
}

// closeAllPositions 平倉所有倉位
func (s *DCAEnhancedStrategy) closeAllPositions(price float64, reason string) error {
	if s.totalQty <= 0 {
		return nil
	}

	// 🔥 精度处理：确保平倉數量符合交易所要求
	qDec := s.exchange.GetQuantityDecimals()
	qty := math.Floor(s.totalQty*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))

	if qty <= 0 {
		return nil
	}

	orderPrice := s.roundPrice(price)

	// 下賣單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:   s.strategyCfg.Symbol,
		Side:     "SELL",
		Quantity: qty,
		Price:    orderPrice,
	})

	if err != nil {
		logger.Error("❌ [%s] 平倉失败: %v", s.name, err)
		return err
	}

	// 计算盈亏
	pnl := s.totalQty*price - s.totalCost

	// 更新统计
	s.stats.TotalTrades++
	s.stats.TotalPnL += pnl
	s.stats.TotalVolume += s.totalCost

	if pnl > 0 {
		// 更新胜率
		winCount := s.stats.WinRate * float64(s.stats.TotalTrades-1)
		winCount++
		s.stats.WinRate = winCount / float64(s.stats.TotalTrades)
	} else {
		winCount := s.stats.WinRate * float64(s.stats.TotalTrades-1)
		s.stats.WinRate = winCount / float64(s.stats.TotalTrades)
	}

	logger.Info("✅ [%s] 平倉完成 (%s): 订單ID=%d, 數量=%.6f, 價格=%.2f, 盈亏=%.2f USDT",
		s.name, reason, order.OrderID, s.totalQty, price, pnl)

	// 重置状態
	s.layers = make([]*DCALayer, 0, s.maxLayers)
	s.totalCost = 0
	s.totalQty = 0
	s.avgEntryPrice = 0
	s.currentLayer = 0
	s.highestProfit = 0
	s.takeProfitTriggered = false

	return nil
}

// isTrendUp 判断趋势是否向上
func (s *DCAEnhancedStrategy) isTrendUp() bool {
	if len(s.priceHistory) < s.strategyCfg.TrendPeriod*2 {
		return true // 數據不足，默认允許开倉
	}

	prices := s.priceHistory[len(s.priceHistory)-s.strategyCfg.TrendPeriod*2:]

	var shortMA, longMA float64
	shortPeriod := s.strategyCfg.TrendPeriod
	longPeriod := s.strategyCfg.TrendPeriod * 2

	switch s.strategyCfg.TrendMethod {
	case "ema":
		shortEMA := indicators.EMA(prices, shortPeriod)
		longEMA := indicators.EMA(prices, longPeriod)
		if shortEMA != nil && len(shortEMA) > 0 {
			shortMA = shortEMA[len(shortEMA)-1]
		}
		if longEMA != nil && len(longEMA) > 0 {
			longMA = longEMA[len(longEMA)-1]
		}
	default: // ma
		shortSMA := indicators.SMA(prices, shortPeriod)
		longSMA := indicators.SMA(prices, longPeriod)
		if shortSMA != nil && len(shortSMA) > 0 {
			shortMA = shortSMA[len(shortSMA)-1]
		}
		if longSMA != nil && len(longSMA) > 0 {
			longMA = longSMA[len(longSMA)-1]
		}
	}

	return shortMA >= longMA
}

// OnOrderUpdate 订單更新处理
func (s *DCAEnhancedStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 查找對应的层级
	for _, layer := range s.layers {
		if layer.OrderID == update.OrderID {
			if update.Status == "FILLED" {
				layer.Status = "filled"
				layer.FilledAt = time.Now()
				logger.Info("📊 [%s] 订單 #%d 成交: 层级=%d", s.name, update.OrderID, layer.Index)
			} else if update.Status == "CANCELED" {
				layer.Status = "canceled"
				logger.Warn("⚠️ [%s] 订單 #%d 已取消: 层级=%d", s.name, update.OrderID, layer.Index)
			}
			break
		}
	}

	return nil
}

// GetPositions 獲取持倉
func (s *DCAEnhancedStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.totalQty <= 0 {
		return []*Position{}
	}

	currentPnL := 0.0
	if s.lastPrice > 0 {
		currentPnL = s.totalQty*s.lastPrice - s.totalCost
	}

	return []*Position{
		{
			Symbol:       s.strategyCfg.Symbol,
			Size:         s.totalQty,
			EntryPrice:   s.avgEntryPrice,
			CurrentPrice: s.lastPrice,
			PnL:          currentPnL,
		},
	}
}

// GetOrders 獲取訂單
func (s *DCAEnhancedStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orders := make([]*Order, 0, len(s.layers))
	for _, layer := range s.layers {
		orders = append(orders, &Order{
			OrderID:  layer.OrderID,
			Symbol:   s.strategyCfg.Symbol,
			Side:     "BUY",
			Price:    layer.Price,
			Quantity: layer.Quantity,
			Status:   layer.Status,
		})
	}

	return orders
}

// GetStatistics 獲取统计
func (s *DCAEnhancedStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// GetLayerInfo 獲取层级信息
func (s *DCAEnhancedStrategy) GetLayerInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("當前层數: %d/%d, 總成本: %.2f, 總持倉: %.6f, 平均成本: %.2f, 动態间距: %.2f%%",
		len(s.layers), s.maxLayers, s.totalCost, s.totalQty, s.avgEntryPrice, s.dynamicInterval)
}

// GetDynamicInterval 獲取當前动態间距
func (s *DCAEnhancedStrategy) GetDynamicInterval() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dynamicInterval
}

// IsPaused 是否暂停加倉
func (s *DCAEnhancedStrategy) IsPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isPaused
}
