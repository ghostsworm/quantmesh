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

// NewCapitalAllocator 創建资金分配器
func NewCapitalAllocator(cfg *config.Config, totalCapital float64) *CapitalAllocator {
	if math.IsNaN(totalCapital) || math.IsInf(totalCapital, 0) || totalCapital < 0 {
		logger.Warn("⚠️ [资金分配] 总资金无效，按零资金关闭策略额度: %.4f", totalCapital)
		totalCapital = 0
	}
	return &CapitalAllocator{
		totalCapital: totalCapital,
		strategies:   make(map[string]*StrategyCapital),
		cfg:          cfg,
	}
}

// GetConfig 獲取配置（用於訪問 bot ID 等信息）
func (ca *CapitalAllocator) GetConfig() *config.Config {
	return ca.cfg
}

// RegisterStrategy 注册策略
func (ca *CapitalAllocator) RegisterStrategy(name string, weight float64, fixedPool float64) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
		logger.Warn("⚠️ [资金分配] 策略 %s 权重无效 %.4f，已归零", name, weight)
		weight = 0
	}
	if math.IsNaN(fixedPool) || math.IsInf(fixedPool, 0) || fixedPool < 0 {
		logger.Warn("⚠️ [资金分配] 策略 %s 固定资金池无效 %.4f，已归零", name, fixedPool)
		fixedPool = 0
	}

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

	// 计算固定资金池總額
	fixedPoolTotal := 0.0
	weightTotal := 0.0

	for _, capital := range ca.strategies {
		if capital.FixedPool > 0 {
			fixedPoolTotal += capital.FixedPool
		} else {
			weightTotal += capital.Weight
		}
	}
	if math.IsNaN(fixedPoolTotal) || math.IsInf(fixedPoolTotal, 0) ||
		math.IsNaN(weightTotal) || math.IsInf(weightTotal, 0) ||
		math.IsNaN(ca.totalCapital) || math.IsInf(ca.totalCapital, 0) || ca.totalCapital < 0 {
		for _, capital := range ca.strategies {
			capital.Allocated = 0
			capital.Available = 0
		}
		logger.Error("[资金分配] 分配输入或汇总溢出，已将所有策略可用额度置零")
		return
	}

	// 剩餘资金用於权重分配
	remainingCapital := ca.totalCapital - fixedPoolTotal
	fixedPoolScale := 1.0
	if fixedPoolTotal > ca.totalCapital && fixedPoolTotal > 0 {
		fixedPoolScale = ca.totalCapital / fixedPoolTotal
		remainingCapital = 0
		logger.Warn("⚠️ [资金分配] 固定资金池總額 %.2f 超過總资金 %.2f，已按比例缩放固定池并暂停权重资金分配",
			fixedPoolTotal, ca.totalCapital)
	}
	if remainingCapital < 0 {
		remainingCapital = 0
	}

	// 分配资金
	for name, capital := range ca.strategies {
		if capital.FixedPool > 0 {
			// 使用固定资金池
			capital.Allocated = capital.FixedPool * fixedPoolScale
		} else if weightTotal > 0 {
			// 按权重分配
			capital.Allocated = remainingCapital * (capital.Weight / weightTotal)
		} else {
			capital.Allocated = 0
		}

		if capital.Allocated < 0 {
			capital.Allocated = 0
		}
		capital.Available = math.Max(0, capital.Allocated-capital.Used)

		logger.Info("💰 [资金分配] 策略 %s: 分配=%.2f, 已用=%.2f, 可用=%.2f (权重=%.2f%%)",
			name, capital.Allocated, capital.Used, capital.Available, capital.Weight*100)
	}
}

// CheckAvailable 检查策略可用资金
func (ca *CapitalAllocator) CheckAvailable(strategyName string, amount float64) bool {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return false
	}
	if amount <= 0 {
		return true
	}

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

// Reserve 預留资金
func (ca *CapitalAllocator) Reserve(strategyName string, amount float64) bool {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return false
	}
	if amount <= 0 {
		return true
	}

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
	capital.Available = math.Max(0, capital.Allocated-capital.Used)
	return true
}

// Release 释放资金
func (ca *CapitalAllocator) Release(strategyName string, amount float64) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return
	}
	if amount <= 0 {
		return
	}

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
	capital.Available = math.Max(0, capital.Allocated-capital.Used)
}

// ReleaseAll 释放策略全部锁定资金（用於手动修正错误锁定）
func (ca *CapitalAllocator) ReleaseAll(strategyName string) float64 {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	capital, exists := ca.strategies[strategyName]
	if !exists {
		return 0
	}

	capital.mu.Lock()
	defer capital.mu.Unlock()

	released := capital.Used
	capital.Used = 0
	capital.Available = capital.Allocated
	return released
}

