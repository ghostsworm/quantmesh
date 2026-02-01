package strategy

import (
	"context"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// StrategyCapital 策略资金
type StrategyCapital struct {
	Allocated float64 // 分配的资金
	Used      float64 // 已使用的资金（保证金）
	Available float64 // 可用资金
	Weight    float64 // 权重
	FixedPool float64 // 固定资金池（如果指定）
	mu        sync.RWMutex
}

// CapitalAllocator 资金分配器
type CapitalAllocator struct {
	totalCapital float64
	strategies   map[string]*StrategyCapital
	cfg          *config.Config
	mu           sync.RWMutex
}

// NewCapitalAllocator 创建资金分配器
func NewCapitalAllocator(cfg *config.Config, totalCapital float64) *CapitalAllocator {
	return &CapitalAllocator{
		totalCapital: totalCapital,
		strategies:   make(map[string]*StrategyCapital),
		cfg:          cfg,
	}
}

// RegisterStrategy 注册策略
func (ca *CapitalAllocator) RegisterStrategy(name string, weight float64, fixedPool float64) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	ca.strategies[name] = &StrategyCapital{
		Weight:    weight,
		FixedPool: fixedPool,
		Allocated: 0,
		Used:      0,
		Available: 0,
	}
}

// Allocate 分配资金（固定比例）
func (ca *CapitalAllocator) Allocate() {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	// 计算固定资金池总额
	fixedPoolTotal := 0.0
	weightTotal := 0.0

	for _, capital := range ca.strategies {
		if capital.FixedPool > 0 {
			fixedPoolTotal += capital.FixedPool
		} else {
			weightTotal += capital.Weight
		}
	}

	// 剩余资金用于权重分配
	remainingCapital := ca.totalCapital - fixedPoolTotal

	// 分配资金
	for name, capital := range ca.strategies {
		if capital.FixedPool > 0 {
			// 使用固定资金池
			capital.Allocated = capital.FixedPool
		} else if weightTotal > 0 {
			// 按权重分配
			capital.Allocated = remainingCapital * (capital.Weight / weightTotal)
		} else {
			capital.Allocated = 0
		}

		capital.Available = capital.Allocated - capital.Used

		logger.Info("💰 [资金分配] 策略 %s: 分配=%.2f, 已用=%.2f, 可用=%.2f (权重=%.2f%%)",
			name, capital.Allocated, capital.Used, capital.Available, capital.Weight*100)
	}
}

// CheckAvailable 检查策略可用资金
func (ca *CapitalAllocator) CheckAvailable(strategyName string, amount float64) bool {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	capital, exists := ca.strategies[strategyName]
	if !exists {
		return false
	}

	capital.mu.RLock()
	defer capital.mu.RUnlock()

	return capital.Available >= amount
}

// Reserve 预留资金
func (ca *CapitalAllocator) Reserve(strategyName string, amount float64) bool {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	capital, exists := ca.strategies[strategyName]
	if !exists {
		return false
	}

	capital.mu.Lock()
	defer capital.mu.Unlock()

	if capital.Available < amount {
		return false
	}

	capital.Used += amount
	capital.Available = capital.Allocated - capital.Used
	return true
}

// Release 释放资金
func (ca *CapitalAllocator) Release(strategyName string, amount float64) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	capital, exists := ca.strategies[strategyName]
	if !exists {
		return
	}

	capital.mu.Lock()
	defer capital.mu.Unlock()

	if capital.Used >= amount {
		capital.Used -= amount
	} else {
		capital.Used = 0
	}
	capital.Available = capital.Allocated - capital.Used
}

// GetAvailable 获取可用资金
func (ca *CapitalAllocator) GetAvailable(strategyName string) float64 {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	capital, exists := ca.strategies[strategyName]
	if !exists {
		return 0
	}

	capital.mu.RLock()
	defer capital.mu.RUnlock()

	return capital.Available
}

// GetUsed 获取已用资金
func (ca *CapitalAllocator) GetUsed(strategyName string) float64 {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	capital, exists := ca.strategies[strategyName]
	if !exists {
		return 0
	}

	capital.mu.RLock()
	defer capital.mu.RUnlock()

	return capital.Used
}

// GetAllStrategiesCapital 获取所有策略资金信息
func (ca *CapitalAllocator) GetAllStrategiesCapital() map[string]*StrategyCapital {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	result := make(map[string]*StrategyCapital)
	for name, capital := range ca.strategies {
		capital.mu.RLock()
		result[name] = &StrategyCapital{
			Allocated: capital.Allocated,
			Used:      capital.Used,
			Available: capital.Available,
			Weight:    capital.Weight,
			FixedPool: capital.FixedPool,
		}
		capital.mu.RUnlock()
	}
	return result
}

