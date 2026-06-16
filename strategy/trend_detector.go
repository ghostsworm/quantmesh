package strategy

import (
	"context"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/monitor"
)

// Trend 趋势類型
type Trend string

const (
	TrendUp   Trend = "up"   // 上涨
	TrendDown Trend = "down" // 下跌
	TrendSide Trend = "side" // 震荡
)

// TrendDetector 趋势检测器
type TrendDetector struct {
	cfg          *config.Config
	priceHistory []float64
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	priceMonitor *monitor.PriceMonitor
	currentTrend Trend
}

// NewTrendDetector 創建趋势检测器
func NewTrendDetector(cfg *config.Config, priceMonitor *monitor.PriceMonitor) *TrendDetector {
	ctx, cancel := context.WithCancel(context.Background())
	return &TrendDetector{
		cfg:          cfg,
		priceHistory: make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
		priceMonitor: priceMonitor,
		currentTrend: TrendSide,
	}
}

// Start 啟动趋势检测器
func (td *TrendDetector) Start() {
	if !td.cfg.Trading.SmartPosition.Enabled {
		return
	}

	// 订阅價格變化
	go td.watchPriceChanges()

	// 啟动趋势检测循环
	if td.cfg.Trading.SmartPosition.TrendDetection.Enabled {
		go td.detectTrendLoop()
	}

	logger.Info("✅ 趋势检测器已啟动")
}

// Stop 停止趋势检测器
func (td *TrendDetector) Stop() {
	if td.cancel != nil {
		td.cancel()
	}
}

// watchPriceChanges 監听價格變化
func (td *TrendDetector) watchPriceChanges() {
	priceCh := td.priceMonitor.Subscribe()
	for {
		select {
		case <-td.ctx.Done():
			return
		case priceChange, ok := <-priceCh:
			if !ok {
				// 价格订阅 channel 已关闭（PriceMonitor 已停止）。
				// 必须 return，否则会无限读取零值导致 CPU 100% 空转。
				return
			}
			td.addPrice(priceChange.NewPrice)
		}
	}
}

// addPrice 新增價格到历史記錄
func (td *TrendDetector) addPrice(price float64) {
	td.mu.Lock()
	defer td.mu.Unlock()

	td.priceHistory = append(td.priceHistory, price)

	// 保持历史記錄在合理範圍内
	maxHistory := td.cfg.Trading.SmartPosition.TrendDetection.LongPeriod
	if maxHistory <= 0 {
		maxHistory = 50
	}

	if len(td.priceHistory) > maxHistory*2 {
		// 保留最近的數據，使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, maxHistory)
		copy(newHistory, td.priceHistory[len(td.priceHistory)-maxHistory:])
		td.priceHistory = newHistory
	}
}

// calculateMA 计算移动平均
func (td *TrendDetector) calculateMA(period int) float64 {
	td.mu.RLock()
	defer td.mu.RUnlock()

	if len(td.priceHistory) < period {
		return 0
	}

	// 使用最近的數據
	start := len(td.priceHistory) - period
	prices := td.priceHistory[start:]

	var sum float64
	for _, price := range prices {
		sum += price
	}

	return sum / float64(len(prices))
}

// calculateEMA 计算指數移动平均
func (td *TrendDetector) calculateEMA(period int) float64 {
	td.mu.RLock()
	defer td.mu.RUnlock()

	if len(td.priceHistory) < period {
		return 0
	}

	// 使用最近的數據
	start := len(td.priceHistory) - period
	prices := td.priceHistory[start:]

	// 初始值使用简單移动平均
	var sum float64
	for i := 0; i < period && i < len(prices); i++ {
		sum += prices[i]
	}
	ema := sum / float64(period)

	// 计算平滑因子
	multiplier := 2.0 / (float64(period) + 1.0)

	// 计算EMA
	for i := period; i < len(prices); i++ {
		ema = (prices[i] * multiplier) + (ema * (1 - multiplier))
	}

	return ema
}

