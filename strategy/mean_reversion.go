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

	// 價格历史
	priceHistory []float64
	mu           sync.RWMutex

	// 参數
	period             int
	stdMultiplier      float64
	reversionThreshold float64

	// 持倉
	position   *Position
	entryPrice float64

	isPaused bool
	eventBus EventBus

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMeanReversionStrategy 創建均值回归策略
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

	// 從配置中读取参數
	if p, ok := strategyCfg["period"].(int); ok {
		mrs.period = p
	} else {
		mrs.period = 20 // 預設 20
	}

	if sm, ok := strategyCfg["std_multiplier"].(float64); ok {
		mrs.stdMultiplier = sm
	} else {
		mrs.stdMultiplier = 2.0 // 預設 2 倍標準差
	}

	if rt, ok := strategyCfg["reversion_threshold"].(float64); ok {
		mrs.reversionThreshold = rt
	} else {
		mrs.reversionThreshold = 0.5 // 預設 0.5σ
	}

	return mrs
}

// Name 回傳策略名稱
func (mrs *MeanReversionStrategy) Name() string {
	return mrs.name
}

// SetEventBus 設置事件總線
func (mrs *MeanReversionStrategy) SetEventBus(bus EventBus) {
	mrs.mu.Lock()
	defer mrs.mu.Unlock()
	mrs.eventBus = bus
}

// Initialize 初始化策略
func (mrs *MeanReversionStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}

// Start 啟动策略
func (mrs *MeanReversionStrategy) Start(ctx context.Context) error {
	logger.Info("✅ [%s] 均值回归策略已啟动 (周期:%d, 標准差倍數:%.2f)",
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

// addPrice 新增價格
func (mrs *MeanReversionStrategy) addPrice(price float64) {
	mrs.mu.Lock()
	defer mrs.mu.Unlock()

	mrs.priceHistory = append(mrs.priceHistory, price)

	// 保持历史記錄
	maxHistory := mrs.period * 2
	if len(mrs.priceHistory) > maxHistory {
		// 使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, maxHistory)
		copy(newHistory, mrs.priceHistory[len(mrs.priceHistory)-maxHistory:])
		mrs.priceHistory = newHistory
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

// calculateStdDev 计算標准差
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

// OnPriceChange 價格變化处理
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

	// 價格低於下轨：買入信号
	if price < lower && mrs.position == nil {
		logger.Info("📊 [%s] 價格低於下轨，買入信号: 價格=%.2f, 下轨=%.2f", mrs.name, price, lower)
		// TODO: 實現買入逻辑
		mrs.entryPrice = price
		mrs.position = &Position{
			Symbol:       mrs.cfg.Trading.Symbol,
			Size:         0, // TODO: 计算倉位大小
			EntryPrice:   price,
			CurrentPrice: price,
			PnL:          0,
		}
	}

	// 價格高於上轨：賣出信号
	if price > upper && mrs.position != nil {
		logger.Info("📊 [%s] 價格高於上轨，賣出信号: 價格=%.2f, 上轨=%.2f", mrs.name, price, upper)
		// TODO: 實現賣出逻辑
		mrs.position = nil
		mrs.entryPrice = 0
	}

	// 價格回归中轨：平倉
	if mrs.position != nil {
		deviation := math.Abs(price - middle)
		stdDev := mrs.calculateStdDev(middle)
		if deviation < stdDev*mrs.reversionThreshold {
			logger.Info("📊 [%s] 價格回归中轨，平倉: 價格=%.2f, 中轨=%.2f", mrs.name, price, middle)
			// TODO: 實現平倉逻辑
			mrs.position = nil
			mrs.entryPrice = 0
		}
	}

	return nil
}

// OnOrderUpdate 订單更新处理
func (mrs *MeanReversionStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// TODO: 处理订單更新
	return nil
}

// GetPositions 獲取持倉
func (mrs *MeanReversionStrategy) GetPositions() []*Position {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()

	if mrs.position == nil {
		return []*Position{}
	}

	return []*Position{mrs.position}
}

// GetOrders 獲取訂單
func (mrs *MeanReversionStrategy) GetOrders() []*Order {
	return []*Order{}
}

// GetStatistics 獲取统计
func (mrs *MeanReversionStrategy) GetStatistics() *StrategyStatistics {
	return &StrategyStatistics{
		TotalTrades: 0,
		WinRate:     0,
		TotalPnL:    0,
		TotalVolume: 0,
	}
}

// GetVisualizationData 獲取策略可视化數據
func (mrs *MeanReversionStrategy) GetVisualizationData() map[string]interface{} {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()

	data := make(map[string]interface{})

	// 计算布林带
	upper, middle, lower := mrs.calculateBollingerBands()
	data["upperBand"] = upper
	data["middleBand"] = middle
	data["lowerBand"] = lower

	// 当前价格
	currentPrice := 0.0
	if len(mrs.priceHistory) > 0 {
		currentPrice = mrs.priceHistory[len(mrs.priceHistory)-1]
	}
	data["currentPrice"] = currentPrice

	// 价格在布林带中的位置（百分比）
	if upper > lower && currentPrice > 0 {
		positionPercent := ((currentPrice - lower) / (upper - lower)) * 100
		data["positionInBand"] = positionPercent

		// 判断是否触及上下轨
		data["touchesUpperBand"] = currentPrice >= upper*0.99 // 允许1%误差
		data["touchesLowerBand"] = currentPrice <= lower*1.01
	} else {
		data["positionInBand"] = 50.0 // 默认中间位置
		data["touchesUpperBand"] = false
		data["touchesLowerBand"] = false
	}

	// 持仓状态
	if mrs.position != nil {
		data["hasPosition"] = true
		data["entryPrice"] = mrs.entryPrice
		if currentPrice > 0 && mrs.entryPrice > 0 {
			pnlPercent := ((currentPrice - mrs.entryPrice) / mrs.entryPrice) * 100
			data["pnlPercent"] = pnlPercent
		}
	} else {
		data["hasPosition"] = false
		data["entryPrice"] = 0
	}

	// 策略参数
	data["period"] = mrs.period
	data["stdMultiplier"] = mrs.stdMultiplier
	data["reversionThreshold"] = mrs.reversionThreshold

	// 买入/卖出信号判断
	if upper > 0 && lower > 0 && currentPrice > 0 {
		hasPos := mrs.position != nil
		// 买入信号：价格触及下轨
		buySignal := currentPrice <= lower*1.01 && !hasPos
		data["buySignal"] = buySignal

		// 卖出信号：价格触及上轨或回归均值
		sellSignal := (currentPrice >= upper*0.99 || currentPrice >= middle) && hasPos
		data["sellSignal"] = sellSignal

		// 距离买入/卖出信号的距离
		if !hasPos {
			distanceToBuy := ((currentPrice - lower) / currentPrice) * 100
			data["distanceToBuy"] = distanceToBuy
		} else {
			distanceToSell := ((upper - currentPrice) / currentPrice) * 100
			distanceToMean := ((middle - currentPrice) / currentPrice) * 100
			data["distanceToSell"] = math.Min(distanceToSell, distanceToMean)
		}
	}

	return data
}
