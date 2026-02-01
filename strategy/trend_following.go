package strategy

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// TrendFollowingStrategy 趋势跟踪策略
type TrendFollowingStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg map[string]interface{}

	// 價格历史
	priceHistory []float64
	mu           sync.RWMutex

	// 均線
	shortMA []float64
	longMA  []float64

	// 持倉
	position   *Position
	entryPrice float64

	// 参數
	method      string // ma/ema
	shortPeriod int
	longPeriod  int
	stopLoss    float64 // 止损比例
	takeProfit  float64 // 止盈比例
	maxPosition float64 // 最大倉位比例

	isPaused bool
	eventBus EventBus

	ctx    context.Context
	cancel context.CancelFunc
}

// NewTrendFollowingStrategy 創建趋势跟踪策略
func NewTrendFollowingStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *TrendFollowingStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	tfs := &TrendFollowingStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  strategyCfg,
		priceHistory: make([]float64, 0, 100),
		shortMA:      make([]float64, 0, 100),
		longMA:       make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
	}

	// 從配置中读取参數
	if method, ok := strategyCfg["method"].(string); ok {
		tfs.method = method
	} else {
		tfs.method = "ema" // 預設 EMA
	}

	if sp, ok := strategyCfg["short_period"].(int); ok {
		tfs.shortPeriod = sp
	} else {
		tfs.shortPeriod = 10 // 預設 10
	}

	if lp, ok := strategyCfg["long_period"].(int); ok {
		tfs.longPeriod = lp
	} else {
		tfs.longPeriod = 30 // 預設 30
	}

	if sl, ok := strategyCfg["stop_loss"].(float64); ok {
		tfs.stopLoss = sl
	} else {
		tfs.stopLoss = 0.02 // 預設 2%
	}

	if tp, ok := strategyCfg["take_profit"].(float64); ok {
		tfs.takeProfit = tp
	} else {
		tfs.takeProfit = 0.05 // 預設 5%
	}

	if mp, ok := strategyCfg["max_position"].(float64); ok {
		tfs.maxPosition = mp
	} else {
		tfs.maxPosition = 0.3 // 預設 30%
	}

	return tfs
}

// Name 回傳策略名稱
func (tfs *TrendFollowingStrategy) Name() string {
	return tfs.name
}

// SetEventBus 設置事件總線
func (tfs *TrendFollowingStrategy) SetEventBus(bus EventBus) {
	tfs.mu.Lock()
	defer tfs.mu.Unlock()
	tfs.eventBus = bus
}

// Initialize 初始化策略
func (tfs *TrendFollowingStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	// 已在構造函數中初始化
	return nil
}

// Start 啟动策略
func (tfs *TrendFollowingStrategy) Start(ctx context.Context) error {
	logger.Info("✅ [%s] 趋势跟踪策略已啟动 (短期:%d, 长期:%d, 方法:%s)",
		tfs.name, tfs.shortPeriod, tfs.longPeriod, tfs.method)
	return nil
}

// Stop 停止策略
func (tfs *TrendFollowingStrategy) Stop() error {
	if tfs.cancel != nil {
		tfs.cancel()
	}
	return nil
}

// addPrice 新增價格
func (tfs *TrendFollowingStrategy) addPrice(price float64) {
	tfs.mu.Lock()
	defer tfs.mu.Unlock()

	tfs.priceHistory = append(tfs.priceHistory, price)

	// 保持历史記錄在合理範圍内
	maxHistory := tfs.longPeriod * 2
	if len(tfs.priceHistory) > maxHistory {
		// 使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, maxHistory)
		copy(newHistory, tfs.priceHistory[len(tfs.priceHistory)-maxHistory:])
		tfs.priceHistory = newHistory
	}
}

// calculateMA 计算移动平均
func (tfs *TrendFollowingStrategy) calculateMA(period int) float64 {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()

	if len(tfs.priceHistory) < period {
		return 0
	}

	start := len(tfs.priceHistory) - period
	prices := tfs.priceHistory[start:]

	var sum float64
	for _, price := range prices {
		sum += price
	}

	return sum / float64(len(prices))
}