// DetectTrend 检测趋势
func (td *TrendDetector) DetectTrend() Trend {
	td.mu.RLock()
	defer td.mu.RUnlock()

	longPeriod := td.cfg.Trading.SmartPosition.TrendDetection.LongPeriod
	if longPeriod <= 0 {
		longPeriod = 30
	}

	if len(td.priceHistory) < longPeriod {
		return TrendSide
	}

	shortPeriod := td.cfg.Trading.SmartPosition.TrendDetection.ShortPeriod
	if shortPeriod <= 0 {
		shortPeriod = 10
	}

	var shortMA, longMA float64
	method := td.cfg.Trading.SmartPosition.TrendDetection.Method

	if method == "ema" {
		shortMA = td.calculateEMA(shortPeriod)
		longMA = td.calculateEMA(longPeriod)
	} else {
		// 預設使用 MA
		shortMA = td.calculateMA(shortPeriod)
		longMA = td.calculateMA(longPeriod)
	}

	if shortMA == 0 || longMA == 0 {
		return TrendSide
	}

	currentPrice := td.priceHistory[len(td.priceHistory)-1]

	// 判断趋势
	if shortMA > longMA && currentPrice > shortMA {
		return TrendUp
	} else if shortMA < longMA && currentPrice < shortMA {
		return TrendDown
	}

	return TrendSide
}

// detectTrendLoop 定期检测趋势
func (td *TrendDetector) detectTrendLoop() {
	checkInterval := time.Duration(td.cfg.Trading.SmartPosition.TrendDetection.CheckInterval) * time.Second
	if checkInterval <= 0 {
		checkInterval = 60 * time.Second
	}

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-td.ctx.Done():
			return
		case <-ticker.C:
			trend := td.DetectTrend()
			if trend != td.currentTrend {
				logger.Info("📊 [趋势变化] %s -> %s", td.currentTrend, trend)
				td.currentTrend = trend
			}
		}
	}
}

// GetCurrentTrend 獲取當前趋势
func (td *TrendDetector) GetCurrentTrend() string {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return string(td.currentTrend)
}

// AdjustWindows 根據趋势調整窗口
func (td *TrendDetector) AdjustWindows() (buyWindow, sellWindow int) {
	trend := td.GetCurrentTrend()
	baseBuyWindow := td.cfg.Trading.BuyWindowSize
	baseSellWindow := td.cfg.Trading.SellWindowSize

	maxAdjustment := td.cfg.Trading.SmartPosition.WindowAdjustment.MaxAdjustment
	if maxAdjustment <= 0 {
		maxAdjustment = 0.5 // 預設 50%
	}

	adjustmentStep := td.cfg.Trading.SmartPosition.WindowAdjustment.AdjustmentStep
	if adjustmentStep <= 0 {
		adjustmentStep = 1
	}

	minBuyWindow := td.cfg.Trading.SmartPosition.WindowAdjustment.MinBuyWindow
	minSellWindow := td.cfg.Trading.SmartPosition.WindowAdjustment.MinSellWindow

	if minBuyWindow <= 0 {
		minBuyWindow = 5
	}
	if minSellWindow <= 0 {
		minSellWindow = 5
	}

	maxAdjustmentValue := int(math.Round(float64(baseBuyWindow) * maxAdjustment))

	switch Trend(trend) {
	case TrendUp:
		// 上涨趋势：减少買單，增加賣單
		buyWindow = baseBuyWindow - maxAdjustmentValue
		sellWindow = baseSellWindow + maxAdjustmentValue
		logger.Info("📈 [智能倉位] 上涨趋势，調整窗口: 買單 %d->%d, 賣單 %d->%d",
			baseBuyWindow, buyWindow, baseSellWindow, sellWindow)

	case TrendDown:
		// 下跌趋势：增加買單，减少賣單
		buyWindow = baseBuyWindow + maxAdjustmentValue
		sellWindow = baseSellWindow - maxAdjustmentValue
		logger.Info("📉 [智能倉位] 下跌趋势，調整窗口: 買單 %d->%d, 賣單 %d->%d",
			baseBuyWindow, buyWindow, baseSellWindow, sellWindow)

	default:
		// 震荡：保持原样
		buyWindow = baseBuyWindow
		sellWindow = baseSellWindow
	}

	// 确保最小值
	if buyWindow < minBuyWindow {
		buyWindow = minBuyWindow
	}
	if sellWindow < minSellWindow {
		sellWindow = minSellWindow
	}

	return buyWindow, sellWindow
}

// UpdateWindows 更新窗口大小到配置
func (td *TrendDetector) UpdateWindows(buyWindow, sellWindow int) {
	td.cfg.Trading.BuyWindowSize = buyWindow
	td.cfg.Trading.SellWindowSize = sellWindow
	logger.Info("✅ [智能倉位] 窗口大小已更新: 買單窗口=%d, 賣單視窗=%d", buyWindow, sellWindow)
}
