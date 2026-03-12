package strategy

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
	"quantmesh/position"
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
	SetEventBus(bus EventBus)                     // 新增：設置事件總線
	GetVisualizationData() map[string]interface{} // 新增：獲取策略可视化數據
}

// EventBus 事件總線接口
type EventBus interface {
	Publish(evt *event.Event)
}

// Position 持倉資訊
type Position struct {
	Symbol       string
	Size         float64
	EntryPrice   float64
	CurrentPrice float64
	PnL          float64
}

// Order 订單信息
type Order struct {
	OrderID  int64
	Symbol   string
	Side     string
	Price    float64
	Quantity float64
	Status   string
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
	strategies       map[string]Strategy
	allocator        *CapitalAllocator
	dynamicAllocator *DynamicAllocator
	cfg              *config.Config
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	eventBus         EventBus // 新增
}

// NewStrategyManager 創建策略管理器
func NewStrategyManager(cfg *config.Config, totalCapital float64) *StrategyManager {
	ctx, cancel := context.WithCancel(context.Background())

	sm := &StrategyManager{
		strategies: make(map[string]Strategy),
		allocator:  NewCapitalAllocator(cfg, totalCapital),
		cfg:        cfg,
		ctx:        ctx,
		cancel:     cancel,
	}

	// 如果啟用动態分配，創建动態分配器
	if cfg.Strategies.CapitalAllocation.DynamicAllocation.Enabled {
		sm.dynamicAllocator = NewDynamicAllocator(cfg)
	}

	return sm
}

// SetEventBus 設置事件總線
func (sm *StrategyManager) SetEventBus(eb EventBus) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.eventBus = eb

	// 同步给所有已注册的策略
	for _, s := range sm.strategies {
		s.SetEventBus(eb)
	}
}

// RegisterStrategy 注册策略
func (sm *StrategyManager) RegisterStrategy(name string, strategy Strategy, weight float64, fixedPool float64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.strategies[name] = strategy

	// 如果已有事件總線，立即設置
	if sm.eventBus != nil {
		strategy.SetEventBus(sm.eventBus)
	}

	// 注册到资金分配器
	sm.allocator.RegisterStrategy(name, weight, fixedPool)

	// 注册到动態分配器
	if sm.dynamicAllocator != nil {
		sm.dynamicAllocator.RegisterStrategy(name, weight)
	}
}

// GetStrategy 獲取策略
func (sm *StrategyManager) GetStrategy(name string) Strategy {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.strategies[name]
}

// GetAllStrategies 獲取所有策略
func (sm *StrategyManager) GetAllStrategies() map[string]Strategy {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]Strategy)
	for name, strategy := range sm.strategies {
		result[name] = strategy
	}
	return result
}

// IsStrategyEnabled 检查策略是否啟用
func (sm *StrategyManager) IsStrategyEnabled(name string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	strategyCfg, exists := sm.cfg.Strategies.Configs[name]
	if !exists {
		return false
	}
	return strategyCfg.Enabled
}

// StartAll 啟动所有策略
func (sm *StrategyManager) StartAll() error {
	// 1. 分配资金
	sm.allocator.Allocate()

	// 2. 啟动每個策略
	sm.mu.RLock()
	for name, strategy := range sm.strategies {
		if sm.IsStrategyEnabled(name) {
			go func(n string, s Strategy) {
				if err := s.Start(sm.ctx); err != nil {
					logger.Error("❌ 策略 %s 啟动失败: %v", n, err)
				} else {
					logger.Info("✅ 策略 %s 已啟动", n)
				}
			}(name, strategy)
		}
	}
	sm.mu.RUnlock()

	// 3. 啟动动態分配（如果啟用）
	if sm.dynamicAllocator != nil && sm.cfg.Strategies.CapitalAllocation.DynamicAllocation.Enabled {
		sm.dynamicAllocator.Start(sm.allocator)
		logger.Info("✅ 动態资金分配已啟动")
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

// OnPriceChange 價格變化時通知所有策略
func (sm *StrategyManager) OnPriceChange(price float64) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for name, strategy := range sm.strategies {
		if sm.IsStrategyEnabled(name) {
			go func(n string, s Strategy) {
				if err := s.OnPriceChange(price); err != nil {
					logger.Warn("⚠️ 策略 %s 处理價格變化失败: %v", n, err)
				}
			}(name, strategy)
		}
	}
}

// OnOrderUpdate 订單更新時通知所有策略
func (sm *StrategyManager) OnOrderUpdate(update *position.OrderUpdate) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for name, strategy := range sm.strategies {
		if sm.IsStrategyEnabled(name) {
			go func(n string, s Strategy) {
				if err := s.OnOrderUpdate(update); err != nil {
					logger.Warn("⚠️ 策略 %s 处理订單更新失败: %v", n, err)
				}
			}(name, strategy)
		}
	}
}

