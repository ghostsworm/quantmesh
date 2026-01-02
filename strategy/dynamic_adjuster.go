package strategy

import (
	"context"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/position"
)

// DynamicAdjuster 动态调整器
type DynamicAdjuster struct {
	cfg          *config.Config
	priceMonitor *monitor.PriceMonitor
	manager      *position.SuperPositionManager
	priceHistory []float64
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewDynamicAdjuster 创建动态调整器
func NewDynamicAdjuster(
	cfg *config.Config,
	priceMonitor *monitor.PriceMonitor,
	manager *position.SuperPositionManager,
) *DynamicAdjuster {
	ctx, cancel := context.WithCancel(context.Background())
	return &DynamicAdjuster{
		cfg:          cfg,
		priceMonitor: priceMonitor,
		manager:      manager,
		priceHistory: make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start 启动动态调整器
func (da *DynamicAdjuster) Start() {
	if !da.cfg.Trading.DynamicAdjustment.Enabled {
		return
	}

	// 订阅价格变化
	go da.watchPriceChanges()

	// 启动价格间隔调整
	if da.cfg.Trading.DynamicAdjustment.PriceInterval.Enabled {
		go da.adjustPriceIntervalLoop()
	}

	// 启动窗口大小调整
	if da.cfg.Trading.DynamicAdjustment.WindowSize.Enabled {
		go da.adjustWindowSizeLoop()
	}

	logger.Info("✅ 动态调整器已启动")
}

// Stop 停止动态调整器
func (da *DynamicAdjuster) Stop() {
	if da.cancel != nil {
		da.cancel()
	}
}

// watchPriceChanges 监听价格变化
func (da *DynamicAdjuster) watchPriceChanges() {
	priceCh := da.priceMonitor.Subscribe()
	for {
		select {
		case <-da.ctx.Done():
			return
		case priceChange := <-priceCh:
			da.addPrice(priceChange.NewPrice)
		}
	}
}

// addPrice 添加价格到历史记录
func (da *DynamicAdjuster) addPrice(price float64) {
	da.mu.Lock()
	defer da.mu.Unlock()

	da.priceHistory = append(da.priceHistory, price)

	// 保持历史记录在合理范围内
	maxHistory := da.cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityWindow
	if maxHistory <= 0 {
		maxHistory = 50 // 默认50
	}

	if len(da.priceHistory) > maxHistory*2 {
		// 保留最近的数据
		da.priceHistory = da.priceHistory[len(da.priceHistory)-maxHistory:]
	}
}

// CalculateVolatility 计算波动率（使用标准差）
func (da *DynamicAdjuster) CalculateVolatility() float64 {
	da.mu.RLock()
	defer da.mu.RUnlock()

	if len(da.priceHistory) < 2 {
		return 0
	}

	window := da.cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityWindow
	if window <= 0 {
		window = 20
	}

	// 使用最近的数据
	start := len(da.priceHistory) - window
	if start < 0 {
		start = 0
	}
	prices := da.priceHistory[start:]

	// 计算收益率
	returns := make([]float64, len(prices)-1)
	for i := 0; i < len(prices)-1; i++ {
		if prices[i] > 0 {
			returns[i] = (prices[i+1] - prices[i]) / prices[i]
		}
	}

	if len(returns) == 0 {
		return 0
	}

	// 计算平均收益率
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// 计算标准差
	var variance float64
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	stdDev := math.Sqrt(variance / float64(len(returns)))

	// 波动率 = 标准差
	return stdDev
}

// adjustPriceIntervalLoop 定期调整价格间隔
func (da *DynamicAdjuster) adjustPriceIntervalLoop() {
	checkInterval := time.Duration(da.cfg.Trading.DynamicAdjustment.PriceInterval.CheckInterval) * time.Second
	if checkInterval <= 0 {
		checkInterval = 60 * time.Second
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-da.ctx.Done():
			return
		case <-ticker.C:
			da.AdjustPriceInterval()
		}
	}
}

// AdjustPriceInterval 调整价格间隔
func (da *DynamicAdjuster) AdjustPriceInterval() {
	volatility := da.CalculateVolatility()
	currentInterval := da.cfg.Trading.PriceInterval
	threshold := da.cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityThreshold
	step := da.cfg.Trading.DynamicAdjustment.PriceInterval.AdjustmentStep
	minInterval := da.cfg.Trading.DynamicAdjustment.PriceInterval.Min
	maxInterval := da.cfg.Trading.DynamicAdjustment.PriceInterval.Max

	if threshold <= 0 {
		threshold = 0.02 // 默认2%
	}
	if step <= 0 {
		step = 0.5
	}
	if minInterval <= 0 {
		minInterval = 0.5
	}
	if maxInterval <= 0 {
		maxInterval = 10.0
	}

	var newInterval float64
	if volatility > threshold {
		// 波动大，增加间隔
		newInterval = currentInterval + step
		if newInterval > maxInterval {
			newInterval = maxInterval
		}
		logger.Info("📈 [动态调整] 波动率 %.4f > 阈值 %.4f，增加价格间隔: %.2f -> %.2f",
			volatility, threshold, currentInterval, newInterval)
	} else {
		// 波动小，减少间隔
		newInterval = currentInterval - step
		if newInterval < minInterval {
			newInterval = minInterval
		}
		logger.Info("📉 [动态调整] 波动率 %.4f <= 阈值 %.4f，减少价格间隔: %.2f -> %.2f",
			volatility, threshold, currentInterval, newInterval)
	}

	if math.Abs(newInterval-currentInterval) > 0.01 {
		da.updatePriceInterval(newInterval)
	}
}

// updatePriceInterval 更新价格间隔
func (da *DynamicAdjuster) updatePriceInterval(newInterval float64) {
	da.cfg.Trading.PriceInterval = newInterval
	logger.Info("✅ [动态调整] 价格间隔已更新为: %.2f", newInterval)
}

// CalculateUtilization 计算资金利用率
func (da *DynamicAdjuster) CalculateUtilization() float64 {
	// 这里需要从交易所获取账户信息
	// 暂时返回一个估算值，实际实现需要调用交易所API
	// TODO: 实现实际的资金利用率计算
	return 0.5 // 占位符
}

// adjustWindowSizeLoop 定期调整窗口大小
func (da *DynamicAdjuster) adjustWindowSizeLoop() {
	checkInterval := time.Duration(da.cfg.Trading.DynamicAdjustment.WindowSize.CheckInterval) * time.Second
	if checkInterval <= 0 {
		checkInterval = 300 * time.Second // 默认5分钟
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-da.ctx.Done():
			return
		case <-ticker.C:
			da.AdjustWindowSize()
		}
	}
}

// AdjustWindowSize 调整窗口大小
func (da *DynamicAdjuster) AdjustWindowSize() {
	utilization := da.CalculateUtilization()
	threshold := da.cfg.Trading.DynamicAdjustment.WindowSize.UtilizationThreshold
	step := da.cfg.Trading.DynamicAdjustment.WindowSize.AdjustmentStep

	if threshold <= 0 {
		threshold = 0.8 // 默认80%
	}
	if step <= 0 {
		step = 2
	}

	currentBuyWindow := da.cfg.Trading.BuyWindowSize
	currentSellWindow := da.cfg.Trading.SellWindowSize

	minBuyWindow := da.cfg.Trading.DynamicAdjustment.WindowSize.BuyWindow.Min
	maxBuyWindow := da.cfg.Trading.DynamicAdjustment.WindowSize.BuyWindow.Max
	minSellWindow := da.cfg.Trading.DynamicAdjustment.WindowSize.SellWindow.Min
	maxSellWindow := da.cfg.Trading.DynamicAdjustment.WindowSize.SellWindow.Max

	if minBuyWindow <= 0 {
		minBuyWindow = 5
	}
	if maxBuyWindow <= 0 {
		maxBuyWindow = 50
	}
	if minSellWindow <= 0 {
		minSellWindow = 5
	}
	if maxSellWindow <= 0 {
		maxSellWindow = 50
	}

	var newBuyWindow, newSellWindow int

	if utilization > threshold {
		// 资金利用率高，减少窗口
		newBuyWindow = currentBuyWindow - step
		newSellWindow = currentSellWindow - step
		if newBuyWindow < minBuyWindow {
			newBuyWindow = minBuyWindow
		}
		if newSellWindow < minSellWindow {
			newSellWindow = minSellWindow
		}
		logger.Info("📉 [动态调整] 资金利用率 %.2f%% > 阈值 %.2f%%，减少窗口: 买%d->%d, 卖%d->%d",
			utilization*100, threshold*100, currentBuyWindow, newBuyWindow, currentSellWindow, newSellWindow)
	} else {
		// 资金利用率低，增加窗口
		newBuyWindow = currentBuyWindow + step
		newSellWindow = currentSellWindow + step
		if newBuyWindow > maxBuyWindow {
			newBuyWindow = maxBuyWindow
		}
		if newSellWindow > maxSellWindow {
			newSellWindow = maxSellWindow
		}
		logger.Info("📈 [动态调整] 资金利用率 %.2f%% <= 阈值 %.2f%%，增加窗口: 买%d->%d, 卖%d->%d",
			utilization*100, threshold*100, currentBuyWindow, newBuyWindow, currentSellWindow, newSellWindow)
	}

	if newBuyWindow != currentBuyWindow || newSellWindow != currentSellWindow {
		da.updateWindowSize(newBuyWindow, newSellWindow)
	}
}

// updateWindowSize 更新窗口大小
func (da *DynamicAdjuster) updateWindowSize(buyWindow, sellWindow int) {
	da.cfg.Trading.BuyWindowSize = buyWindow
	da.cfg.Trading.SellWindowSize = sellWindow
	logger.Info("✅ [动态调整] 窗口大小已更新: 买单窗口=%d, 卖单窗口=%d", buyWindow, sellWindow)
}
