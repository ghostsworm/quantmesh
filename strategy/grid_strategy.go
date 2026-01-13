package strategy

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// GridStrategy 网格策略包装
type GridStrategy struct {
	name     string
	cfg      *config.Config
	executor position.OrderExecutorInterface
	exchange position.IExchange
	manager  *position.SuperPositionManager
	eventBus EventBus

	mu        sync.RWMutex
	ctx       context.Context
	isRunning bool
	isPaused  bool // 暂停标志
}

// NewGridStrategy 创建网格策略
func NewGridStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	manager *position.SuperPositionManager,
) *GridStrategy {
	return &GridStrategy{
		name:     name,
		cfg:      cfg,
		executor: executor,
		exchange: exchange,
		manager:  manager,
		ctx:      context.Background(),
	}
}

// Name 返回策略名称
func (gs *GridStrategy) Name() string {
	return gs.name
}

// SetEventBus 设置事件总线
func (gs *GridStrategy) SetEventBus(bus EventBus) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.eventBus = bus
}

// Initialize 初始化策略
func (gs *GridStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	// 已在构造函数中初始化
	return nil
}

// Start 启动策略
func (gs *GridStrategy) Start(ctx context.Context) error {
	gs.mu.Lock()
	gs.ctx = ctx
	gs.mu.Unlock()

	logger.Info("✅ [%s] 网格策略已启动", gs.name)
	return nil
}

// Stop 停止策略
func (gs *GridStrategy) Stop() error {
	logger.Info("⏹️ [%s] 网格策略已停止", gs.name)
	return nil
}

// OnPriceChange 价格变化处理
func (gs *GridStrategy) OnPriceChange(price float64) error {
	gs.mu.Lock()
	if gs.isPaused {
		gs.mu.Unlock()
		return nil
	}
	gs.mu.Unlock()

	// 调用 SuperPositionManager 的 AdjustOrders
	return gs.manager.AdjustOrders(price)
}

// OnOrderUpdate 订单更新处理
func (gs *GridStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// 调用 SuperPositionManager 的 OnOrderUpdate（需要传递值类型）
	gs.manager.OnOrderUpdate(*update)
	return nil
}

// GetPositions 获取持仓
func (gs *GridStrategy) GetPositions() []*Position {
	// 从 SuperPositionManager 获取持仓信息
	// TODO: 实现从 SuperPositionManager 获取持仓的逻辑
	// 目前返回空，因为 SuperPositionManager 的持仓信息结构不同
	return []*Position{}
}

// GetOrders 获取订单
func (gs *GridStrategy) GetOrders() []*Order {
	// TODO: 实现从 SuperPositionManager 获取订单的逻辑
	return []*Order{}
}

// GetStatistics 获取统计
func (gs *GridStrategy) GetStatistics() *StrategyStatistics {
	// TODO: 实现从 SuperPositionManager 获取统计的逻辑
	return &StrategyStatistics{
		TotalTrades: 0,
		WinRate:     0,
		TotalPnL:    0,
		TotalVolume: 0,
	}
}

// GetManager 获取 SuperPositionManager（用于外部访问）
func (gs *GridStrategy) GetManager() *position.SuperPositionManager {
	return gs.manager
}
