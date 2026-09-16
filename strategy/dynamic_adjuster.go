package strategy

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/indicators"
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

	// 波动率检测
	volatilityAlert       *event.VolatilityAlertService
	currentRegime         indicators.VolatilityRegime
	currentSymbol         string // 当前交易对

	// 趋势状态（用于判断上涨/下跌行情）
	currentTrend         string // "up", "down", "sideways"
	trendHistory         []float64 // 价格历史用于趋势判断

	// utilizationWarned 资金利用率不可用的告警只打印一次
	utilizationWarned atomic.Bool
}

// volatilityPauseReasonPrefix 波動率暫停開倉的原因前綴；恢復時只解除帶此前綴的暫停
const volatilityPauseReasonPrefix = "波动率暂停"

// NewDynamicAdjuster 創建動態調整器
func NewDynamicAdjuster(
	cfg *config.Config,
	priceMonitor *monitor.PriceMonitor,
	manager *position.SuperPositionManager,
) *DynamicAdjuster {
	ctx, cancel := context.WithCancel(context.Background())

	da := &DynamicAdjuster{
		cfg:          cfg,
		priceMonitor: priceMonitor,
		manager:      manager,
		priceHistory: make([]float64, 0, 100),
		trendHistory: make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
	}

	// 获取当前交易对符号
	if len(cfg.Trading.Symbols) > 0 {
		da.currentSymbol = cfg.Trading.Symbols[0].Symbol
	}

	// 初始化波动率检测服务
	if cfg.Trading.DynamicAdjustment.VolatilityDetection.Enabled {
		var volConfig indicators.VolatilityRegimeConfig

		// 优先使用内置预设
		if len(cfg.Trading.Symbols) > 0 {
			symbol := cfg.Trading.Symbols[0].Symbol
			preset := indicators.GetVolatilityPreset(symbol)
			volConfig = preset.ConvertToConfig()
			logger.Info("📊 [波动率检测] 使用 %s 预设配置: %s",
				preset.Name, indicators.GetPresetForSymbol(symbol))
		} else {
			volConfig = indicators.DefaultVolatilityRegimeConfig()
		}

		// 从配置覆盖（如果用户自定义了）
		userConfig := cfg.Trading.DynamicAdjustment.VolatilityDetection
		if userConfig.ShortPeriod > 0 {
			volConfig.ShortPeriod = userConfig.ShortPeriod
		}
		if userConfig.LowThreshold > 0 {
			volConfig.LowThreshold = userConfig.LowThreshold
		}
		if userConfig.NormalThreshold > 0 {
			volConfig.NormalThreshold = userConfig.NormalThreshold
		}
		if userConfig.HighThreshold > 0 {
			volConfig.HighThreshold = userConfig.HighThreshold
		}
		if userConfig.ExtremeThreshold > 0 {
			volConfig.ExtremeThreshold = userConfig.ExtremeThreshold
		}
		if userConfig.PriceRangePeriod > 0 {
			volConfig.PriceRangePeriod = userConfig.PriceRangePeriod
		}
		if userConfig.PriceRangeThreshold > 0 {
			volConfig.PriceRangeThreshold = userConfig.PriceRangeThreshold
		}

		da.volatilityAlert = event.NewVolatilityAlertService(volConfig)

		// 订阅波动率预警
		alertCh := da.volatilityAlert.Subscribe("dynamic_adjuster")
		go da.handleVolatilityAlerts(alertCh)
	}

	return da
}

