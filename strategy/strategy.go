package strategy

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	OpeningFee   float64
	CurrentPrice float64
	PnL          float64
}

// Order 订單信息
type Order struct {
	OrderID          int64
	ClientOrderID    string
	Symbol           string
	Side             string
	Price            float64
	Quantity         float64
	Status           string
	FillProgress     position.FillProgress
	FeeVerifiedQty   float64
	FeeProgress      float64
	clientOrderAlias string // broker-qualified CID, also retained before a REST acknowledgement
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
	strategies              map[string]Strategy
	allocator               *CapitalAllocator
	dynamicAllocator        *DynamicAllocator
	cfg                     *config.Config
	mu                      sync.RWMutex
	ctx                     context.Context
	cancel                  context.CancelFunc
	eventBus                EventBus // 新增
	orderUpdateErrorHandler func(strategyName string, err error)
	orderUpdateMu           sync.Mutex
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

func (sm *StrategyManager) SetOrderUpdateErrorHandler(handler func(strategyName string, err error)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.orderUpdateErrorHandler = handler
}

func (sm *StrategyManager) reportOrderUpdateError(strategyName string, err error) {
	if err == nil {
		return
	}
	sm.mu.RLock()
	handler := sm.orderUpdateErrorHandler
	sm.mu.RUnlock()
	if handler != nil {
		handler(strategyName, err)
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
	return sm.isStrategyEnabledLocked(name)
}

func (sm *StrategyManager) isStrategyEnabledLocked(name string) bool {
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

	// 2. Start synchronously so startup/recovery failures reach the Bot runtime.
	type namedStrategy struct {
		name     string
		strategy Strategy
	}
	var enabled []namedStrategy
	sm.mu.RLock()
	for name, strategy := range sm.strategies {
		if sm.isStrategyEnabledLocked(name) {
			enabled = append(enabled, namedStrategy{name: name, strategy: strategy})
		}
	}
	sm.mu.RUnlock()
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].name < enabled[j].name })
	started := make([]namedStrategy, 0, len(enabled))
	for _, item := range enabled {
		if err := item.strategy.Start(sm.ctx); err != nil {
			if stopErr := item.strategy.Stop(); stopErr != nil {
				logger.Error("❌ 回滚启动失败的策略 %s 时停止失败: %v", item.name, stopErr)
			}
			for i := len(started) - 1; i >= 0; i-- {
				if stopErr := started[i].strategy.Stop(); stopErr != nil {
					logger.Error("❌ 启动回滚时停止策略 %s 失败: %v", started[i].name, stopErr)
				}
			}
			return fmt.Errorf("start strategy %s: %w", item.name, err)
		}
		started = append(started, item)
		logger.Info("✅ 策略 %s 已启动", item.name)
	}

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
		if sm.isStrategyEnabledLocked(name) {
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
		if sm.isStrategyEnabledLocked(name) {
			go func(n string, s Strategy) {
				if err := s.OnOrderUpdate(update); err != nil {
					sm.reportOrderUpdateError(n, err)
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
			if sm.isStrategyEnabledLocked(name) {
				go func(n string, s Strategy) {
					if err := s.OnOrderUpdate(update); err != nil {
						sm.reportOrderUpdateError(n, err)
						logger.Warn("⚠️ 策略 %s 处理订單更新失败: %v", n, err)
					}
				}(name, strategy)
			}
		}
		return
	}
	strategy, ok := sm.strategies[strategyName]
	if !ok || !sm.isStrategyEnabledLocked(strategyName) {
		go sm.reportOrderUpdateError(strategyName, fmt.Errorf("owned order routed to unavailable strategy"))
		return
	}
	go func(n string, s Strategy) {
		if err := s.OnOrderUpdate(update); err != nil {
			sm.reportOrderUpdateError(n, err)
			logger.Warn("⚠️ 策略 %s 处理订單更新失败: %v", n, err)
		}
	}(strategyName, strategy)
}

// ApplyOrderUpdateForStrategy applies one exchange event synchronously so the
// caller can hold the shared opening gate on any accounting/persistence error.
func (sm *StrategyManager) ApplyOrderUpdateForStrategy(strategyName string, update *position.OrderUpdate) error {
	if update == nil {
		return nil
	}
	sm.orderUpdateMu.Lock()
	defer sm.orderUpdateMu.Unlock()

	sm.mu.RLock()
	targets := make([]struct {
		name string
		impl Strategy
	}, 0, len(sm.strategies))
	if strategyName != "" {
		impl, ok := sm.strategies[strategyName]
		if !ok || !sm.isStrategyEnabledLocked(strategyName) {
			sm.mu.RUnlock()
			return fmt.Errorf("owned order routed to unavailable strategy %q", strategyName)
		}
		targets = append(targets, struct {
			name string
			impl Strategy
		}{strategyName, impl})
	} else {
		for name, impl := range sm.strategies {
			if sm.isStrategyEnabledLocked(name) {
				targets = append(targets, struct {
					name string
					impl Strategy
				}{name, impl})
			}
		}
	}
	sm.mu.RUnlock()

	var updateErrors []error
	for _, target := range targets {
		if err := target.impl.OnOrderUpdate(update); err != nil {
			updateErrors = append(updateErrors, fmt.Errorf("strategy %s order update: %w", target.name, err))
		}
	}
	return errors.Join(updateErrors...)
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

type strategyRuntimeReporter interface {
	IsRunning() bool
}

func isStrategyActuallyRunning(strategy Strategy, isEnabled bool) bool {
	if !isEnabled {
		return false
	}
	if reporter, ok := strategy.(strategyRuntimeReporter); ok {
		return reporter.IsRunning()
	}
	return isEnabled
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
			IsRunning:         isStrategyActuallyRunning(strategy, isEnabled),
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
		IsRunning:         isStrategyActuallyRunning(strategy, isEnabled),
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