// ReleaseAllStrategies 释放所有策略的锁定资金（用於手动修正错误锁定）
func (ca *CapitalAllocator) ReleaseAllStrategies() map[string]float64 {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	released := make(map[string]float64)
	for name, capital := range ca.strategies {
		capital.mu.Lock()
		released[name] = capital.Used
		capital.Used = 0
		capital.Available = capital.Allocated
		capital.mu.Unlock()
	}
	return released
}

// GetAvailable 獲取可用资金
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

// GetUsed 獲取已用资金
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

// GetAllocated 獲取已分配资金
func (ca *CapitalAllocator) GetAllocated(strategyName string) float64 {
	ca.mu.RLock()
	defer ca.mu.RUnlock()

	capital, exists := ca.strategies[strategyName]
	if !exists {
		return 0
	}

	capital.mu.RLock()
	defer capital.mu.RUnlock()

	return capital.Allocated
}

// GetAllStrategiesCapital 獲取所有策略资金信息
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

// StrategyPerformance 策略表現
type StrategyPerformance struct {
	TotalPnL        float64
	CapitalBaseline float64
	WinRate         float64
	SharpeRatio     float64
	MaxDrawdown     float64
	CurrentWeight   float64
	TargetWeight    float64
	TotalTrades     int
	WinningTrades   int
	LosingTrades    int
	mu              sync.RWMutex
}

// DynamicAllocator 动態分配器
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

// NewDynamicAllocator 創建动態分配器
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
		da.rebalanceInterval = 3600 * time.Second // 預設 1 小時
	}
	if math.IsNaN(da.maxChangePerRebalance) || math.IsInf(da.maxChangePerRebalance, 0) || da.maxChangePerRebalance <= 0 || da.maxChangePerRebalance > 1 {
		da.maxChangePerRebalance = 0.05 // 默认5%
	}
	if math.IsNaN(da.minWeight) || math.IsInf(da.minWeight, 0) || da.minWeight <= 0 || da.minWeight > 1 {
		da.minWeight = 0.1 // 預設 10%
	}
	if math.IsNaN(da.maxWeight) || math.IsInf(da.maxWeight, 0) || da.maxWeight <= 0 || da.maxWeight > 1 {
		da.maxWeight = 0.7 // 默认70%
	}
	if da.minWeight > da.maxWeight {
		da.minWeight, da.maxWeight = 0.1, 0.7
	}

	// 設置默认性能权重
	if len(da.performanceWeights) == 0 {
		da.performanceWeights = map[string]float64{
			"total_pnl": 0.7,
			"win_rate":  0.3,
		}
	} else {
		weights := make(map[string]float64, len(da.performanceWeights))
		for name, weight := range da.performanceWeights {
			if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 || weight > 1 ||
				(weight > 0 && name != "total_pnl" && name != "win_rate") {
				logger.Warn("⚠️ [动態分配] 忽略无效或未支持的绩效权重: %s", name)
				continue
			}
			weights[name] = weight
		}
		da.performanceWeights = weights
	}

	return da
}

