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

// DynamicAdjuster 動態調整器
type DynamicAdjuster struct {
	cfg          *config.Config
	priceMonitor *monitor.PriceMonitor
	manager      *position.SuperPositionManager
	priceHistory []float64
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewDynamicAdjuster 創建動態調整器
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

// Start 啟动動態調整器
func (da *DynamicAdjuster) Start() {
	if !da.cfg.Trading.DynamicAdjustment.Enabled {
		return
	}

	// 订阅價格變化
	go da.watchPriceChanges()

	// 啟動價格间隔調整
	if da.cfg.Trading.DynamicAdjustment.PriceInterval.Enabled {
		go da.adjustPriceIntervalLoop()
	}

	// 啟动窗口大小調整
	if da.cfg.Trading.DynamicAdjustment.WindowSize.Enabled {
		go da.adjustWindowSizeLoop()
	}

	// 啟动單筆金額動態調整
	if da.cfg.Trading.DynamicAdjustment.OrderQuantity.Enabled {
		go da.adjustOrderQuantityLoop()
	}

	logger.Info("✅ 動態調整器已啟动")
}

// Stop 停止動態調整器
func (da *DynamicAdjuster) Stop() {
	if da.cancel != nil {
		da.cancel()
	}
}

// watchPriceChanges 監听價格變化
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

// addPrice 新增價格到历史記錄
func (da *DynamicAdjuster) addPrice(price float64) {
	da.mu.Lock()
	defer da.mu.Unlock()

	da.priceHistory = append(da.priceHistory, price)

	// 保持历史記錄在合理範圍内
	maxHistory := da.cfg.Trading.DynamicAdjustment.PriceInterval.VolatilityWindow
	if maxHistory <= 0 {
		maxHistory = 50 // 默认50
	}

	if len(da.priceHistory) > maxHistory*2 {
		// 保留最近的數據，使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, maxHistory)
		copy(newHistory, da.priceHistory[len(da.priceHistory)-maxHistory:])
		da.priceHistory = newHistory
	}
}

// CalculateVolatility 计算波动率（使用標准差）
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

	// 使用最近的數據
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

	// 计算標准差
	var variance float64
	for _, r := range returns {
		variance += math.Pow(r-mean, 2)
	}
	stdDev := math.Sqrt(variance / float64(len(returns)))

	// 波动率 = 標准差
	return stdDev
}

// adjustPriceIntervalLoop 定期調整價格間隔
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

// AdjustPriceInterval 調整價格間隔
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
		logger.Info("📈 [動態調整] 波动率 %.4f > 阈值 %.4f，增加價格間隔: %.2f -> %.2f",
			volatility, threshold, currentInterval, newInterval)
	} else {
		// 波动小，减少间隔
		newInterval = currentInterval - step
		if newInterval < minInterval {
			newInterval = minInterval
		}
		logger.Info("📉 [動態調整] 波动率 %.4f <= 阈值 %.4f，减少價格間隔: %.2f -> %.2f",
			volatility, threshold, currentInterval, newInterval)
	}

	if math.Abs(newInterval-currentInterval) > 0.01 {
		da.updatePriceInterval(newInterval)
	}
}

// updatePriceInterval 更新價格間隔
func (da *DynamicAdjuster) updatePriceInterval(newInterval float64) {
	da.cfg.Trading.PriceInterval = newInterval
	logger.Info("✅ [動態調整] 價格間隔已更新為: %.2f", newInterval)
}

// CalculateUtilization 计算资金利用率
func (da *DynamicAdjuster) CalculateUtilization() float64 {
	// 这里需要從交易所獲取帳戶信息
	// 暂時返回一個估算值，實際實現需要呼叫交易所API
	// TODO: 實現實際的资金利用率计算
	return 0.5 // 占位符
}

// adjustWindowSizeLoop 定期調整窗口大小
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

// AdjustWindowSize 調整窗口大小
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
		logger.Info("📉 [動態調整] 资金利用率 %.2f%% > 阈值 %.2f%%，减少窗口: 買%d->%d, 賣%d->%d",
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
		logger.Info("📈 [動態調整] 资金利用率 %.2f%% <= 阈值 %.2f%%，增加窗口: 買%d->%d, 賣%d->%d",
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
	logger.Info("✅ [動態調整] 窗口大小已更新: 買單窗口=%d, 賣單視窗=%d", buyWindow, sellWindow)
}

// adjustOrderQuantityLoop 定期調整單筆金額
func (da *DynamicAdjuster) adjustOrderQuantityLoop() {
	checkInterval := da.cfg.Trading.DynamicAdjustment.OrderQuantity.CheckInterval
	if checkInterval <= 0 {
		checkInterval = 60 // 預設 60 秒
	}
	interval := time.Duration(checkInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-da.ctx.Done():
			return
		case <-ticker.C:
			da.AdjustOrderQuantity()
		}
	}
}

// AdjustOrderQuantity 根據交易頻率動態調整單筆金額
func (da *DynamicAdjuster) AdjustOrderQuantity() {
	if da.manager == nil {
		return
	}
	oq := da.cfg.Trading.DynamicAdjustment.OrderQuantity
	minQty := oq.Min
	maxQty := oq.Max
	threshold := oq.FrequencyThreshold
	step := oq.AdjustmentStep

	if minQty <= 0 {
		minQty = 50 // 保守預設，需滿足交易所最小訂單金額
	}
	if maxQty <= 0 {
		maxQty = 500
	}
	if threshold <= 0 {
		threshold = 5
	}
	if step <= 0 {
		step = 20
	}

	fillCount := da.manager.GetFillCountInLastMinute()
	currentQty := da.cfg.Trading.OrderQuantity
	if currentQty <= 0 {
		currentQty = minQty
	}

	var newQty float64
	if fillCount > threshold {
		// 交易過於頻繁，降低單筆金額
		newQty = currentQty - step
		if newQty < minQty {
			newQty = minQty
		}
		logger.Info("📉 [動態調整] 交易頻率 %d/分鐘 > 閾值 %d，減少單筆金額: %.2f -> %.2f",
			fillCount, threshold, currentQty, newQty)
	} else {
		// 交易過少，適當提高單筆金額
		newQty = currentQty + step
		if newQty > maxQty {
			newQty = maxQty
		}
		logger.Info("📈 [動態調整] 交易頻率 %d/分鐘 <= 閾值 %d，增加單筆金額: %.2f -> %.2f",
			fillCount, threshold, currentQty, newQty)
	}

	if math.Abs(newQty-currentQty) > 0.01 {
		da.updateOrderQuantity(newQty)
	}
}

// updateOrderQuantity 更新單筆金額
func (da *DynamicAdjuster) updateOrderQuantity(newQty float64) {
	da.cfg.Trading.OrderQuantity = newQty
	logger.Info("✅ [動態調整] 單筆金額已更新為: %.2f", newQty)
}
