package strategy

import (
	"context"
	"sync"

	"opensqt/config"
	"opensqt/logger"
	"opensqt/position"
)

// MomentumStrategy 动量策略
type MomentumStrategy struct {
	name      string
	cfg       *config.Config
	executor  position.OrderExecutorInterface
	exchange  position.IExchange
	strategyCfg map[string]interface{}

	// 价格历史
	priceHistory []float64
	mu           sync.RWMutex

	// RSI 相关
	rsiPeriod   int
	rsiValues   []float64
	overbought  float64
	oversold    float64
	momentumThreshold float64

	// 持仓
	position *Position
	entryPrice float64

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMomentumStrategy 创建动量策略
func NewMomentumStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *MomentumStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	ms := &MomentumStrategy{
		name:        name,
		cfg:         cfg,
		executor:    executor,
		exchange:    exchange,
		strategyCfg: strategyCfg,
		priceHistory: make([]float64, 0, 100),
		rsiValues:    make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
	}

	// 从配置中读取参数
	if rp, ok := strategyCfg["rsi_period"].(int); ok {
		ms.rsiPeriod = rp
	} else {
		ms.rsiPeriod = 14 // 默认14
	}

	if ob, ok := strategyCfg["overbought"].(float64); ok {
		ms.overbought = ob
	} else {
		ms.overbought = 70 // 默认70
	}

	if os, ok := strategyCfg["oversold"].(float64); ok {
		ms.oversold = os
	} else {
		ms.oversold = 30 // 默认30
	}

	if mt, ok := strategyCfg["momentum_threshold"].(float64); ok {
		ms.momentumThreshold = mt
	} else {
		ms.momentumThreshold = 0.5 // 默认0.5
	}

	return ms
}

// Name 返回策略名称
func (ms *MomentumStrategy) Name() string {
	return ms.name
}

// Initialize 初始化策略
func (ms *MomentumStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}

// Start 启动策略
func (ms *MomentumStrategy) Start(ctx context.Context) error {
	logger.Info("✅ [%s] 动量策略已启动 (RSI周期:%d, 超买:%d, 超卖:%d)",
		ms.name, ms.rsiPeriod, int(ms.overbought), int(ms.oversold))
	return nil
}

// Stop 停止策略
func (ms *MomentumStrategy) Stop() error {
	if ms.cancel != nil {
		ms.cancel()
	}
	return nil
}

// addPrice 添加价格
func (ms *MomentumStrategy) addPrice(price float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.priceHistory = append(ms.priceHistory, price)

	// 保持历史记录
	maxHistory := ms.rsiPeriod * 2
	if len(ms.priceHistory) > maxHistory {
		ms.priceHistory = ms.priceHistory[len(ms.priceHistory)-maxHistory:]
	}
}

// calculateRSI 计算RSI
func (ms *MomentumStrategy) calculateRSI() float64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if len(ms.priceHistory) < ms.rsiPeriod+1 {
		return 50 // 默认中性
	}

	// 计算价格变化
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
		return 100 // 没有下跌，RSI为100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// OnPriceChange 价格变化处理
func (ms *MomentumStrategy) OnPriceChange(price float64) error {
	ms.addPrice(price)

	rsi := ms.calculateRSI()
	if rsi == 50 {
		return nil // 数据不足
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// RSI < 30：超卖，买入信号
	if rsi < ms.oversold && ms.position == nil {
		logger.Info("📊 [%s] RSI超卖，买入信号: RSI=%.2f, 价格=%.2f", ms.name, rsi, price)
		// TODO: 实现买入逻辑
		ms.entryPrice = price
		ms.position = &Position{
			Symbol:       ms.cfg.Trading.Symbol,
			Size:         0,
			EntryPrice:   price,
			CurrentPrice: price,
			PnL:          0,
		}
	}

	// RSI > 70：超买，卖出信号
	if rsi > ms.overbought && ms.position != nil {
		logger.Info("📊 [%s] RSI超买，卖出信号: RSI=%.2f, 价格=%.2f", ms.name, rsi, price)
		// TODO: 实现卖出逻辑
		ms.position = nil
		ms.entryPrice = 0
	}

	return nil
}

// OnOrderUpdate 订单更新处理
func (ms *MomentumStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// TODO: 处理订单更新
	return nil
}

// GetPositions 获取持仓
func (ms *MomentumStrategy) GetPositions() []*Position {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if ms.position == nil {
		return []*Position{}
	}

	return []*Position{ms.position}
}

// GetOrders 获取订单
func (ms *MomentumStrategy) GetOrders() []*Order {
	return []*Order{}
}

// GetStatistics 获取统计
func (ms *MomentumStrategy) GetStatistics() *StrategyStatistics {
	return &StrategyStatistics{
		TotalTrades: 0,
		WinRate:     0,
		TotalPnL:    0,
		TotalVolume: 0,
	}
}