// RegisterStrategy 注册策略
func (da *DynamicAllocator) RegisterStrategy(name string, initialWeight float64) {
	if math.IsNaN(initialWeight) || math.IsInf(initialWeight, 0) || initialWeight < 0 || initialWeight > 1 {
		logger.Warn("⚠️ [动態分配] 策略 %s 初始权重无效，已设为零", name)
		initialWeight = 0
	}
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

func (da *DynamicAllocator) setCapitalBaseline(name string, capital float64) {
	if !finiteNumber(capital) || capital <= 0 {
		return
	}
	da.mu.RLock()
	perf, exists := da.strategies[name]
	da.mu.RUnlock()
	if !exists {
		return
	}
	perf.mu.Lock()
	defer perf.mu.Unlock()
	if perf.CapitalBaseline == 0 {
		perf.CapitalBaseline = capital
	}
}

// UpdatePerformance 更新策略表現
func (da *DynamicAllocator) UpdatePerformance(strategyName string, pnl float64, isWin bool) {
	wins := 0
	if isWin {
		wins = 1
	}
	da.updatePerformance(strategyName, pnl, 1, wins)
}

func (da *DynamicAllocator) updatePerformance(strategyName string, pnl float64, trades, wins int) {
	if math.IsNaN(pnl) || math.IsInf(pnl, 0) {
		logger.Warn("⚠️ [动態分配] 策略 %s 收到非有限盈亏样本，已忽略", strategyName)
		return
	}
	if trades < 0 || wins < 0 || wins > trades {
		logger.Warn("⚠️ [动態分配] 策略 %s 收到无效的成交统计增量，已忽略", strategyName)
		return
	}
	da.mu.Lock()
	defer da.mu.Unlock()

	perf, exists := da.strategies[strategyName]
	if !exists {
		return
	}

	perf.mu.Lock()
	defer perf.mu.Unlock()

	nextPnL := perf.TotalPnL + pnl
	if math.IsNaN(nextPnL) || math.IsInf(nextPnL, 0) {
		logger.Warn("⚠️ [动態分配] 策略 %s 累计盈亏溢出，已忽略本次样本", strategyName)
		return
	}
	maxInt := int(^uint(0) >> 1)
	if perf.TotalTrades > maxInt-trades || perf.WinningTrades > maxInt-wins || perf.LosingTrades > maxInt-(trades-wins) {
		logger.Warn("⚠️ [动態分配] 策略 %s 成交统计溢出，已忽略本次样本", strategyName)
		return
	}
	// Commit the sample only after every field has passed validation. A rejected
	// trade-count increment must not leave its PnL partially applied.
	perf.TotalPnL = nextPnL
	perf.TotalTrades += trades
	perf.WinningTrades += wins
	perf.LosingTrades += trades - wins

	if perf.TotalTrades > 0 {
		perf.WinRate = float64(perf.WinningTrades) / float64(perf.TotalTrades)
	}

	// TODO: 计算夏普比率和最大回撤
}

// CalculateTargetWeights 计算目標权重
func (da *DynamicAllocator) CalculateTargetWeights() map[string]float64 {
	da.mu.RLock()
	defer da.mu.RUnlock()

	scores := make(map[string]float64)
	allHaveSamples := len(da.strategies) > 0

	for name, perf := range da.strategies {
		perf.mu.RLock()
		if perf.TotalTrades == 0 {
			allHaveSamples = false
		}
		score := da.calculateScore(perf)
		scores[name] = score
		perf.mu.RUnlock()
	}
	if !allHaveSamples {
		return da.currentWeightsLocked()
	}

	totalScore := 0.0
	for _, score := range scores {
		totalScore += score
	}
	if totalScore <= 0 || math.IsNaN(totalScore) || math.IsInf(totalScore, 0) {
		// 如果所有策略得分都為0，使用當前权重
		return da.currentWeightsLocked()
	}
	result, feasible := projectBoundedWeights(scores, da.minWeight, da.maxWeight)
	if !feasible {
		logger.Warn("⚠️ [动態分配] 策略数量与权重上下限不相容，保留当前权重")
		return da.currentWeightsLocked()
	}
	return result
}

// projectBoundedWeights projects values onto the unit simplex while preserving
// the configured per-strategy bounds. It returns false when the bounds cannot
// sum to one for the current number of strategies.
func projectBoundedWeights(values map[string]float64, minWeight, maxWeight float64) (map[string]float64, bool) {
	count := len(values)
	if count == 0 || !finiteNumber(minWeight) || !finiteNumber(maxWeight) ||
		minWeight < 0 || maxWeight > 1 || minWeight > maxWeight ||
		float64(count)*minWeight > 1+1e-12 || float64(count)*maxWeight < 1-1e-12 {
		return nil, false
	}

	low, high := math.Inf(1), math.Inf(-1)
	for _, value := range values {
		if !finiteNumber(value) {
			return nil, false
		}
		low = math.Min(low, value-maxWeight)
		high = math.Max(high, value-minWeight)
	}
	for range 80 {
		shift := (low + high) / 2
		total := 0.0
		for _, value := range values {
			total += math.Max(minWeight, math.Min(maxWeight, value-shift))
		}
		if total > 1 {
			low = shift
		} else {
			high = shift
		}
	}

	result := make(map[string]float64, count)
	total := 0.0
	for name, value := range values {
		weight := math.Max(minWeight, math.Min(maxWeight, value-high))
		result[name] = weight
		total += weight
	}
	// Correct only floating-point residue, without crossing any configured bound.
	residue := 1 - total
	for name, weight := range result {
		if math.Abs(residue) <= 1e-12 {
			break
		}
		capacity := maxWeight - weight
		if residue < 0 {
			capacity = weight - minWeight
		}
		adjustment := math.Copysign(math.Min(math.Abs(residue), capacity), residue)
		result[name] = weight + adjustment
		residue -= adjustment
	}
	return result, math.Abs(residue) <= 1e-9
}

func (da *DynamicAllocator) currentWeightsLocked() map[string]float64 {
	result := make(map[string]float64, len(da.strategies))
	for name, perf := range da.strategies {
		perf.mu.RLock()
		result[name] = perf.CurrentWeight
		perf.mu.RUnlock()
	}
	return result
}

func (da *DynamicAllocator) matchesCurrentWeightsLocked(targetWeights map[string]float64) bool {
	if len(targetWeights) != len(da.strategies) {
		return false
	}
	for name, perf := range da.strategies {
		target, exists := targetWeights[name]
		if !exists {
			return false
		}
		perf.mu.RLock()
		matches := target == perf.CurrentWeight
		perf.mu.RUnlock()
		if !matches {
			return false
		}
	}
	return true
}

// calculateScore 计算策略得分
func (da *DynamicAllocator) calculateScore(perf *StrategyPerformance) float64 {
	score := 0.0

	// 總盈亏得分（越高越好）
	if pnlWeight, ok := da.performanceWeights["total_pnl"]; ok && pnlWeight > 0 {
		// Compare percentage returns against the fixed startup allocation. A
		// missing baseline is not evidence for ranking by absolute currency PnL.
		if perf.CapitalBaseline > 0 {
			pnlReturn := perf.TotalPnL / perf.CapitalBaseline
			if finiteNumber(pnlReturn) {
				pnlScore := math.Max(0, math.Min(1, pnlReturn/0.1))
				score += pnlScore * pnlWeight
			}
		}
	}

	// 胜率得分（越高越好）
	if winRateWeight, ok := da.performanceWeights["win_rate"]; ok && winRateWeight > 0 {
		score += perf.WinRate * winRateWeight
	}
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
		return 0
	}

	// SharpeRatio and MaxDrawdown are not computed from a verified return/equity
	// series yet. Never score their zero-value fields as real risk observations.

	return score
}

