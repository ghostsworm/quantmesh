package strategy

import (
	"context"
	"sync"

	"opensqt/config"
	"opensqt/logger"
	"opensqt/position"
)

// Strategy 策略接口
type Strategy interface {
	Name() string
	Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error
	OnPriceChange(price float64) error
	OnOrderUpdate(update *position.OrderUpdate) error
	GetPositions() []*Position
	GetOrders() []*Order
	GetStatistics() *StrategyStatistics
	Start(ctx context.Context) error
	Stop() error
}

// Position 持仓信息
type Position struct {
	Symbol      string
	Size        float64
	EntryPrice  float64
	CurrentPrice float64
	PnL         float64
}

// Order 订单信息
type Order struct {
	OrderID int64
	Symbol  string
	Side    string
	Price   float64
	Quantity float64
	Status  string
}

// StrategyStatistics 策略统计
type StrategyStatistics struct {
	TotalTrades int
	WinRate     float64
	TotalPnL    float64
	TotalVolume float64
}

// StrategyManager 策略管理器
type StrategyManager struct {
	strategies map[string]Strategy
	allocator  *CapitalAllocator
	dynamicAllocator *DynamicAllocator
	cfg        *config.Config
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewStrategyManager 创建策略管理器
func NewStrategyManager(cfg *config.Config, totalCapital float64) *StrategyManager {
	ctx, cancel := context.WithCancel(context.Background())

	sm := &StrategyManager{
		strategies: make(map[string]Strategy),
		allocator: NewCapitalAllocator(cfg, totalCapital),
		cfg:        cfg,
		ctx:        ctx,
		cancel:     cancel,
	}

	// 如果启用动态分配，创建动态分配器
	if cfg.Strategies.CapitalAllocation.DynamicAllocation.Enabled {
		sm.dynamicAllocator = NewDynamicAllocator(cfg)
	}

	return sm
}

// RegisterStrategy 注册策略
func (sm *StrategyManager) RegisterStrategy(name string, strategy Strategy, weight float64, fixedPool float64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.strategies[name] = strategy

	// 注册到资金分配器
	sm.allocator.RegisterStrategy(name, weight, fixedPool)

	// 注册到动态分配器
	if sm.dynamicAllocator != nil {
		sm.dynamicAllocator.RegisterStrategy(name, weight)
	}
}

// GetStrategy 获取策略
func (sm *StrategyManager) GetStrategy(name string) Strategy {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.strategies[name]
}

// GetAllStrategies 获取所有策略
func (sm *StrategyManager) GetAllStrategies() map[string]Strategy {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]Strategy)
	for name, strategy := range sm.strategies {
		result[name] = strategy
	}
	return result
}

// IsStrategyEnabled 检查策略是否启用
func (sm *StrategyManager) IsStrategyEnabled(name string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	strategyCfg, exists := sm.cfg.Strategies.Configs[name]
	if !exists {
		return false
	}
	return strategyCfg.Enabled
}

// StartAll 启动所有策略
func (sm *StrategyManager) StartAll() error {
	// 1. 分配资金
	sm.allocator.Allocate()

	// 2. 启动每个策略
	sm.mu.RLock()
	for name, strategy := range sm.strategies {
		if sm.IsStrategyEnabled(name) {
			go func(n string, s Strategy) {
				if err := s.Start(sm.ctx); err != nil {
					logger.Error("❌ 策略 %s 启动失败: %v", n, err)
				} else {
					logger.Info("✅ 策略 %s 已启动", n)
				}
			}(name, strategy)
		}
	}
	sm.mu.RUnlock()

	// 3. 启动动态分配（如果启用）
	if sm.dynamicAllocator != nil && sm.cfg.Strategies.CapitalAllocation.DynamicAllocation.Enabled {
		sm.dynamicAllocator.Start(sm.allocator)
		logger.Info("✅ 动态资金分配已启动")
	}

	return nil
}

// StopAll 停止所有策略
func (sm *StrategyManager) StopAll() {
	if sm.cancel != nil {
		sm.cancel()
	}

	sm.mu.RLock()
	for name, strategy := range sm.strategies {
		if err := strategy.Stop(); err != nil {
			logger.Error("❌ 策略 %s 停止失败: %v", name, err)
		}
	}
	sm.mu.RUnlock()

	if sm.dynamicAllocator != nil {
		sm.dynamicAllocator.Stop()
	}
}

// OnPriceChange 价格变化时通知所有策略
func (sm *StrategyManager) OnPriceChange(price float64) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for name, strategy := range sm.strategies {
		if sm.IsStrategyEnabled(name) {
			go func(n string, s Strategy) {
				if err := s.OnPriceChange(price); err != nil {
					logger.Warn("⚠️ 策略 %s 处理价格变化失败: %v", n, err)
				}
			}(name, strategy)
		}
	}
}

// OnOrderUpdate 订单更新时通知所有策略
func (sm *StrategyManager) OnOrderUpdate(update *position.OrderUpdate) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for name, strategy := range sm.strategies {
		if sm.IsStrategyEnabled(name) {
			go func(n string, s Strategy) {
				if err := s.OnOrderUpdate(update); err != nil {
					logger.Warn("⚠️ 策略 %s 处理订单更新失败: %v", n, err)
				}
			}(name, strategy)
		}
	}
}

// GetCapitalAllocator 获取资金分配器
func (sm *StrategyManager) GetCapitalAllocator() *CapitalAllocator {
	return sm.allocator
}

// GetDynamicAllocator 获取动态分配器
func (sm *StrategyManager) GetDynamicAllocator() *DynamicAllocator {
	return sm.dynamicAllocator
}