// StrategyPerformance 策略表现
type StrategyPerformance struct {
	TotalPnL      float64
	WinRate       float64
	SharpeRatio   float64
	MaxDrawdown   float64
	CurrentWeight float64
	TargetWeight  float64
	TotalTrades   int
	WinningTrades int
	LosingTrades  int
	mu            sync.RWMutex
}

// DynamicAllocator 动态分配器
type DynamicAllocator struct {
	strategies            map[string]*StrategyPerformance
	rebalanceInterval     time.Duration
	maxChangePerRebalance float64
	minWeight             float64
	maxWeight             float64
	performanceWeights    map[string]float64
	ctx                   context.Context
	cancel                context.CancelFunc
	mu                    sync.RWMutex
}

// NewDynamicAllocator 创建动态分配器
func NewDynamicAllocator(cfg *config.Config) *DynamicAllocator {
	ctx, cancel := context.WithCancel(context.Background())

	da := &DynamicAllocator{
		strategies:            make(map[string]*StrategyPerformance),
		rebalanceInterval:     time.Duration(cfg.Strategies.CapitalAllocation.DynamicAllocation.RebalanceInterval) * time.Second,
		maxChangePerRebalance: cfg.Strategies.CapitalAllocation.DynamicAllocation.MaxChangePerRebalance,
		minWeight:             cfg.Strategies.CapitalAllocation.DynamicAllocation.MinWeight,
		maxWeight:             cfg.Strategies.CapitalAllocation.DynamicAllocation.MaxWeight,
		performanceWeights:    cfg.Strategies.CapitalAllocation.DynamicAllocation.PerformanceWeights,
		ctx:                   ctx,
		cancel:                cancel,
	}

	if da.rebalanceInterval <= 0 {
		da.rebalanceInterval = 3600 * time.Second // 默认1小时
	}
	if da.maxChangePerRebalance <= 0 {
		da.maxChangePerRebalance = 0.05 // 默认5%
	}
	if da.minWeight <= 0 {
		da.minWeight = 0.1 // 默认10%
	}
	if da.maxWeight <= 0 {
		da.maxWeight = 0.7 // 默认70%
	}

	// 设置默认性能权重
	if da.performanceWeights == nil {
		da.performanceWeights = map[string]float64{
			"total_pnl":    0.4,
			"sharpe_ratio": 0.3,
			"win_rate":     0.2,
			"max_drawdown": 0.1,
		}
	}

	return da
}

// RegisterStrategy 注册策略
func (da *DynamicAllocator) RegisterStrategy(name string, initialWeight float64) {
	da.mu.Lock()
	defer da.mu.Unlock()

	da.strategies[name] = &StrategyPerformance{
		CurrentWeight: initialWeight,
		TargetWeight:  initialWeight,
		TotalPnL:      0,
		WinRate:       0,
		SharpeRatio:   0,
		MaxDrawdown:   0,
		TotalTrades:   0,
		WinningTrades: 0,
		LosingTrades:  0,
	}
}

// UpdatePerformance 更新策略表现
func (da *DynamicAllocator) UpdatePerformance(strategyName string, pnl float64, isWin bool) {
	da.mu.Lock()
	defer da.mu.Unlock()

	perf, exists := da.strategies[strategyName]
	if !exists {
		return
	}

	perf.mu.Lock()
	defer perf.mu.Unlock()

	perf.TotalPnL += pnl
	perf.TotalTrades++

	if isWin {
		perf.WinningTrades++
	} else {
		perf.LosingTrades++
	}

	if perf.TotalTrades > 0 {
		perf.WinRate = float64(perf.WinningTrades) / float64(perf.TotalTrades)
	}

	// TODO: 计算夏普比率和最大回撤
}

// CalculateTargetWeights 计算目标权重
func (da *DynamicAllocator) CalculateTargetWeights() map[string]float64 {
	da.mu.RLock()
	defer da.mu.RUnlock()

	scores := make(map[string]float64)

	for name, perf := range da.strategies {
		perf.mu.RLock()
		score := da.calculateScore(perf)
		scores[name] = score
		perf.mu.RUnlock()
	}

	// 归一化权重
	totalScore := 0.0
	for _, score := range scores {
		if score > 0 {
			totalScore += score
		}
	}

	if totalScore == 0 {
		// 如果所有策略得分都为0，使用当前权重
		result := make(map[string]float64)
		for name, perf := range da.strategies {
			perf.mu.RLock()
			result[name] = perf.CurrentWeight
			perf.mu.RUnlock()
		}
		return result
	}

	result := make(map[string]float64)
	for name, score := range scores {
		if score > 0 {
			weight := score / totalScore
			// 限制在最小和最大权重之间
			if weight < da.minWeight {
				weight = da.minWeight
			}
			if weight > da.maxWeight {
				weight = da.maxWeight
			}
			result[name] = weight
		} else {
			// 得分<=0的策略使用最小权重
			result[name] = da.minWeight
		}
	}

	// 再次归一化（因为可能有最小权重限制）
	totalWeight := 0.0
	for _, weight := range result {
		totalWeight += weight
	}
	if totalWeight > 0 {
		for name := range result {
			result[name] = result[name] / totalWeight
		}
	}

	return result
}