// calculateEMA 计算指數移动平均
func (tfs *TrendFollowingStrategy) calculateEMA(period int) float64 {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()

	if len(tfs.priceHistory) < period {
		return 0
	}

	start := len(tfs.priceHistory) - period
	prices := tfs.priceHistory[start:]

	// 初始值使用简單移动平均
	var sum float64
	for i := 0; i < period && i < len(prices); i++ {
		sum += prices[i]
	}
	ema := sum / float64(period)

	// 计算平滑因子
	multiplier := 2.0 / (float64(period) + 1.0)

	// 计算EMA
	for i := period; i < len(prices); i++ {
		ema = (prices[i] * multiplier) + (ema * (1 - multiplier))
	}

	return ema
}

// detectTrend 检测趋势
func (tfs *TrendFollowingStrategy) detectTrend() Trend {
	var shortMA, longMA float64

	if tfs.method == "ema" {
		shortMA = tfs.calculateEMA(tfs.shortPeriod)
		longMA = tfs.calculateEMA(tfs.longPeriod)
	} else {
		shortMA = tfs.calculateMA(tfs.shortPeriod)
		longMA = tfs.calculateMA(tfs.longPeriod)
	}

	if shortMA == 0 || longMA == 0 {
		return TrendSide
	}

	tfs.mu.RLock()
	currentPrice := tfs.priceHistory[len(tfs.priceHistory)-1]
	tfs.mu.RUnlock()

	if shortMA > longMA && currentPrice > shortMA {
		return TrendUp
	} else if shortMA < longMA && currentPrice < shortMA {
		return TrendDown
	}

	return TrendSide
}

// OnPriceChange 價格變化处理
func (tfs *TrendFollowingStrategy) OnPriceChange(price float64) error {
	if tfs.isPaused {
		return nil
	}
	tfs.addPrice(price)

	trend := tfs.detectTrend()
	if trend == TrendSide {
		return nil
	}

	tfs.mu.Lock()
	defer tfs.mu.Unlock()

	// 检查止损止盈
	if tfs.position != nil && tfs.entryPrice > 0 {
		currentPrice := price
		pnlPercent := (currentPrice - tfs.entryPrice) / tfs.entryPrice

		// 止损
		if pnlPercent <= -tfs.stopLoss {
			logger.Warn("🛑 [%s] 触发止损: 入场價=%.2f, 當前價=%.2f, 亏损=%.2f%%",
				tfs.name, tfs.entryPrice, currentPrice, pnlPercent*100)
			// TODO: 平倉
			tfs.position = nil
			tfs.entryPrice = 0
			return nil
		}

		// 止盈
		if pnlPercent >= tfs.takeProfit {
			logger.Info("💰 [%s] 触发止盈: 入场價=%.2f, 當前價=%.2f, 盈利=%.2f%%",
				tfs.name, tfs.entryPrice, currentPrice, pnlPercent*100)
			// TODO: 平倉
			tfs.position = nil
			tfs.entryPrice = 0
			return nil
		}
	}

	// 趋势向上：开多倉或加倉
	if trend == TrendUp {
		if tfs.position == nil {
			// 开倉
			// TODO: 實現开倉逻辑
			logger.Info("📈 [%s] 上涨趋势，准备开多倉", tfs.name)
		}
	} else if trend == TrendDown {
		// 趋势向下：平倉
		if tfs.position != nil {
			// 平倉
			logger.Info("📉 [%s] 下跌趋势，准备平倉", tfs.name)
			// TODO: 實現平倉逻辑
			tfs.position = nil
			tfs.entryPrice = 0
		}
	}

	return nil
}

// OnOrderUpdate 订單更新处理
func (tfs *TrendFollowingStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// TODO: 处理订單更新
	return nil
}

// GetPositions 獲取持倉
func (tfs *TrendFollowingStrategy) GetPositions() []*Position {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()

	if tfs.position == nil {
		return []*Position{}
	}

	return []*Position{tfs.position}
}

// GetOrders 獲取訂單
func (tfs *TrendFollowingStrategy) GetOrders() []*Order {
	// TODO: 實現订單查詢
	return []*Order{}
}

// GetStatistics 獲取统计
func (tfs *TrendFollowingStrategy) GetStatistics() *StrategyStatistics {
	// TODO: 實現统计计算
	return &StrategyStatistics{
		TotalTrades: 0,
		WinRate:     0,
		TotalPnL:    0,
		TotalVolume: 0,
	}
}