// Rebalance 重新平衡（平滑調整）
func (da *DynamicAllocator) Rebalance(targetWeights map[string]float64) map[string]float64 {
	da.mu.Lock()
	defer da.mu.Unlock()
	if da.matchesCurrentWeightsLocked(targetWeights) {
		return da.currentWeightsLocked()
	}
	if len(targetWeights) < len(da.strategies) {
		return map[string]float64{}
	}
	requested, current := make(map[string]float64, len(da.strategies)), make(map[string]float64, len(da.strategies))
	for name, perf := range da.strategies {
		target, exists := targetWeights[name]
		if !exists || !finiteNumber(target) || target < 0 || target > 1 {
			logger.Warn("⚠️ [动態分配] 策略 %s 目標权重缺失或无效，取消本轮调整", name)
			return map[string]float64{}
		}
		requested[name] = target
		perf.mu.RLock()
		current[name] = perf.CurrentWeight
		perf.mu.RUnlock()
	}
	projectedTarget, feasible := projectBoundedWeights(requested, da.minWeight, da.maxWeight)
	if !feasible {
		logger.Warn("⚠️ [动態分配] 目標权重不可行，取消本轮调整")
		return da.currentWeightsLocked()
	}
	projectedCurrent, feasible := projectBoundedWeights(current, da.minWeight, da.maxWeight)
	if !feasible {
		logger.Warn("⚠️ [动態分配] 当前策略数量与权重上下限不相容，保留当前权重")
		return da.currentWeightsLocked()
	}

	maxDiff := 0.0
	for name, target := range projectedTarget {
		maxDiff = math.Max(maxDiff, math.Abs(target-projectedCurrent[name]))
	}
	progress := 1.0
	if maxDiff > da.maxChangePerRebalance {
		progress = da.maxChangePerRebalance / maxDiff
	}
	adjustedWeights := make(map[string]float64, len(da.strategies))
	for name, target := range projectedTarget {
		perf := da.strategies[name]
		oldWeight := current[name]
		newWeight := projectedCurrent[name] + (target-projectedCurrent[name])*progress
		perf.mu.Lock()
		perf.CurrentWeight = newWeight
		perf.TargetWeight = target
		perf.mu.Unlock()
		adjustedWeights[name] = newWeight
		if math.Abs(newWeight-oldWeight) > 0.001 {
			logger.Info("📊 [动態分配] 策略 %s: 权重 %.2f%% -> %.2f%% (目標: %.2f%%)",
				name, oldWeight*100, newWeight*100, target*100)
		}
	}

	return adjustedWeights
}

// Start 啟动动態分配器
func (da *DynamicAllocator) Start(allocator *CapitalAllocator) {
	da.StartWithPerformanceProvider(allocator, nil)
}

func (da *DynamicAllocator) StartWithPerformanceProvider(allocator *CapitalAllocator, syncPerformance func()) {
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
				if syncPerformance != nil {
					syncPerformance()
				}
				// 计算目標权重
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

// Stop 停止动態分配器
func (da *DynamicAllocator) Stop() {
	if da.cancel != nil {
		da.cancel()
	}
}

// GetPerformance 獲取策略表現
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
		TotalPnL:        perf.TotalPnL,
		CapitalBaseline: perf.CapitalBaseline,
		WinRate:         perf.WinRate,
		SharpeRatio:     perf.SharpeRatio,
		MaxDrawdown:     perf.MaxDrawdown,
		CurrentWeight:   perf.CurrentWeight,
		TargetWeight:    perf.TargetWeight,
		TotalTrades:     perf.TotalTrades,
		WinningTrades:   perf.WinningTrades,
		LosingTrades:    perf.LosingTrades,
	}
}