// Start 啟动動態調整器
func (da *DynamicAdjuster) Start() {
	if !da.cfg.Trading.DynamicAdjustment.Enabled {
		return
	}

	// 啟动波动率检测
	if da.volatilityAlert != nil {
		da.volatilityAlert.Start()
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
	if da.volatilityAlert != nil {
		da.volatilityAlert.Stop()
	}
}

// watchPriceChanges 監听價格變化
func (da *DynamicAdjuster) watchPriceChanges() {
	priceCh := da.priceMonitor.Subscribe()
	for {
		select {
		case <-da.ctx.Done():
			return
		case priceChange, ok := <-priceCh:
			if !ok {
				// 价格订阅 channel 已关闭（PriceMonitor 已停止）。
				// 必须 return，否则会无限读取零值导致 CPU 100% 空转。
				return
			}
			da.addPrice(priceChange.NewPrice)

			// 更新趋势状态
			da.updateTrend(priceChange.NewPrice)

			// 更新波动率检测器
			if da.volatilityAlert != nil {
				// 使用价格变动中的高低价，如果没有则使用当前价格
				high := priceChange.HighPrice
				low := priceChange.LowPrice
				volume := priceChange.Volume

				if high == 0 {
					high = priceChange.NewPrice
				}
				if low == 0 {
					low = priceChange.NewPrice
				}

				da.volatilityAlert.UpdatePrice(priceChange.NewPrice, high, low, volume)
			}
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
	minInterval, maxInterval := da.priceIntervalBounds()

	if threshold <= 0 {
		threshold = 0.02 // 默认2%
	}
	if step <= 0 {
		step = 0.5
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
	da.applyTradingParams(newInterval, 0, 0, 0)
	logger.Info("✅ [動態調整] 價格間隔已更新為: %.2f", newInterval)
}

// ========== 參數邊界與寫入 ==========

// 未配置 Min/Max 時使用的保守默認邊界
const (
	defaultMinPriceInterval = 0.5
	defaultMaxPriceInterval = 10.0
	defaultMinWindowSize    = 5
	defaultMaxWindowSize    = 50
	defaultMinOrderQuantity = 50.0 // 需滿足交易所最小訂單金額
	defaultMaxOrderQuantity = 500.0
)

// priceIntervalBounds 返回帶默認值且已校驗（min>0、max>=min）的價格間隔邊界
func (da *DynamicAdjuster) priceIntervalBounds() (float64, float64) {
	pi := da.cfg.Trading.DynamicAdjustment.PriceInterval
	return floatBoundsWithDefaults(pi.Min, pi.Max, defaultMinPriceInterval, defaultMaxPriceInterval)
}

// windowSizeBounds 返回帶默認值且已校驗的買/賣窗口邊界
func (da *DynamicAdjuster) windowSizeBounds() (minBuy, maxBuy, minSell, maxSell int) {
	ws := da.cfg.Trading.DynamicAdjustment.WindowSize
	minBuy, maxBuy = intBoundsWithDefaults(ws.BuyWindow.Min, ws.BuyWindow.Max, defaultMinWindowSize, defaultMaxWindowSize)
	minSell, maxSell = intBoundsWithDefaults(ws.SellWindow.Min, ws.SellWindow.Max, defaultMinWindowSize, defaultMaxWindowSize)
	return minBuy, maxBuy, minSell, maxSell
}

// orderQuantityBounds 返回帶默認值且已校驗的單筆金額邊界
func (da *DynamicAdjuster) orderQuantityBounds() (float64, float64) {
	oq := da.cfg.Trading.DynamicAdjustment.OrderQuantity
	return floatBoundsWithDefaults(oq.Min, oq.Max, defaultMinOrderQuantity, defaultMaxOrderQuantity)
}

func floatBoundsWithDefaults(minV, maxV, defMin, defMax float64) (float64, float64) {
	if minV <= 0 {
		minV = defMin
	}
	if maxV <= 0 {
		maxV = defMax
	}
	if maxV < minV {
		maxV = minV
	}
	return minV, maxV
}

func intBoundsWithDefaults(minV, maxV, defMin, defMax int) (int, int) {
	if minV <= 0 {
		minV = defMin
	}
	if maxV <= 0 {
		maxV = defMax
	}
	if maxV < minV {
		maxV = minV
	}
	return minV, maxV
}

func clampAdjustFloat(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(v, hi))
}

func clampAdjustInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// applyTradingParams 寫入交易參數（<=0 的參數保持不變）。
// S11：網格讀取 cfg.Trading.* 的路徑（AdjustOrders 等）持有 SuperPositionManager.mu，
// 因此經由 SuperPositionManager.UpdateTradingParams（同一把鎖）寫入，避免與網格計算數據競爭；
// 生產環境中調整器與倉位管理器共用同一份 config 指針。無倉位管理器時（僅測試/離線場景）直接寫配置。
func (da *DynamicAdjuster) applyTradingParams(priceInterval, orderQuantity float64, buyWindow, sellWindow int) {
	if da.manager != nil {
		const keepProfitSpread = -1 // UpdateTradingParams 對負值不做修改
		da.manager.UpdateTradingParams(priceInterval, keepProfitSpread, orderQuantity, buyWindow, sellWindow)
		return
	}
	if priceInterval > 0 {
		da.cfg.Trading.PriceInterval = priceInterval
	}
	if orderQuantity > 0 {
		da.cfg.Trading.OrderQuantity = orderQuantity
	}
	if buyWindow > 0 {
		da.cfg.Trading.BuyWindowSize = buyWindow
	}
	if sellWindow > 0 {
		da.cfg.Trading.SellWindowSize = sellWindow
	}
}

// CalculateUtilization 计算资金利用率（已用 / 上限），數據來自倉位管理器的资金分配管理器。
// 第二個返回值為 false 表示無可用數據（未啟用资金分配或未配置該交易對上限），調用方不得用猜測值替代。
func (da *DynamicAdjuster) CalculateUtilization() (float64, bool) {
	if da.manager == nil {
		return 0, false
	}
	am := da.manager.GetAllocationManager()
	if am == nil {
		return 0, false
	}
	status := am.GetStatus(da.manager.GetExchange(), da.manager.GetSymbol())
	if status == nil || status.MaxAmount <= 0 {
		return 0, false
	}
	return status.UsedAmount / status.MaxAmount, true
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
	utilization, ok := da.CalculateUtilization()
	if !ok {
		// 沒有真實资金利用率數據時拒絕調整，避免用假數字改窗口（只告警一次）
		if da.utilizationWarned.CompareAndSwap(false, true) {
			logger.Warn("⚠️ [動態調整] 無法獲取资金利用率（需啟用 position_allocation 並配置該交易對上限），窗口大小動態調整不生效")
		}
		return
	}
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

	minBuyWindow, maxBuyWindow, minSellWindow, maxSellWindow := da.windowSizeBounds()

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
	da.applyTradingParams(0, 0, buyWindow, sellWindow)
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
	minQty, maxQty := da.orderQuantityBounds()
	threshold := oq.FrequencyThreshold
	step := oq.AdjustmentStep

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
	da.applyTradingParams(0, newQty, 0, 0)
	logger.Info("✅ [動態調整] 單筆金額已更新為: %.2f", newQty)
}

// ========== 波动率检测相关方法 ==========

// handleVolatilityAlerts 处理波动率预警
func (da *DynamicAdjuster) handleVolatilityAlerts(alertCh <-chan indicators.VolatilityRegimeEvent) {
	for event := range alertCh {
		da.handleVolatilityRegimeChange(event)
	}
}

// handleVolatilityRegimeChange 处理波动率区间变化
func (da *DynamicAdjuster) handleVolatilityRegimeChange(event indicators.VolatilityRegimeEvent) {
	da.mu.Lock()
	defer da.mu.Unlock()

	oldRegime := da.currentRegime
	da.currentRegime = event.NewRegime

	logger.Info("📊 [波动率] 区间变化: %s -> %s", oldRegime, event.NewRegime)

	// 根据新的波动率区间调整策略参数
	da.adjustForVolatilityRegime(event.NewRegime, event.Severity)

	// 检查是否需要暂停开仓
	da.checkVolatilityPause(event)
}

// adjustForVolatilityRegime 根据波动率区间调整策略
func (da *DynamicAdjuster) adjustForVolatilityRegime(regime indicators.VolatilityRegime, severity string) {
	switch regime {
	case indicators.RegimeLow:
		// 低波动：适合网格，可以缩小间距增加收益
		da.adjustForLowVolatility()

	case indicators.RegimeNormal:
		// 正常波动：保持默认设置
		logger.Info("ℹ️ [波动率] 正常波动区间，保持默认设置")

	case indicators.RegimeHigh:
		// 高波动：需要谨慎
		if severity == "critical" {
			// 从低波动突然进入高波动
			da.adjustForSuddenHighVolatility()
		} else {
			da.adjustForHighVolatility()
		}

	case indicators.RegimeExtreme:
		// 极端波动：危险
		da.adjustForExtremeVolatility()
	}
}

// adjustForLowVolatility 低波动调整
func (da *DynamicAdjuster) adjustForLowVolatility() {
	logger.Info("🟢 [波动率调整] 低波动区间，优化网格参数")

	// 缩小价格间距 20% 提高交易频率、增加窗口 20% 捕获更多机会；均夾在（帶默認值的）邊界內，間距不會降到 0
	da.scaleTradingParams("✅ [波动率调整]", lowVolIntervalFactor, lowVolWindowFactor, 0)
}

// 波動率區間調整倍數（0 表示不調整該參數）
const (
	lowVolIntervalFactor    = 0.8
	lowVolWindowFactor      = 1.2
	highVolIntervalFactor   = 1.5
	highVolWindowFactor     = 0.7
	suddenVolIntervalFactor = 2.0
	suddenVolWindowFactor   = 0.5
	suddenVolQtyFactor      = 0.5

	priceIntervalChangeEpsilon = 1e-9
)

// scaleTradingParams 按倍數調整價格間距 / 窗口 / 單筆金額，結果夾在帶默認值的 Min/Max 內，一次寫入。
// 倍數 <= 0 的參數不調整；對應動態調整開關未啟用的參數也不調整。
func (da *DynamicAdjuster) scaleTradingParams(logTag string, intervalFactor, windowFactor, qtyFactor float64) {
	adj := da.cfg.Trading.DynamicAdjustment
	var newInterval, newQty float64
	var newBuy, newSell int

	if adj.PriceInterval.Enabled && intervalFactor > 0 {
		minI, maxI := da.priceIntervalBounds()
		cur := da.cfg.Trading.PriceInterval
		if v := clampAdjustFloat(cur*intervalFactor, minI, maxI); math.Abs(v-cur) > priceIntervalChangeEpsilon {
			newInterval = v
			logger.Info("%s 价格间距: %.2f -> %.2f", logTag, cur, v)
		}
	}

	if adj.WindowSize.Enabled && windowFactor > 0 {
		minBuy, maxBuy, minSell, maxSell := da.windowSizeBounds()
		curBuy := da.cfg.Trading.BuyWindowSize
		curSell := da.cfg.Trading.SellWindowSize
		b := clampAdjustInt(int(float64(curBuy)*windowFactor), minBuy, maxBuy)
		s := clampAdjustInt(int(float64(curSell)*windowFactor), minSell, maxSell)
		if b != curBuy || s != curSell {
			newBuy, newSell = b, s
			logger.Info("%s 窗口大小: 买 %d->%d, 卖 %d->%d", logTag, curBuy, b, curSell, s)
		}
	}

	if adj.OrderQuantity.Enabled && qtyFactor > 0 {
		minQ, maxQ := da.orderQuantityBounds()
		cur := da.cfg.Trading.OrderQuantity
		if v := clampAdjustFloat(cur*qtyFactor, minQ, maxQ); math.Abs(v-cur) > priceIntervalChangeEpsilon {
			newQty = v
			logger.Info("%s 单笔金额: %.2f -> %.2f", logTag, cur, v)
		}
	}

	if newInterval > 0 || newQty > 0 || newBuy > 0 || newSell > 0 {
		da.applyTradingParams(newInterval, newQty, newBuy, newSell)
	}
}

// adjustForHighVolatility 高波动调整
func (da *DynamicAdjuster) adjustForHighVolatility() {
	logger.Warn("⚠️ [波动率调整] 高波动区间，采取保守策略")

	// 扩大价格间距 50% 减少频繁交易，减少窗口 30% 降低风险
	da.scaleTradingParams("✅ [波动率调整]", highVolIntervalFactor, highVolWindowFactor, 0)
}

// adjustForSuddenHighVolatility 突然进入高波动的调整
func (da *DynamicAdjuster) adjustForSuddenHighVolatility() {
	logger.Error("🚨 [波动率调整] 检测到波动率突然升高，启动紧急保护")

	// 间距翻倍、窗口减半、单笔金额减半，均夾在邊界內
	da.scaleTradingParams("🛡️ [紧急保护]", suddenVolIntervalFactor, suddenVolWindowFactor, suddenVolQtyFactor)
}

// adjustForExtremeVolatility 极端波动调整：设置到最保守的参数（使用帶默認值、已校驗的邊界，未配置 Min/Max 時不會寫 0）
func (da *DynamicAdjuster) adjustForExtremeVolatility() {
	logger.Error("🚨🚨 [波动率调整] 极端波动区间，建议暂停策略！")

	adj := da.cfg.Trading.DynamicAdjustment
	var newInterval, newQty float64
	var newBuy, newSell int

	if adj.PriceInterval.Enabled {
		_, newInterval = da.priceIntervalBounds()
		logger.Info("🛡️ [极端保护] 价格间距设置为最大值: %.2f", newInterval)
	}
	if adj.WindowSize.Enabled {
		newBuy, _, newSell, _ = da.windowSizeBounds()
		logger.Info("🛡️ [极端保护] 窗口大小设置为最小值: 买 %d, 卖 %d", newBuy, newSell)
	}
	if adj.OrderQuantity.Enabled {
		newQty, _ = da.orderQuantityBounds()
		logger.Info("🛡️ [极端保护] 单笔金额设置为最小值: %.2f", newQty)
	}

	if newInterval > 0 || newQty > 0 || newBuy > 0 || newSell > 0 {
		da.applyTradingParams(newInterval, newQty, newBuy, newSell)
	}
}

// GetCurrentVolatilityRegime 获取当前波动率区间
func (da *DynamicAdjuster) GetCurrentVolatilityRegime() indicators.VolatilityRegime {
	da.mu.RLock()
	defer da.mu.RUnlock()

	if da.volatilityAlert == nil {
		return indicators.RegimeNormal
	}

	return da.volatilityAlert.GetCurrentRegime()
}

// IsGridFriendly 判断当前是否适合网格策略
func (da *DynamicAdjuster) IsGridFriendly() bool {
	da.mu.RLock()
	defer da.mu.RUnlock()

	if da.volatilityAlert == nil {
		return true // 默认返回 true
	}

	return da.volatilityAlert.IsGridFriendly()
}

// GetVolatilityRiskLevel 获取波动率风险等级 (0-10)
func (da *DynamicAdjuster) GetVolatilityRiskLevel() int {
	da.mu.RLock()
	defer da.mu.RUnlock()

	if da.volatilityAlert == nil {
		return 3 // 默认中等风险
	}

	return da.volatilityAlert.GetRiskLevel()
}

// GetVolatilityStatistics 获取波动率统计信息
func (da *DynamicAdjuster) GetVolatilityStatistics() map[string]interface{} {
	da.mu.RLock()
	defer da.mu.RUnlock()

	if da.volatilityAlert == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	return da.volatilityAlert.GetStatistics()
}

// ========== 波动率暂停开仓相关方法 ==========

// updateTrend 更新趋势状态
func (da *DynamicAdjuster) updateTrend(price float64) {
	da.mu.Lock()
	defer da.mu.Unlock()

	// 添加到趋势历史
	da.trendHistory = append(da.trendHistory, price)

	// 保持历史长度
	if len(da.trendHistory) > 100 {
		da.trendHistory = da.trendHistory[len(da.trendHistory)-100:]
	}

	// 至少需要2个点才能判断趋势
	if len(da.trendHistory) < 2 {
		da.currentTrend = "sideways"
		return
	}

	// 计算最近N个点的变化率
	checkPeriod := 15 // 默认检查15分钟
	if len(da.trendHistory) < checkPeriod {
		checkPeriod = len(da.trendHistory)
	}

	start := len(da.trendHistory) - checkPeriod
	firstPrice := da.trendHistory[start]
	lastPrice := da.trendHistory[len(da.trendHistory)-1]

	changePercent := (lastPrice - firstPrice) / firstPrice * 100

	// 获取配置的阈值
	downThreshold := -2.0 // 默认下跌2%视为下跌
	upThreshold := 2.0    // 默认上涨2%视为上涨

	if len(da.cfg.Trading.Symbols) > 0 {
		botRisk := da.cfg.Trading.Symbols[0].OpenPositionControl.BotRiskControl
		if botRisk != nil && botRisk.VolatilityPauseConfig.TrendDownThreshold != 0 {
			downThreshold = -botRisk.VolatilityPauseConfig.TrendDownThreshold
		}
		if botRisk != nil && botRisk.VolatilityPauseConfig.TrendUpThreshold != 0 {
			upThreshold = botRisk.VolatilityPauseConfig.TrendUpThreshold
		}
	}

	// 判断趋势
	if changePercent <= downThreshold {
		da.currentTrend = "down"
	} else if changePercent >= upThreshold {
		da.currentTrend = "up"
	} else {
		da.currentTrend = "sideways"
	}
}

// checkVolatilityPause 检查是否需要暂停开仓
func (da *DynamicAdjuster) checkVolatilityPause(event indicators.VolatilityRegimeEvent) {
	if len(da.cfg.Trading.Symbols) == 0 {
		return
	}

	symbolConfig := &da.cfg.Trading.Symbols[0]
	botRisk := symbolConfig.OpenPositionControl.BotRiskControl

	// 检查是否启用了波动率暂停开仓
	if botRisk == nil || !botRisk.VolatilityPauseEnabled {
		return
	}

	volConfig := botRisk.VolatilityPauseConfig
	shouldPause := false
	reason := ""

	// 检查高波动暂停
	if volConfig.PauseOnHighVolatility && event.NewRegime == indicators.RegimeHigh {
		shouldPause = true
		reason = fmt.Sprintf("高波动区间 (波动率 %.2f%%)", event.ShortVolatility)
	}

	// 检查极端波动暂停
	if volConfig.PauseOnExtremeVolatility && event.NewRegime == indicators.RegimeExtreme {
		shouldPause = true
		reason = fmt.Sprintf("极端波动区间 (波动率 %.2f%%)", event.ShortVolatility)
	}

	// 检查突变暂停
	if volConfig.PauseOnSuddenIncrease && event.Severity == "critical" {
		shouldPause = true
		reason = fmt.Sprintf("波动率突然增加 (从 %s 到 %s)", event.OldRegime, event.NewRegime)
	}

	// 检查趋势+波动率组合暂停
	if da.currentTrend != "" {
		// 获取策略方向（这里简化处理，实际需要从策略配置获取）
		strategyDirection := da.getStrategyDirection()

		// 做多策略 + 下跌趋势 + 高波动
		if volConfig.PauseOnDowntrend && strategyDirection == "long" &&
			da.currentTrend == "down" && event.NewRegime >= indicators.RegimeHigh {
			shouldPause = true
			reason = fmt.Sprintf("做多策略遇到高波动下跌行情 (趋势: %s, 波动率: %.2f%%)",
				da.currentTrend, event.ShortVolatility)
		}

		// 做空策略 + 上涨趋势 + 高波动
		if volConfig.PauseOnUptrend && strategyDirection == "short" &&
			da.currentTrend == "up" && event.NewRegime >= indicators.RegimeHigh {
			shouldPause = true
			reason = fmt.Sprintf("做空策略遇到高波动上涨行情 (趋势: %s, 波动率: %.2f%%)",
				da.currentTrend, event.ShortVolatility)
		}
	}

	// 执行暂停或恢复
	if da.manager == nil {
		return
	}
	if shouldPause {
		reason := volatilityPauseReasonPrefix + ": " + reason
		// 已被熔斷 / 複合風控 / 手動等其他來源暫停時不覆蓋原因，否則回落後會誤把風控暫停當成自己的解除
		if da.manager.PauseOpeningUnlessHeld(reason, isVolatilityPauseReason) {
			botRisk.PauseOpening = true
			botRisk.PauseOpeningReason = reason
			logger.Error("🚨 [波动率暂停] 已暂停开仓: %s", reason)
		}
	} else if volConfig.AutoResumeOnNormal && event.NewRegime <= indicators.RegimeNormal {
		// 波动率回到正常或低波动即自动恢复；只解除波动率自己设置的暂停，不解除风控暂停
		if da.manager.ResumeOpeningIfOwned(isVolatilityPauseReason) {
			botRisk.PauseOpening = false
			botRisk.PauseOpeningReason = ""
			logger.Info("✅ [波动率恢复] 波动率回归 %s，已恢复开仓", event.NewRegime)
		}
	}
}

// isVolatilityPauseReason 暂停原因是否由波动率暂停设置
func isVolatilityPauseReason(reason string) bool {
	return strings.HasPrefix(reason, volatilityPauseReasonPrefix)
}

// getStrategyDirection 获取策略方向（简化版）
func (da *DynamicAdjuster) getStrategyDirection() string {
	// 检查 Symbols 配置中的策略方向
	if len(da.cfg.Trading.Symbols) > 0 {
		direction := da.cfg.Trading.Symbols[0].Direction
		if direction == "short" || direction == "short_only" {
			return "short"
		}
	}
	// 默认返回做多
	return "long"
}