// OnOrderUpdateForStrategy 精確路由到單一策略，若未命中則回退為廣播
func (sm *StrategyManager) OnOrderUpdateForStrategy(strategyName string, update *position.OrderUpdate) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if strategyName == "" {
		for name, strategy := range sm.strategies {
			if sm.IsStrategyEnabled(name) {
				go func(n string, s Strategy) {
					if err := s.OnOrderUpdate(update); err != nil {
						logger.Warn("⚠️ 策略 %s 处理订單更新失败: %v", n, err)
					}
				}(name, strategy)
			}
		}
		return
	}
	strategy, ok := sm.strategies[strategyName]
	if !ok || !sm.IsStrategyEnabled(strategyName) {
		return
	}
	go func(n string, s Strategy) {
		if err := s.OnOrderUpdate(update); err != nil {
			logger.Warn("⚠️ 策略 %s 处理订單更新失败: %v", n, err)
		}
	}(strategyName, strategy)
}

// GetCapitalAllocator 獲取资金分配器
func (sm *StrategyManager) GetCapitalAllocator() *CapitalAllocator {
	return sm.allocator
}

// GetDynamicAllocator 獲取动態分配器
func (sm *StrategyManager) GetDynamicAllocator() *DynamicAllocator {
	return sm.dynamicAllocator
}

// StrategyRuntimeStatus 策略運行時狀態
type StrategyRuntimeStatus struct {
	Name              string                 `json:"name"`
	Type              string                 `json:"type"`
	IsEnabled         bool                   `json:"isEnabled"`
	IsRunning         bool                   `json:"isRunning"`
	Weight            float64                `json:"weight"`
	AllocatedFunds    float64                `json:"allocatedFunds"`
	UsedFunds         float64                `json:"usedFunds"`
	AvailableFunds    float64                `json:"availableFunds"`
	Positions         []*Position            `json:"positions"`
	Orders            []*Order               `json:"orders"`
	Statistics        *StrategyStatistics    `json:"statistics"`
	PositionCount     int                    `json:"positionCount"`
	OrderCount        int                    `json:"orderCount"`
	VisualizationData map[string]interface{} `json:"visualizationData,omitempty"` // 新增：策略可视化數據
}

// GetAllStrategyStatus 獲取所有策略的運行狀態
func (sm *StrategyManager) GetAllStrategyStatus() []StrategyRuntimeStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	statuses := make([]StrategyRuntimeStatus, 0, len(sm.strategies))

	for name, strategy := range sm.strategies {
		// 獲取策略配置
		strategyCfg, exists := sm.cfg.Strategies.Configs[name]
		isEnabled := exists && strategyCfg.Enabled
		weight := 0.0
		strategyType := name
		if exists {
			weight = strategyCfg.Weight
			if strategyCfg.Type != "" {
				strategyType = strategyCfg.Type
			}
		}

		// 獲取資金分配信息
		allocatedFunds := 0.0
		usedFunds := 0.0
		availableFunds := 0.0
		if sm.allocator != nil {
			allocatedFunds = sm.allocator.GetAllocated(name)
			usedFunds = sm.allocator.GetUsed(name)
			availableFunds = sm.allocator.GetAvailable(name)
		}

		// 獲取策略數據
		positions := strategy.GetPositions()
		orders := strategy.GetOrders()
		stats := strategy.GetStatistics()
		visualizationData := strategy.GetVisualizationData()

		status := StrategyRuntimeStatus{
			Name:              name,
			Type:              strategyType,
			IsEnabled:         isEnabled,
			IsRunning:         isEnabled, // 如果啟用就是運行中
			Weight:            weight,
			AllocatedFunds:    allocatedFunds,
			UsedFunds:         usedFunds,
			AvailableFunds:    availableFunds,
			Positions:         positions,
			Orders:            orders,
			Statistics:        stats,
			PositionCount:     len(positions),
			OrderCount:        len(orders),
			VisualizationData: visualizationData,
		}

		statuses = append(statuses, status)
	}

	return statuses
}

// GetStrategyStatus 獲取單個策略的運行狀態
func (sm *StrategyManager) GetStrategyStatus(name string) *StrategyRuntimeStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	strategy, exists := sm.strategies[name]
	if !exists {
		return nil
	}

	// 獲取策略配置
	strategyCfg, cfgExists := sm.cfg.Strategies.Configs[name]
	isEnabled := cfgExists && strategyCfg.Enabled
	weight := 0.0
	strategyType := name
	if cfgExists {
		weight = strategyCfg.Weight
		if strategyCfg.Type != "" {
			strategyType = strategyCfg.Type
		}
	}

	// 獲取資金分配信息
	allocatedFunds := 0.0
	usedFunds := 0.0
	availableFunds := 0.0
	if sm.allocator != nil {
		allocatedFunds = sm.allocator.GetAllocated(name)
		usedFunds = sm.allocator.GetUsed(name)
		availableFunds = sm.allocator.GetAvailable(name)
	}

	// 獲取策略數據
	positions := strategy.GetPositions()
	orders := strategy.GetOrders()
	stats := strategy.GetStatistics()
	visualizationData := strategy.GetVisualizationData()

	return &StrategyRuntimeStatus{
		Name:              name,
		Type:              strategyType,
		IsEnabled:         isEnabled,
		IsRunning:         isEnabled,
		Weight:            weight,
		AllocatedFunds:    allocatedFunds,
		UsedFunds:         usedFunds,
		AvailableFunds:    availableFunds,
		Positions:         positions,
		Orders:            orders,
		Statistics:        stats,
		PositionCount:     len(positions),
		OrderCount:        len(orders),
		VisualizationData: visualizationData,
	}
}
