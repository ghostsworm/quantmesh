package strategy

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// MomentumStrategy 动量策略
type MomentumStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg map[string]interface{}

	// 價格历史
	priceHistory []float64
	mu           sync.RWMutex

	// RSI 相关
	rsiPeriod         int
	rsiValues         []float64
	overbought        float64
	oversold          float64
	momentumThreshold float64

	// 持倉
	position   *Position
	entryPrice float64

	isPaused  bool
	isRunning bool
	eventBus  EventBus

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMomentumStrategy 創建动量策略
func NewMomentumStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *MomentumStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	ms := &MomentumStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  strategyCfg,
		priceHistory: make([]float64, 0, 100),
		rsiValues:    make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
	}

	// 從配置中读取参數
	if rp, ok := strategyCfg["rsi_period"].(int); ok {
		ms.rsiPeriod = rp
	} else {
		ms.rsiPeriod = 14 // 預設 14
	}

	if ob, ok := strategyCfg["overbought"].(float64); ok {
		ms.overbought = ob
	} else {
		ms.overbought = 70 // 預設 70
	}

	if os, ok := strategyCfg["oversold"].(float64); ok {
		ms.oversold = os
	} else {
		ms.oversold = 30 // 預設 30
	}

	if mt, ok := strategyCfg["momentum_threshold"].(float64); ok {
		ms.momentumThreshold = mt
	} else {
		ms.momentumThreshold = 0.5 // 預設 0.5
	}

	return ms
}

// Name 回傳策略名稱
func (ms *MomentumStrategy) Name() string {
	return ms.name
}

// SetEventBus 設置事件總線
func (ms *MomentumStrategy) SetEventBus(bus EventBus) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.eventBus = bus
}

// Initialize 初始化策略
func (ms *MomentumStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}

// Start 啟动策略
func (ms *MomentumStrategy) Start(ctx context.Context) error {
	ms.mu.Lock()
	ms.isRunning = true
	ms.mu.Unlock()

	logger.Warn("⚠️ [%s] 动量策略以信号模式啟动，当前不会自动下单 (RSI周期:%d, 超買:%d, 超賣:%d)",
		ms.name, ms.rsiPeriod, int(ms.overbought), int(ms.oversold))
	return nil
}

// Stop 停止策略
func (ms *MomentumStrategy) Stop() error {
	ms.mu.Lock()
	ms.isRunning = false
	ms.mu.Unlock()

	if ms.cancel != nil {
		ms.cancel()
	}
	return nil
}

// IsRunning 回傳策略是否已成功啟动
func (ms *MomentumStrategy) IsRunning() bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.isRunning
}

// addPrice 新增價格
func (ms *MomentumStrategy) addPrice(price float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.priceHistory = append(ms.priceHistory, price)

	// 保持历史記錄
	maxHistory := ms.rsiPeriod * 2
	if len(ms.priceHistory) > maxHistory {
		// 使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, maxHistory)
		copy(newHistory, ms.priceHistory[len(ms.priceHistory)-maxHistory:])
		ms.priceHistory = newHistory
	}
}

// calculateRSI 计算RSI
func (ms *MomentumStrategy) calculateRSI() float64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if len(ms.priceHistory) < ms.rsiPeriod+1 {
		return 50 // 預設中性
	}

	// 计算價格變化
	gains := make([]float64, 0)
	losses := make([]float64, 0)

	for i := len(ms.priceHistory) - ms.rsiPeriod; i < len(ms.priceHistory); i++ {
		if i == 0 {
			continue
		}
		change := ms.priceHistory[i] - ms.priceHistory[i-1]
		if change > 0 {
			gains = append(gains, change)
			losses = append(losses, 0)
		} else {
			gains = append(gains, 0)
			losses = append(losses, -change)
		}
	}

	// 计算平均上涨和平均下跌
	var avgGain, avgLoss float64
	for _, gain := range gains {
		avgGain += gain
	}
	for _, loss := range losses {
		avgLoss += loss
	}

	avgGain /= float64(len(gains))
	avgLoss /= float64(len(losses))

	if avgLoss == 0 {
		return 100 // 没有下跌，RSI為100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// OnPriceChange 價格變化处理
func (ms *MomentumStrategy) OnPriceChange(price float64) error {
	ms.mu.RLock()
	if !ms.isRunning || ms.isPaused {
		ms.mu.RUnlock()
		return nil
	}
	ms.mu.RUnlock()
	ms.addPrice(price)

	rsi := ms.calculateRSI()
	if rsi == 50 {
		return nil // 數據不足
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// RSI < 30：超賣，買入信号
	if rsi < ms.oversold && ms.position == nil {
		logger.Info("📊 [%s] RSI超賣，買入信号: RSI=%.2f, 價格=%.2f", ms.name, rsi, price)
		// TODO: 實現買入逻辑
		ms.entryPrice = price
		ms.position = &Position{
			Symbol:       ms.cfg.Trading.Symbol,
			Size:         0,
			EntryPrice:   price,
			CurrentPrice: price,
			PnL:          0,
		}
	}

	// RSI > 70：超買，賣出信号
	if rsi > ms.overbought && ms.position != nil {
		logger.Info("📊 [%s] RSI超買，賣出信号: RSI=%.2f, 價格=%.2f", ms.name, rsi, price)
		// TODO: 實現賣出逻辑
		ms.position = nil
		ms.entryPrice = 0
	}

	return nil
}

// OnOrderUpdate 订單更新处理
func (ms *MomentumStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// TODO: 处理订單更新
	return nil
}

// GetPositions 獲取持倉
func (ms *MomentumStrategy) GetPositions() []*Position {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if ms.position == nil {
		return []*Position{}
	}

	return []*Position{ms.position}
}

// GetOrders 獲取訂單
func (ms *MomentumStrategy) GetOrders() []*Order {
	return []*Order{}
}

// GetStatistics 獲取统计
func (ms *MomentumStrategy) GetStatistics() *StrategyStatistics {
	return &StrategyStatistics{
		TotalTrades: 0,
		WinRate:     0,
		TotalPnL:    0,
		TotalVolume: 0,
	}
}

// GetVisualizationData 獲取策略可视化數據
func (ms *MomentumStrategy) GetVisualizationData() map[string]interface{} {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	currentPrice := 0.0
	if len(ms.priceHistory) > 0 {
		currentPrice = ms.priceHistory[len(ms.priceHistory)-1]
	}
	hasPosition := ms.position != nil
	entryPrice := ms.entryPrice

	return map[string]interface{}{
		"currentPrice":       currentPrice,
		"rsiPeriod":          ms.rsiPeriod,
		"overbought":         ms.overbought,
		"oversold":           ms.oversold,
		"momentumThreshold":  ms.momentumThreshold,
		"hasPosition":        hasPosition,
		"entryPrice":         entryPrice,
		"executionMode":      "signal_only",
		"autoTradingEnabled": false,
		"isRunning":          ms.isRunning,
	}
}
