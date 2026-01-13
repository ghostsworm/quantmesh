package strategy

import (
	"context"
	"math"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// MeanReversionStrategy 均值回归策略
type MeanReversionStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg map[string]interface{}

	// 价格历史
	priceHistory []float64
	mu           sync.RWMutex

	// 参数
	period             int
	stdMultiplier      float64
	reversionThreshold float64

	// 持仓
	position   *Position
	entryPrice float64

	isPaused bool
	eventBus EventBus

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMeanReversionStrategy 创建均值回归策略
func NewMeanReversionStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *MeanReversionStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	mrs := &MeanReversionStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  strategyCfg,
		priceHistory: make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
	}

	// 从配置中读取参数
	if p, ok := strategyCfg["period"].(int); ok {
		mrs.period = p
	} else {
		mrs.period = 20 // 默认20
	}

	if sm, ok := strategyCfg["std_multiplier"].(float64); ok {
		mrs.stdMultiplier = sm
	} else {
		mrs.stdMultiplier = 2.0 // 默认2倍标准差
	}

	if rt, ok := strategyCfg["reversion_threshold"].(float64); ok {
		mrs.reversionThreshold = rt
	} else {
		mrs.reversionThreshold = 0.5 // 默认0.5σ
	}

	return mrs
}

// Name 返回策略名称
func (mrs *MeanReversionStrategy) Name() string {
	return mrs.name
}

// SetEventBus 设置事件总线
func (mrs *MeanReversionStrategy) SetEventBus(bus EventBus) {
	mrs.mu.Lock()
	defer mrs.mu.Unlock()
	mrs.eventBus = bus
}

// Initialize 初始化策略
func (mrs *MeanReversionStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}

// Start 启动策略
func (mrs *MeanReversionStrategy) Start(ctx context.Context) error {
	logger.Info("✅ [%s] 均值回归策略已启动 (周期:%d, 标准差倍数:%.2f)",
		mrs.name, mrs.period, mrs.stdMultiplier)
	return nil
}

// Stop 停止策略
func (mrs *MeanReversionStrategy) Stop() error {
	if mrs.cancel != nil {
		mrs.cancel()
	}
	return nil
}

// addPrice 添加价格
func (mrs *MeanReversionStrategy) addPrice(price float64) {
	mrs.mu.Lock()
	defer mrs.mu.Unlock()

	mrs.priceHistory = append(mrs.priceHistory, price)

	// 保持历史记录
	maxHistory := mrs.period * 2
	if len(mrs.priceHistory) > maxHistory {
		mrs.priceHistory = mrs.priceHistory[len(mrs.priceHistory)-maxHistory:]
	}
}

// calculateMA 计算移动平均
func (mrs *MeanReversionStrategy) calculateMA() float64 {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()

	if len(mrs.priceHistory) < mrs.period {
		return 0
	}

	start := len(mrs.priceHistory) - mrs.period
	prices := mrs.priceHistory[start:]

	var sum float64
	for _, price := range prices {
		sum += price
	}

	return sum / float64(len(prices))
}

// calculateStdDev 计算标准差
func (mrs *MeanReversionStrategy) calculateStdDev(ma float64) float64 {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()

	if len(mrs.priceHistory) < mrs.period {
		return 0
	}

	start := len(mrs.priceHistory) - mrs.period
	prices := mrs.priceHistory[start:]

	var variance float64
	for _, price := range prices {
		diff := price - ma
		variance += diff * diff
	}

	stdDev := math.Sqrt(variance / float64(len(prices)))
	return stdDev
}

// calculateBollingerBands 计算布林带
func (mrs *MeanReversionStrategy) calculateBollingerBands() (upper, middle, lower float64) {
	middle = mrs.calculateMA()
	if middle == 0 {
		return 0, 0, 0
	}

	stdDev := mrs.calculateStdDev(middle)
	upper = middle + stdDev*mrs.stdMultiplier
	lower = middle - stdDev*mrs.stdMultiplier

	return upper, middle, lower
}

// OnPriceChange 价格变化处理
func (mrs *MeanReversionStrategy) OnPriceChange(price float64) error {
	if mrs.isPaused {
		return nil
	}
	mrs.addPrice(price)

	upper, middle, lower := mrs.calculateBollingerBands()
	if upper == 0 || middle == 0 || lower == 0 {
		return nil
	}

	mrs.mu.Lock()
	defer mrs.mu.Unlock()

	// 价格低于下轨：买入信号
	if price < lower && mrs.position == nil {
		logger.Info("📊 [%s] 价格低于下轨，买入信号: 价格=%.2f, 下轨=%.2f", mrs.name, price, lower)
		// TODO: 实现买入逻辑
		mrs.entryPrice = price
		mrs.position = &Position{
			Symbol:       mrs.cfg.Trading.Symbol,
			Size:         0, // TODO: 计算仓位大小
			EntryPrice:   price,
			CurrentPrice: price,
			PnL:          0,
		}
	}

	// 价格高于上轨：卖出信号
	if price > upper && mrs.position != nil {
		logger.Info("📊 [%s] 价格高于上轨，卖出信号: 价格=%.2f, 上轨=%.2f", mrs.name, price, upper)
		// TODO: 实现卖出逻辑
		mrs.position = nil
		mrs.entryPrice = 0
	}

	// 价格回归中轨：平仓
	if mrs.position != nil {
		deviation := math.Abs(price - middle)
		stdDev := mrs.calculateStdDev(middle)
		if deviation < stdDev*mrs.reversionThreshold {
			logger.Info("📊 [%s] 价格回归中轨，平仓: 价格=%.2f, 中轨=%.2f", mrs.name, price, middle)
			// TODO: 实现平仓逻辑
			mrs.position = nil
			mrs.entryPrice = 0
		}
	}

	return nil
}

// OnOrderUpdate 订单更新处理
func (mrs *MeanReversionStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// TODO: 处理订单更新
	return nil
}

// GetPositions 获取持仓
func (mrs *MeanReversionStrategy) GetPositions() []*Position {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()

	if mrs.position == nil {
		return []*Position{}
	}

	return []*Position{mrs.position}
}

// GetOrders 获取订单
func (mrs *MeanReversionStrategy) GetOrders() []*Order {
	return []*Order{}
}

// GetStatistics 获取统计
func (mrs *MeanReversionStrategy) GetStatistics() *StrategyStatistics {
	return &StrategyStatistics{
		TotalTrades: 0,
		WinRate:     0,
		TotalPnL:    0,
		TotalVolume: 0,
	}
}
