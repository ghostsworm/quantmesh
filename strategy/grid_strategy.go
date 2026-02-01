package strategy

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// GridStrategy 網格策略包装
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
	isPaused  bool // 暂停標志
}

// NewGridStrategy 創建網格策略
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

// Name 回傳策略名稱
func (gs *GridStrategy) Name() string {
	return gs.name
}

// SetEventBus 設置事件總線
func (gs *GridStrategy) SetEventBus(bus EventBus) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.eventBus = bus
}

// Initialize 初始化策略
func (gs *GridStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	// 已在構造函數中初始化
	return nil
}

// Start 啟动策略
func (gs *GridStrategy) Start(ctx context.Context) error {
	gs.mu.Lock()
	gs.ctx = ctx
	gs.mu.Unlock()

	logger.Info("✅ [%s] 網格策略已啟动", gs.name)
	return nil
}

// Stop 停止策略
func (gs *GridStrategy) Stop() error {
	logger.Info("⏹️ [%s] 網格策略已停止", gs.name)
	return nil
}

// OnPriceChange 價格變化处理
func (gs *GridStrategy) OnPriceChange(price float64) error {
	gs.mu.Lock()
	if gs.isPaused {
		gs.mu.Unlock()
		return nil
	}
	gs.mu.Unlock()

	// 調用 SuperPositionManager 的 AdjustOrders
	return gs.manager.AdjustOrders(price)
}

// OnOrderUpdate 订單更新处理
func (gs *GridStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// 調用 SuperPositionManager 的 OnOrderUpdate（需要傳遞值類型）
	gs.manager.OnOrderUpdate(*update)
	return nil
}

// GetPositions 獲取持倉
func (gs *GridStrategy) GetPositions() []*Position {
	// 從 SuperPositionManager 獲取持倉信息
	// TODO: 實現從 SuperPositionManager 獲取持倉的逻辑
	// 目前返回空，因為 SuperPositionManager 的持倉資訊結構不同
	return []*Position{}
}

// GetOrders 獲取訂單
func (gs *GridStrategy) GetOrders() []*Order {
	// TODO: 實現從 SuperPositionManager 獲取訂單的逻辑
	return []*Order{}
}

// GetStatistics 獲取统计
func (gs *GridStrategy) GetStatistics() *StrategyStatistics {
	// TODO: 實現從 SuperPositionManager 獲取统计的逻辑
	return &StrategyStatistics{
		TotalTrades: 0,
		WinRate:     0,
		TotalPnL:    0,
		TotalVolume: 0,
	}
}

// GetManager 獲取 SuperPositionManager（用於外部访问）
func (gs *GridStrategy) GetManager() *position.SuperPositionManager {
	return gs.manager
}