// calculateScore 计算策略得分
func (da *DynamicAllocator) calculateScore(perf *StrategyPerformance) float64 {
	score := 0.0

	// 总盈亏得分（越高越好）
	if pnlWeight, ok := da.performanceWeights["total_pnl"]; ok && pnlWeight > 0 {
		// 归一化到0-1范围（假设最大盈亏为总资金的10%）
		pnlScore := math.Max(0, math.Min(1, perf.TotalPnL/1000))
		score += pnlScore * pnlWeight
	}

	// 胜率得分（越高越好）
	if winRateWeight, ok := da.performanceWeights["win_rate"]; ok && winRateWeight > 0 {
		score += perf.WinRate * winRateWeight
	}

	// 夏普比率得分（越高越好）
	if sharpeWeight, ok := da.performanceWeights["sharpe_ratio"]; ok && sharpeWeight > 0 {
		// 归一化夏普比率（假设范围0-3）
		sharpeScore := math.Max(0, math.Min(1, perf.SharpeRatio/3))
		score += sharpeScore * sharpeWeight
	}

	// 最大回撤得分（越小越好，所以取反）
	if drawdownWeight, ok := da.performanceWeights["max_drawdown"]; ok && drawdownWeight > 0 {
		// 回撤越小得分越高
		drawdownScore := math.Max(0, 1-math.Abs(perf.MaxDrawdown))
		score += drawdownScore * drawdownWeight
	}

	return score
}

// Rebalance 重新平衡（平滑调整）
func (da *DynamicAllocator) Rebalance(targetWeights map[string]float64) map[string]float64 {
	da.mu.Lock()
	defer da.mu.Unlock()

	adjustedWeights := make(map[string]float64)

	for name, targetWeight := range targetWeights {
		perf, exists := da.strategies[name]
		if !exists {
			continue
		}

		perf.mu.Lock()
		currentWeight := perf.CurrentWeight
		diff := targetWeight - currentWeight

		// 平滑调整：每次调整不超过 maxChangePerRebalance
		if math.Abs(diff) > da.maxChangePerRebalance {
			if diff > 0 {
				currentWeight += da.maxChangePerRebalance
			} else {
				currentWeight -= da.maxChangePerRebalance
			}
		} else {
			currentWeight = targetWeight
		}

		// 确保在合理范围内
		if currentWeight < da.minWeight {
			currentWeight = da.minWeight
		}
		if currentWeight > da.maxWeight {
			currentWeight = da.maxWeight
		}

		perf.CurrentWeight = currentWeight
		perf.TargetWeight = targetWeight
		adjustedWeights[name] = currentWeight
		perf.mu.Unlock()

		if math.Abs(diff) > 0.001 {
			logger.Info("📊 [动态分配] 策略 %s: 权重 %.2f%% -> %.2f%% (目标: %.2f%%)",
				name, perf.CurrentWeight*100, currentWeight*100, targetWeight*100)
		}
	}

	return adjustedWeights
}

// Start 启动动态分配器
func (da *DynamicAllocator) Start(allocator *CapitalAllocator) {
	if da.rebalanceInterval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(da.rebalanceInterval)
		defer ticker.Stop()

		for {
			select {
			case <-da.ctx.Done():
				return
			case <-ticker.C:
				// 计算目标权重
				targetWeights := da.CalculateTargetWeights()

				// 重新平衡
				adjustedWeights := da.Rebalance(targetWeights)

				// 更新资金分配器
				allocator.mu.Lock()
				for name, weight := range adjustedWeights {
					if capital, exists := allocator.strategies[name]; exists {
						capital.Weight = weight
					}
				}
				allocator.mu.Unlock()

				// 重新分配资金
				allocator.Allocate()
			}
		}
	}()
}

// Stop 停止动态分配器
func (da *DynamicAllocator) Stop() {
	if da.cancel != nil {
		da.cancel()
	}
}

// GetPerformance 获取策略表现
func (da *DynamicAllocator) GetPerformance(strategyName string) *StrategyPerformance {
	da.mu.RLock()
	defer da.mu.RUnlock()

	perf, exists := da.strategies[strategyName]
	if !exists {
		return nil
	}

	perf.mu.RLock()
	defer perf.mu.RUnlock()

	return &StrategyPerformance{
		TotalPnL:      perf.TotalPnL,
		WinRate:       perf.WinRate,
		SharpeRatio:   perf.SharpeRatio,
		MaxDrawdown:   perf.MaxDrawdown,
		CurrentWeight: perf.CurrentWeight,
		TargetWeight:  perf.TargetWeight,
		TotalTrades:   perf.TotalTrades,
		WinningTrades: perf.WinningTrades,
		LosingTrades:  perf.LosingTrades,
	}
}
